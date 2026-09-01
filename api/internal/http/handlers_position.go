package http

import (
	"context"
	"errors"
	"log/slog"
	"math"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/collinpendleton/backhog/api/internal/auth"
	bookaudio "github.com/collinpendleton/backhog/api/internal/books/audio"
	"github.com/collinpendleton/backhog/api/internal/books/position"
	"github.com/collinpendleton/backhog/api/internal/models"
	"github.com/collinpendleton/backhog/api/internal/store"
)

// positionResponse is one position seen from all three angles. char_offset is
// the stored truth; audio and page are computed from it on the way out, which
// is why they carry their own derived/confidence rather than being presented
// as facts.
//
// audio is null when the book has no audiobook attached, and page is null
// until a page map exists for the printing. derived at the top level reports
// whether every view in this payload was computed from char_offset through a
// map — false means at least one of them is a separately stored raw value
// that may have drifted, and confidence is then 0 because nothing was
// interpolated.
type positionResponse struct {
	CharOffset int          `json:"char_offset"`
	Source     string       `json:"source"`
	Percent    float64      `json:"percent"`
	CharCount  int          `json:"char_count"`
	Chapter    *chapterView `json:"chapter"`
	Audio      *audioView   `json:"audio"`
	Page       *pageView    `json:"page"`
	Derived    bool         `json:"derived"`
	Confidence float64      `json:"confidence"`
	UpdatedAt  *time.Time   `json:"updated_at"`
}

// chapterView locates a position in the spine, so a client can say "Chapter
// 12" without fetching the whole chapter index.
type chapterView struct {
	SpineIndex int    `json:"spine_index"`
	Title      string `json:"title"`
	Href       string `json:"href"`
	CharStart  int    `json:"char_start"`
	CharEnd    int    `json:"char_end"`
}

// audioView is where the player should start. Seconds is global across the
// whole book; the track fields are the same point expressed as an offset
// inside one file, which is what a media element actually needs.
type audioView struct {
	Seconds       float64 `json:"seconds"`
	TrackID       int64   `json:"track_id"`
	TrackNumber   int     `json:"track_number"`
	TrackSeconds  float64 `json:"track_seconds"`
	TotalDuration float64 `json:"total_duration"`
	// Derived false means this came from the raw stored timestamp because
	// the book has no alignment yet, not from char_offset.
	Derived    bool    `json:"derived"`
	Confidence float64 `json:"confidence"`
}

// pageView is the printed page of the entry's edition. It only ever appears
// derived: there is no raw page fallback, because a page number nobody can
// map back into the text is not a position.
type pageView struct {
	Page       int     `json:"page"`
	Derived    bool    `json:"derived"`
	Confidence float64 `json:"confidence"`
}

// positionRequest carries exactly one of the three ways to say where you are.
// The server translates whatever it is given into a character offset when it
// can, and stores the raw value when it cannot.
type positionRequest struct {
	CharOffset   *int     `json:"char_offset"`
	AudioSeconds *float64 `json:"audio_seconds"`
	AudioFileID  *int64   `json:"audio_file_id"`
	Page         *int     `json:"page"`
	// Source overrides what produced the offset. It defaults per shape:
	// 'read' for a char offset, 'listen' for audio, 'manual' for a page.
	Source string `json:"source"`
}

// readingSessionRequest logs a stretch of reading or listening. Seconds may
// be omitted, in which case the wall-clock span between the endpoints is
// used; a client that pauses mid-session sends its own smaller total.
type readingSessionRequest struct {
	StartedAt     *time.Time `json:"started_at"`
	EndedAt       *time.Time `json:"ended_at"`
	Mode          string     `json:"mode"`
	CharsAdvanced int        `json:"chars_advanced"`
	Seconds       int        `json:"seconds"`
}

