package http

import (
	"errors"
	"net/http"
	"net/mail"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/collinpendleton/backhog/api/internal/auth"
	"github.com/collinpendleton/backhog/api/internal/models"
	"github.com/collinpendleton/backhog/api/internal/store"
)

// failAdmin maps the account-management errors that mean something specific
// to the person clicking, and falls through to fail for everything else.
func failAdmin(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrLastAdmin):
		fail(w, errorf(http.StatusConflict,
			"this is the only administrator — promote someone else first"))
	case errors.Is(err, store.ErrNotFound):
		fail(w, errNotFound)
	case errors.Is(err, store.ErrConflict):
		fail(w, errorf(http.StatusConflict, "that email or username is already taken"))
	default:
		fail(w, err)
	}
}

func (s *Server) handleListUsers(w http.ResponseWriter, r *http.Request) {
	users, err := s.store.ListUsers(r.Context())
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"users": users})
}

type userUpdate struct {
	Role     *string `json:"role"`
	Disabled *bool   `json:"disabled"`
}

// handleUpdateUser changes an account's role or suspends it. Both live on one
// PATCH because the panel offers them as one row of controls, and because the
// last-administrator guard has to see the account's whole intended state
// rather than one field of it at a time.
//
// An admin may not disable or demote themselves. The guard against removing
// the last administrator already covers the dangerous half of that, but the
// harmless half is still confusing — clicking "reader" on your own row and
// watching the page you are standing on disappear.
func (s *Server) handleUpdateUser(w http.ResponseWriter, r *http.Request) {
	actor, ok := auth.UserFrom(r.Context())
	if !ok {
		fail(w, errUnauthorized)
		return
	}
	targetID := chi.URLParam(r, "userID")

	var body userUpdate
	if err := decode(r, &body); err != nil {
		fail(w, err)
		return
	}
	if body.Role == nil && body.Disabled == nil {
		fail(w, errorf(http.StatusBadRequest, "nothing to change"))
		return
	}
	if targetID == actor.ID {
		fail(w, errorf(http.StatusConflict,
			"you cannot change your own role or disable your own account"))
		return
	}

	user := models.User{}
	var err error
	if body.Role != nil {
		if !models.ValidRole(*body.Role) {
			fail(w, errorf(http.StatusBadRequest,
				"role must be one of: "+strings.Join(models.AllRoles, ", ")))
			return
		}
		if user, err = s.store.SetUserRole(r.Context(), targetID, *body.Role); err != nil {
			failAdmin(w, err)
			return
		}
	}
	if body.Disabled != nil {
		if user, err = s.store.SetUserDisabled(r.Context(), targetID, *body.Disabled); err != nil {
			failAdmin(w, err)
			return
		}
	}
	writeJSON(w, http.StatusOK, user)
}

type adminPassword struct {
	NewPassword string `json:"new_password"`
}

