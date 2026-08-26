package http

import (
	"net/http"
	"strconv"
	"time"

	"github.com/collinpendleton/backhog/api/internal/auth"
)

func (s *Server) handleAchievements(w http.ResponseWriter, r *http.Request) {
	userID, err := auth.MustUserID(r.Context())
	if err != nil {
		fail(w, errUnauthorized)
		return
	}
	achievements, err := s.store.Achievements(r.Context(), userID)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"achievements": achievements})
}

// handleSeason derives the "YYYY Backlog Challenge" rollup. Defaults to the
// current year.
func (s *Server) handleSeason(w http.ResponseWriter, r *http.Request) {
	userID, err := auth.MustUserID(r.Context())
	if err != nil {
		fail(w, errUnauthorized)
		return
	}

	year := time.Now().Year()
	if raw := r.URL.Query().Get("year"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1900 || parsed > 9999 {
			fail(w, errorf(http.StatusBadRequest, "year must be a four-digit year"))
			return
		}
		year = parsed
	}

	season, err := s.store.Season(r.Context(), userID, year)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, season)
}
