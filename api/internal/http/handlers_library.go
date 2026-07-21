package http

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/collinpendleton/backhog/api/internal/auth"
	"github.com/collinpendleton/backhog/api/internal/metadata"
	"github.com/collinpendleton/backhog/api/internal/models"
	"github.com/collinpendleton/backhog/api/internal/store"
)

func (s *Server) handleListLibrary(w http.ResponseWriter, r *http.Request) {
	userID, err := auth.MustUserID(r.Context())
	if err != nil {
		fail(w, errUnauthorized)
		return
	}

	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	offset, _ := strconv.Atoi(q.Get("offset"))

	filter := store.LibraryFilter{
		Status:     q.Get("status"),
		Query:      q.Get("q"),
		ListID:     q.Get("list"),
		Sort:       q.Get("sort"),
		PlatformID: optionalInt64(q.Get("platform")),
		GenreID:    optionalInt64(q.Get("genre")),
		Limit:      limit,
		Offset:     offset,
	}

	entries, err := s.store.ListEntries(r.Context(), userID, filter)
	if err != nil {
		fail(w, err)
		return
	}
	total, err := s.store.CountEntries(r.Context(), userID, filter)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"entries": entries, "total": total})
}

type addEntryRequest struct {
	GameID     int64  `json:"game_id"`
	Status     string `json:"status"`
	PlatformID *int64 `json:"platform_id"`
}

// handleAddToLibrary adds a game, fetching and caching its metadata and cover
// first if we have not seen it before.
func (s *Server) handleAddToLibrary(w http.ResponseWriter, r *http.Request) {
	userID, err := auth.MustUserID(r.Context())
	if err != nil {
		fail(w, errUnauthorized)
		return
	}

	var body addEntryRequest
	if err := decode(r, &body); err != nil {
		fail(w, err)
		return
	}
	if body.GameID <= 0 {
		fail(w, errorf(http.StatusBadRequest, "game_id is required"))
		return
	}

	// Search results are cached without playtime to keep the palette fast, so a
	// game may be present but incomplete. Enrich it on the way into the library,
	// which is the point where the number actually gets shown.
	cached, err := s.store.GetGame(r.Context(), body.GameID)
	switch {
	case errors.Is(err, store.ErrNotFound), err == nil && cached.TimeToBeatMain == nil:
		fetched, ferr := s.provider.GetByID(r.Context(), body.GameID)
		if ferr != nil {
			if errors.Is(err, store.ErrNotFound) {
				fail(w, errorf(http.StatusBadGateway, "could not look up that game"))
				return
			}
			// We already have usable metadata; losing playtime is not fatal.
			slog.Warn("enrich game on add", "game_id", body.GameID, "error", ferr)
		} else if uerr := s.store.UpsertGame(r.Context(), fetched, ""); uerr != nil {
			fail(w, uerr)
			return
		}
	case err != nil:
		fail(w, err)
		return
	}

	s.cacheCover(r, body.GameID)

	status := body.Status
	if status == "" {
		status = models.StatusBacklog
	}
	entry, err := s.store.AddEntry(r.Context(), userID, body.GameID, status, body.PlatformID)
	if errors.Is(err, store.ErrConflict) {
		fail(w, errorf(http.StatusConflict, "that game is already in your library"))
		return
	}
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, entry)
}

func (s *Server) handleGetEntry(w http.ResponseWriter, r *http.Request) {
	userID, err := auth.MustUserID(r.Context())
	if err != nil {
		fail(w, errUnauthorized)
		return
	}
	entry, err := s.store.GetEntry(r.Context(), userID, chi.URLParam(r, "entryID"))
	if errors.Is(err, store.ErrNotFound) {
		fail(w, errNotFound)
		return
	}
	if err != nil {
		fail(w, err)
		return
	}

	// The rich metadata is fetched lazily: games cached before this feature (or
	// added via the lean search/import path) have no extras, so backfill them the
	// first time their detail page is opened. Best-effort — a lookup failure or
	// missing IGDB credentials just serves what we already have.
	if len(entry.Game.Extras) == 0 {
		entry = s.backfillGameExtras(r, userID, entry)
	}

	writeJSON(w, http.StatusOK, entry)
}

// backfillGameExtras refetches a game's full metadata from the provider and
// re-caches it, returning the reloaded entry. On any failure it returns the
// entry unchanged.
func (s *Server) backfillGameExtras(r *http.Request, userID string, entry models.Entry) models.Entry {
	fetched, err := s.provider.GetByID(r.Context(), entry.Game.ID)
	if err != nil {
		if !errors.Is(err, metadata.ErrUnavailable) {
			slog.Warn("backfill game metadata", "game_id", entry.Game.ID, "error", err)
		}
		return entry
	}
	if err := s.store.UpsertGame(r.Context(), fetched, ""); err != nil {
		slog.Warn("store backfilled metadata", "game_id", entry.Game.ID, "error", err)
		return entry
	}
	refreshed, err := s.store.GetEntry(r.Context(), userID, entry.ID)
	if err != nil {
		return entry
	}
	return refreshed
}