// handleAdminResetPassword sets someone else's password without knowing
// their old one, for the friend who is locked out on a Sunday. Every one of
// their sessions is revoked with it.
func (s *Server) handleAdminResetPassword(w http.ResponseWriter, r *http.Request) {
	var body adminPassword
	if err := decode(r, &body); err != nil {
		fail(w, err)
		return
	}
	if len(body.NewPassword) < 8 {
		fail(w, errorf(http.StatusBadRequest, "password must be at least 8 characters"))
		return
	}

	hash, err := auth.HashPassword(body.NewPassword)
	if err != nil {
		fail(w, err)
		return
	}
	if err := s.store.AdminSetPassword(r.Context(), chi.URLParam(r, "userID"), hash); err != nil {
		failAdmin(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// handleDeleteUser removes an account and everything cascading off it. The
// panel says what that is — entry and share counts come back on every user
// row — because this is the one irreversible button in the app.
func (s *Server) handleDeleteUser(w http.ResponseWriter, r *http.Request) {
	actor, ok := auth.UserFrom(r.Context())
	if !ok {
		fail(w, errUnauthorized)
		return
	}
	targetID := chi.URLParam(r, "userID")
	if targetID == actor.ID {
		fail(w, errorf(http.StatusConflict, "you cannot delete your own account"))
		return
	}
	if err := s.store.DeleteUser(r.Context(), targetID); err != nil {
		failAdmin(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleGetServerSettings(w http.ResponseWriter, r *http.Request) {
	settings, err := s.store.Settings(r.Context())
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, settings)
}

// handleUpdateServerSettings writes the admin-editable server settings:
// whether anyone may sign themselves up, and what role they land on if they
// do. Turning registration off never locks anyone out — existing accounts
// and outstanding invites keep working.
func (s *Server) handleUpdateServerSettings(w http.ResponseWriter, r *http.Request) {
	var body models.ServerSettings
	if err := decode(r, &body); err != nil {
		fail(w, err)
		return
	}
	if !models.ValidRole(body.DefaultRole) {
		fail(w, errorf(http.StatusBadRequest,
			"default_role must be one of: "+strings.Join(models.AllRoles, ", ")))
		return
	}
	settings, err := s.store.SaveSettings(r.Context(), body)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, settings)
}

func (s *Server) handleListInvites(w http.ResponseWriter, r *http.Request) {
	invites, err := s.store.ListInvites(r.Context())
	if err != nil {
		fail(w, err)
		return
	}
	now := time.Now()
	out := make([]map[string]any, 0, len(invites))
	for _, inv := range invites {
		out = append(out, inviteJSON(inv, now))
	}
	writeJSON(w, http.StatusOK, map[string]any{"invites": out})
}

type inviteRequest struct {
	Email string `json:"email"`
	Role  string `json:"role"`
	Note  string `json:"note"`
	// Days overrides the default lifetime. Zero means the default.
	Days int `json:"days"`
}

// handleCreateInvite mints a sign-up link. The token comes back exactly once,
// in this response: the row holds only its hash, so a link the admin loses
// cannot be recovered — it is revoked and reissued instead.
func (s *Server) handleCreateInvite(w http.ResponseWriter, r *http.Request) {
	actor, ok := auth.UserFrom(r.Context())
	if !ok {
		fail(w, errUnauthorized)
		return
	}

	var body inviteRequest
	if err := decode(r, &body); err != nil {
		fail(w, err)
		return
	}
	if body.Role == "" {
		body.Role = models.RoleReader
	}
	if !models.ValidRole(body.Role) {
		fail(w, errorf(http.StatusBadRequest,
			"role must be one of: "+strings.Join(models.AllRoles, ", ")))
		return
	}
	if email := strings.TrimSpace(body.Email); email != "" {
		if _, err := mail.ParseAddress(email); err != nil {
			fail(w, errorf(http.StatusBadRequest, "that is not a valid email address"))
			return
		}
	}
	if body.Days < 0 || body.Days > 90 {
		fail(w, errorf(http.StatusBadRequest, "an invite may last between 1 and 90 days"))
		return
	}

	ttl := time.Duration(body.Days) * 24 * time.Hour
	invite, err := s.store.CreateInvite(r.Context(), actor.ID, body.Email, body.Role, body.Note, ttl)
	if err != nil {
		fail(w, err)
		return
	}
	invite.CreatedByAs = actor.Username

	payload := inviteJSON(invite, time.Now())
	payload["token"] = invite.Token
	writeJSON(w, http.StatusCreated, payload)
}

func (s *Server) handleRevokeInvite(w http.ResponseWriter, r *http.Request) {
	if err := s.store.RevokeInvite(r.Context(), chi.URLParam(r, "inviteID")); err != nil {
		failAdmin(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleDeleteInvite(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteInvite(r.Context(), chi.URLParam(r, "inviteID")); err != nil {
		failAdmin(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// inviteJSON renders an invite with its lifecycle state resolved server-side,
// so the client never has to compare an expiry against its own clock.
func inviteJSON(inv models.Invite, now time.Time) map[string]any {
	out := map[string]any{
		"id":                  inv.ID,
		"email":               inv.Email,
		"role":                inv.Role,
		"note":                inv.Note,
		"status":              inv.Status(now),
		"created_by_username": inv.CreatedByAs,
		"expires_at":          inv.ExpiresAt,
		"created_at":          inv.CreatedAt,
	}
	if inv.AcceptedAt != nil {
		out["accepted_at"] = inv.AcceptedAt
		out["accepted_by_username"] = inv.AcceptedAs
	}
	if inv.RevokedAt != nil {
		out["revoked_at"] = inv.RevokedAt
	}
	return out
}
