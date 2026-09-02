package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/collinpendleton/backhog/api/internal/models"
)

// charsPerPage converts the canonical text's byte length into printed pages.
// A trade paperback page is roughly 300 words at about six bytes a word, so
// 1800 is the constant that makes "pages" and "characters" one unit. It is
// reported in the pace payload rather than hidden, because every page figure
// derived from an EPUB depends on it.
const charsPerPage = 1800

// defaultPagesPerHour is the fallback reading speed, used until there is
// enough logged reading to measure yours. Roughly 200 words a minute, which
// is an unhurried adult pace — deliberately not flattering, since a fast
// default would shrink the debt for free.
const defaultPagesPerHour = 40.0

// minPaceSeconds is how much instrumented reading a measured pace needs
// before it beats the default. Below half an hour, one distracted chapter
// sets your lifetime reading speed.
const minPaceSeconds = 1800

// shortBookPages is the "quick wins" slice of the reading debt: anything
// under a couple of hundred pages is an evening, not a project.
const shortBookPages = 250

// Minimum signal before a bucket stat is worth calling a superlative: three
// owned books by one author, five in one subject. Authors get the lower bar —
// owning five books by one person is already a commitment, and five in
// "Fiction" is nothing.
const (
	minAuthorOwned  = 3
	minSubjectOwned = 5
)

// minRestarts is how many separate times a book must have been picked up
// before "you keep starting this" is a fact rather than a coincidence.
const minRestarts = 2

// readingScenarios are the fixed hrs/week projections shown beside your real
// pace. Reading happens in smaller slices than playing, so these are not the
// debt report's 5/10/15.
var readingScenarios = []float64{2, 5, 10}

// bookSize is how long one book entry is and how much of it is left. Exactly
// one sizing wins per entry, in descending order of honesty: a real audiobook
// duration, then the canonical text's own length, then the printing's page
// count. A book with none of the three is unsized and contributes nothing,
// the same way a game with no time-to-beat contributes nothing to the debt.
type bookSize struct {
	// pages is the printing's page count; chars is the canonical text length
	// when an EPUB is attached. Either may be zero.
	pages int
	chars int
	// audioSeconds is the summed duration of the attached audiobook, 0 when
	// there is no audio or none of it has been measured yet.
	audioSeconds float64
	// percent is how far through the book the stored position says you are.
	percent float64
}

// effectivePages is the page count to reason about: derived from the
// canonical text when there is one, since that is measured rather than
// reported, and the printing's own count otherwise.
func (b bookSize) effectivePages() float64 {
	if b.chars > 0 {
		return float64(b.chars) / charsPerPage
	}
	return float64(b.pages)
}

// remainingFraction is how much of the book is still ahead of you.
func (b bookSize) remainingFraction() float64 {
	return math.Min(1, math.Max(0, 1-b.percent/100))
}

// ReadingDebt is the books counterpart of Debt: unread pages, the hours they
// cost at your measured pace, real audiobook hours where files are attached,
// and when the pile clears. Everything is derived from the shared spine —
// library_entries, book_progress and reading_sessions — plus page counts and
// attached media.
func (s *Store) ReadingDebt(ctx context.Context, userID string) (models.ReadingDebt, error) {
	stats, err := s.BookStats(ctx, userID)
	if err != nil {
		return models.ReadingDebt{}, err
	}
	pace, err := s.ReadingPace(ctx, userID)
	if err != nil {
		return models.ReadingDebt{}, err
	}
	sizes, err := s.bookSizes(ctx, userID)
	if err != nil {
		return models.ReadingDebt{}, err
	}

	debt := models.ReadingDebt{
		// A wishlist is a reading list, not a book you own.
		BooksOwned:  stats.Total - stats.Wishlist,
		UnreadBooks: stats.Backlog + stats.Reading,
		Pace:        pace,
	}

	var shortPages float64
	for _, size := range sizes {
		remaining := size.remainingFraction()
		switch {
		case size.audioSeconds > 0:
			// Real durations beat any estimate, so a book you own the audio
			// of is counted in hours and never in pages.
			debt.AudioHours += size.audioSeconds / 3600 * remaining
			debt.AudioBooks++
		case size.effectivePages() > 0:
			pages := size.effectivePages() * remaining
			debt.PagesOwed += pages
			if size.effectivePages() < shortBookPages {
				shortPages += pages
			}
		default:
			debt.UnsizedBooks++
		}
	}

	debt.PagesOwed = round1(debt.PagesOwed)
	debt.PageHours = round1(debt.PagesOwed / pace.PagesPerHour)
	debt.AudioHours = round1(debt.AudioHours)
	debt.ShortBooksHours = round1(shortPages / pace.PagesPerHour)
	debt.HoursOwed = round1(debt.PageHours + debt.AudioHours)
	debt.Projection = buildReadingProjection(debt.HoursOwed, pace, time.Now().UTC())
	return debt, nil
}

