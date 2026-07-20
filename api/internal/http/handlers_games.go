package http

import (
	"errors"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/collinpendleton/backhog/api/internal/metadata"
	"github.com/collinpendleton/backhog/api/internal/store"
)

// handleGameSearch proxies a search to the metadata provider and caches every
// result locally, so adding one afterwards needs no second upstream call.
func (s *Server) handleGameSearch(w http.ResponseWriter, r *http.Request) {
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	if query == "" {
		writeJSON(w, http.StatusOK, map[string]any{"results": []any{}})
		return
	}

	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	results, err := s.provider.Search(r.Context(), query, limit)
	if errors.Is(err, metadata.ErrUnavailable) {
		fail(w, errorf(http.StatusServiceUnavailable,
			"game search is unavailable: set IGDB_CLIENT_ID and IGDB_CLIENT_SECRET"))
		return
	}
	if err != nil {
		slog.Error("game search failed", "query", query, "error", err)
		fail(w, errorf(http.StatusBadGateway, "game search failed, please try again"))
		return
	}

	ids := make([]int64, 0, len(results))
	for _, g := range results {
		// Cache metadata now, but not covers: downloading 20 images per
		// keystroke would be wasteful. Covers are fetched on add.
		if err := s.store.UpsertGame(r.Context(), g, ""); err != nil {
			slog.Error("cache searched game", "game_id", g.ID, "error", err)
			continue
		}
		ids = append(ids, g.ID)
	}

	games, err := s.store.GamesByIDs(r.Context(), ids)
	if err != nil {
		fail(w, err)
		return
	}
	owned, err := s.ownedGameIDs(r, ids)
	if err != nil {
		fail(w, err)
		return
	}

	// Preserve the provider's relevance ordering, which the map lookup loses.
	out := make([]map[string]any, 0, len(ids))
	for _, id := range ids {
		g, ok := games[id]
		if !ok {
			continue
		}
		out = append(out, map[string]any{"game": g, "in_library": owned[id]})
	}
	writeJSON(w, http.StatusOK, map[string]any{"results": out})
}

func (s *Server) handleGetGame(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "gameID"), 10, 64)
	if err != nil {
		fail(w, errorf(http.StatusBadRequest, "invalid game id"))
		return
	}

	game, err := s.store.GetGame(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		// Not cached yet — fall back to the provider and cache it.
		fetched, ferr := s.provider.GetByID(r.Context(), id)
		if ferr != nil {
			fail(w, errNotFound)
			return
		}
		if err := s.store.UpsertGame(r.Context(), fetched, ""); err != nil {
			fail(w, err)
			return
		}
		game, err = s.store.GetGame(r.Context(), id)
	}
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, game)
}

// handleCover serves a locally cached cover, downloading it on first request if
// the file is missing but the upstream URL is known.
func (s *Server) handleCover(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "gameID"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	if !s.covers.Has(id) {
		url, err := s.store.CoverURLFor(r.Context(), id)
		if err != nil || url == "" {
			http.NotFound(w, r)
			return
		}
		accent, err := s.covers.Fetch(r.Context(), id, url)
		if err != nil {
			// The IGDB CDN is occasionally slow, especially when a search fires
			// a dozen cover requests at once. Falling back to the upstream URL
			// means the user sees artwork now instead of a broken tile, and the
			// next request will retry the local cache.
			slog.Warn("fetch cover, redirecting upstream", "game_id", id, "error", err)
			http.Redirect(w, r, url, http.StatusFound)
			return
		}
		if err := s.store.SetAccent(r.Context(), id, accent); err != nil {
			slog.Warn("store accent", "game_id", id, "error", err)
		}
	}

	path := s.covers.Path(id)
	f, err := os.Open(path)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		http.NotFound(w, r)
		return
	}

	// Covers are immutable for a given game id, so cache them hard.
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	http.ServeContent(w, r, path, info.ModTime(), f)
}
