package http

import (
	"context"
	"net/http"
	"strconv"

	"github.com/collinpendleton/backhog/api/internal/auth"
	"github.com/collinpendleton/backhog/api/internal/models"
)

// The spoiler-safety seam of the Books arena: a client cannot get text past
// the reader's position unless it explicitly asks to.
//
// Every read path — text slices, the chapter index, search, passage, export —
// resolves one `until` parameter through the single helper here and clamps
// itself to the offset that comes back. The clamp is a property of the
// *request*, not of any model: token-authenticated requests (the external
// clients MAD-465 exists for) default to `until=position`, because an
// assistant holding a user's key has no business reading ahead of them, while
// cookie requests from the app itself default to `until=none`, because the
// in-app reader is the thing that *moves* the position and must keep working
// exactly as it did. Spoilers are always available to whoever asks for them
// out loud, and every response echoes the bound it was served under so a
// client can say "as of chapter 7, 62%" instead of implying it saw the book.

// The named values `until` accepts. A bare non-negative integer is the third
// shape: an explicit offset to stop at, for a client that wants a window
// narrower than its position.
const (
	untilNone     = "none"
	untilPosition = "position"
	untilOffset   = "offset"
)

// boundView is the effective clamp a response echoes: where it stopped, in
// the chapter it stopped in, as a percentage of the text. Chapter is null
// when the bound is the whole book (nothing was cut) or the book's spine is
// not loaded on the path that answered.
type boundView struct {
	Until      string       `json:"until"`
	CharOffset int          `json:"char_offset"`
	Chapter    *chapterView `json:"chapter"`
	Percent    float64      `json:"percent"`
}

// readBound is one request's resolved clamp. Offset is the last readable
// character position — everything at or after it is withheld — and equals
// charCount when nothing is (until=none).
type readBound struct {
	until  string
	offset int
	view   boundView
}

// parseUntil reads the `until` parameter with its auth-aware default: a
// personal API token defaults to the caller's position, a cookie session to
// the whole book. The default is the whole spoiler-safety feature in one
// line — everything else here is just honoring what was asked for.
func parseUntil(r *http.Request) (mode string, offset int, ok bool) {
	return parseUntilDefault(r, "")
}

// parseUntilDefault is parseUntil with a forced default for the surfaces
// that never move a position and so clamp every caller — the library
// search, the name index. An explicit `until` always wins; only the
// omitted-parameter default changes.
func parseUntilDefault(r *http.Request, def string) (mode string, offset int, ok bool) {
	v := r.URL.Query().Get("until")
	if v == "" {
		v = def
	}
	switch v {
	case "":
		if auth.UsingAPIToken(r.Context()) {
			return untilPosition, 0, true
		}
		return untilNone, 0, true
	case untilNone:
		return untilNone, 0, true
	case untilPosition:
		return untilPosition, 0, true
	default:
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			return "", 0, false
		}
		return untilOffset, n, true
	}
}

// resolveReadBound parses and resolves the clamp for one request. charCount
// is the canonical text's length the caller already holds. The bool reports
// whether the handler may continue; the error response is written here.
//
// The offset bound resolves through the unified position model — the stored
// character offset, which is what an EPUB read writes directly and what an
// aligned audio write was translated into at the moment it was stored — so
// audio and EPUB positions clamp identically without a second translation.
// A book whose position lives on the page axis (a paged primary) has no text
// position to offer, and the honest bound for its text is nothing.
func (s *Server) resolveReadBound(w http.ResponseWriter, r *http.Request,
	userID, entryID, bookID string, charCount int) (readBound, bool) {
	return s.resolveReadBoundDefault(w, r, userID, entryID, bookID, charCount, "")
}

// resolveReadBoundDefault is resolveReadBound for the surfaces whose
// omitted-`until` default is the position for every caller (see
// parseUntilDefault). The named modes, the explicit offset and every error
// shape are identical.
func (s *Server) resolveReadBoundDefault(w http.ResponseWriter, r *http.Request,
	userID, entryID, bookID string, charCount int, def string) (readBound, bool) {

	mode, offset, ok := parseUntilDefault(r, def)
	if !ok {
		fail(w, errorf(http.StatusBadRequest,
			"until must be \"position\", \"none\" or a non-negative character offset"))
		return readBound{}, false
	}

	if mode == untilNone {
		return readBound{until: untilNone, offset: charCount, view: boundView{
			Until: untilNone, CharOffset: charCount, Percent: 100,
		}}, true
	}

	ctx := r.Context()
	views, err := s.loadBookViews(ctx, userID, entryID, bookID, true)
	if err != nil {
		fail(w, err)
		return readBound{}, false
	}

	if mode == untilPosition {
		progress, err := s.store.BookProgress(ctx, userID, entryID)
		if err != nil {
			fail(w, err)
			return readBound{}, false
		}
		offset = progress.CharOffset
		if progress.PositionMode == models.PositionModePage {
			// A page-axis position says nothing about the text; clamping
			// to zero is the answer that cannot spoil.
			offset = 0
		}
		// A finished book is fully read: nothing left in it can spoil its
		// own reader, whatever spot the last session happened to stop on.
		// The series memory (MAD-469) counts on this — a "story so far"
		// spans every finished book whole, with no per-call opt-in.
		if s.entryFinished(ctx, userID, entryID) {
			offset = charCount
		}
	}
	if offset > charCount {
		if mode == untilOffset {
			fail(w, errorf(http.StatusBadRequest,
				"until is outside this book's text of "+strconv.Itoa(charCount)+" characters"))
			return readBound{}, false
		}
		offset = charCount
	}

	return readBound{until: mode, offset: offset, view: boundView{
		Until:      mode,
		CharOffset: offset,
		Chapter:    chapterAt(views.chapters, offset),
		Percent:    percentAtOffset(offset, views),
	}}, true
}

