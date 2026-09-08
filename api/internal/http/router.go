package http

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/collinpendleton/backhog/api/internal/auth"
	"github.com/collinpendleton/backhog/api/internal/backfill"
	booktext "github.com/collinpendleton/backhog/api/internal/books"
	bookaudio "github.com/collinpendleton/backhog/api/internal/books/audio"
	"github.com/collinpendleton/backhog/api/internal/books/passage"
	"github.com/collinpendleton/backhog/api/internal/books/position"
	"github.com/collinpendleton/backhog/api/internal/books/search"
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
	// passage places paper-page text in the canonical text; it reads the
	// same companion files the ingester writes and is nil exactly when
	// the ingester is.
	passage *passage.Matcher
	// search finds a phrase in the canonical text. It reads the same
	// companion files as the passage matcher and follows the ingester into
	// nil alongside it.
	search *search.Searcher
	// searchViews caches the derivation inputs a search result is rendered
	// through, because searching is a keystroke path and the database runs
	// on one connection.
	searchViews *viewsCache
	// anchors supplies the alignment and page-map data the position
	// translator interpolates over: alignment anchors arrive through the
	// worker queue, page anchors from the physical copies' scans.
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
	// The passage matcher shares the ingester's directory, so it follows
	// it into nil on the same failure.
	var match *passage.Matcher
	var find *search.Searcher
	if epubs != nil {
		ing := epubs
		load := func(_ context.Context, textID string) (string, error) {
			data, err := os.ReadFile(ing.TextPath(textID))
			if err != nil {
				return "", fmt.Errorf("read canonical text: %w", err)
			}
			return string(data), nil
		}
		match = passage.New(load)
		find = search.New(load)
	}
	return &Server{
		cfg: cfg, store: st, provider: provider, books: books, covers: covers,
		steam: steam, backfill: backfill, media: mediaRunner, epubs: epubs,
		passage:     match,
		search:      find,
		searchViews: newViewsCache(searchViewsTTL),
		matcher:     media.NewMatcher(st, books),
		anchors:     alignmentAnchors{store: st},
		audio:       bookaudio.NewService(st, cfg.MediaDirs),
		eggLimiter:  newEggLimiter(eggRateLimit, time.Minute),
	}
}

