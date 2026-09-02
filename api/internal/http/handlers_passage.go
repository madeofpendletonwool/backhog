package http

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/collinpendleton/backhog/api/internal/books/passage"
	"github.com/collinpendleton/backhog/api/internal/models"
	"github.com/collinpendleton/backhog/api/internal/store"
)

// The physical-copy half of the Books arena's paper bridge. A scan of a
// printed page arrives as text; the passage matcher places it in the
// canonical text; the offset is then recorded as a page anchor on the
// copy of the printing the user holds. From there the position endpoints
// treat printed pages like any other derived view of the one stored
// character offset.

// passageContextBytes is how much canonical text surrounds the matched
// span in a passage response — enough for the scan UI to show the reader
// where the match landed without another round trip.
const passageContextBytes = 160

// passageMatchView is one placed passage: where it sits in the canonical
// text, and how much the match was believed.
type passageMatchView struct {
	CharOffset int     `json:"char_offset"`
	CharEnd    int     `json:"char_end"`
	Confidence float64 `json:"confidence"`
}

func passageMatchViews(matches []passage.Match) []passageMatchView {
	out := make([]passageMatchView, 0, len(matches))
	for _, m := range matches {
		out = append(out, passageMatchView(m))
	}
	return out
}

// handleBookPassage places a passage in a book's canonical text: the
// "where in the ebook is what I'm reading on paper" question. The text
// is parsed on demand like every text endpoint, and the answer carries
// the surrounding canonical text so a client can confirm the match
// before pinning an anchor to it. A passage that recurs comes back with
// alternatives — the client asks which one, the server does not guess.
func (s *Server) handleBookPassage(w http.ResponseWriter, r *http.Request) {
	if _, _, _, ok := s.bookEntry(w, r); !ok {
		return
	}
	if s.epubs == nil || s.passage == nil {
		fail(w, errorf(http.StatusServiceUnavailable, "canonical text storage unavailable"))
		return
	}
	et, ok := s.ensureBookText(w, r)
	if !ok {
		return
	}

	var body struct {
		Text string `json:"text"`
	}
	if err := decode(r, &body); err != nil {
		fail(w, err)
		return
	}

	res, err := s.passage.Find(r.Context(), et.ID, body.Text)
	switch {
	case errors.Is(err, passage.ErrTooShort):
		fail(w, errorf(http.StatusUnprocessableEntity,
			"that passage is too short to place reliably; read a few more lines of the page"))
		return
	case errors.Is(err, passage.ErrNoMatch):
		fail(w, errorf(http.StatusUnprocessableEntity,
			"no place in this book's text matches that passage"))
		return
	case err != nil:
		fail(w, err)
		return
	}

	// Context comes from the canonical text itself: it is the address
	// space the offset lives in, so before and after splice onto the
	// matched span without translation.
	from := max(0, res.Match.CharOffset-passageContextBytes)
	to := min(et.CharCount, res.Match.CharEnd+passageContextBytes)
	span, err := s.epubs.ReadText(r.Context(), et, from, to)
	if err != nil {
		fail(w, err)
		return
	}
	start, end := res.Match.CharOffset-from, res.Match.CharEnd-from
	writeJSON(w, http.StatusOK, map[string]any{
		"match":        passageMatchView(res.Match),
		"alternatives": passageMatchViews(res.Alternatives),
		"ambiguous":    len(res.Alternatives) > 0,
		"context": map[string]any{
			"before":  span[:start],
			"passage": span[start:end],
			"after":   span[end:],
		},
	})
}

// physicalCopyRequest registers a printing of a book the user holds.
type physicalCopyRequest struct {
	EditionID string `json:"edition_id"`
	Notes     string `json:"notes"`
}

// handleCreateBookCopy registers one printing against one of the
// caller's book entries. The edition must be a printing of the entry's
// work — page numbers of some other book are a client bug worth saying
// out loud, not a 404.
func (s *Server) handleCreateBookCopy(w http.ResponseWriter, r *http.Request) {
	userID, entryID, _, ok := s.bookEntry(w, r)
	if !ok {
		return
	}

	var body physicalCopyRequest
	if err := decode(r, &body); err != nil {
		fail(w, err)
		return
	}
	c, err := s.store.CreatePhysicalCopy(r.Context(), userID, entryID, body.EditionID, body.Notes)
	switch {
	case errors.Is(err, store.ErrNotFound):
		fail(w, errNotFound)
		return
	case errors.Is(err, store.ErrConflict):
		fail(w, errorf(http.StatusConflict, "this printing is already registered for this book"))
		return
	case err != nil:
		fail(w, errorf(http.StatusBadRequest, err.Error()))
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"copy": c})
}