// buildReadingProjection pairs the current-pace estimate (when there is one)
// with the fixed reading scenarios, reusing the debt report's clearance math
// so the two pages can never disagree about what a week of progress means.
func buildReadingProjection(totalHours float64, pace models.ReadingPace, now time.Time) models.DebtProjection {
	proj := models.DebtProjection{Scenarios: make([]models.ClearanceScenario, 0, len(readingScenarios))}
	if pace.HoursPerWeek90d != nil && *pace.HoursPerWeek90d > 0 {
		scenario := projectClearance(totalHours, *pace.HoursPerWeek90d, now)
		proj.CurrentPace = &scenario
	}
	for _, rate := range readingScenarios {
		proj.Scenarios = append(proj.Scenarios, projectClearance(totalHours, rate, now))
	}
	return proj
}

// ReadingPace measures how fast you read from your own logged sessions: the
// characters the reader actually advanced over the seconds it took, converted
// to pages. Listening sessions are excluded from the speed — an audiobook
// runs at the narrator's pace, not yours — but both modes count towards how
// much time per week you spend on books.
func (s *Store) ReadingPace(ctx context.Context, userID string) (models.ReadingPace, error) {
	pace := models.ReadingPace{
		PagesPerHour: defaultPagesPerHour,
		CharsPerHour: defaultPagesPerHour * charsPerPage,
		CharsPerPage: charsPerPage,
	}

	var chars, seconds float64
	err := s.db.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(chars_advanced), 0), COALESCE(SUM(seconds), 0)
		FROM reading_sessions
		WHERE user_id = ? AND mode = 'read' AND chars_advanced > 0 AND seconds > 0`, userID).
		Scan(&chars, &seconds)
	if err != nil {
		return pace, err
	}
	if seconds >= minPaceSeconds && chars > 0 {
		pace.CharsPerHour = round1(chars / (seconds / 3600))
		pace.PagesPerHour = round1(pace.CharsPerHour / charsPerPage)
		pace.Measured = true
		pace.SessionHours = round1(seconds / 3600)
	}
	// A measurement can round to zero on absurd input (a long session that
	// advanced a handful of characters). Falling back keeps every division by
	// the pace finite.
	if pace.PagesPerHour <= 0 {
		pace.PagesPerHour = defaultPagesPerHour
		pace.Measured = false
	}

	var totalSeconds, recentSeconds float64
	var firstReadOn sql.NullString
	err = s.db.QueryRowContext(ctx, `
		SELECT
			COALESCE(SUM(seconds), 0),
			COALESCE(SUM(CASE WHEN date(started_at) >= date('now', '-90 days') THEN seconds END), 0),
			date(MIN(started_at))
		FROM reading_sessions
		WHERE user_id = ?`, userID).
		Scan(&totalSeconds, &recentSeconds, &firstReadOn)
	if err != nil {
		return pace, err
	}
	// computePace works in minutes, the unit play sessions are logged in; the
	// weekly-rate maths is identical, so it is reused rather than rewritten.
	weekly := computePace(recentSeconds/60, totalSeconds/60, firstReadOn, time.Now().UTC())
	pace.HoursPerWeek90d = weekly.HoursPerWeek90d
	pace.HoursPerWeekAll = weekly.HoursPerWeekAll
	return pace, nil
}

// unreadBooksWhere scopes a query to the books you still owe yourself: on the
// shelf or in progress, never wishlisted, dropped or finished.
const unreadBooksWhere = `e.user_id = ? AND e.media_type = 'book' AND e.status IN ('backlog','playing')`

// bookSizeSelect is the shared sizing projection: the entry's own printing
// falling back to the work's earliest one with a page count, the canonical
// text length of any attached EPUB, the summed duration of any attached
// audiobook, and the stored reading position.
const bookSizeSelect = `
	COALESCE(ed.page_count,
		(SELECT ed2.page_count FROM book_editions ed2
		 WHERE ed2.book_id = e.book_id AND ed2.page_count IS NOT NULL
		 ORDER BY ed2.published_year, ed2.id LIMIT 1), 0),
	COALESCE((SELECT et.char_count FROM epub_texts et
	          JOIN media_files mf ON mf.id = et.media_file_id
	          WHERE mf.book_id = e.book_id AND mf.kind = 'epub'
	          ORDER BY mf.id LIMIT 1), 0),
	COALESCE((SELECT SUM(mf2.duration_seconds) FROM media_files mf2
	          WHERE mf2.book_id = e.book_id AND mf2.kind = 'audio'
	            AND mf2.duration_seconds IS NOT NULL), 0),
	COALESCE((SELECT bp.percent_complete FROM book_progress bp WHERE bp.entry_id = e.id), 0)`

// bookSizes loads the sizing facts for every unread book entry, keyed by
// entry id. One query for the whole shelf: the dashboard needs all of them,
// and a per-entry lookup here would be the N+1 the games side avoids.
func (s *Store) bookSizes(ctx context.Context, userID string) (map[string]bookSize, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT e.id,`+bookSizeSelect+`
		FROM library_entries e
		LEFT JOIN book_editions ed ON ed.id = e.edition_id
		WHERE `+unreadBooksWhere, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[string]bookSize{}
	for rows.Next() {
		var entryID string
		var size bookSize
		if err := rows.Scan(&entryID, &size.pages, &size.chars, &size.audioSeconds, &size.percent); err != nil {
			return nil, err
		}
		out[entryID] = size
	}
	return out, rows.Err()
}

