package auth

import (
	"context"
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