// Close releases the server's background workers (the media matcher's
// enrichment worker). Safe to call more than once.
func (s *Server) Close() {
	if s.matcher != nil {
		s.matcher.Close()
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
			// Unauthenticated on purpose: it is what the login page reads
			// before anyone has an account, to know whether to offer a
			// sign-up link and to resolve an invite token into the offer
			// it represents.
			r.Get("/config", s.handleAuthConfig)
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
			// The same blocks as prose, which is what the reader renders;
			// the canonical text above is folded for matching, not reading.
			r.Get("/books/{entryID}/text/display", s.handleBookTextDisplay)
			// The reader's illustrations, read straight out of the EPUB.
			// Same containment rules as the audio stream, and the only way
			// a book's images reach the page — nothing loads off-origin.
			r.Get("/books/{entryID}/text/asset", s.handleBookTextAsset)

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
			// POST is the same write, for the one caller that cannot use
			// PUT: navigator.sendBeacon, which is the only request a
			// browser promises to deliver after a tab closes or a phone
			// backgrounds the player, and which can only POST. The session
			// cookie is SameSite=Lax, so a cross-site beacon carries no
			// credentials and this opens nothing PUT did not.
			r.Post("/books/{entryID}/position", s.handlePutBookPosition)
			r.Get("/books/{entryID}/sessions", s.handleGetReadingSessions)
			r.Post("/books/{entryID}/sessions", s.handleAddReadingSession)

			// The attach flow: files on the NAS become this book's audio
			// timeline and canonical text. Reading the list is open to
			// anyone who may read the book — it is how the detail page
			// knows there is an audiobook at all, and the handler blanks
			// the NAS paths for a reader — but every mutation is the file
			// layer, and the file layer belongs to managers.
			r.Get("/books/{entryID}/files", s.handleBookFiles)
			r.With(auth.RequireMediaManager).Post("/books/{entryID}/files", s.handleAttachFiles)
			r.With(auth.RequireMediaManager).Delete("/books/{entryID}/files/{fileID}", s.handleDetachFile)
			r.With(auth.RequireMediaManager).Put("/books/{entryID}/files/{fileID}/primary", s.handlePrimaryTextFile)
			// The same choice on the audio side, made over a whole
			// recording rather than one file: which of the audiobooks
			// attached to this book is the one that plays.
			r.With(auth.RequireMediaManager).Put("/books/{entryID}/audio-editions/{editionID}/primary", s.handlePrimaryAudioEdition)

			// Sharing: who may read the files behind this book. The
			// picker and both grants are the owner's own — a reader can
			// share the books they attached, which is none of them, and
			// the store scopes every one of these through an entry the
			// caller owns.
			r.Get("/books/{entryID}/shares", s.handleBookShares)
			r.Post("/books/{entryID}/shares", s.handleShareBook)
			r.Delete("/books/{entryID}/shares/{userID}", s.handleUnshareBook)

			// Alignment: queue the text↔audio mapping, watch it run,
			// clear it. The job is worked by an optional container
			// through /internal; with no worker it just sits queued, and
			// everything else about the book keeps working. Watching is
			// open to any reader of the book; starting and clearing a run
			// writes shared state and costs real time on the worker, so
			// both sit behind the file-layer gate.
			r.With(auth.RequireMediaManager).Post("/books/{entryID}/align", s.handleBookAlignEnqueue)
			r.Get("/books/{entryID}/align", s.handleBookAlignStatus)
			r.With(auth.RequireMediaManager).Delete("/books/{entryID}/align", s.handleBookAlignDelete)

			// Search inside one book's text. It is the passage matcher's
			// query profile inverted — a few remembered words instead of a
			// scanned page — and it answers in offsets, so every hit comes
			// back already placed in the audiobook and the printed page.
			r.Get("/books/{entryID}/search", s.handleSearchInBook)

			// The physical-copy bridge: place text read off a paper page
			// in the canonical text, register printings of a book the
			// user holds (owned or borrowed), and pin pages as anchors
			// the position endpoints interpolate over. The borrowed
			// lifecycle is explicit transitions, not a mutable PATCH:
			// return, check out again, upgrade to owned — all of them
			// keep the page map; only delete ever drops one.
			r.Post("/books/{entryID}/passage", s.handleBookPassage)
			r.Get("/books/{entryID}/copies", s.handleListBookCopies)
			r.Post("/books/{entryID}/copies", s.handleCreateBookCopy)
			r.Patch("/books/{entryID}/copies/{copyID}", s.handleUpdateBookCopy)
			r.Delete("/books/{entryID}/copies/{copyID}", s.handleDeleteBookCopy)
			r.Post("/books/{entryID}/copies/{copyID}/return", s.handleReturnBookCopy)
			r.Post("/books/{entryID}/copies/{copyID}/reopen", s.handleReopenBookCopy)
			r.Post("/books/{entryID}/copies/{copyID}/own", s.handleOwnBookCopy)
			r.Get("/books/{entryID}/copies/{copyID}/pages", s.handleListBookCopyPages)
			r.Post("/books/{entryID}/copies/{copyID}/pages", s.handleSaveBookPageAnchor)

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
				// The whole inventory is raw NAS paths and the controls
				// that repoint them. None of it is a reader's business.
				r.Use(auth.RequireMediaManager)
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
				r.Get("/reading-season", s.handleReadingSeason)
				r.Post("/{achievementID}/egg", s.handleAchievementEgg)
			})

			// Both halves of "who has what": books this account shared
			// out, and books other people shared with it.
			r.Get("/shares", s.handleSharesOverview)

			// Account management. Everything under here needs the admin
			// role, which answers 403 rather than 404: this is a fixed
			// route that either exists for you or does not, not a row
			// whose existence a status code could leak.
			r.Route("/admin", func(r chi.Router) {
				r.Use(auth.RequireAdmin)

				r.Get("/users", s.handleListUsers)
				r.Patch("/users/{userID}", s.handleUpdateUser)
				r.Post("/users/{userID}/password", s.handleAdminResetPassword)
				r.Delete("/users/{userID}", s.handleDeleteUser)

				r.Get("/settings", s.handleGetServerSettings)
				r.Put("/settings", s.handleUpdateServerSettings)

				r.Get("/invites", s.handleListInvites)
				r.Post("/invites", s.handleCreateInvite)
				r.Post("/invites/{inviteID}/revoke", s.handleRevokeInvite)
				r.Delete("/invites/{inviteID}", s.handleDeleteInvite)
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

	// The internal alignment worker API. It lives outside /api on
	// purpose: nginx proxies exactly /api/ to this process, so nothing
	// under /internal is reachable from the public vhost — only from
	// the compose network, by a worker holding the shared token.
	r.Route("/internal", func(r chi.Router) {
		r.Use(s.requireAlignWorker)
		r.Post("/align/claim", s.handleAlignClaim)
		r.Post("/align/{jobID}/progress", s.handleAlignProgress)
		r.Post("/align/{jobID}/segments", s.handleAlignSegments)
		r.Post("/align/{jobID}/anchors", s.handleAlignAnchors)
		r.Post("/align/{jobID}/complete", s.handleAlignComplete)
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