// ReadingInsights builds the "Your Reading Problem" dashboard: a headline
// summary plus the superlatives. The headline reuses the reading debt's pages
// and hours so the two views can never disagree, exactly as Insights reuses
// the debt report.
func (s *Store) ReadingInsights(ctx context.Context, userID string) (models.ReadingInsights, error) {
	debt, err := s.ReadingDebt(ctx, userID)
	if err != nil {
		return models.ReadingInsights{}, err
	}

	headline := models.ReadingHeadline{
		BooksOwned:  debt.BooksOwned,
		UnreadBooks: debt.UnreadBooks,
		PagesOwed:   debt.PagesOwed,
		HoursOwed:   debt.HoursOwed,
	}
	if debt.Projection.CurrentPace != nil {
		years := round1(debt.Projection.CurrentPace.Weeks / 52)
		headline.YearsAtCurrentRate = &years
	}

	insights := models.ReadingInsights{
		Headline:     headline,
		Pace:         debt.Pace,
		Superlatives: []models.BookSuperlative{},
	}

	for _, load := range []func() (*models.BookSuperlative, error){
		func() (*models.BookSuperlative, error) { return s.oldestUnopened(ctx, userID) },
		func() (*models.BookSuperlative, error) { return s.longestUnread(ctx, userID, debt.Pace) },
		func() (*models.BookSuperlative, error) { return s.unreadAuthor(ctx, userID) },
		func() (*models.BookSuperlative, error) { return s.neglectedSubject(ctx, userID) },
		func() (*models.BookSuperlative, error) { return s.restartedBook(ctx, userID) },
	} {
		sup, err := load()
		if err != nil {
			return models.ReadingInsights{}, err
		}
		if sup != nil {
			insights.Superlatives = append(insights.Superlatives, *sup)
		}
	}
	return insights, nil
}

// unopenedWhere selects shelf books with no logged reading and no stored
// position past page one — bought, shelved, never opened.
const unopenedWhere = `e.user_id = ? AND e.media_type = 'book' AND e.status = 'backlog'
	AND NOT EXISTS (SELECT 1 FROM reading_sessions rs WHERE rs.entry_id = e.id)
	AND NOT EXISTS (SELECT 1 FROM book_progress bp WHERE bp.entry_id = e.id AND bp.char_offset > 0)`

