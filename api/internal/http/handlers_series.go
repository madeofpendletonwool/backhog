package http

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/collinpendleton/backhog/api/internal/auth"
	"github.com/collinpendleton/backhog/api/internal/store"
)

func (s *Server) handleSeriesIndex(w http.ResponseWriter, r *http.Request) {
	userID, err := auth.MustUserID(r.Context())
	if err != nil {
		fail(w, errUnauthorized)
		return
	}
	series, err := s.store.SeriesIndex(r.Context(), userID)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"series": series})
}

func (s *Server) handleSeriesDetail(w http.ResponseWriter, r *http.Request) {
	userID, err := auth.MustUserID(r.Context())
	if err != nil {
		fail(w, errUnauthorized)
		return
	}
	detail, err := s.store.SeriesDetail(r.Context(), userID, chi.URLParam(r, "seriesID"))
	if errors.Is(err, store.ErrNotFound) {
		fail(w, errNotFound)
		return
	}
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, detail)
}

type playOrderRequest struct {
	PlayOrder string `json:"play_order"`
}

func (s *Server) handleSeriesPlayOrder(w http.ResponseWriter, r *http.Request) {
	userID, err := auth.MustUserID(r.Context())
	if err != nil {
		fail(w, errUnauthorized)
		return
	}

	var body playOrderRequest
	if err := decode(r, &body); err != nil {
		fail(w, err)
		return
	}

	err = s.store.SetSeriesPlayOrder(r.Context(), userID, chi.URLParam(r, "seriesID"), body.PlayOrder)
	if errors.Is(err, store.ErrNotFound) {
		fail(w, errNotFound)
		return
	}
	if err != nil {
		fail(w, errorf(http.StatusBadRequest, err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

type seriesReorderRequest struct {
	GameID   int64 `json:"game_id"`
	BeforeID int64 `json:"before_id"`
	AfterID  int64 `json:"after_id"`
}

func (s *Server) handleSeriesReorder(w http.ResponseWriter, r *http.Request) {
	userID, err := auth.MustUserID(r.Context())
	if err != nil {
		fail(w, errUnauthorized)
		return
	}

	var body seriesReorderRequest
	if err := decode(r, &body); err != nil {
		fail(w, err)
		return
	}
	if body.GameID == 0 {
		fail(w, errorf(http.StatusBadRequest, "game_id is required"))
		return
	}

	err = s.store.MoveSeriesGame(r.Context(), userID, chi.URLParam(r, "seriesID"),
		body.GameID, body.BeforeID, body.AfterID)
	if errors.Is(err, store.ErrNotFound) {
		fail(w, errNotFound)
		return
	}
	if err != nil {
		fail(w, errorf(http.StatusBadRequest, err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// handleSeriesBackfill is the on-demand trigger for the IGDB enrichment walk:
// it starts a run when none is active and always reports whether one is.
func (s *Server) handleSeriesBackfill(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		started := s.backfill.Kick(r.Context(), backfillKickCap)
		writeJSON(w, http.StatusOK, map[string]bool{"started": started})
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"running": s.backfill.Running()})
}

func (s *Server) handleGameSeries(w http.ResponseWriter, r *http.Request) {
	if _, err := auth.MustUserID(r.Context()); err != nil {
		fail(w, errUnauthorized)
		return
	}
	id, err := strconv.ParseInt(chi.URLParam(r, "gameID"), 10, 64)
	if err != nil {
		fail(w, errorf(http.StatusBadRequest, "invalid game id"))
		return
	}
	series, err := s.store.SeriesForGame(r.Context(), id)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"series": series})
}
