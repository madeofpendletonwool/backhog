package http

import (
	"errors"
	"net/http"
	"net/mail"
	"strings"
	"time"

	"github.com/collinpendleton/backhog/api/internal/auth"
	"github.com/collinpendleton/backhog/api/internal/models"
	"github.com/collinpendleton/backhog/api/internal/store"
)

type credentials struct {
	Email    string `json:"email"`
	Username string `json:"username"`
	Password string `json:"password"`
	// Invite is the token from a sign-up link. It is what lets an account
	// be created once self-service registration is switched off, and it
	// carries the role the account lands on.
	Invite string `json:"invite,omitempty"`
}

// authConfig is the unauthenticated payload the login and sign-up pages read
// to know what they may offer. It says nothing about who exists: only whether
// the front door is open, and — when a token is supplied — what that
// particular invite grants.
type authConfig struct {
	RegistrationEnabled bool `json:"registration_enabled"`
	// Setup marks an installation with no accounts at all. The first
	// registration is always allowed and always becomes the administrator,
	// whatever the registration setting says, or a fresh deployment that
	// shipped with the door shut could never open it.
	Setup bool `json:"setup"`
	// Invite describes the supplied token, when one was supplied and is
	// still redeemable. Absent for a missing, spent or expired token —
	// the four cases are not distinguished.
	Invite *inviteOffer `json:"invite,omitempty"`
}

// inviteOffer is the public half of an invite: enough to show the person
// what they are accepting, and nothing about who else has an account.
type inviteOffer struct {
	Email     string    `json:"email"`
	Role      string    `json:"role"`
	InvitedBy string    `json:"invited_by"`
	ExpiresAt time.Time `json:"expires_at"`
}

// handleAuthConfig tells the sign-in pages whether registration is open and
// resolves an invite token if one came with the request. It is deliberately
// unauthenticated — it is what the login page calls before anyone has an
// account — and deliberately thin.
func (s *Server) handleAuthConfig(w http.ResponseWriter, r *http.Request) {
	settings, err := s.store.Settings(r.Context())
	if err != nil {
		fail(w, err)
		return
	}
	count, err := s.store.CountUsers(r.Context())
	if err != nil {
		fail(w, err)
		return
	}

	cfg := authConfig{RegistrationEnabled: settings.RegistrationEnabled, Setup: count == 0}
	if token := strings.TrimSpace(r.URL.Query().Get("invite")); token != "" {
		if inv, err := s.store.InviteForToken(r.Context(), token); err == nil {
			cfg.Invite = &inviteOffer{
				Email:     inv.Email,
				Role:      inv.Role,
				InvitedBy: inv.CreatedByAs,
				ExpiresAt: inv.ExpiresAt,
			}
		} else if !errors.Is(err, store.ErrInviteInvalid) {
			fail(w, err)
			return
		}
	}
	writeJSON(w, http.StatusOK, cfg)
}