// oldestUnopened finds the unopened book that has been on the shelf longest.
func (s *Store) oldestUnopened(ctx context.Context, userID string) (*models.BookSuperlative, error) {
	var entryID, bookID, addedOn string
	err := s.db.QueryRowContext(ctx, `
		SELECT e.id, e.book_id, date(e.created_at)
		FROM library_entries e
		WHERE `+unopenedWhere+`
		ORDER BY e.created_at ASC, e.id ASC
		LIMIT 1`, userID).
		Scan(&entryID, &bookID, &addedOn)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	book, err := s.bookForInsight(ctx, bookID)
	if err != nil {
		return nil, err
	}
	year := addedOn
	if len(year) >= 4 {
		year = year[:4]
	}
	return &models.BookSuperlative{
		Kind: models.BookSuperlativeOldestUnopened,
		Payload: models.BookSuperlativePayload{
			Book:    book,
			EntryID: entryID,
			AddedOn: addedOn,
		},
		Label: fmt.Sprintf("Shelved %s · never opened", year),
	}, nil
}

// longestUnread finds the biggest single block of unread book: the longest
// unopened title on the shelf, sized the same way the debt sizes it.
func (s *Store) longestUnread(ctx context.Context, userID string, pace models.ReadingPace) (*models.BookSuperlative, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT e.id, e.book_id,`+bookSizeSelect+`
		FROM library_entries e
		LEFT JOIN book_editions ed ON ed.id = e.edition_id
		WHERE `+unopenedWhere, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var bestEntry, bestBook string
	var bestPages float64
	var bestSize bookSize
	for rows.Next() {
		var entryID, bookID string
		var size bookSize
		if err := rows.Scan(&entryID, &bookID, &size.pages, &size.chars,
			&size.audioSeconds, &size.percent); err != nil {
			return nil, err
		}
		pages := size.effectivePages()
		// Ties break on entry id so the same shelf always names the same book.
		if pages <= 0 || pages < bestPages || (pages == bestPages && entryID >= bestEntry) {
			continue
		}
		bestEntry, bestBook, bestPages, bestSize = entryID, bookID, pages, size
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if bestEntry == "" {
		return nil, nil
	}

	book, err := s.bookForInsight(ctx, bestBook)
	if err != nil {
		return nil, err
	}
	pages := int(math.Round(bestPages))
	hours := round1(bestPages / pace.PagesPerHour)
	payload := models.BookSuperlativePayload{
		Book:    book,
		EntryID: bestEntry,
		Pages:   &pages,
		Hours:   &hours,
	}
	label := fmt.Sprintf("%s · %s at your pace", pagesLabel(pages), hoursLabel(hours))
	// An attached audiobook is a measured length, so it replaces the estimate
	// rather than sitting beside it.
	if bestSize.audioSeconds > 0 {
		audioHours := round1(bestSize.audioSeconds / 3600)
		payload.Hours = &audioHours
		label = fmt.Sprintf("%s · %s of audio", pagesLabel(pages), hoursLabel(audioHours))
	}
	return &models.BookSuperlative{
		Kind:    models.BookSuperlativeLongestUnread,
		Payload: payload,
		Label:   label,
	}, nil
}

// unreadAuthor finds the author you keep buying and keep not reading: the
// worst read ratio among authors you own at least minAuthorOwned books by.
func (s *Store) unreadAuthor(ctx context.Context, userID string) (*models.BookSuperlative, error) {
	return s.bookBucketSuperlative(ctx, userID, models.BookSuperlativeUnreadAuthor, minAuthorOwned, `
		SELECT je.value, COUNT(*), COALESCE(SUM(e.status = 'played'), 0)
		FROM library_entries e
		JOIN books b ON b.id = e.book_id, json_each(b.authors_json) je
		WHERE e.user_id = ? AND e.media_type = 'book' AND e.status != 'wishlist'
		  AND je.value <> ''
		GROUP BY je.value
		HAVING COUNT(*) >= ? AND SUM(e.status = 'played') < COUNT(*)
		ORDER BY SUM(e.status = 'played') * 1.0 / COUNT(*) ASC, COUNT(*) DESC, je.value ASC
		LIMIT 1`)
}

// neglectedSubject finds the subject carrying the worst backlog, on the same
// shape as unreadAuthor with a higher bar: subjects are broad.
func (s *Store) neglectedSubject(ctx context.Context, userID string) (*models.BookSuperlative, error) {
	return s.bookBucketSuperlative(ctx, userID, models.BookSuperlativeNeglectedSubject, minSubjectOwned, `
		SELECT je.value, COUNT(*), COALESCE(SUM(e.status = 'played'), 0)
		FROM library_entries e
		JOIN books b ON b.id = e.book_id, json_each(b.subjects_json) je
		WHERE e.user_id = ? AND e.media_type = 'book' AND e.status != 'wishlist'
		  AND je.value <> ''
		GROUP BY je.value
		HAVING COUNT(*) >= ? AND SUM(e.status = 'played') < COUNT(*)
		ORDER BY SUM(e.status = 'played') * 1.0 / COUNT(*) ASC, COUNT(*) DESC, je.value ASC
		LIMIT 1`)
}

// bookBucketSuperlative runs an owned/read bucket query and renders it. Both
// bucket stats have the same shape, so the copy lives in one place.
func (s *Store) bookBucketSuperlative(ctx context.Context, userID, kind string, minOwned int, query string) (*models.BookSuperlative, error) {
	var name string
	var owned, read int
	err := s.db.QueryRowContext(ctx, query, userID, minOwned).Scan(&name, &owned, &read)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &models.BookSuperlative{
		Kind: kind,
		Payload: models.BookSuperlativePayload{
			Name: name, Owned: owned, Read: read,
		},
		Label: fmt.Sprintf("%s — %d books / %d read", name, owned, read),
	}, nil
}

// restartedBook finds the book you have picked up and put down the most: the
// entry with the most transitions into 'playing' that is still unfinished.
// entry_status_history is the only record of a status you are no longer in,
// which is exactly what "started it again" means.
func (s *Store) restartedBook(ctx context.Context, userID string) (*models.BookSuperlative, error) {
	var entryID, bookID string
	var starts int
	err := s.db.QueryRowContext(ctx, `
		SELECT e.id, e.book_id, COUNT(*)
		FROM library_entries e
		JOIN entry_status_history h ON h.entry_id = e.id AND h.to_status = 'playing'
		WHERE e.user_id = ? AND e.media_type = 'book' AND e.status != 'played'
		GROUP BY e.id
		HAVING COUNT(*) >= ?
		ORDER BY COUNT(*) DESC, e.created_at ASC, e.id ASC
		LIMIT 1`, userID, minRestarts).
		Scan(&entryID, &bookID, &starts)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	book, err := s.bookForInsight(ctx, bookID)
	if err != nil {
		return nil, err
	}
	return &models.BookSuperlative{
		Kind: models.BookSuperlativeRestarted,
		Payload: models.BookSuperlativePayload{
			Book: book, EntryID: entryID, Starts: starts,
		},
		Label: fmt.Sprintf("Started %s · still unfinished", timesLabel(starts)),
	}, nil
}

// bookForInsight loads one hydrated work for a book-backed superlative.
func (s *Store) bookForInsight(ctx context.Context, bookID string) (*models.Book, error) {
	book, err := s.bookByID(ctx, bookID)
	if err != nil {
		return nil, err
	}
	return &book, nil
}

// pagesLabel renders a page count with thousands separators: "1,216 pages".
func pagesLabel(pages int) string {
	if pages == 1 {
		return "1 page"
	}
	return fmt.Sprintf("%s pages", groupThousands(pages))
}

func groupThousands(n int) string {
	digits := fmt.Sprintf("%d", n)
	if len(digits) <= 3 {
		return digits
	}
	var parts []string
	for len(digits) > 3 {
		parts = append([]string{digits[len(digits)-3:]}, parts...)
		digits = digits[:len(digits)-3]
	}
	return strings.Join(append([]string{digits}, parts...), ",")
}

// timesLabel renders a restart count as "twice" / "3 separate times".
func timesLabel(n int) string {
	if n <= 2 {
		return "twice"
	}
	return fmt.Sprintf("%d separate times", n)
}

// readingCandidate distils one book entry down to the signals the reading
// scorer needs, the mirror of tonightCandidate. The DB layer fills these in;
// the scorer is a pure function so the heuristics stay table-testable.
type readingCandidate struct {
	entry models.Entry
	// remaining is hours left in the book at your pace (or from the real
	// audiobook duration). Nil when the book has no known length.
	remaining *float64
	// percent is how far in the stored position says you are.
	percent float64
	// lastRead is the newest reading session, falling back to started_at.
	lastRead *time.Time
	// ownedDays is how long the entry has been on the shelf.
	ownedDays float64
	// queueRank is the 1-based position in the reading queue; 0 when unqueued.
	queueRank int
	// audio reports that the length came from an attached audiobook.
	audio bool
}

// ReadingPicks answers "I have N minutes, what should I read?" with one pick
// per category: continue something in progress, a short win that fits the
// budget, a wildcard you have never opened, and the longest-owned rescue.
// Same four categories and the same deterministic scoring as TonightPicks —
// what changes is that a book's length comes from pages and audio rather than
// from a crowd-sourced time to beat.
func (s *Store) ReadingPicks(ctx context.Context, userID string, minutes int, exclude []string) (models.ReadingPicksResult, error) {
	now := time.Now().UTC()

	candidates, err := s.readingCandidates(ctx, userID, now)
	if err != nil {
		return models.ReadingPicksResult{}, err
	}

	skip := make(map[string]bool, len(exclude))
	for _, id := range exclude {
		skip[id] = true
	}
	return selectReadingPicks(candidates, float64(minutes), skip, now), nil
}

// readingCandidates loads and distils every shelf and in-progress book.
func (s *Store) readingCandidates(ctx context.Context, userID string, now time.Time) ([]readingCandidate, error) {
	entries, err := s.queryEntries(ctx, entrySelect+` WHERE `+unreadBooksWhere, userID)
	if err != nil {
		return nil, err
	}

	pace, err := s.ReadingPace(ctx, userID)
	if err != nil {
		return nil, err
	}
	sizes, err := s.bookSizes(ctx, userID)
	if err != nil {
		return nil, err
	}
	lastRead, err := s.lastReadByEntry(ctx, userID)
	if err != nil {
		return nil, err
	}

	// Queue ranks come from the same ordering the queue endpoint uses,
	// narrowed to the books half — the queue holds both arenas, and "#3 in
	// your queue" has to mean third among the books you can see.
	queued := make([]models.Entry, 0, len(entries))
	for _, e := range entries {
		if e.Status == models.StatusBacklog {
			queued = append(queued, e)
		}
	}
	sort.Slice(queued, func(i, j int) bool {
		a, b := queued[i], queued[j]
		ap, bp := a.QueuePosition, b.QueuePosition
		switch {
		case ap != nil && bp != nil && *ap != *bp:
			return *ap < *bp
		case ap != nil && bp == nil:
			return true
		case ap == nil && bp != nil:
			return false
		}
		return a.CreatedAt.Before(b.CreatedAt)
	})
	rankByID := make(map[string]int, len(queued))
	for i, e := range queued {
		rankByID[e.ID] = i + 1
	}

	candidates := make([]readingCandidate, 0, len(entries))
	for _, e := range entries {
		// The query is book-scoped, so a nil Book means a broken row rather
		// than a game — better to skip than to reason about it.
		if e.Book == nil {
			continue
		}
		size := sizes[e.ID]
		c := readingCandidate{
			entry:     e,
			percent:   size.percent,
			ownedDays: now.Sub(e.CreatedAt).Hours() / 24,
			queueRank: rankByID[e.ID],
			lastRead:  lastRead[e.ID],
		}
		if c.lastRead == nil {
			c.lastRead = e.StartedAt
		}
		switch {
		case size.audioSeconds > 0:
			remaining := size.audioSeconds / 3600 * size.remainingFraction()
			c.remaining = &remaining
			c.audio = true
		case size.effectivePages() > 0:
			remaining := size.effectivePages() * size.remainingFraction() / pace.PagesPerHour
			c.remaining = &remaining
		}
		candidates = append(candidates, c)
	}
	return candidates, nil
}

// lastReadByEntry maps entry id to the start of its newest reading session.
func (s *Store) lastReadByEntry(ctx context.Context, userID string) (map[string]*time.Time, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT entry_id, date(MAX(started_at)) FROM reading_sessions
		WHERE user_id = ? GROUP BY entry_id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[string]*time.Time{}
	for rows.Next() {
		var entryID string
		var readOn sql.NullString
		if err := rows.Scan(&entryID, &readOn); err != nil {
			return nil, err
		}
		if !readOn.Valid {
			continue
		}
		if t, err := time.Parse("2006-01-02", readOn.String); err == nil {
			out[entryID] = &t
		}
	}
	return out, rows.Err()
}

// readingCategory pairs one category's eligibility+score with the reason text
// built from the same signals, so a pick's explanation can never drift from
// the formula that chose it.
type readingCategory struct {
	score  func(readingCandidate) (float64, bool)
	reason func(readingCandidate) string
}

// selectReadingPicks picks the best candidate per category. Categories claim
// their winner in display order, and a book already chosen for one category
// is not offered again for another. Ties break alphabetically by title so the
// same shelf always yields the same picks.
func selectReadingPicks(candidates []readingCandidate, budgetMinutes float64, skip map[string]bool, now time.Time) models.ReadingPicksResult {
	continueCat := readingCategory{
		score:  func(c readingCandidate) (float64, bool) { return scoreKeepReading(c, budgetMinutes, now) },
		reason: func(c readingCandidate) string { return reasonKeepReading(c, budgetMinutes, now) },
	}
	shortWinCat := readingCategory{
		score:  func(c readingCandidate) (float64, bool) { return scoreShortRead(c, budgetMinutes) },
		reason: reasonShortRead,
	}
	wildcardCat := readingCategory{score: scoreUnopened, reason: reasonUnopened}
	rescueCat := readingCategory{score: scoreShelfRescue, reason: reasonShelfRescue}

	used := make(map[string]bool, len(skip)+4)
	taken := func(c readingCandidate) bool { return used[c.entry.ID] || skip[c.entry.ID] }
	claim := func(cat readingCategory) *models.ReadingPick {
		best := -1.0
		var bestPick *models.ReadingPick
		for _, c := range candidates {
			if taken(c) {
				continue
			}
			score, ok := cat.score(c)
			if !ok {
				continue
			}
			if bestPick != nil && (score < best || (score == best && c.entry.Book.Title >= bestPick.Entry.Book.Title)) {
				continue
			}
			best = score
			bestPick = &models.ReadingPick{Entry: c.entry, Score: score, Reason: cat.reason(c)}
		}
		if bestPick != nil {
			used[bestPick.Entry.ID] = true
		}
		return bestPick
	}

	return models.ReadingPicksResult{
		Continue: claim(continueCat),
		ShortWin: claim(shortWinCat),
		Wildcard: claim(wildcardCat),
		Rescue:   claim(rescueCat),
	}
}

// scoreKeepReading favours the book with momentum: the more recently you read
// it the better, with a boost for one you could actually finish tonight.
func scoreKeepReading(c readingCandidate, budgetMinutes float64, now time.Time) (float64, bool) {
	if c.entry.Status != models.StatusPlaying {
		return 0, false
	}
	score := 30.0
	if c.remaining != nil {
		score = 50
		if *c.remaining*60 <= budgetMinutes {
			score += 25
		}
	}
	score += math.Max(0, 40-2*c.daysSinceLastRead(now))
	return score, true
}

// scoreShortRead wants a shelf book that fits entirely in the budget,
// preferring ones high in the reading queue and ones you rated well.
func scoreShortRead(c readingCandidate, budgetMinutes float64) (float64, bool) {
	if c.entry.Status != models.StatusBacklog || c.remaining == nil || *c.remaining*60 > budgetMinutes {
		return 0, false
	}
	score := 0.0
	if c.queueRank > 0 {
		score += math.Max(0, 40-4*float64(c.queueRank-1))
	}
	score += readerRatingScore(c, 35)
	return score, true
}

// scoreUnopened ignores fit on purpose: something you have never opened, with
// a bonus for the buried ones the queue never surfaces. Books carry no crowd
// rating, so a shelf with no ratings at all still scores — the buried bonus
// and the alphabetical tiebreak keep it deterministic.
func scoreUnopened(c readingCandidate) (float64, bool) {
	if c.entry.Status != models.StatusBacklog || c.percent > 0 {
		return 0, false
	}
	score := readerRatingScore(c, 60)
	if c.queueRank == 0 || c.queueRank > 10 {
		score += 15
	}
	return score, true
}

// scoreShelfRescue is pure guilt: owned the longest, read the least.
func scoreShelfRescue(c readingCandidate) (float64, bool) {
	if c.entry.Status != models.StatusBacklog && c.entry.Status != models.StatusPlaying {
		return 0, false
	}
	score := math.Min(60, c.ownedDays/365.25*12)
	score += (1 - math.Min(1, c.percent/100)) * 40
	return score, true
}

// readerRatingScore weights your own rating, the only rating a book has here:
// Open Library carries no crowd score worth projecting a night's reading on.
func readerRatingScore(c readingCandidate, weight float64) float64 {
	if u := c.entry.UserRating; u != nil {
		return float64(*u) / 10 * weight
	}
	return 0
}

// daysSinceLastRead falls back to shelf age when there is no session or start
// date at all, so an untouched "reading" entry reads as long stalled.
func (c readingCandidate) daysSinceLastRead(now time.Time) float64 {
	if c.lastRead != nil {
		days := now.Sub(*c.lastRead).Hours() / 24
		if days < 0 {
			return 0
		}
		return days
	}
	return c.ownedDays
}

// reasonKeepReading explains the momentum pick: how far in you are, what is
// left, and when you last touched it.
func reasonKeepReading(c readingCandidate, budgetMinutes float64, now time.Time) string {
	parts := []string{}
	if c.percent > 0 {
		parts = append(parts, fmt.Sprintf("%d%% in", int(math.Round(c.percent))))
	}
	switch {
	case c.remaining == nil:
		parts = append(parts, "no length on file")
	case *c.remaining*60 <= budgetMinutes:
		parts = append(parts, fmt.Sprintf("%s left — you could finish it tonight", formatPickHours(*c.remaining)))
	default:
		parts = append(parts, fmt.Sprintf("%s left", formatPickHours(*c.remaining)))
	}
	if c.lastRead != nil {
		parts = append(parts, "last read "+daysAgoText(c.daysSinceLastRead(now)))
	}
	return strings.Join(parts, " · ")
}

// reasonShortRead explains the budget fit: "~2h to read · #3 in your queue".
func reasonShortRead(c readingCandidate) string {
	verb := "to read"
	if c.audio {
		verb = "of audio"
	}
	parts := []string{fmt.Sprintf("~%s %s", formatPickHours(*c.remaining), verb)}
	if c.queueRank > 0 {
		parts = append(parts, fmt.Sprintf("#%d in your queue", c.queueRank))
	}
	parts = append(parts, readerRatingSuffix(c)...)
	return strings.Join(parts, " · ")
}

// reasonUnopened explains the spice pick: fit deliberately ignored.
func reasonUnopened(c readingCandidate) string {
	parts := []string{"You've never opened it"}
	if c.remaining != nil {
		parts = append(parts, formatPickHours(*c.remaining)+" cover to cover")
	}
	return strings.Join(append(parts, readerRatingSuffix(c)...), " · ")
}

// reasonShelfRescue explains the guilt pick: how long owned, how little read.
func reasonShelfRescue(c readingCandidate) string {
	owned := fmt.Sprintf("It's been on the shelf %s and ", ownedForText(c.ownedDays))
	if c.percent <= 0 {
		return owned + "you've never opened it"
	}
	return owned + fmt.Sprintf("you're %d%% in", int(math.Round(c.percent)))
}

// readerRatingSuffix appends your own rating when there is one.
func readerRatingSuffix(c readingCandidate) []string {
	if u := c.entry.UserRating; u != nil {
		return []string{fmt.Sprintf("you rated it %d", *u)}
	}
	return nil
}