type updateEntryRequest struct {
	Status     *string `json:"status"`
	PlatformID *int64  `json:"platform_id"`
	UserRating *int    `json:"user_rating"`
	Notes      *string `json:"notes"`
}

func (s *Server) handleUpdateEntry(w http.ResponseWriter, r *http.Request) {
	userID, err := auth.MustUserID(r.Context())
	if err != nil {
		fail(w, errUnauthorized)
		return
	}

	// Decode into a raw map first so an explicit `null` can be told apart from
	// an omitted field: null clears the rating, omission leaves it alone.
	var raw map[string]any
	if err := decodeRaw(r, &raw); err != nil {
		fail(w, err)
		return
	}
	body, err := parseUpdateEntry(raw)
	if err != nil {
		fail(w, err)
		return
	}

	entry, err := s.store.UpdateEntry(r.Context(), userID, chi.URLParam(r, "entryID"), body)
	if errors.Is(err, store.ErrNotFound) {
		fail(w, errNotFound)
		return
	}
	if err != nil {
		fail(w, errorf(http.StatusBadRequest, err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, entry)
}

func (s *Server) handleDeleteEntry(w http.ResponseWriter, r *http.Request) {
	userID, err := auth.MustUserID(r.Context())
	if err != nil {
		fail(w, errUnauthorized)
		return
	}
	err = s.store.DeleteEntry(r.Context(), userID, chi.URLParam(r, "entryID"))
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

func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	userID, err := auth.MustUserID(r.Context())
	if err != nil {
		fail(w, errUnauthorized)
		return
	}
	stats, err := s.store.Stats(r.Context(), userID)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, stats)
}

func (s *Server) handleFacets(w http.ResponseWriter, r *http.Request) {
	userID, err := auth.MustUserID(r.Context())
	if err != nil {
		fail(w, errUnauthorized)
		return
	}
	platforms, genres, err := s.store.Facets(r.Context(), userID)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"platforms": platforms, "genres": genres})
}

func (s *Server) handleQueue(w http.ResponseWriter, r *http.Request) {
	userID, err := auth.MustUserID(r.Context())
	if err != nil {
		fail(w, errUnauthorized)
		return
	}
	entries, err := s.store.Queue(r.Context(), userID)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"entries": entries})
}

type reorderRequest struct {
	EntryID  string `json:"entry_id"`
	BeforeID string `json:"before_id"`
	AfterID  string `json:"after_id"`
}

// handleReorder moves an entry between two neighbours in the play queue.
func (s *Server) handleReorder(w http.ResponseWriter, r *http.Request) {
	userID, err := auth.MustUserID(r.Context())
	if err != nil {
		fail(w, errUnauthorized)
		return
	}

	var body reorderRequest
	if err := decode(r, &body); err != nil {
		fail(w, err)
		return
	}
	if body.EntryID == "" {
		fail(w, errorf(http.StatusBadRequest, "entry_id is required"))
		return
	}

	err = s.store.MoveEntry(r.Context(), userID, body.EntryID, body.BeforeID, body.AfterID)
	if errors.Is(err, store.ErrNotFound) {
		fail(w, errNotFound)
		return
	}
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// cacheCover downloads a cover and records its accent colour. Failures are
// logged but never block adding a game.
func (s *Server) cacheCover(r *http.Request, gameID int64) {
	if s.covers.Has(gameID) {
		return
	}
	url, err := s.store.CoverURLFor(r.Context(), gameID)
	if err != nil || url == "" {
		return
	}
	accent, err := s.covers.Fetch(r.Context(), gameID, url)
	if err != nil {
		slog.Warn("cache cover", "game_id", gameID, "error", err)
		return
	}
	if err := s.store.SetAccent(r.Context(), gameID, accent); err != nil {
		slog.Warn("store accent", "game_id", gameID, "error", err)
	}
}

func (s *Server) ownedGameIDs(r *http.Request, ids []int64) (map[int64]bool, error) {
	userID, err := auth.MustUserID(r.Context())
	if err != nil {
		return nil, errUnauthorized
	}
	return s.store.OwnedGameIDs(r.Context(), userID, ids)
}

func optionalInt64(raw string) *int64 {
	if raw == "" {
		return nil
	}
	v, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return nil
	}
	return &v
}
