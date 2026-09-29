package auth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/collinpendleton/backhog/api/internal/models"
)

// CookieName is the session cookie key.
const CookieName = "backhog_session"

type ctxKey int

const (
	userKey ctxKey = iota
	sessionKey
	tokenScopesKey
)

// Resolver looks up the user owning a session token.
type Resolver interface {
	UserForSession(ctx context.Context, sessionID string) (models.User, error)
}

// TokenResolver looks up the user a personal API token acts as, with the
// scopes that token carries. It rejects revoked, expired and disabled
// credentials itself; the middleware only decides what to do with the answer.
type TokenResolver interface {
	UserForAPIToken(ctx context.Context, token string) (models.User, []string, error)
}

// SetCookie writes the session cookie. Secure is set only in production so that
// plain-HTTP local development still works.
func SetCookie(w http.ResponseWriter, sessionID string, expires time.Time, production bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    sessionID,
		Path:     "/",
		Expires:  expires,
		HttpOnly: true,
		Secure:   production,
		SameSite: http.SameSiteLaxMode,
	})
}

// ClearCookie expires the session cookie.
func ClearCookie(w http.ResponseWriter, production bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   production,
		SameSite: http.SameSiteLaxMode,
	})
}

// Middleware attaches the authenticated user to the request context when a
// valid credential is present. Two credentials, two halves:
//
//   - An `Authorization: Bearer bh_…` header is a personal API token, the
//     external client's key. It resolves to the same user context the cookie
//     path produces, so every handler's authorization — shares, ownership,
//     roles — applies unchanged. Token requests never set cookies and carry
//     no cookie, so there is nothing for a cross-site request to ride; and a
//     bearer value without the bh_ prefix is not ours at all — the /internal
//     worker gates authenticate their own tokens, and those pass through
//     untouched.
//   - The session cookie stays the browser's credential, unchanged.
//
// It does not reject anonymous requests — Require does that — so optional
// auth routes can share the same chain. An *invalid* personal token is the
// one anonymous case that answers instead of passing through: a client that
// presented a credential it believes in deserves the 401, not whatever the
// anonymous shape of the route happens to be.
func Middleware(r Resolver, tokens TokenResolver) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			if bearer := bearerToken(req); bearer != "" {
				if !strings.HasPrefix(bearer, models.TokenPrefix) {
					next.ServeHTTP(w, req)
					return
				}
				user, scopes, err := tokens.UserForAPIToken(req.Context(), bearer)
				if err != nil {
					writeError(w, http.StatusUnauthorized, "invalid or expired token")
					return
				}
				// A token is read-only until a write scope exists to say
				// otherwise. The check is on the method, not the route,
				// so no write endpoint can appear beside it unguarded.
				if isWriteMethod(req.Method) {
					writeError(w, http.StatusForbidden,
						"this token is read-only — manage your library from the app")
					return
				}
				ctx := context.WithValue(req.Context(), userKey, user)
				ctx = context.WithValue(ctx, tokenScopesKey, scopes)
				next.ServeHTTP(w, req.WithContext(ctx))
				return
			}

			cookie, err := req.Cookie(CookieName)
			if err != nil || cookie.Value == "" {
				next.ServeHTTP(w, req)
				return
			}
			user, err := r.UserForSession(req.Context(), cookie.Value)
			if err != nil {
				next.ServeHTTP(w, req)
				return
			}
			ctx := context.WithValue(req.Context(), userKey, user)
			ctx = context.WithValue(ctx, sessionKey, cookie.Value)
			next.ServeHTTP(w, req.WithContext(ctx))
		})
	}
}

// bearerToken returns the bearer credential from the Authorization header,
// or "" when the request carries none. The secret itself is never logged.
func bearerToken(req *http.Request) string {
	header := req.Header.Get("Authorization")
	const prefix = "Bearer "
	if len(header) <= len(prefix) || !strings.EqualFold(header[:len(prefix)], prefix) {
		return ""
	}
	return strings.TrimSpace(header[len(prefix):])
}

// isWriteMethod reports whether the method changes server state.
func isWriteMethod(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	}
	return false
}

// Require rejects requests that Middleware did not authenticate.
func Require(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if _, ok := UserFrom(req.Context()); !ok {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"error":"not authenticated"}`))
			return
		}
		next.ServeHTTP(w, req)
	})
}

// RequireAdmin rejects anyone who is not an administrator. It is mounted
// under Require, so an anonymous request has already been turned away by the
// time it runs; what is left is an authenticated account without the role.
//
// That answers 403 rather than 404. Hiding the account panel from a signed-in
// member protects nothing — they can read the shipped source — and a 404
// would send someone chasing a broken link instead of asking the owner for
// access. The 404-not-403 rule is for *data*, where the status leaks whether
// a row exists; this is a fixed route that either exists for you or does not.
func RequireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		user, ok := UserFrom(req.Context())
		if !ok {
			writeError(w, http.StatusUnauthorized, "not authenticated")
			return
		}
		if !user.IsAdmin() {
			writeError(w, http.StatusForbidden, "this needs an administrator account")
			return
		}
		next.ServeHTTP(w, req)
	})
}

// RequireMediaManager rejects readers from the file layer: attaching and
// detaching media, promoting a primary text, kicking the NAS scan, browsing
// raw library paths, queueing an alignment run.
//
// The line it draws is between using the library and administering the files
// under it. A reader can read every book they have been given, listen to it,
// track it and pin their own printing's pages; they cannot repoint the file
// the whole installation's stored offsets are measured against.
func RequireMediaManager(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		user, ok := UserFrom(req.Context())
		if !ok {
			writeError(w, http.StatusUnauthorized, "not authenticated")
			return
		}
		if !user.CanManageMedia() {
			writeError(w, http.StatusForbidden,
				"your account can read and listen, but not manage library files")
			return
		}
		next.ServeHTTP(w, req)
	})
}

// writeError emits the same JSON error envelope the http package's fail()
// does. It is duplicated here rather than imported because internal/http
// already imports this package.
func writeError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
}

// UserFrom returns the authenticated user, if any.
func UserFrom(ctx context.Context) (models.User, bool) {
	u, ok := ctx.Value(userKey).(models.User)
	return u, ok
}

// ErrNoUser indicates a handler ran without authentication, which means it was
// mounted outside Require.
var ErrNoUser = errors.New("no authenticated user in context")

// MustUserID returns the authenticated user's id, or an error if absent.
func MustUserID(ctx context.Context) (string, error) {
	u, ok := UserFrom(ctx)
	if !ok {
		return "", ErrNoUser
	}
	return u.ID, nil
}

// SessionFrom returns the current session token.
func SessionFrom(ctx context.Context) string {
	s, _ := ctx.Value(sessionKey).(string)
	return s
}

// TokenScopesFrom returns the scopes of the API token that authenticated the
// request. The bool is false for every cookie-authenticated request, which
// is the distinction the token routes care about: a session may manage
// tokens, a token may not.
func TokenScopesFrom(ctx context.Context) ([]string, bool) {
	scopes, ok := ctx.Value(tokenScopesKey).([]string)
	return scopes, ok
}

// UsingAPIToken reports whether the request authenticated with a personal
// API token rather than the session cookie.
func UsingAPIToken(ctx context.Context) bool {
	_, ok := TokenScopesFrom(ctx)
	return ok
}
