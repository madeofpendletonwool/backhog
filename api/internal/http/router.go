package http

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/collinpendleton/backhog/api/internal/auth"
	"github.com/collinpendleton/backhog/api/internal/backfill"
	booktext "github.com/collinpendleton/backhog/api/internal/books"
	bookaudio "github.com/collinpendleton/backhog/api/internal/books/audio"
	"github.com/collinpendleton/backhog/api/internal/books/position"
	"github.com/collinpendleton/backhog/api/internal/config"
	"github.com/collinpendleton/backhog/api/internal/media"
	"github.com/collinpendleton/backhog/api/internal/metadata"
	"github.com/collinpendleton/backhog/api/internal/store"
)

// backfillKickCap is the per-run budget when a user triggers the enrichment
// walk from the UI.
const backfillKickCap = backfill.KickCap

// eggRateLimit is how many egg attempts one user may make per achievement
// per window — enough for honest curiosity, boring for scripts.
const eggRateLimit = 10

// Server holds the dependencies shared by all handlers.
type Server struct {
	cfg      config.Config
	store    *store.Store
	provider metadata.Provider
	books    metadata.BookProvider
	covers   *metadata.CoverCache
	steam    *metadata.Steam
	backfill *backfill.Runner
	media    *media.Runner
	matcher  *media.Matcher
	epubs    *booktext.Ingester
	audio    *bookaudio.Service
	// anchors supplies the alignment and page-map data the position
	// translator interpolates over. Until forced alignment (Stage 7) and
	// page anchors (Stage 9) land there is nothing to supply, so every
	// translation reports itself underived and the API falls back to raw
	// stored positions.
	anchors    position.Provider
	eggLimiter eggLimiter
}

func NewServer(cfg config.Config, st *store.Store, provider metadata.Provider, books metadata.BookProvider, covers *metadata.CoverCache, steam *metadata.Steam, backfill *backfill.Runner, mediaRunner *media.Runner) *Server {
	// The EPUB ingester is pure cfg+store glue, so it is built here rather
	// than threaded through every caller. A failed directory creates a nil
	// ingester: the text endpoints answer 503 instead of taking the whole
	// server down.
	epubs, err := booktext.NewIngester(st, cfg.EpubTextDir)
	if err != nil {
		slog.Error("epub text dir unavailable", "dir", cfg.EpubTextDir, "error", err)
		epubs = nil
	}
	return &Server{
		cfg: cfg, store: st, provider: provider, books: books, covers: covers,
		steam: steam, backfill: backfill, media: mediaRunner, epubs: epubs,
		matcher:    media.NewMatcher(st, books),
		anchors:    position.NoAnchors{},
		audio:      bookaudio.NewService(st, cfg.MediaDirs),
		eggLimiter: newEggLimiter(eggRateLimit, time.Minute),
	}
}