func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
	var body credentials
	if err := decode(r, &body); err != nil {
		fail(w, err)
		return
	}

	email := strings.TrimSpace(body.Email)
	username := strings.TrimSpace(body.Username)
	if _, err := mail.ParseAddress(email); err != nil {
		fail(w, errorf(http.StatusBadRequest, "a valid email address is required"))
		return
	}
	if len(username) < 2 || len(username) > 32 {
		fail(w, errorf(http.StatusBadRequest, "username must be 2-32 characters"))
		return
	}
	if len(body.Password) < 8 {
		fail(w, errorf(http.StatusBadRequest, "password must be at least 8 characters"))
		return
	}

	// Who may create an account, and what it may do, is settled before the
	// password is hashed — three doors, in order of precedence.
	invite := strings.TrimSpace(body.Invite)
	count, err := s.store.CountUsers(r.Context())
	if err != nil {
		fail(w, err)
		return
	}
	settings, err := s.store.Settings(r.Context())
	if err != nil {
		fail(w, err)
		return
	}
	// A fresh install always accepts its first account and makes it the
	// administrator: a deployment shipped with registration off would
	// otherwise have no way to create the account that could turn it on.
	bootstrap := count == 0
	if !bootstrap && invite == "" && !settings.RegistrationEnabled {
		fail(w, errorf(http.StatusForbidden,
			"this server is invite-only — ask the owner for a sign-up link"))
		return
	}

	hash, err := auth.HashPassword(body.Password)
	if err != nil {
		fail(w, err)
		return
	}

	var user models.User
	switch {
	case invite != "":
		// Redeeming and creating are one transaction: an invite is
		// single-use, and checking it in one statement and spending it in
		// another is exactly the window two people clicking the same link
		// would slip through.
		user, err = s.store.CreateUserFromInvite(r.Context(), invite, email, username, hash)
		if errors.Is(err, store.ErrInviteInvalid) {
			fail(w, errorf(http.StatusForbidden,
				"that sign-up link is not valid any more — ask for a new one"))
			return
		}
	case bootstrap:
		user, err = s.store.CreateUser(r.Context(), email, username, hash, models.RoleAdmin)
	default:
		user, err = s.store.CreateUser(r.Context(), email, username, hash, settings.DefaultRole)
	}
	if errors.Is(err, store.ErrConflict) {
		fail(w, errorf(http.StatusConflict, "that email or username is already taken"))
		return
	}
	if err != nil {
		fail(w, err)
		return
	}

	// Give every new account the built-in smart lists so the app is never empty.
	if err := s.store.SeedDefaultLists(r.Context(), user.ID); err != nil {
		fail(w, err)
		return
	}

	if err := s.startSession(w, r, user.ID); err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, user)
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var body credentials
	if err := decode(r, &body); err != nil {
		fail(w, err)
		return
	}

	invalid := errorf(http.StatusUnauthorized, "incorrect email or password")

	user, hash, err := s.store.GetUserByEmail(r.Context(), strings.TrimSpace(body.Email))
	if errors.Is(err, store.ErrNotFound) {
		// Hash anyway so a missing account and a wrong password take the same
		// time, and the response cannot be used to enumerate registered emails.
		_, _ = auth.HashPassword(body.Password)
		fail(w, invalid)
		return
	}
	if err != nil {
		fail(w, err)
		return
	}
	if err := auth.VerifyPassword(body.Password, hash); err != nil {
		fail(w, invalid)
		return
	}
	// Only after the password checks out. Telling someone their account is
	// suspended before they have proved it is theirs would turn login into a
	// way to ask which addresses are registered — but telling them *after*
	// is the difference between a five-word answer and an afternoon spent
	// resetting a password that was never the problem.
	if user.DisabledAt != nil {
		fail(w, errorf(http.StatusForbidden,
			"this account has been disabled — ask the server's owner"))
		return
	}

	if err := s.startSession(w, r, user.ID); err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, user)
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if sessionID := auth.SessionFrom(r.Context()); sessionID != "" {
		if err := s.store.DeleteSession(r.Context(), sessionID); err != nil {
			fail(w, err)
			return
		}
	}
	auth.ClearCookie(w, s.cfg.CookieSecure)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	user, ok := auth.UserFrom(r.Context())
	if !ok {
		fail(w, errUnauthorized)
		return
	}
	writeJSON(w, http.StatusOK, user)
}

type passwordChange struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}

func (s *Server) handleChangePassword(w http.ResponseWriter, r *http.Request) {
	userID, err := auth.MustUserID(r.Context())
	if err != nil {
		fail(w, errUnauthorized)
		return
	}

	var body passwordChange
	if err := decode(r, &body); err != nil {
		fail(w, err)
		return
	}
	if len(body.NewPassword) < 8 {
		fail(w, errorf(http.StatusBadRequest, "password must be at least 8 characters"))
		return
	}

	current, err := s.store.GetPasswordHash(r.Context(), userID)
	if err != nil {
		fail(w, err)
		return
	}
	if err := auth.VerifyPassword(body.CurrentPassword, current); err != nil {
		fail(w, errorf(http.StatusUnauthorized, "current password is incorrect"))
		return
	}

	hash, err := auth.HashPassword(body.NewPassword)
	if err != nil {
		fail(w, err)
		return
	}
	// Keep this session alive but sign out everywhere else.
	if err := s.store.UpdatePassword(r.Context(), userID, hash, auth.SessionFrom(r.Context())); err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) startSession(w http.ResponseWriter, r *http.Request, userID string) error {
	sessionID, expires, err := s.store.CreateSession(r.Context(), userID)
	if err != nil {
		return err
	}
	auth.SetCookie(w, sessionID, expires, s.cfg.CookieSecure)
	return nil
}
