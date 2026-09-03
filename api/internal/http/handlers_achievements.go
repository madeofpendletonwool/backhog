package http

import (
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/collinpendleton/backhog/api/internal/achievements"
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

// handleAchievementEgg unlocks an easter egg. The client fires it when the
// user plays with the app in the right way (the Konami code, logo clicks,
// ...); the endpoint only accepts ids on the egg whitelist, is scoped to the
// caller, and is lightly rate-limited per user+id so scripting it stays
// boring. The response is the toast payload: the revealed achievement and
// whether this call is the one that unlocked it.
func (s *Server) handleAchievementEgg(w http.ResponseWriter, r *http.Request) {
	userID, err := auth.MustUserID(r.Context())
	if err != nil {
		fail(w, errUnauthorized)
		return
	}

	id := chi.URLParam(r, "achievementID")
	def := achievements.ByID(id)
	if def == nil {
		fail(w, errNotFound)
		return
	}
	if !def.Egg {
		fail(w, errorf(http.StatusBadRequest, "that achievement is not an easter egg"))
		return
	}
	if !s.eggLimiter.allow(userID+"\x00"+id, time.Now()) {
		fail(w, errorf(http.StatusTooManyRequests, "easy there — the hog noticed"))
		return
	}

	status, newly, err := s.store.UnlockEgg(r.Context(), userID, id)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"unlocked": newly, "achievement": status})
}

// eggLimiter is a fixed-window per-key counter — small, boring, and enough
// to keep scripted egg spam from mattering. Keys are user+achievement, so
// one curious user cannot starve another.
type eggLimiter struct {
	mu     sync.Mutex
	window time.Time
	counts map[string]int
	limit  int
	per    time.Duration
}

func newEggLimiter(limit int, per time.Duration) eggLimiter {
	return eggLimiter{counts: map[string]int{}, limit: limit, per: per}
}

// allow reports whether key is under the limit for the current window.
func (l *eggLimiter) allow(key string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.window.IsZero() || now.Sub(l.window) >= l.per {
		l.window = now
		l.counts = map[string]int{}
	}
	l.counts[key]++
	return l.counts[key] <= l.limit
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

// handleReadingSeason derives the books arena's per-year rollup — the
// "YYYY Reading Challenge" card. Defaults to the current year, like the
// games one.
func (s *Server) handleReadingSeason(w http.ResponseWriter, r *http.Request) {
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

	season, err := s.store.ReadingSeason(r.Context(), userID, year)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, season)
}
