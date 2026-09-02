package store

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/collinpendleton/backhog/api/internal/db"
	"github.com/collinpendleton/backhog/api/internal/models"
)

// newBookFixtureStore mirrors newFixtureStore for the books arena: b1 owns a
// full shelf whose every number is checkable by hand, b2 owns one unread book
// and has never logged a session, b3 owns nothing.
//
// The shelf is laid out so each superlative has exactly one winner:
//
//	be1 The Long Walk   backlog  400p  shelved 2019 — oldest unopened
//	be2 Tidewrack       reading  300p  50% in
//	be3 Quiet Machines  read     200p
//	be4 Salt Roads      backlog 1200p  — longest unread
//	be5 Pocket Atlas    backlog   60p  — fits an evening
//	be6 Deep Field      backlog  600p  + 10h of attached audio
//	be7 Harbour Lights  backlog    —   no page count anywhere
//	be8 Winter Errand   reading  999p  but a 450,000-char EPUB → 250p
//	be9 Wanted Thing    wishlist       — owned counts must skip it
func newBookFixtureStore(t *testing.T) *Store {
	t.Helper()
	database, err := db.Open(filepath.Join(t.TempDir(), "books_insights.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	if err := db.Migrate(database); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	s := New(database)

	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := database.Exec(query, args...); err != nil {
			t.Fatalf("seed %q: %v", query, err)
		}
	}

	exec(`INSERT INTO users (id, email, username, password_hash) VALUES
		('b1', 'b1@example.com', 'b1', 'x'),
		('b2', 'b2@example.com', 'b2', 'x'),
		('b3', 'b3@example.com', 'b3', 'x')`)

	type work struct {
		id       string
		title    string
		authors  string
		subjects string
		pages    int // 0 = the printing carries no page count
	}
	addWork := func(w work) {
		t.Helper()
		exec(`INSERT INTO books (id, title, authors_json, subjects_json) VALUES (?, ?, ?, ?)`,
			w.id, w.title, w.authors, w.subjects)
		if w.pages > 0 {
			exec(`INSERT INTO book_editions (id, book_id, page_count, published_year) VALUES (?, ?, ?, 2000)`,
				editionOf(w.id), w.id, w.pages)
			return
		}
		exec(`INSERT INTO book_editions (id, book_id, published_year) VALUES (?, ?, 2000)`,
			editionOf(w.id), w.id)
	}

	const sf = `["Science fiction"]`
	for _, w := range []work{
		{"ob1", "The Long Walk", `["Ana Vance"]`, `["Science fiction","Travel"]`, 400},
		{"ob2", "Tidewrack", `["Ana Vance"]`, sf, 300},
		{"ob3", "Quiet Machines", `["Ana Vance"]`, sf, 200},
		{"ob4", "Salt Roads", `["Bo Iyer"]`, `["History"]`, 1200},
		{"ob5", "Pocket Atlas", `["Bo Iyer"]`, sf, 60},
		{"ob6", "Deep Field", `["Cai Nour"]`, sf, 600},
		{"ob7", "Harbour Lights", `["Cai Nour"]`, `["History"]`, 0},
		{"ob8", "Winter Errand", `["Ana Vance"]`, sf, 999},
		{"ob9", "Wanted Thing", `["Dee Quill"]`, `["History"]`, 100},
		{"ob10", "Sparse Shelf", `["Eli Rook"]`, `["History"]`, 320},
	} {
		addWork(w)
	}

	// stamp renders a timestamp the way CURRENT_TIMESTAMP does, so entries
	// read back as real times rather than bare dates.
	stamp := func(ts time.Time) string { return ts.UTC().Format("2006-01-02 15:04:05") }
	day := func(y int, m time.Month, d int) time.Time {
		return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
	}
	ago := func(days int) time.Time { return time.Now().UTC().AddDate(0, 0, -days) }

	type entry struct {
		id     string
		user   string
		book   string
		status string
		added  time.Time
	}
	for _, e := range []entry{
		{"be1", "b1", "ob1", models.StatusBacklog, day(2019, time.March, 1)},
		{"be2", "b1", "ob2", models.StatusPlaying, day(2021, time.May, 2)},
		{"be3", "b1", "ob3", models.StatusPlayed, day(2020, time.January, 1)},
		{"be4", "b1", "ob4", models.StatusBacklog, day(2022, time.February, 2)},
		{"be5", "b1", "ob5", models.StatusBacklog, day(2023, time.January, 1)},
		{"be6", "b1", "ob6", models.StatusBacklog, day(2023, time.February, 2)},
		{"be7", "b1", "ob7", models.StatusBacklog, day(2023, time.March, 3)},
		{"be8", "b1", "ob8", models.StatusPlaying, day(2018, time.January, 1)},
		{"be9", "b1", "ob9", models.StatusWishlist, day(2024, time.April, 4)},
		{"xb1", "b2", "ob10", models.StatusBacklog, day(2024, time.June, 1)},
	} {
		exec(`INSERT INTO library_entries (id, user_id, media_type, book_id, edition_id, status, created_at)
		      VALUES (?, ?, 'book', ?, ?, ?, ?)`,
			e.id, e.user, e.book, editionOf(e.book), e.status, stamp(e.added))
	}

	// Halfway through Tidewrack: 300 pages owed becomes 150.
	exec(`INSERT INTO book_progress (entry_id, char_offset, char_offset_source, percent_complete)
	      VALUES ('be2', 120000, 'read', 50)`)

	// Deep Field's audiobook: two tracks, five hours each.
	for i, seconds := range []float64{18000, 18000} {
		exec(`INSERT INTO media_files (id, root, path, kind, size_bytes, mtime,
		                               duration_seconds, book_id, track_number, scanned_at)
		      VALUES (?, '/nas', ?, 'audio', 1, 1, ?, 'ob6', ?, CURRENT_TIMESTAMP)`,
			100+i, fmt.Sprintf("/nas/deep-field-%d.m4b", i+1), seconds, i+1)
	}

	// Winter Errand's EPUB: a canonical text is a measured length, so its
	// 450,000 characters (250 pages) must beat the printing's 999.
	exec(`INSERT INTO media_files (id, root, path, kind, size_bytes, mtime, book_id, scanned_at)
	      VALUES (200, '/nas', '/nas/winter-errand.epub', 'epub', 1, 1, 'ob8', CURRENT_TIMESTAMP)`)
	exec(`INSERT INTO epub_texts (id, media_file_id, char_count, word_count, normalized_sha256, parser_version)
	      VALUES ('et1', 200, 450000, 75000, 'sha', 'v1')`)

	// Reading pace: 108,000 characters over exactly one hour of reading —
	// 60 pages an hour at 1,800 characters a page. The listening hour adds
	// time spent on books without touching the reading speed.
	type session struct {
		id      string
		entry   string
		mode    string
		chars   int
		seconds int
		daysAgo int
	}
	for _, rs := range []session{
		{"rs1", "be2", models.ReadingModeRead, 54000, 1800, 5},
		{"rs2", "be8", models.ReadingModeRead, 54000, 1800, 10},
		{"rs3", "be2", models.ReadingModeListen, 0, 3600, 3},
	} {
		start := ago(rs.daysAgo)
		exec(`INSERT INTO reading_sessions (id, user_id, entry_id, started_at, ended_at,
		                                    mode, chars_advanced, seconds)
		      VALUES (?, 'b1', ?, ?, ?, ?, ?, ?)`,
			rs.id, rs.entry, stamp(start), stamp(start.Add(time.Duration(rs.seconds)*time.Second)),
			rs.mode, rs.chars, rs.seconds)
	}

	// Winter Errand has been picked up three separate times and finished
	// none of them; Tidewrack was started once, which is not a pattern.
	for i := 0; i < 3; i++ {
		exec(`INSERT INTO entry_status_history (id, entry_id, user_id, from_status, to_status, changed_at)
		      VALUES (?, 'be8', 'b1', 'backlog', 'playing', ?)`,
			fmt.Sprintf("h%d", i), stamp(day(2019+i, time.June, 1)))
	}
	exec(`INSERT INTO entry_status_history (id, entry_id, user_id, from_status, to_status, changed_at)
	      VALUES ('h9', 'be2', 'b1', 'backlog', 'playing', ?)`, stamp(day(2021, time.June, 1)))

	return s
}

func editionOf(bookID string) string { return bookID + "-ed" }

func bookSuperlativeByKind(insights models.ReadingInsights, kind string) *models.BookSuperlative {
	for i := range insights.Superlatives {
		if insights.Superlatives[i].Kind == kind {
			return &insights.Superlatives[i]
		}
	}
	return nil
}

func TestReadingPaceMeasured(t *testing.T) {
	s := newBookFixtureStore(t)
	pace, err := s.ReadingPace(context.Background(), "b1")
	if err != nil {
		t.Fatalf("ReadingPace: %v", err)
	}

	if !pace.Measured {
		t.Error("Measured = false, want true — an hour of logged reading is enough")
	}
	// 108,000 characters in 3,600 seconds.
	if pace.CharsPerHour != 108000 {
		t.Errorf("CharsPerHour = %v, want 108000", pace.CharsPerHour)
	}
	if pace.PagesPerHour != 60 {
		t.Errorf("PagesPerHour = %v, want 60", pace.PagesPerHour)
	}
	if pace.CharsPerPage != charsPerPage {
		t.Errorf("CharsPerPage = %d, want %d", pace.CharsPerPage, charsPerPage)
	}
	if pace.SessionHours != 1 {
		t.Errorf("SessionHours = %v, want 1", pace.SessionHours)
	}
	// Two hours of books (one read, one listened) over the 90-day window.
	if pace.HoursPerWeek90d == nil {
		t.Fatal("HoursPerWeek90d = nil, want a rate")
	}
	if *pace.HoursPerWeek90d != 0.2 {
		t.Errorf("HoursPerWeek90d = %v, want 0.2", *pace.HoursPerWeek90d)
	}
	if pace.HoursPerWeekAll == nil {
		t.Error("HoursPerWeekAll = nil, want a rate")
	}
}

func TestReadingPaceFallsBackToDefault(t *testing.T) {
	s := newBookFixtureStore(t)
	pace, err := s.ReadingPace(context.Background(), "b2")
	if err != nil {
		t.Fatalf("ReadingPace: %v", err)
	}
	if pace.Measured {
		t.Error("Measured = true, want false — b2 has never logged a session")
	}
	if pace.PagesPerHour != defaultPagesPerHour {
		t.Errorf("PagesPerHour = %v, want the %v default", pace.PagesPerHour, defaultPagesPerHour)
	}
	if pace.HoursPerWeek90d != nil {
		t.Errorf("HoursPerWeek90d = %v, want nil", *pace.HoursPerWeek90d)
	}
}

func TestReadingDebtFullShelf(t *testing.T) {
	s := newBookFixtureStore(t)
	debt, err := s.ReadingDebt(context.Background(), "b1")
	if err != nil {
		t.Fatalf("ReadingDebt: %v", err)
	}

	// Nine entries, one of them wishlisted; seven of the eight owned are
	// still unread.
	if debt.BooksOwned != 8 {
		t.Errorf("BooksOwned = %d, want 8", debt.BooksOwned)
	}
	if debt.UnreadBooks != 7 {
		t.Errorf("UnreadBooks = %d, want 7", debt.UnreadBooks)
	}

	// 400 + (300 × 50%) + 1200 + 60 + 250 = 2060. Deep Field is sized from
	// its audiobook and Harbour Lights from nothing, so neither adds pages.
	if debt.PagesOwed != 2060 {
		t.Errorf("PagesOwed = %v, want 2060", debt.PagesOwed)
	}
	if debt.PageHours != 34.3 { // 2060 / 60 pages an hour
		t.Errorf("PageHours = %v, want 34.3", debt.PageHours)
	}
	if debt.AudioHours != 10 { // two 5-hour tracks, none of it listened to
		t.Errorf("AudioHours = %v, want 10", debt.AudioHours)
	}
	if debt.HoursOwed != 44.3 {
		t.Errorf("HoursOwed = %v, want 44.3", debt.HoursOwed)
	}
	if debt.AudioBooks != 1 {
		t.Errorf("AudioBooks = %d, want 1", debt.AudioBooks)
	}
	if debt.UnsizedBooks != 1 {
		t.Errorf("UnsizedBooks = %d, want 1", debt.UnsizedBooks)
	}
	if debt.ShortBooksHours != 1 { // Pocket Atlas alone: 60 pages
		t.Errorf("ShortBooksHours = %v, want 1", debt.ShortBooksHours)
	}

	// 44.3 hours at 0.2 hrs/week.
	if debt.Projection.CurrentPace == nil {
		t.Fatal("Projection.CurrentPace = nil, want a projection")
	}
	if got := debt.Projection.CurrentPace.Weeks; got != 221.5 {
		t.Errorf("CurrentPace.Weeks = %v, want 221.5", got)
	}
	if len(debt.Projection.Scenarios) != len(readingScenarios) {
		t.Fatalf("got %d scenarios, want %d", len(debt.Projection.Scenarios), len(readingScenarios))
	}
	// 44.3 hours at the middle scenario's 5 hrs/week.
	if got := debt.Projection.Scenarios[1].Weeks; got != 8.9 {
		t.Errorf("5 hrs/week scenario = %v weeks, want 8.9", got)
	}
}

func TestReadingDebtEmptyShelf(t *testing.T) {
	s := newBookFixtureStore(t)
	debt, err := s.ReadingDebt(context.Background(), "b3")
	if err != nil {
		t.Fatalf("ReadingDebt: %v", err)
	}
	if debt.BooksOwned != 0 || debt.UnreadBooks != 0 {
		t.Errorf("owned/unread = %d/%d, want 0/0", debt.BooksOwned, debt.UnreadBooks)
	}
	if debt.PagesOwed != 0 || debt.HoursOwed != 0 {
		t.Errorf("pages/hours = %v/%v, want 0/0", debt.PagesOwed, debt.HoursOwed)
	}
	if debt.Projection.CurrentPace != nil {
		t.Error("CurrentPace set, want nil — nothing has ever been read")
	}
}

func TestReadingInsightsSuperlatives(t *testing.T) {
	s := newBookFixtureStore(t)
	insights, err := s.ReadingInsights(context.Background(), "b1")
	if err != nil {
		t.Fatalf("ReadingInsights: %v", err)
	}

	if insights.Headline.PagesOwed != 2060 || insights.Headline.HoursOwed != 44.3 {
		t.Errorf("headline = %+v, want 2060 pages / 44.3 hours", insights.Headline)
	}
	if insights.Headline.YearsAtCurrentRate == nil {
		t.Fatal("YearsAtCurrentRate = nil, want a projection")
	}
	if got := *insights.Headline.YearsAtCurrentRate; got != 4.3 { // 221.5 / 52
		t.Errorf("YearsAtCurrentRate = %v, want 4.3", got)
	}
	if !insights.Pace.Measured || insights.Pace.PagesPerHour != 60 {
		t.Errorf("pace = %+v, want a measured 60 pages/hour", insights.Pace)
	}
	if got, want := len(insights.Superlatives), 5; got != want {
		t.Fatalf("got %d superlatives, want %d: %+v", got, want, insights.Superlatives)
	}

	oldest := bookSuperlativeByKind(insights, models.BookSuperlativeOldestUnopened)
	if oldest == nil {
		t.Fatal("missing oldest_unopened superlative")
	}
	if oldest.Payload.EntryID != "be1" || oldest.Payload.Book == nil ||
		oldest.Payload.Book.Title != "The Long Walk" {
		t.Errorf("oldest_unopened = %+v, want be1 / The Long Walk", oldest.Payload)
	}
	if want := "Shelved 2019 · never opened"; oldest.Label != want {
		t.Errorf("oldest_unopened label = %q, want %q", oldest.Label, want)
	}

	longest := bookSuperlativeByKind(insights, models.BookSuperlativeLongestUnread)
	if longest == nil {
		t.Fatal("missing longest_unread superlative")
	}
	if longest.Payload.EntryID != "be4" {
		t.Errorf("longest_unread entry = %q, want be4", longest.Payload.EntryID)
	}
	if longest.Payload.Pages == nil || *longest.Payload.Pages != 1200 {
		t.Errorf("longest_unread pages = %v, want 1200", longest.Payload.Pages)
	}
	if want := "1,200 pages · 20h at your pace"; longest.Label != want {
		t.Errorf("longest_unread label = %q, want %q", longest.Label, want)
	}

	author := bookSuperlativeByKind(insights, models.BookSuperlativeUnreadAuthor)
	if author == nil {
		t.Fatal("missing unread_author superlative")
	}
	if want := "Ana Vance — 4 books / 1 read"; author.Label != want {
		t.Errorf("unread_author label = %q, want %q", author.Label, want)
	}

	subject := bookSuperlativeByKind(insights, models.BookSuperlativeNeglectedSubject)
	if subject == nil {
		t.Fatal("missing neglected_subject superlative")
	}
	if want := "Science fiction — 6 books / 1 read"; subject.Label != want {
		t.Errorf("neglected_subject label = %q, want %q", subject.Label, want)
	}

	restarted := bookSuperlativeByKind(insights, models.BookSuperlativeRestarted)
	if restarted == nil {
		t.Fatal("missing restarted superlative")
	}
	if restarted.Payload.EntryID != "be8" || restarted.Payload.Starts != 3 {
		t.Errorf("restarted = %+v, want be8 started 3 times", restarted.Payload)
	}
	if want := "Started 3 separate times · still unfinished"; restarted.Label != want {
		t.Errorf("restarted label = %q, want %q", restarted.Label, want)
	}
}

// A shelf below every threshold must produce no superlatives rather than
// crowning a winner out of two books.
func TestReadingInsightsThinShelf(t *testing.T) {
	s := newBookFixtureStore(t)
	insights, err := s.ReadingInsights(context.Background(), "b2")
	if err != nil {
		t.Fatalf("ReadingInsights: %v", err)
	}
	if insights.Headline.BooksOwned != 1 || insights.Headline.UnreadBooks != 1 {
		t.Errorf("headline = %+v, want one owned and unread", insights.Headline)
	}
	// 320 pages at the 40 pages/hour default.
	if insights.Headline.HoursOwed != 8 {
		t.Errorf("HoursOwed = %v, want 8", insights.Headline.HoursOwed)
	}
	kinds := []string{}
	for _, sup := range insights.Superlatives {
		kinds = append(kinds, sup.Kind)
	}
	// Only the two that need no bucket threshold can fire on one book.
	want := []string{models.BookSuperlativeOldestUnopened, models.BookSuperlativeLongestUnread}
	if strings.Join(kinds, ",") != strings.Join(want, ",") {
		t.Errorf("superlatives = %v, want %v", kinds, want)
	}
}

func TestReadingPicksFullShelf(t *testing.T) {
	s := newBookFixtureStore(t)
	picks, err := s.ReadingPicks(context.Background(), "b1", 90, nil)
	if err != nil {
		t.Fatalf("ReadingPicks: %v", err)
	}

	// Tidewrack is the only in-progress book that was read recently.
	if picks.Continue == nil || picks.Continue.Entry.ID != "be2" {
		t.Fatalf("Continue = %+v, want be2", picks.Continue)
	}
	if !strings.Contains(picks.Continue.Reason, "50% in") {
		t.Errorf("Continue reason = %q, want the position in it", picks.Continue.Reason)
	}

	// Pocket Atlas is 60 pages — one hour at 60 pages an hour, inside a
	// 90-minute evening. Everything else on the shelf is longer.
	if picks.ShortWin == nil || picks.ShortWin.Entry.ID != "be5" {
		t.Fatalf("ShortWin = %+v, want be5", picks.ShortWin)
	}
	if !strings.Contains(picks.ShortWin.Reason, "~1h to read") {
		t.Errorf("ShortWin reason = %q, want the estimate", picks.ShortWin.Reason)
	}

	// Nothing on the shelf is rated, so the wildcard falls to the
	// alphabetical tiebreak — which is what makes the picks reproducible.
	if picks.Wildcard == nil || picks.Wildcard.Entry.ID != "be6" {
		t.Fatalf("Wildcard = %+v, want be6", picks.Wildcard)
	}

	// The rescue is the guilt pick and cannot repeat a book already claimed.
	if picks.Rescue == nil || picks.Rescue.Entry.ID != "be1" {
		t.Fatalf("Rescue = %+v, want be1", picks.Rescue)
	}
	if !strings.Contains(picks.Rescue.Reason, "never opened") {
		t.Errorf("Rescue reason = %q, want the guilt", picks.Rescue.Reason)
	}
}

func TestReadingPicksExclude(t *testing.T) {
	s := newBookFixtureStore(t)
	picks, err := s.ReadingPicks(context.Background(), "b1", 90, []string{"be5"})
	if err != nil {
		t.Fatalf("ReadingPicks: %v", err)
	}
	// Excluding the only book that fits leaves the category empty rather
	// than promoting something that does not fit.
	if picks.ShortWin != nil {
		t.Errorf("ShortWin = %+v, want nil once be5 is excluded", picks.ShortWin)
	}
	if picks.Continue == nil || picks.Continue.Entry.ID != "be2" {
		t.Errorf("Continue = %+v, want be2 still", picks.Continue)
	}
}

func TestReadingPicksEmptyShelf(t *testing.T) {
	s := newBookFixtureStore(t)
	picks, err := s.ReadingPicks(context.Background(), "b3", 60, nil)
	if err != nil {
		t.Fatalf("ReadingPicks: %v", err)
	}
	if picks.Continue != nil || picks.ShortWin != nil || picks.Wildcard != nil || picks.Rescue != nil {
		t.Errorf("picks = %+v, want every category empty", picks)
	}
}

// --- pure scorer ------------------------------------------------------

var readingNow = time.Date(2026, 8, 25, 20, 0, 0, 0, time.UTC)

// readCand builds a scorer candidate with the fields the heuristics read.
func readCand(title, status string, mutators ...func(*readingCandidate)) readingCandidate {
	c := readingCandidate{entry: models.Entry{ID: title, Status: status, Book: &models.Book{Title: title}}}
	for _, m := range mutators {
		m(&c)
	}
	return c
}

func withRemaining(hours float64) func(*readingCandidate) {
	return func(c *readingCandidate) { c.remaining = &hours }
}

func withPercent(percent float64) func(*readingCandidate) {
	return func(c *readingCandidate) { c.percent = percent }
}

func withShelfDays(days float64) func(*readingCandidate) {
	return func(c *readingCandidate) { c.ownedDays = days }
}

func withLastRead(daysAgo float64) func(*readingCandidate) {
	return func(c *readingCandidate) {
		t := readingNow.AddDate(0, 0, -int(daysAgo))
		c.lastRead = &t
	}
}

func withReadQueueRank(rank int) func(*readingCandidate) {
	return func(c *readingCandidate) { c.queueRank = rank }
}

func withReaderRating(rating int) func(*readingCandidate) {
	return func(c *readingCandidate) { c.entry.UserRating = &rating }
}

func TestSelectReadingPicksCategories(t *testing.T) {
	candidates := []readingCandidate{
		readCand("Stalled", models.StatusPlaying, withRemaining(20), withLastRead(40), withPercent(10)),
		readCand("Fresh", models.StatusPlaying, withRemaining(1), withLastRead(1), withPercent(80)),
		readCand("Novella", models.StatusBacklog, withRemaining(1), withReadQueueRank(1)),
		readCand("Doorstop", models.StatusBacklog, withRemaining(30), withReadQueueRank(2)),
		readCand("Buried", models.StatusBacklog, withRemaining(6), withReadQueueRank(40), withReaderRating(9)),
		readCand("Ancient", models.StatusBacklog, withRemaining(8), withShelfDays(3000)),
	}

	picks := selectReadingPicks(candidates, 90, nil, readingNow)

	// Recency plus a book you could finish tonight beats a stalled tome.
	if picks.Continue == nil || picks.Continue.Entry.Book.Title != "Fresh" {
		t.Fatalf("Continue = %+v, want Fresh", picks.Continue)
	}
	// Novella fits the 90-minute budget and sits at the top of the queue.
	if picks.ShortWin == nil || picks.ShortWin.Entry.Book.Title != "Novella" {
		t.Fatalf("ShortWin = %+v, want Novella", picks.ShortWin)
	}
	// The wildcard ignores fit: rated highly and buried out of sight.
	if picks.Wildcard == nil || picks.Wildcard.Entry.Book.Title != "Buried" {
		t.Fatalf("Wildcard = %+v, want Buried", picks.Wildcard)
	}
	// The rescue is the longest-owned, least-read thing still available.
	if picks.Rescue == nil || picks.Rescue.Entry.Book.Title != "Ancient" {
		t.Fatalf("Rescue = %+v, want Ancient", picks.Rescue)
	}
}

func TestSelectReadingPicksNeverRepeats(t *testing.T) {
	// One book, eligible for every category it can be: it may only be
	// claimed once.
	only := readCand("Only Book", models.StatusBacklog, withRemaining(0.5), withReadQueueRank(1), withShelfDays(4000))
	picks := selectReadingPicks([]readingCandidate{only}, 90, nil, readingNow)

	if picks.ShortWin == nil {
		t.Fatal("ShortWin = nil, want the only book")
	}
	if picks.Wildcard != nil || picks.Rescue != nil {
		t.Errorf("book reused: wildcard=%v rescue=%v", picks.Wildcard, picks.Rescue)
	}
}

func TestSelectReadingPicksSkipsUnsized(t *testing.T) {
	// A book with no known length can still be continued and rescued, but it
	// can never be the short win — "fits in your evening" is a claim about a
	// length nobody knows.
	unsized := readCand("No Length", models.StatusBacklog, withShelfDays(500))
	picks := selectReadingPicks([]readingCandidate{unsized}, 90, nil, readingNow)

	if picks.ShortWin != nil {
		t.Errorf("ShortWin = %+v, want nil for an unsized book", picks.ShortWin)
	}
	if picks.Wildcard == nil || picks.Wildcard.Entry.Book.Title != "No Length" {
		t.Errorf("Wildcard = %+v, want the unsized book", picks.Wildcard)
	}
}

func TestGroupThousands(t *testing.T) {
	for _, tt := range []struct {
		in   int
		want string
	}{{7, "7"}, {999, "999"}, {1000, "1,000"}, {1216, "1,216"}, {1234567, "1,234,567"}} {
		if got := groupThousands(tt.in); got != tt.want {
			t.Errorf("groupThousands(%d) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
