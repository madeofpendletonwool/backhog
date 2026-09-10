package http

import (
	"bytes"
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/collinpendleton/backhog/api/internal/auth"
	"github.com/collinpendleton/backhog/api/internal/books"
	"github.com/collinpendleton/backhog/api/internal/books/pdf"
	"github.com/collinpendleton/backhog/api/internal/store"
)

// The paged reader's endpoints: the manifest of an image-native PDF's
// pages, and one page's raster. The scrolled reader's text endpoints are
// the mirror image — each population's endpoints name the other's books as
// refusals, and an entry is dispatched by its classification, never its
// extension: a text-native PDF (even one full of full-page art) reads as
// prose, and a comic never enters a text-mode path.

// handleBookPages serves the page manifest of a paged book:
// GET /api/books/{entryID}/pages.
//
// It is the paged reader's chapters call: one fetch lays out the whole
// book — every page's image shape is a stub lookup, no page decoded — so
// "page N of M" and every placeholder are honest before the first image
// byte is paid for.
func (s *Server) handleBookPages(w http.ResponseWriter, r *http.Request) {
	if !s.pagesReady(w, r) {
		return
	}
	userID, err := auth.MustUserID(r.Context())
	if err != nil {
		fail(w, errUnauthorized)
		return
	}
	manifest, err := s.epubs.Pages(r.Context(), userID, chi.URLParam(r, "entryID"))
	if !failPages(w, r, err) {
		return
	}
	writeJSON(w, http.StatusOK, manifest)
}

// handleBookPageImage serves one page's raster:
// GET /api/books/{entryID}/pages/{page}.
//
// The bytes are extracted lazily on first request and served from the
// companion cache ever after; the response carries the same headers as the
// EPUB asset endpoint (authenticated per request, ETagged, cached hard and
// private, sandboxed) because it is the same job: a book's picture, from
// our own origin, for a reader who has proven they may read it.
func (s *Server) handleBookPageImage(w http.ResponseWriter, r *http.Request) {
	if !s.pagesReady(w, r) {
		return
	}
	userID, err := auth.MustUserID(r.Context())
	if err != nil {
		fail(w, errUnauthorized)
		return
	}
	page, err := strconv.Atoi(chi.URLParam(r, "page"))
	if err != nil || page < 0 {
		fail(w, errorf(http.StatusBadRequest, "page must be a non-negative page index"))
		return
	}
	asset, err := s.epubs.PageImage(r.Context(), userID, chi.URLParam(r, "entryID"), page)
	if !failPages(w, r, err) {
		return
	}

	w.Header().Set("Content-Type", asset.ContentType)
	w.Header().Set("Cache-Control", audioCacheControl)
	w.Header().Set("ETag", audioETag(int64(len(asset.Data)), asset.ModTime))
	// The bytes are untrusted book content (nosniff + CSP, the asset
	// endpoint's reasoning verbatim: never worth an executable re-type).
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; sandbox")

	http.ServeContent(w, r, "", asset.ModTime, bytes.NewReader(asset.Data))
}

// pagesReady answers the two preconditions both handlers share.
func (s *Server) pagesReady(w http.ResponseWriter, r *http.Request) bool {
	if s.epubs == nil {
		fail(w, errorf(http.StatusServiceUnavailable, "canonical text storage unavailable"))
		return false
	}
	return true
}

// failPages maps the page machinery's errors onto their HTTP names. Every
// refusal says what the reader is looking at, the honesty `.kfx` set the
// bar for: an epub is not pages, a text-native PDF is prose, vector art
// has no page images, DRM is DRM, and somebody else's book is nobody's.
func failPages(w http.ResponseWriter, r *http.Request, err error) bool {
	var notPaged *books.NotPagedError
	switch {
	case err == nil:
		return true
	case errors.Is(err, store.ErrNotFound):
		fail(w, errNotFound)
	case errors.Is(err, books.ErrNoEpub):
		fail(w, errorf(http.StatusNotFound, "no ebook is attached to this book"))
	case errors.As(err, &notPaged):
		fail(w, errorf(http.StatusUnprocessableEntity, notPaged.Reason))
	case errors.Is(err, books.ErrPageOutOfRange), errors.Is(err, pdf.ErrPageOutOfRange):
		fail(w, errorf(http.StatusBadRequest, "page is outside this book's pages"))
	case errors.Is(err, pdf.ErrNoPageImage):
		fail(w, errorf(http.StatusUnprocessableEntity,
			"this page carries no image — it is vector art or blank, which the reader cannot serve"))
	case errors.Is(err, pdf.ErrUnsupportedPageImage):
		fail(w, errorf(http.StatusUnprocessableEntity,
			"this page's image uses a codec the reader cannot serve (JPX, JBIG2 or TIFF)"))
	case errors.Is(err, pdf.ErrDRM):
		fail(w, errorf(http.StatusUnprocessableEntity,
			"this PDF is DRM-protected (/Encrypt) and cannot be read"))
	case errors.Is(err, pdf.ErrCorrupt):
		fail(w, errorf(http.StatusUnprocessableEntity, "this PDF could not be read structurally"))
	default:
		slog.ErrorContext(r.Context(), "pages endpoint failed", "error", err)
		fail(w, errorf(http.StatusInternalServerError, "could not read this book's pages"))
	}
	return false
}