// handleListBookCopies lists the caller's printings for one entry, each
// with a count of its recorded page anchors.
func (s *Server) handleListBookCopies(w http.ResponseWriter, r *http.Request) {
	userID, entryID, _, ok := s.bookEntry(w, r)
	if !ok {
		return
	}
	copies, err := s.store.PhysicalCopies(r.Context(), userID, entryID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			fail(w, errNotFound)
			return
		}
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"copies": copies})
}

// handleUpdateBookCopy rewrites a copy's notes. Notes are the only
// editable field: the edition is the copy's identity, and anchors hang
// off it.
func (s *Server) handleUpdateBookCopy(w http.ResponseWriter, r *http.Request) {
	userID, entryID, _, ok := s.bookEntry(w, r)
	if !ok {
		return
	}
	copyID := chi.URLParam(r, "copyID")

	var body struct {
		Notes *string `json:"notes"`
	}
	if err := decode(r, &body); err != nil {
		fail(w, err)
		return
	}
	if body.Notes == nil {
		fail(w, errorf(http.StatusBadRequest, "notes is required"))
		return
	}
	c, err := s.store.UpdatePhysicalCopyNotes(r.Context(), userID, entryID, copyID, *body.Notes)
	switch {
	case errors.Is(err, store.ErrNotFound):
		fail(w, errNotFound)
		return
	case err != nil:
		fail(w, errorf(http.StatusBadRequest, err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"copy": c})
}

// handleDeleteBookCopy drops a printing and its whole page map; the
// copies can be re-scanned back into existence at any time.
func (s *Server) handleDeleteBookCopy(w http.ResponseWriter, r *http.Request) {
	userID, entryID, _, ok := s.bookEntry(w, r)
	if !ok {
		return
	}
	err := s.store.DeletePhysicalCopy(r.Context(), userID, entryID, chi.URLParam(r, "copyID"))
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			fail(w, errNotFound)
			return
		}
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// pageAnchorRequest pins one printed page to a canonical offset. The
// scan flow sends the matcher's confidence with source 'ocr'; a reader
// typing a page number by hand sends 'manual', whose confidence is
// exact by declaration.
type pageAnchorRequest struct {
	PrintedPage int      `json:"printed_page"`
	CharOffset  int      `json:"char_offset"`
	Source      string   `json:"source"`
	Confidence  *float64 `json:"confidence"`
}

// handleSaveBookPageAnchor records one page anchor, last-write-wins: a
// re-scan of the same page corrects it rather than conflicting with it.
// The offset is validated against the canonical text — an anchor into
// nowhere would silently corrupt every interpolation over it — so the
// text is parsed on demand here too.
func (s *Server) handleSaveBookPageAnchor(w http.ResponseWriter, r *http.Request) {
	userID, entryID, _, ok := s.bookEntry(w, r)
	if !ok {
		return
	}
	copyID := chi.URLParam(r, "copyID")

	et, ok := s.ensureBookText(w, r)
	if !ok {
		return
	}

	var body pageAnchorRequest
	if err := decode(r, &body); err != nil {
		fail(w, err)
		return
	}
	if body.CharOffset > et.CharCount {
		fail(w, errorf(http.StatusBadRequest, "char_offset is outside this book's text"))
		return
	}
	confidence := 1.0
	if body.Confidence != nil {
		confidence = *body.Confidence
	}

	anchor, err := s.store.SavePageAnchor(r.Context(), userID, entryID, copyID, models.PageAnchor{
		PrintedPage: body.PrintedPage,
		CharOffset:  body.CharOffset,
		Source:      body.Source,
		Confidence:  confidence,
	})
	switch {
	case errors.Is(err, store.ErrNotFound):
		fail(w, errNotFound)
		return
	case err != nil:
		fail(w, errorf(http.StatusBadRequest, err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"anchor": anchor})
}

// handleListBookCopyPages lists one copy's page map by page number.
func (s *Server) handleListBookCopyPages(w http.ResponseWriter, r *http.Request) {
	userID, entryID, _, ok := s.bookEntry(w, r)
	if !ok {
		return
	}
	anchors, err := s.store.PageAnchors(r.Context(), userID, entryID, chi.URLParam(r, "copyID"))
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			fail(w, errNotFound)
			return
		}
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"anchors": anchors})
}
