package http

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/collinpendleton/backhog/api/internal/metadata"
	"github.com/collinpendleton/backhog/api/internal/store"
)

// bookCoversResponse is the cover picker's payload: every cover Open Library
// has for the work, and which of them the work wears now.
type bookCoversResponse struct {
	CurrentID int64                  `json:"current_id"`
	Covers    []metadata.CoverOption `json:"covers"`
}

// handleBookCovers lists the art a work could wear. The work must already be
// cached — the picker lives on a book's own page — and the list comes live
// from the provider, since a cover someone uploaded to Open Library yesterday
// is exactly the one they came here to pick.
func (s *Server) handleBookCovers(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "bookID")
	if !workKeyPattern.MatchString(id) {
		fail(w, errorf(http.StatusBadRequest, "invalid book id"))
		return
	}
	current, err := s.store.CoverURLForBook(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		fail(w, errNotFound)
		return
	}
	if err != nil {
		fail(w, err)
		return
	}

	lister, ok := s.books.(metadata.CoverLister)
	if s.books == nil || !ok {
		fail(w, errorf(http.StatusServiceUnavailable, "cover lookup is unavailable"))
		return
	}
	covers, err := lister.GetCovers(r.Context(), id)
	if err != nil && !errors.Is(err, metadata.ErrBookNotFound) {
		slog.Error("list book covers", "book_id", id, "error", err)
		fail(w, errorf(http.StatusBadGateway, "could not reach Open Library, please try again"))
		return
	}
	if covers == nil {
		covers = []metadata.CoverOption{}
	}
	writeJSON(w, http.StatusOK, bookCoversResponse{
		CurrentID: metadata.OpenLibraryCoverID(current),
		Covers:    covers,
	})
}

// handleSetBookCover pins one Open Library cover on a work. The client names
// a cover id, never a URL (see CoverLister.CoverURL). The new art is
// downloaded before anything is recorded, so a dead id leaves the old cover.
func (s *Server) handleSetBookCover(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "bookID")
	if !workKeyPattern.MatchString(id) {
		fail(w, errorf(http.StatusBadRequest, "invalid book id"))
		return
	}
	var body struct {
		CoverID int64 `json:"cover_id"`
	}
	if err := decode(r, &body); err != nil || body.CoverID <= 0 {
		fail(w, errorf(http.StatusBadRequest, "cover_id is required"))
		return
	}
	lister, ok := s.books.(metadata.CoverLister)
	if s.books == nil || !ok {
		fail(w, errorf(http.StatusServiceUnavailable, "cover lookup is unavailable"))
		return
	}
	if _, err := s.store.CoverURLForBook(r.Context(), id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			err = errNotFound
		}
		fail(w, err)
		return
	}

	url := lister.CoverURL(body.CoverID)
	accent, err := s.covers.Replace(r.Context(), id, url)
	if err != nil {
		slog.Warn("download chosen book cover", "book_id", id, "cover_id", body.CoverID, "error", err)
		fail(w, errorf(http.StatusBadGateway, "could not download that cover, please try another"))
		return
	}
	if err := s.store.SetBookCover(r.Context(), id, url, s.covers.Path(id), accent); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			err = errNotFound
		}
		fail(w, err)
		return
	}
	book, err := s.store.GetBook(r.Context(), id)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, book)
}