// bookViews is everything needed to render or write a position for one entry:
// the canonical text's length and spine (absent until the EPUB has been
// parsed), the audio timeline (absent until an audiobook is attached), and
// the translator holding this entry's anchor maps.
type bookViews struct {
	charCount   int
	chapters    []models.EpubChapter
	timeline    bookaudio.Timeline
	hasTimeline bool
	translator  *position.Translator
}

// loadBookViews gathers the derivation inputs for an entry. It deliberately
// does *not* parse the EPUB on demand the way the text endpoints do: a
// position read happens on every page turn and every player tick, and paying
// a full parse for it would make the cheap endpoint the expensive one. A book
// whose text has never been parsed simply has no percentage and no chapter.
func (s *Server) loadBookViews(ctx context.Context, userID, entryID, bookID string) (bookViews, error) {
	v := bookViews{}

	if f, err := s.store.EpubMediaFileForBook(ctx, bookID); err == nil {
		if et, err := s.store.GetEpubText(ctx, f.ID); err == nil {
			v.charCount = et.CharCount
			if chapters, err := s.store.ListEpubChapters(ctx, et.ID); err == nil {
				v.chapters = chapters
			} else {
				slog.ErrorContext(ctx, "position: list chapters", "entry", entryID, "error", err)
			}
		} else if !errors.Is(err, store.ErrNotFound) {
			return v, err
		}
	} else if !errors.Is(err, store.ErrNotFound) {
		return v, err
	}

	switch tl, err := s.audio.Timeline(ctx, userID, entryID); {
	case err == nil:
		v.timeline, v.hasTimeline = tl, true
	case errors.Is(err, bookaudio.ErrEmptyTimeline), errors.Is(err, store.ErrNotFound):
		// No audiobook attached: the audio view is simply absent.
	default:
		return v, err
	}

	tr, err := position.Load(ctx, s.anchors, entryID)
	if err != nil {
		return v, err
	}
	v.translator = tr
	return v, nil
}

// handleGetBookPosition serves a book entry's position in all three spaces.
func (s *Server) handleGetBookPosition(w http.ResponseWriter, r *http.Request) {
	userID, entryID, bookID, ok := s.bookEntry(w, r)
	if !ok {
		return
	}

	progress, err := s.store.BookProgress(r.Context(), userID, entryID)
	if err != nil {
		fail(w, err)
		return
	}
	views, err := s.loadBookViews(r.Context(), userID, entryID, bookID)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, renderPosition(progress, views))
}

