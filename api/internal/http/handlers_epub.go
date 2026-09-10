package http

import (
	"bytes"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"path"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/collinpendleton/backhog/api/internal/auth"
	"github.com/collinpendleton/backhog/api/internal/books"
	"github.com/collinpendleton/backhog/api/internal/books/epub"
	"github.com/collinpendleton/backhog/api/internal/books/pdf"
	"github.com/collinpendleton/backhog/api/internal/models"
	"github.com/collinpendleton/backhog/api/internal/store"
)

// bookTextChapter is one chapter of the chapters payload, with the
// block-level offsets from the sidecar merged in so the reader can map
// charOffset → (href, blockIndex) and back without another round trip.
//
// A chapter may span several spine documents; SpineIndex names the first,
// which is what the display endpoint is keyed on.
type bookTextChapter struct {
	SpineIndex int    `json:"spine_index"`
	Href       string `json:"href"`
	Title      string `json:"title"`
	// TitleSource is who named this chapter: "toc" (the book's own table of
	// contents), "heading" (a heading in its markup), "text" (inferred from
	// the shape of its opening line) or "none". The reader marks the
	// inferred ones rather than passing a guess off as the book's word.
	TitleSource string `json:"title_source"`
	// Number is the chapter's 1-based position among the chapters that hold
	// text — what an unnamed chapter is called after. SpineIndex cannot
	// serve: a chapter spanning three spine documents leaves two indexes
	// unused, so numbering by it produced the "5, 6, 7, 8, 9, 11" the reader
	// used to show.
	Number    int   `json:"number"`
	CharStart int   `json:"char_start"`
	CharEnd   int   `json:"char_end"`
	Depth     int   `json:"depth"`
	Blocks    []int `json:"blocks"`
	// Images are the document's internal illustrations, anchored to the
	// blocks above. Every href is a path inside the EPUB — the parser
	// dropped the remote ones — and is only fetchable through the asset
	// endpoint below, which re-checks who is asking.
	Images []books.IndexedImage `json:"images"`
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
	case errors.Is(err, pdf.ErrDRM):
		fail(w, errorf(http.StatusUnprocessableEntity,
			"this PDF is DRM-protected (/Encrypt) and cannot be parsed"))
	case errors.Is(err, pdf.ErrImageNative):
		// The quality gate's verdict, not a parse failure: comics, scans
		// and picture books are pages, not prose, and a plausible-wrong
		// text would silently poison every offset downstream. The paged
		// reader for them is stage 2; until then this label is the honest
		// interim answer.
		fail(w, errorf(http.StatusUnprocessableEntity,
			"this PDF has no readable text layer — it is pages, not prose (a comic, scan or picture book); paged reading for those is not here yet"))
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
	number := 0
	for _, ch := range chapters {
		if ch.CharEnd > ch.CharStart {
			number++
		}
		c := bookTextChapter{
			SpineIndex: ch.SpineIndex, Href: ch.Href, Title: ch.Title,
			TitleSource: ch.TitleSource, Number: number,
			CharStart: ch.CharStart, CharEnd: ch.CharEnd, Depth: ch.Depth,
		}
		if index != nil {
			for i := range index.Documents {
				if index.Documents[i].SpineIndex == ch.SpineIndex {
					c.Blocks = index.Documents[i].Blocks
					c.Images = index.Documents[i].Images
					break
				}
			}
		}
		if c.Images == nil {
			c.Images = []books.IndexedImage{}
		}
		out = append(out, c)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"char_count":     et.CharCount,
		"parser_version": et.ParserVersion,
		"chapters":       out,
		// How good the book's own table of contents was. The reader uses it
		// to explain inferred chapter names rather than presenting them as
		// the book's word.
		"toc": map[string]any{
			"source":  et.TOCSource,
			"entries": et.TOCEntries,
			"error":   et.TOCError,
		},
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

// handleBookTextDisplay serves one spine document as prose:
// GET /api/books/{entryID}/text/display?spine=N.
//
// The canonical text is folded for matching — lowercased, punctuation
// dropped — which makes it an address space, not something anyone would want
// to read. This returns the same blocks in the same order with the book's own
// characters, so blocks[i] here is the text that starts at the chapters
// payload's blocks[i] offset. That correspondence is the whole contract: it
// is what lets the reader render prose and still report a canonical offset.
func (s *Server) handleBookTextDisplay(w http.ResponseWriter, r *http.Request) {
	et, ok := s.ensureBookText(w, r)
	if !ok {
		return
	}

	spine, err := strconv.Atoi(r.URL.Query().Get("spine"))
	if err != nil || spine < 0 {
		fail(w, errorf(http.StatusBadRequest, "spine must be a non-negative spine index"))
		return
	}

	index, err := s.epubs.LoadIndex(r.Context(), et)
	if err != nil {
		slog.ErrorContext(r.Context(), "load block index", "error", err)
		fail(w, errorf(http.StatusInternalServerError, "could not read this ebook's index"))
		return
	}
	var doc *books.IndexedDoc
	for i := range index.Documents {
		if index.Documents[i].SpineIndex == spine {
			doc = &index.Documents[i]
			break
		}
	}
	if doc == nil {
		fail(w, errNotFound)
		return
	}

	blocks, err := s.epubs.ReadDisplayBlocks(r.Context(), et, *doc)
	if err != nil {
		slog.ErrorContext(r.Context(), "read display text", "error", err)
		fail(w, errorf(http.StatusInternalServerError, "could not read this ebook's text"))
		return
	}
	if blocks == nil {
		blocks = []string{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"spine_index": spine,
		"href":        doc.Href,
		"blocks":      blocks,
	})
}

// handleBookTextAsset serves one image out of the EPUB behind a book entry:
// GET /api/books/{entryID}/text/asset?href=OEBPS/Images/plate1.png.
//
// It exists so the reader can show a book's illustrations without the page
// ever loading from a third party (design invariant 5). The hrefs it accepts
// are the ones the parser put in the block index, which are internal by
// construction; everything else — a remote URL, a path climbing out of the
// zip, a stylesheet, another user's book — answers 404 and looks identical
// doing it.
func (s *Server) handleBookTextAsset(w http.ResponseWriter, r *http.Request) {
	if s.epubs == nil {
		fail(w, errorf(http.StatusServiceUnavailable, "canonical text storage unavailable"))
		return
	}
	userID, err := auth.MustUserID(r.Context())
	if err != nil {
		fail(w, errUnauthorized)
		return
	}

	asset, err := s.epubs.OpenAsset(r.Context(), userID,
		chi.URLParam(r, "entryID"), r.URL.Query().Get("href"))
	switch {
	case errors.Is(err, store.ErrNotFound), errors.Is(err, books.ErrNoEpub),
		errors.Is(err, books.ErrNotAsset):
		fail(w, errNotFound)
		return
	case errors.Is(err, epub.ErrDRM):
		fail(w, errorf(http.StatusUnprocessableEntity,
			"this EPUB is DRM-protected and cannot be parsed"))
		return
	case err != nil:
		slog.ErrorContext(r.Context(), "epub asset read failed", "error", err)
		fail(w, errorf(http.StatusInternalServerError, "could not read this ebook's images"))
		return
	}

	// Bytes inside a book do not change: a rewritten EPUB is a new mtime on
	// the file and a fresh parse, so the same reasoning as the audio stream
	// applies — cache hard, keep it private because it is user-scoped.
	w.Header().Set("Content-Type", asset.ContentType)
	w.Header().Set("Cache-Control", audioCacheControl)
	w.Header().Set("ETag", audioETag(int64(len(asset.Data)), asset.ModTime))
	// The bytes are untrusted book content. nosniff keeps a mislabelled
	// file from being re-typed into something executable, and the CSP is
	// belt and braces for the case where someone navigates to this URL
	// directly rather than loading it in an <img>.
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; sandbox")

	http.ServeContent(w, r, path.Base(asset.Href), asset.ModTime, bytes.NewReader(asset.Data))
}
