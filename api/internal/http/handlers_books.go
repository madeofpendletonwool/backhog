package http

import (
	"errors"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/collinpendleton/backhog/api/internal/auth"
	"github.com/collinpendleton/backhog/api/internal/metadata"
	"github.com/collinpendleton/backhog/api/internal/store"
)

// workKeyPattern matches an Open Library work key ("OL12345W"). Validating the
// path parameter keeps provider URLs honest — a crafted key must never be able
// to escape the /works/{key}.json path.
var workKeyPattern = regexp.MustCompile(`^OL\d+W$`)

// handleBookSearch proxies a search to the book metadata provider and caches
// every result locally, mirroring handleGameSearch.
func (s *Server) handleBookSearch(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("q")
	if query == "" {
		writeJSON(w, http.StatusOK, map[string]any{"results": []any{}})
		return
	}

	if s.books == nil {
		fail(w, errorf(http.StatusServiceUnavailable, "book search is unavailable"))
		return
	}

	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	results, err := s.books.Search(r.Context(), query, limit)
	if err != nil {
		slog.Error("book search failed", "query", query, "error", err)
		fail(w, errorf(http.StatusBadGateway, "book search failed, please try again"))
		return
	}

	ids := make([]string, 0, len(results))
	for _, b := range results {
		// Metadata now, covers later — same policy as game search.
		if err := s.store.UpsertBook(r.Context(), b, ""); err != nil {
			slog.Error("cache searched book", "book_id", b.ID, "error", err)
			continue
		}
		ids = append(ids, b.ID)
	}

	books, err := s.store.BooksByIDs(r.Context(), ids)
	if err != nil {
		fail(w, err)
		return
	}
	owned, err := s.ownedBookIDs(r, ids)
	if err != nil {
		fail(w, err)
		return
	}
	userID, err := auth.MustUserID(r.Context())
	if err != nil {
		fail(w, errUnauthorized)
		return
	}
	entryIDs, err := s.store.BookEntryIDs(r.Context(), userID, ids)
	if err != nil {
		fail(w, err)
		return
	}

	// Preserve the provider's relevance ordering, which the map lookup loses.
	out := make([]map[string]any, 0, len(ids))
	for _, id := range ids {
		b, ok := books[id]
		if !ok {
			continue
		}
		out = append(out, map[string]any{
			"book":       b,
			"in_library": owned[id],
			// The attach flow is entry-keyed; hand the entry over when the
			// book is owned so confirming needs no second lookup.
			"entry_id": entryIDs[id],
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"results": out})
}

// handleGetBook serves a cached work, falling back to the provider and caching
// what comes back.
func (s *Server) handleGetBook(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "bookID")
	if !workKeyPattern.MatchString(id) {
		fail(w, errorf(http.StatusBadRequest, "invalid book id"))
		return
	}

	book, err := s.store.GetBook(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		if s.books == nil {
			fail(w, errNotFound)
			return
		}
		fetched, ferr := s.books.GetByWorkKey(r.Context(), id)
		if ferr != nil {
			fail(w, errNotFound)
			return
		}
		if err := s.store.UpsertBook(r.Context(), fetched, ""); err != nil {
			fail(w, err)
			return
		}
		book, err = s.store.GetBook(r.Context(), id)
	}
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, book)
}

// handleBookByISBN resolves an ISBN to its edition and parent work, caching
// both. This is the "add the physical book I'm holding" entry point.
func (s *Server) handleBookByISBN(w http.ResponseWriter, r *http.Request) {
	if s.books == nil {
		fail(w, errorf(http.StatusServiceUnavailable, "book lookup is unavailable"))
		return
	}

	isbn := metadata.NormalizeISBN(chi.URLParam(r, "isbn"))
	if !metadata.ValidISBN(isbn) {
		fail(w, errorf(http.StatusBadRequest, "invalid ISBN"))
		return
	}

	book, err := s.books.GetByISBN(r.Context(), isbn)
	if errors.Is(err, metadata.ErrBookNotFound) {
		fail(w, errorf(http.StatusNotFound, "no book found for ISBN "+isbn))
		return
	}
	if err != nil {
		slog.Error("isbn lookup failed", "isbn", isbn, "error", err)
		fail(w, errorf(http.StatusBadGateway, "ISBN lookup failed, please try again"))
		return
	}

	if err := s.store.UpsertBook(r.Context(), book, ""); err != nil {
		fail(w, err)
		return
	}
	out, err := s.store.GetBook(r.Context(), book.ID)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// handleBookCover serves a locally cached book cover, downloading it on first
// request if the file is missing but the upstream URL is known — the mirror of
// handleCover for works.
func (s *Server) handleBookCover(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "bookID")
	if !workKeyPattern.MatchString(id) {
		http.NotFound(w, r)
		return
	}

	if !s.covers.Has(id) {
		url, err := s.store.CoverURLForBook(r.Context(), id)
		if err != nil || url == "" {
			http.NotFound(w, r)
			return
		}
		accent, err := s.covers.Fetch(r.Context(), id, url)
		if err != nil {
			slog.Warn("fetch book cover, redirecting upstream", "book_id", id, "error", err)
			http.Redirect(w, r, url, http.StatusFound)
			return
		}
		if err := s.store.RecordBookCover(r.Context(), id, s.covers.Path(id), accent); err != nil {
			slog.Warn("record book cover", "book_id", id, "error", err)
		}
	}

	serveCoverFile(w, r, s.covers.Path(id))
}

// handleLegacyCoverRedirect keeps the original single-media cover route
// working for cached clients and bookmarks.
func (s *Server) handleLegacyCoverRedirect(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/api/covers/game/"+chi.URLParam(r, "gameID"), http.StatusMovedPermanently)
}

func (s *Server) ownedBookIDs(r *http.Request, ids []string) (map[string]bool, error) {
	userID, err := auth.MustUserID(r.Context())
	if err != nil {
		return nil, errUnauthorized
	}
	return s.store.OwnedBookIDs(r.Context(), userID, ids)
}