// handlePutBookPosition stores a position given in whichever space the client
// is working in.
func (s *Server) handlePutBookPosition(w http.ResponseWriter, r *http.Request) {
	userID, entryID, bookID, ok := s.bookEntry(w, r)
	if !ok {
		return
	}

	var body positionRequest
	if err := decode(r, &body); err != nil {
		fail(w, err)
		return
	}
	if n := given(body.CharOffset != nil, body.AudioSeconds != nil, body.Page != nil); n != 1 {
		fail(w, errorf(http.StatusBadRequest,
			"send exactly one of char_offset, audio_seconds or page"))
		return
	}
	if body.Source != "" && !models.ValidPositionSource(body.Source) {
		fail(w, errorf(http.StatusBadRequest, "unknown source"))
		return
	}

	current, err := s.store.BookProgress(r.Context(), userID, entryID)
	if err != nil {
		fail(w, err)
		return
	}
	views, err := s.loadBookViews(r.Context(), userID, entryID, bookID)
	if err != nil {
		fail(w, err)
		return
	}

	// The write starts from where the entry already is, so a shape that
	// cannot be translated leaves the other coordinates alone instead of
	// resetting the reader to page one.
	write := store.ProgressWrite{
		CharOffset:      current.CharOffset,
		Source:          current.CharOffsetSource,
		RawAudioSeconds: current.RawAudioSeconds,
		RawAudioFileID:  current.RawAudioFileID,
	}
	if write.Source == "" {
		write.Source = models.PositionSourceManual
	}

	switch {
	case body.CharOffset != nil:
		if *body.CharOffset < 0 || (views.charCount > 0 && *body.CharOffset > views.charCount) {
			fail(w, errorf(http.StatusBadRequest, "char_offset is outside this book's text"))
			return
		}
		write.CharOffset = *body.CharOffset
		write.Source = defaultSource(body.Source, models.PositionSourceRead)
		// A known-good text offset supersedes any raw audio fallback: the
		// canonical position is now the truth for both views.
		write.RawAudioSeconds, write.RawAudioFileID = nil, nil

	case body.AudioSeconds != nil:
		if !s.applyAudioWrite(w, r, userID, entryID, bookID, body, views, &write) {
			return
		}

	case body.Page != nil:
		if *body.Page <= 0 {
			fail(w, errorf(http.StatusBadRequest, "page must be a positive page number"))
			return
		}
		offset, _, ok := views.translator.PageToChar(*body.Page)
		if !ok {
			fail(w, errorf(http.StatusUnprocessableEntity,
				"no page map exists for this printing yet, so a page number cannot be placed in the text"))
			return
		}
		write.CharOffset = offset
		write.Source = defaultSource(body.Source, models.PositionSourceManual)
		write.RawAudioSeconds, write.RawAudioFileID = nil, nil
	}

	write.PercentComplete = percentComplete(write, views)

	result, err := s.store.SaveBookProgress(r.Context(), userID, entryID, write)
	if errors.Is(err, store.ErrNotFound) {
		fail(w, errNotFound)
		return
	}
	if err != nil {
		fail(w, errorf(http.StatusBadRequest, err.Error()))
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"position":       renderPosition(result.Progress, views),
		"status":         result.Status,
		"status_changed": result.StatusChanged,
		"offer_finished": result.OfferFinished,
	})
}

// applyAudioWrite turns a track-relative timestamp into whatever the entry
// can actually store: a character offset when an alignment exists, and the
// raw (file, seconds) pair when it does not. It writes the error response and
// returns false on failure.
func (s *Server) applyAudioWrite(w http.ResponseWriter, r *http.Request, userID, entryID, bookID string,
	body positionRequest, views bookViews, write *store.ProgressWrite) bool {

	if body.AudioFileID == nil {
		fail(w, errorf(http.StatusBadRequest, "audio_seconds needs the audio_file_id it was measured in"))
		return false
	}
	if *body.AudioSeconds < 0 {
		fail(w, errorf(http.StatusBadRequest, "audio_seconds must not be negative"))
		return false
	}
	// The file has to be this book's audio; an id from someone else's
	// library answers exactly like an unknown one.
	if _, err := s.store.AudioMediaFileForBook(r.Context(), bookID, *body.AudioFileID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			fail(w, errNotFound)
			return false
		}
		fail(w, err)
		return false
	}
	if !views.hasTimeline {
		fail(w, errorf(http.StatusNotFound, "no audiobook is attached to this book"))
		return false
	}

	global, err := views.timeline.Global(*body.AudioFileID, *body.AudioSeconds)
	if err != nil {
		fail(w, errorf(http.StatusBadRequest, "audio_seconds is outside that track"))
		return false
	}

	if offset, _, ok := views.translator.AudioToChar(global); ok {
		write.CharOffset = offset
		write.Source = defaultSource(body.Source, models.PositionSourceListen)
		write.RawAudioSeconds, write.RawAudioFileID = nil, nil
		return true
	}

	// Unaligned: the timestamp is the only truth there is. The existing
	// character offset is left where it was rather than being reset — it is
	// still the best-known text position — and the raw pair is what the
	// player resumes from. It is stored track-relative so re-measuring or
	// re-ordering the timeline cannot move it.
	write.RawAudioSeconds, write.RawAudioFileID = body.AudioSeconds, body.AudioFileID
	return true
}

