package http

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/collinpendleton/backhog/api/internal/auth"
	"github.com/collinpendleton/backhog/api/internal/models"
	"github.com/collinpendleton/backhog/api/internal/store"
)

type tokenRequest struct {
	Name string `json:"name"`
	// Scopes defaults to read-only when empty.
	Scopes []string `json:"scopes"`
	// ExpiresDays sets an optional lifetime; zero means no expiry.
	ExpiresDays int `json:"expires_days"`
}

// handleListAPITokens shows the caller's own tokens with their last use and
// lifecycle state, resolved server-side so the client never compares an
// expiry against its own clock.
func (s *Server) handleListAPITokens(w http.ResponseWriter, r *http.Request) {
	if refuseTokenClient(w, r) {
		return
	}
	userID, err := auth.MustUserID(r.Context())
	if err != nil {
		fail(w, errUnauthorized)
		return
	}

	tokens, err := s.store.ListAPITokens(r.Context(), userID)
	if err != nil {
		fail(w, err)
		return
	}
	now := time.Now()
	out := make([]map[string]any, 0, len(tokens))
	for _, t := range tokens {
		out = append(out, tokenJSON(t, now))
	}
	writeJSON(w, http.StatusOK, map[string]any{"tokens": out})
}

// handleCreateAPIToken mints a personal bearer credential. The secret comes
// back exactly once, in this response: the row holds only its hash, so a
// token the user loses is revoked and reissued, never recovered.
func (s *Server) handleCreateAPIToken(w http.ResponseWriter, r *http.Request) {
	if refuseTokenClient(w, r) {
		return
	}
	userID, err := auth.MustUserID(r.Context())
	if err != nil {
		fail(w, errUnauthorized)
		return
	}

	var body tokenRequest
	if err := decode(r, &body); err != nil {
		fail(w, err)
		return
	}
	name := strings.TrimSpace(body.Name)
	if name == "" || len(name) > 64 {
		fail(w, errorf(http.StatusBadRequest, "a token needs a name, 64 characters at most"))
		return
	}
	if len(body.Scopes) == 0 {
		body.Scopes = []string{models.ScopeBooksRead}
	}
	for _, scope := range body.Scopes {
		if !models.ValidScope(scope) {
			fail(w, errorf(http.StatusBadRequest,
				"scopes must be one of: "+strings.Join(models.AllScopes, ", ")))
			return
		}
	}
	if body.ExpiresDays < 0 || body.ExpiresDays > 3650 {
		fail(w, errorf(http.StatusBadRequest, "an expiry runs between 1 and 3650 days"))
		return
	}

	var expiresAt *time.Time
	if body.ExpiresDays > 0 {
		t := time.Now().Add(time.Duration(body.ExpiresDays) * 24 * time.Hour).UTC()
		expiresAt = &t
	}

	token, err := s.store.CreateAPIToken(r.Context(), userID, name, body.Scopes, expiresAt)
	if err != nil {
		fail(w, err)
		return
	}

	payload := tokenJSON(token, time.Now())
	payload["token"] = token.Token
	writeJSON(w, http.StatusCreated, payload)
}

// handleRevokeAPIToken kills one of the caller's own tokens. The row stays
// as the record that it existed.
func (s *Server) handleRevokeAPIToken(w http.ResponseWriter, r *http.Request) {
	if refuseTokenClient(w, r) {
		return
	}
	userID, err := auth.MustUserID(r.Context())
	if err != nil {
		fail(w, errUnauthorized)
		return
	}
	if err := s.store.RevokeAPIToken(r.Context(), userID, chi.URLParam(r, "tokenID")); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			fail(w, errNotFound)
			return
		}
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// refuseTokenClient keeps the token routes cookie-only: a token cannot mint,
// list or revoke tokens. The middleware already turns a token's writes away;
// this guards the read too, and says why in terms of the mistake the caller
// is making. Returns true when it answered the request.
func refuseTokenClient(w http.ResponseWriter, r *http.Request) bool {
	if auth.UsingAPIToken(r.Context()) {
		fail(w, errorf(http.StatusForbidden,
			"API tokens are managed with your session — sign in to the app"))
		return true
	}
	return false
}

// tokenJSON renders a token with its lifecycle state resolved server-side.
// The plaintext secret is never here; only the create handler adds it.
func tokenJSON(t models.APIToken, now time.Time) map[string]any {
	out := map[string]any{
		"id":         t.ID,
		"name":       t.Name,
		"scopes":     t.Scopes,
		"status":     t.Status(now),
		"created_at": t.CreatedAt,
	}
	if t.LastUsedAt != nil {
		out["last_used_at"] = t.LastUsedAt
	}
	if t.ExpiresAt != nil {
		out["expires_at"] = t.ExpiresAt
	}
	if t.RevokedAt != nil {
		out["revoked_at"] = t.RevokedAt
	}
	return out
}
