package http

import (
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/collinpendleton/backhog/api/internal/auth"
	"github.com/collinpendleton/backhog/api/internal/store"
)

type sessionRequest struct {
	PlayedOn string `json:"played_on"`
	Minutes  int    `json:"minutes"`
	Note     string `json:"note"`
}

func (s *Server) handleAddSession(w http.ResponseWriter, r *http.Request) {
	userID, err := auth.MustUserID(r.Context())
	if err != nil {
		fail(w, errUnauthorized)
		return
	}

	var body sessionRequest
	if err := decode(r, &body); err != nil {
		fail(w, err)
		return
	}
	if body.PlayedOn == "" {
		// Logging is usually same-day and after the fact, so default to today
		// rather than making the client always send it.
		body.PlayedOn = time.Now().Format("2006-01-02")
	}

	session, err := s.store.AddSession(r.Context(), userID,
		chi.URLParam(r, "entryID"), body.PlayedOn, body.Minutes, body.Note)
	if errors.Is(err, store.ErrNotFound) {
		fail(w, errNotFound)
		return
	}
	if err != nil {
		fail(w, errorf(http.StatusBadRequest, err.Error()))
		return
	}
	writeJSON(w, http.StatusCreated, session)
}

func (s *Server) handleGetSessions(w http.ResponseWriter, r *http.Request) {
	userID, err := auth.MustUserID(r.Context())
	if err != nil {
		fail(w, errUnauthorized)
		return
	}

	// Confirm ownership so this can't be used to probe entry ids.
	if _, err := s.store.GetEntry(r.Context(), userID, chi.URLParam(r, "entryID")); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			fail(w, errNotFound)
			return
		}
		fail(w, err)
		return
	}

	sessions, err := s.store.Sessions(r.Context(), userID, chi.URLParam(r, "entryID"))
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"sessions": sessions})
}

func (s *Server) handleDeleteSession(w http.ResponseWriter, r *http.Request) {
	userID, err := auth.MustUserID(r.Context())
	if err != nil {
		fail(w, errUnauthorized)
		return
	}
	err = s.store.DeletePlaySession(r.Context(), userID, chi.URLParam(r, "sessionID"))
	if errors.Is(err, store.ErrNotFound) {
		fail(w, errNotFound)
		return
	}
	if err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handlePick returns one random backlog game, optionally constrained by how
// long it takes and how well it reviews.
func (s *Server) handlePick(w http.ResponseWriter, r *http.Request) {
	userID, err := auth.MustUserID(r.Context())
	if err != nil {
		fail(w, errUnauthorized)
		return
	}

	q := r.URL.Query()
	filter := store.PickFilter{
		MaxHours:   parseFloat(q.Get("max_hours")),
		MinRating:  parseFloat(q.Get("min_rating")),
		GenreID:    optionalInt64(q.Get("genre")),
		PlatformID: optionalInt64(q.Get("platform")),
	}

	entry, err := s.store.PickRandom(r.Context(), userID, filter)
	if errors.Is(err, store.ErrNotFound) {
		fail(w, errorf(http.StatusNotFound, "nothing in your backlog matches that"))
		return
	}
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, entry)
}

func parseFloat(raw string) float64 {
	var v float64
	if raw == "" {
		return 0
	}
	if _, err := fmt.Sscanf(raw, "%f", &v); err != nil {
		return 0
	}
	return v
}
