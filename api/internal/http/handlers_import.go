package http

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/collinpendleton/backhog/api/internal/auth"
	"github.com/collinpendleton/backhog/api/internal/metadata"
	"github.com/collinpendleton/backhog/api/internal/models"
	"github.com/collinpendleton/backhog/api/internal/store"
)

type steamPreviewRequest struct {
	SteamID string `json:"steam_id"`
}

type steamMatch struct {
	SteamName string       `json:"steam_name"`
	AppID     int64        `json:"app_id"`
	Game      *models.Game `json:"game"`
	InLibrary bool         `json:"in_library"`
}

// handleSteamPreview resolves a Steam profile, maps its owned games onto IGDB
// entries, and reports what would be imported. Nothing is written to the user's
// library here — the client confirms first.
func (s *Server) handleSteamPreview(w http.ResponseWriter, r *http.Request) {
	userID, err := auth.MustUserID(r.Context())
	if err != nil {
		fail(w, errUnauthorized)
		return
	}

	if !s.steam.Enabled() {
		fail(w, errorf(http.StatusServiceUnavailable,
			"Steam import is not configured: set STEAM_API_KEY and restart"))
		return
	}

	var body steamPreviewRequest
	if err := decode(r, &body); err != nil {
		fail(w, err)
		return
	}
	if body.SteamID == "" {
		fail(w, errorf(http.StatusBadRequest, "a SteamID, vanity name or profile URL is required"))
		return
	}

	steamID, err := s.steam.ResolveID(r.Context(), body.SteamID)
	if err != nil {
		fail(w, errorf(http.StatusBadRequest, err.Error()))
		return
	}

	owned, err := s.steam.OwnedGames(r.Context(), steamID)
	if errors.Is(err, metadata.ErrSteamPrivate) {
		fail(w, errorf(http.StatusBadRequest,
			"that profile's game details are private — set Game details to Public in Steam privacy settings"))
		return
	}
	if err != nil {
		slog.Error("steam owned games", "error", err)
		fail(w, errorf(http.StatusBadGateway, "could not read that Steam library"))
		return
	}

	appIDs := make([]int64, 0, len(owned))
	for _, g := range owned {
		appIDs = append(appIDs, g.AppID)
	}

	igdbProvider, ok := s.provider.(*metadata.IGDB)
	if !ok {
		fail(w, errorf(http.StatusServiceUnavailable,
			"game metadata is not configured: set IGDB credentials and restart"))
		return
	}

	matched, err := igdbProvider.GamesBySteamAppIDs(r.Context(), appIDs)
	if err != nil {
		slog.Error("steam to igdb mapping", "error", err)
		fail(w, errorf(http.StatusBadGateway, "could not match those games against IGDB"))
		return
	}

	// Cache what matched so the confirm step needs no further upstream calls.
	gameIDs := make([]int64, 0, len(matched))
	for _, g := range matched {
		if err := s.store.UpsertGame(r.Context(), g, ""); err != nil {
			slog.Error("cache steam game", "game_id", g.ID, "error", err)
			continue
		}
		gameIDs = append(gameIDs, g.ID)
	}

	cached, err := s.store.GamesByIDs(r.Context(), gameIDs)
	if err != nil {
		fail(w, err)
		return
	}
	inLibrary, err := s.store.OwnedGameIDs(r.Context(), userID, gameIDs)
	if err != nil {
		fail(w, err)
		return
	}

	matches := make([]steamMatch, 0, len(owned))
	unmatched := 0
	for _, sg := range owned {
		m := steamMatch{SteamName: sg.Name, AppID: sg.AppID}
		if g, ok := matched[sg.AppID]; ok {
			if full, ok := cached[g.ID]; ok {
				m.Game = &full
				m.InLibrary = inLibrary[g.ID]
			}
		}
		if m.Game == nil {
			unmatched++
		}
		matches = append(matches, m)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"steam_id":  steamID,
		"total":     len(owned),
		"unmatched": unmatched,
		"matches":   matches,
	})
}

type bulkAddRequest struct {
	GameIDs []int64 `json:"game_ids"`
	Status  string  `json:"status"`
}

// handleBulkAdd adds many already-cached games at once. Used by the import
// confirm step; games the user already has are skipped rather than erroring, so
// a partial re-import is harmless.
func (s *Server) handleBulkAdd(w http.ResponseWriter, r *http.Request) {
	userID, err := auth.MustUserID(r.Context())
	if err != nil {
		fail(w, errUnauthorized)
		return
	}

	var body bulkAddRequest
	if err := decode(r, &body); err != nil {
		fail(w, err)
		return
	}
	if len(body.GameIDs) == 0 {
		fail(w, errorf(http.StatusBadRequest, "game_ids is required"))
		return
	}
	if len(body.GameIDs) > 2000 {
		fail(w, errorf(http.StatusBadRequest, "that's more than 2000 games; import in smaller batches"))
		return
	}
	if body.Status == "" {
		body.Status = models.StatusBacklog
	}
	if !models.ValidStatus(body.Status) {
		fail(w, errorf(http.StatusBadRequest, "invalid status"))
		return
	}

	var added, skipped int
	for _, gameID := range body.GameIDs {
		_, err := s.store.AddEntry(r.Context(), userID, gameID, body.Status, nil)
		switch {
		case errors.Is(err, store.ErrConflict):
			skipped++
		case err != nil:
			slog.Error("bulk add", "game_id", gameID, "error", err)
			skipped++
		default:
			added++
		}
	}

	writeJSON(w, http.StatusOK, map[string]int{"added": added, "skipped": skipped})
}