// Routes builds the API router. Everything is mounted under /api so nginx can
// proxy a single prefix and serve the SPA from the same origin.
func (s *Server) Routes() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(30 * time.Second))
	r.Use(auth.Middleware(s.store))

	r.Route("/api", func(r chi.Router) {
		r.Get("/healthz", s.handleHealth)

		r.Route("/auth", func(r chi.Router) {
			r.Post("/register", s.handleRegister)
			r.Post("/login", s.handleLogin)
			r.Post("/logout", s.handleLogout)
			r.With(auth.Require).Get("/me", s.handleMe)
			r.With(auth.Require).Post("/password", s.handleChangePassword)
		})

		// Covers are public: they are just images, and making them public keeps
		// <img> tags simple and cacheable. Keyed by media namespace; the
		// original single-media path stays as a permanent redirect.
		r.Get("/covers/game/{gameID}", s.handleCover)
		r.Get("/covers/book/{bookID}", s.handleBookCover)
		r.Get("/covers/{gameID}", s.handleLegacyCoverRedirect)

		r.Group(func(r chi.Router) {
			r.Use(auth.Require)

			r.Get("/games/search", s.handleGameSearch)
			r.Get("/games/{gameID}", s.handleGetGame)
			r.Get("/games/{gameID}/series", s.handleGameSeries)

			r.Get("/books/search", s.handleBookSearch)
			r.Get("/books/isbn/{isbn}", s.handleBookByISBN)
			r.Get("/books/{bookID}", s.handleGetBook)

			// Canonical text for a library entry: the spine index and ranged
			// slices of the normalized text (byte offsets). Parsing is on
			// demand — never part of the NAS scan.
			r.Get("/books/{entryID}/text/chapters", s.handleBookTextChapters)
			r.Get("/books/{entryID}/text", s.handleBookText)

			// The audiobook as one continuous timeline, and its tracks
			// streamed from the NAS with byte-range support so a browser
			// can seek into the middle of a 400MB m4b.
			r.Get("/books/{entryID}/audio", s.handleBookAudioTimeline)
			r.Get("/books/{entryID}/audio/{trackID}", s.handleBookAudioTrack)

			// One position, three views: the canonical character offset is
			// the truth, the audio timestamp and printed page are derived
			// from it on read.
			r.Get("/books/{entryID}/position", s.handleGetBookPosition)
			r.Put("/books/{entryID}/position", s.handlePutBookPosition)
			r.Get("/books/{entryID}/sessions", s.handleGetReadingSessions)
			r.Post("/books/{entryID}/sessions", s.handleAddReadingSession)

			// The attach flow: files on the NAS become this book's audio
			// timeline and canonical text.
			r.Get("/books/{entryID}/files", s.handleBookFiles)
			r.Post("/books/{entryID}/files", s.handleAttachFiles)
			r.Delete("/books/{entryID}/files/{fileID}", s.handleDetachFile)

			r.Route("/series", func(r chi.Router) {
				r.Get("/", s.handleSeriesIndex)
				// Static path first: chi routes it before the {seriesID} param.
				r.Get("/backfill", s.handleSeriesBackfill)
				r.Post("/backfill", s.handleSeriesBackfill)
				r.Get("/{seriesID}", s.handleSeriesDetail)
				r.Put("/{seriesID}/order", s.handleSeriesPlayOrder)
				r.Post("/{seriesID}/reorder", s.handleSeriesReorder)
			})

			r.Route("/library", func(r chi.Router) {
				r.Get("/", s.handleListLibrary)
				r.Post("/", s.handleAddToLibrary)
				r.Get("/stats", s.handleStats)
				r.Get("/debt", s.handleDebt)
				r.Get("/insights", s.handleInsights)
				r.Get("/queue", s.handleQueue)
				r.Post("/reorder", s.handleReorder)
				r.Get("/facets", s.handleFacets)
				r.Get("/pick", s.handlePick)
				r.Get("/tonight", s.handleTonight)
				r.Post("/bulk", s.handleBulkAdd)
				r.Get("/{entryID}", s.handleGetEntry)
				r.Get("/{entryID}/lists", s.handleEntryLists)
				r.Get("/{entryID}/projects", s.handleEntryProjects)
				r.Get("/{entryID}/sessions", s.handleGetSessions)
				r.Post("/{entryID}/sessions", s.handleAddSession)
				r.Patch("/{entryID}", s.handleUpdateEntry)
				r.Delete("/{entryID}", s.handleDeleteEntry)
			})

			// The scanned NAS library: kick and poll the inventory walk,
			// list files for the attach UI, and serve the attach review
			// queue.
			r.Route("/media", func(r chi.Router) {
				r.Get("/scan", s.handleMediaScan)
				r.Post("/scan", s.handleMediaScan)
				r.Get("/files", s.handleMediaFiles)
				r.Get("/candidates", s.handleMediaCandidates)
				r.Post("/ignore", s.handleMediaIgnore)
				r.Delete("/ignore/{fileID}", s.handleMediaUnignore)
			})

			r.Route("/achievements", func(r chi.Router) {
				r.Get("/", s.handleAchievements)
				r.Get("/season", s.handleSeason)
				r.Post("/{achievementID}/egg", s.handleAchievementEgg)
			})

			r.Delete("/sessions/{sessionID}", s.handleDeleteSession)
			r.Post("/import/steam/preview", s.handleSteamPreview)

			r.Route("/lists", func(r chi.Router) {
				r.Get("/", s.handleGetLists)
				r.Post("/", s.handleCreateList)
				r.Get("/fields", s.handleSmartFields)
				r.Get("/{listID}", s.handleGetList)
				r.Patch("/{listID}", s.handleUpdateList)
				r.Delete("/{listID}", s.handleDeleteList)
				r.Post("/{listID}/items", s.handleAddListItem)
				r.Delete("/{listID}/items/{entryID}", s.handleRemoveListItem)
				r.Post("/{listID}/reorder", s.handleReorderListItem)
			})

			r.Route("/projects", func(r chi.Router) {
				r.Get("/", s.handleGetProjects)
				r.Post("/", s.handleCreateProject)
				r.Get("/{projectID}", s.handleGetProject)
				r.Patch("/{projectID}", s.handleUpdateProject)
				r.Delete("/{projectID}", s.handleDeleteProject)
				r.Post("/{projectID}/items", s.handleAddProjectItem)
				r.Patch("/{projectID}/items/{entryID}", s.handleSetProjectItemDone)
				r.Delete("/{projectID}/items/{entryID}", s.handleRemoveProjectItem)
				r.Post("/{projectID}/reorder", s.handleReorderProjectItem)
			})
		})
	})

	return r
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DB().PingContext(r.Context()); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "db unavailable"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":   "ok",
		"metadata": s.cfg.MetadataEnabled(),
		"steam":    s.steam.Enabled(),
	})
}
