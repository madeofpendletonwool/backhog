package http

import (
	"errors"
	"net/http"
	"net/mail"
	"strings"

	"github.com/collinpendleton/backhog/api/internal/auth"
	"github.com/collinpendleton/backhog/api/internal/store"
)

type credentials struct {
	Email    string `json:"email"`
	Username string `json:"username"`
	Password string `json:"password"`
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

	hash, err := auth.HashPassword(body.Password)
	if err != nil {
		fail(w, err)
		return
	}

	user, err := s.store.CreateUser(r.Context(), email, username, hash)
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