// entryFinished reports whether the caller's entry is marked played: the
// reader has been through the whole book, so the position clamp resolves
// to everything. A lookup that fails leaves the stored position standing —
// the safe direction is always less text, and ownership was already proven
// by the progress read that precedes every caller of this.
func (s *Server) entryFinished(ctx context.Context, userID, entryID string) bool {
	entry, err := s.store.GetEntry(ctx, userID, entryID)
	return err == nil && entry.Status == models.StatusPlayed
}

// deepLink is the provenance anchor every hit and span carries: a jump into
// the reader that lands on the offset without ever moving the saved
// position, because it arrives flagged as a peek (MAD-441).
func deepLink(entryID string, offset int) string {
	return "/books/" + entryID + "/read?offset=" + strconv.Itoa(offset) + "&peek=1"
}

// provenanceView is the citation a hit or span answers with: which book, which
// chapter, which characters, and the link that jumps a reader straight there.
// Anything an answer is grounded in, the reader can check.
type provenanceView struct {
	BookID    string       `json:"book_id"`
	Chapter   *chapterView `json:"chapter"`
	CharStart int          `json:"char_start"`
	CharEnd   int          `json:"char_end"`
	DeepLink  string       `json:"deep_link"`
}

// spanProvenance cites a canonical range: the chapter its start sits in (a
// range may cross into later ones; the anchor is where it begins) and the
// peek link to it.
func spanProvenance(entryID string, chapters []models.EpubChapter, start, end int) provenanceView {
	return provenanceView{
		BookID:    entryID,
		Chapter:   chapterAt(chapters, start),
		CharStart: start,
		CharEnd:   end,
		DeepLink:  deepLink(entryID, start),
	}
}

// untilQuery echoes a resolved bound as the query string a client would send
// to ask for exactly it again.
func untilQuery(b readBound) string {
	if b.until == untilOffset {
		return strconv.Itoa(b.offset)
	}
	return b.until
}

// resolvePageBound is the paged twin of resolveReadBound for the books whose
// axis is a page index (an image-native PDF primary served through the OCR
// lettering search). The same `until` parameter, the same auth-aware default,
// a different axis: the clamp is the stored page position.
func (s *Server) resolvePageBound(w http.ResponseWriter, r *http.Request,
	userID, entryID string, pageCount int) (readBound, bool) {

	mode, page, ok := parseUntil(r)
	if !ok {
		fail(w, errorf(http.StatusBadRequest,
			"until must be \"position\", \"none\" or a non-negative page index"))
		return readBound{}, false
	}

	if mode == untilNone {
		return readBound{until: untilNone, offset: pageCount, view: boundView{
			Until: untilNone, CharOffset: pageCount, Percent: 100,
		}}, true
	}

	ctx := r.Context()
	if mode == untilPosition {
		progress, err := s.store.BookProgress(ctx, userID, entryID)
		if err != nil {
			fail(w, err)
			return readBound{}, false
		}
		page = 0
		if progress.PageIndex != nil {
			page = *progress.PageIndex
		}
		// The finished-book rule, on the page axis: a reader who has been
		// through the whole book has nothing left to be shown early.
		if s.entryFinished(ctx, userID, entryID) {
			page = pageCount
		}
	}
	if page >= pageCount {
		if mode == untilOffset {
			fail(w, errorf(http.StatusBadRequest,
				"until is outside this book's "+strconv.Itoa(pageCount)+" pages"))
			return readBound{}, false
		}
		page = pageCount - 1
	}
	if page < 0 {
		page = 0
	}

	percent := 0.0
	if pageCount > 1 {
		percent = float64(page) / float64(pageCount-1) * 100
	}
	return readBound{until: mode, offset: page, view: boundView{
		Until: mode, CharOffset: page, Percent: percent,
	}}, true
}
