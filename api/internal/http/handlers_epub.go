package http

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/collinpendleton/backhog/api/internal/auth"
	"github.com/collinpendleton/backhog/api/internal/books"
	"github.com/collinpendleton/backhog/api/internal/books/epub"
	"github.com/collinpendleton/backhog/api/internal/models"
	"github.com/collinpendleton/backhog/api/internal/store"
)

// bookTextChapter is one spine document of the chapters payload, with the
// block-level offsets from the sidecar merged in so the reader can map
// charOffset → (href, blockIndex) and back without another round trip.
type bookTextChapter struct {
	SpineIndex int    `json:"spine_index"`
	Href       string `json:"href"`
	Title      string `json:"title"`
	CharStart  int    `json:"char_start"`
	CharEnd    int    `json:"char_end"`
	Depth      int    `json:"depth"`
	Blocks     []int  `json:"blocks"`
}

// ensureBookText runs the parse-on-demand path shared by both text
// endpoints: resolve the entry to its attached EPUB (ownership-scoped),
// parse it if no current canonical text exists, and return the row. On
// failure it has already written the error response.
func (s *Server) ensureBookText(w http.ResponseWriter, r *http.Request) (models.EpubText, bool) {
	if s.epubs == nil {
		fail(w, errorf(http.StatusServiceUnavailable, "canonical text storage unavailable"))
		return models.EpubText{}, false
	}
	userID, err := auth.MustUserID(r.Context())
	if err != nil {
		fail(w, errUnauthorized)
		return models.EpubText{}, false
	}

	et, err := s.epubs.EnsureForEntry(r.Context(), userID, chi.URLParam(r, "entryID"))
	switch {
	case errors.Is(err, store.ErrNotFound):
		fail(w, errNotFound)
	case errors.Is(err, books.ErrNoEpub):
		fail(w, errorf(http.StatusNotFound, "no ebook is attached to this book"))
	case errors.Is(err, epub.ErrDRM):
		fail(w, errorf(http.StatusUnprocessableEntity,
			"this EPUB is DRM-protected and cannot be parsed"))
	case err != nil:
		slog.ErrorContext(r.Context(), "epub parse failed", "error", err)
		fail(w, errorf(http.StatusInternalServerError, "could not parse this ebook"))
	default:
		return et, true
	}
	return models.EpubText{}, false
}

// handleBookTextChapters serves the spine index of a book's canonical text,
// parsing the attached EPUB on demand.
func (s *Server) handleBookTextChapters(w http.ResponseWriter, r *http.Request) {
	et, ok := s.ensureBookText(w, r)
	if !ok {
		return
	}

	chapters, err := s.store.ListEpubChapters(r.Context(), et.ID)
	if err != nil {
		fail(w, err)
		return
	}

	var index *books.BlockIndex
	if idx, err := s.epubs.LoadIndex(r.Context(), et); err == nil {
		index = idx
	} else {
		slog.ErrorContext(r.Context(), "load block index", "error", err)
	}

	out := make([]bookTextChapter, 0, len(chapters))
	for _, ch := range chapters {
		c := bookTextChapter{
			SpineIndex: ch.SpineIndex, Href: ch.Href, Title: ch.Title,
			CharStart: ch.CharStart, CharEnd: ch.CharEnd, Depth: ch.Depth,
		}
		if index != nil {
			for i := range index.Documents {
				if index.Documents[i].SpineIndex == ch.SpineIndex {
					c.Blocks = index.Documents[i].Blocks
					break
				}
			}
		}
		out = append(out, c)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"char_count":     et.CharCount,
		"parser_version": et.ParserVersion,
		"chapters":       out,
	})
}

// handleBookText serves a ranged slice of the canonical text, in byte
// offsets: GET /api/books/{entryID}/text?from=&to=. Omitted bounds read the
// whole text; out-of-range or inverted ranges are rejected.
func (s *Server) handleBookText(w http.ResponseWriter, r *http.Request) {
	et, ok := s.ensureBookText(w, r)
	if !ok {
		return
	}

	q := r.URL.Query()
	from, to := 0, et.CharCount
	var err error
	if v := q.Get("from"); v != "" {
		if from, err = strconv.Atoi(v); err != nil || from < 0 {
			fail(w, errorf(http.StatusBadRequest, "from must be a non-negative byte offset"))
			return
		}
	}
	if v := q.Get("to"); v != "" {
		if to, err = strconv.Atoi(v); err != nil || to < 0 {
			fail(w, errorf(http.StatusBadRequest, "to must be a non-negative byte offset"))
			return
		}
	}
	if from > to || to > et.CharCount {
		fail(w, errorf(http.StatusBadRequest, fmt.Sprintf(
			"range [%d,%d) is outside the text of %d bytes", from, to, et.CharCount)))
		return
	}

	text, err := s.epubs.ReadText(r.Context(), et, from, to)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"from":       from,
		"to":         to,
		"char_count": et.CharCount,
		"text":       text,
	})
}