// handleAddReadingSession logs a stretch of reading or listening.
func (s *Server) handleAddReadingSession(w http.ResponseWriter, r *http.Request) {
	userID, entryID, _, ok := s.bookEntry(w, r)
	if !ok {
		return
	}

	var body readingSessionRequest
	if err := decode(r, &body); err != nil {
		fail(w, err)
		return
	}
	if body.StartedAt == nil || body.EndedAt == nil {
		fail(w, errorf(http.StatusBadRequest, "started_at and ended_at are required"))
		return
	}

	session, err := s.store.AddReadingSession(r.Context(), userID, entryID, models.ReadingSession{
		StartedAt:     *body.StartedAt,
		EndedAt:       *body.EndedAt,
		Mode:          body.Mode,
		CharsAdvanced: body.CharsAdvanced,
		Seconds:       body.Seconds,
	})
	if errors.Is(err, store.ErrNotFound) {
		fail(w, errNotFound)
		return
	}
	if err != nil {
		fail(w, errorf(http.StatusBadRequest, err.Error()))
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"session": session})
}

// handleGetReadingSessions lists an entry's logged sessions with the per-mode
// totals the reading dashboard reports.
func (s *Server) handleGetReadingSessions(w http.ResponseWriter, r *http.Request) {
	userID, entryID, _, ok := s.bookEntry(w, r)
	if !ok {
		return
	}

	sessions, err := s.store.ReadingSessions(r.Context(), userID, entryID)
	if err != nil {
		fail(w, err)
		return
	}
	totals, err := s.store.ReadingTime(r.Context(), userID, entryID)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"sessions": sessions, "seconds_by_mode": totals})
}

// bookEntry authenticates the caller and resolves the {entryID} URL parameter
// to the book it points at, answering 401/404 itself. The bool reports
// whether the handler may continue.
func (s *Server) bookEntry(w http.ResponseWriter, r *http.Request) (userID, entryID, bookID string, ok bool) {
	userID, err := auth.MustUserID(r.Context())
	if err != nil {
		fail(w, errUnauthorized)
		return "", "", "", false
	}
	entryID = chi.URLParam(r, "entryID")
	bookID, err = s.store.BookIDForEntry(r.Context(), userID, entryID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			fail(w, errNotFound)
			return "", "", "", false
		}
		fail(w, err)
		return "", "", "", false
	}
	return userID, entryID, bookID, true
}

// renderPosition derives the audio and page views from the stored character
// offset and assembles the response.
func renderPosition(p models.BookProgress, v bookViews) positionResponse {
	out := positionResponse{
		CharOffset: p.CharOffset,
		Source:     p.CharOffsetSource,
		Percent:    p.PercentComplete,
		CharCount:  v.charCount,
		Chapter:    chapterAt(v.chapters, p.CharOffset),
		Audio:      audioAt(p, v),
		Page:       pageAt(p, v),
	}
	if out.Source == "" {
		out.Source = models.PositionSourceManual
	}
	if !p.UpdatedAt.IsZero() {
		t := p.UpdatedAt
		out.UpdatedAt = &t
	}

	// Nothing to derive means nothing was derived — an unaligned book with
	// no audiobook is honestly reported as derived: false rather than
	// claiming a precision it does not have.
	derived, confidence, any := true, math.Inf(1), false
	for _, view := range []struct {
		present, derived bool
		confidence       float64
	}{
		{out.Audio != nil, out.Audio != nil && out.Audio.Derived, audioConfidence(out.Audio)},
		{out.Page != nil, out.Page != nil && out.Page.Derived, pageConfidence(out.Page)},
	} {
		if !view.present {
			continue
		}
		any = true
		derived = derived && view.derived
		confidence = math.Min(confidence, view.confidence)
	}
	if any && derived {
		out.Derived, out.Confidence = true, confidence
	}
	return out
}

func audioConfidence(a *audioView) float64 {
	if a == nil {
		return 0
	}
	return a.Confidence
}

