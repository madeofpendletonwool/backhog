package http

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/collinpendleton/backhog/api/internal/auth"
	"github.com/collinpendleton/backhog/api/internal/config"
	"github.com/collinpendleton/backhog/api/internal/metadata"
	"github.com/collinpendleton/backhog/api/internal/store"
)

// Server holds the dependencies shared by all handlers.
type Server struct {
	cfg      config.Config
	store    *store.Store
	provider metadata.Provider
	covers   *metadata.CoverCache
}

func NewServer(cfg config.Config, st *store.Store, provider metadata.Provider, covers *metadata.CoverCache) *Server {
	return &Server{cfg: cfg, store: st, provider: provider, covers: covers}
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
		// <img> tags simple and cacheable.
		r.Get("/covers/{gameID}", s.handleCover)

		r.Group(func(r chi.Router) {
			r.Use(auth.Require)

			r.Get("/games/search", s.handleGameSearch)
			r.Get("/games/{gameID}", s.handleGetGame)

			r.Route("/library", func(r chi.Router) {
				r.Get("/", s.handleListLibrary)
				r.Post("/", s.handleAddToLibrary)
				r.Get("/stats", s.handleStats)
				r.Get("/queue", s.handleQueue)
				r.Post("/reorder", s.handleReorder)
				r.Get("/facets", s.handleFacets)
				r.Get("/{entryID}", s.handleGetEntry)
				r.Get("/{entryID}/lists", s.handleEntryLists)
				r.Patch("/{entryID}", s.handleUpdateEntry)
				r.Delete("/{entryID}", s.handleDeleteEntry)
			})

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
	})
}
