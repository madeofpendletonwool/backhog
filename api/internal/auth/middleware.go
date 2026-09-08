package auth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/collinpendleton/backhog/api/internal/models"
)

// CookieName is the session cookie key.
const CookieName = "backhog_session"

type ctxKey int

const (
	userKey ctxKey = iota
	sessionKey
)

// Resolver looks up the user owning a session token.
type Resolver interface {
	UserForSession(ctx context.Context, sessionID string) (models.User, error)
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
// valid session cookie is present. It does not reject anonymous requests —
// Require does that — so optional-auth routes can share the same chain.
func Middleware(r Resolver) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
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