func pageConfidence(p *pageView) float64 {
	if p == nil {
		return 0
	}
	return p.Confidence
}

// audioAt places the stored position on the audio timeline: through the
// alignment map when there is one, and from the raw stored timestamp when
// there is not. Without an audiobook there is nothing to place, so the view
// is absent rather than zeroed.
func audioAt(p models.BookProgress, v bookViews) *audioView {
	if !v.hasTimeline {
		return nil
	}

	global, confidence, derived := 0.0, 0.0, false
	switch {
	case v.translator != nil && v.translator.HasAudio():
		seconds, conf, ok := v.translator.CharToAudio(p.CharOffset)
		if !ok {
			return nil
		}
		global, confidence, derived = seconds, conf, true
	case p.RawAudioSeconds != nil && p.RawAudioFileID != nil:
		seconds, err := v.timeline.Global(*p.RawAudioFileID, *p.RawAudioSeconds)
		if err != nil {
			// The file the position was measured in has been detached or
			// re-ordered off the timeline; there is nowhere to resume.
			return nil
		}
		global = seconds
	default:
		// An audiobook exists but nothing has been listened to and nothing
		// can be derived: the player starts at the beginning.
		global = 0
	}

	out := &audioView{
		Seconds:       global,
		TotalDuration: v.timeline.TotalDuration,
		Derived:       derived,
		Confidence:    confidence,
	}
	if pos, err := v.timeline.Locate(global); err == nil {
		out.TrackID, out.TrackNumber, out.TrackSeconds = pos.MediaFileID, pos.Number, pos.TrackSeconds
	}
	return out
}

// pageAt derives the printed page, which exists only once a page map does.
func pageAt(p models.BookProgress, v bookViews) *pageView {
	if v.translator == nil || !v.translator.HasPages() {
		return nil
	}
	page, confidence, ok := v.translator.CharToPage(p.CharOffset)
	if !ok {
		return nil
	}
	return &pageView{Page: page, Derived: true, Confidence: confidence}
}

// chapterAt finds the spine document owning an offset. Ranges are
// [CharStart, CharEnd), so the very end of the book belongs to the last
// document that holds any text rather than to nothing.
func chapterAt(chapters []models.EpubChapter, offset int) *chapterView {
	var last *models.EpubChapter
	for i := range chapters {
		ch := chapters[i]
		if ch.CharEnd > ch.CharStart {
			last = &chapters[i]
		}
		if offset >= ch.CharStart && offset < ch.CharEnd {
			return newChapterView(ch)
		}
	}
	if last != nil && offset >= last.CharEnd {
		return newChapterView(*last)
	}
	return nil
}

func newChapterView(ch models.EpubChapter) *chapterView {
	return &chapterView{
		SpineIndex: ch.SpineIndex, Title: ch.Title, Href: ch.Href,
		CharStart: ch.CharStart, CharEnd: ch.CharEnd,
	}
}

// percentComplete measures progress against the canonical text when it has
// been parsed, and falls back to the audiobook's duration for a book that is
// audio-only. A book with neither has no denominator, so it reports 0 rather
// than a made-up number.
func percentComplete(w store.ProgressWrite, v bookViews) float64 {
	if v.charCount > 0 {
		return math.Min(100, float64(w.CharOffset)/float64(v.charCount)*100)
	}
	if v.hasTimeline && v.timeline.TotalDuration > 0 && w.RawAudioSeconds != nil && w.RawAudioFileID != nil {
		if global, err := v.timeline.Global(*w.RawAudioFileID, *w.RawAudioSeconds); err == nil {
			return math.Min(100, global/v.timeline.TotalDuration*100)
		}
	}
	return 0
}

func defaultSource(given, fallback string) string {
	if given == "" {
		return fallback
	}
	return given
}

// given counts how many of the mutually exclusive request shapes were sent.
func given(flags ...bool) int {
	n := 0
	for _, f := range flags {
		if f {
			n++
		}
	}
	return n
}
