package store

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/collinpendleton/backhog/api/internal/db"
	"github.com/collinpendleton/backhog/api/internal/models"
)

// newBooksStore opens a migrated store seeded with u1's reading history:
// ten finished books through the year (the first owned six years and 700
// pages, the third held in all three formats, the tenth twice-abandoned),
// an honest drop of a three-year read, ten shelf warmers, and one finished
// game so the two arenas can be watched for bleeding into each other.
// u2 owns nothing, for scoping checks.
func newBooksStore(t *testing.T) *Store {
	t.Helper()
	database, err := db.Open(filepath.Join(t.TempDir(), "book_achievements.db"))
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
		('u1', 'u1@example.com', 'u1', 'x'),
		('u2', 'u2@example.com', 'u2', 'x')`)

	year := time.Now().UTC().Year()
	ymd := func(offsetYears int, monthDay string) string {
		return fmt.Sprintf("%04d-%s 12:00:00", year+offsetYears, monthDay)
	}

	// Works: the authors matter for the season test (Author A owns two,
	// both finished this year; every other author owns at most one
	// finished book, so nobody else clears the two-book bar).
	for _, b := range []struct{ id, author string }{
		{"OL1W", "Author A"}, {"OL2W", "Author A"}, {"OL3W", "Author B"},
		{"OL4W", "Author Z"}, {"OL5W", "Author C"}, {"OL6W", "Author D"},
		{"OL7W", "Author E"}, {"OL8W", "Author F"}, {"OL9W", "Author G"},
		{"OL10W", "Author H"}, {"OL11W", "Author I"},
	} {
		exec(`INSERT INTO books (id, title, authors_json) VALUES (?, ?, ?)`,
			b.id, "Book "+b.id, fmt.Sprintf(`["%s"]`, b.author))
	}
	for i := 20; i < 30; i++ {
		exec(`INSERT INTO books (id, title, authors_json) VALUES (?, ?, '[]')`,
			fmt.Sprintf("OL%dW", i), fmt.Sprintf("Filler %d", i))
	}

	// Editions: b1's printing is a 700-page doorstop; b3's carries the
	// physical copy the page map hangs off.
	exec(`INSERT INTO book_editions (id, book_id, page_count) VALUES ('OL1M', 'OL1W', 700)`)
	exec(`INSERT INTO book_editions (id, book_id, page_count) VALUES ('OL3M', 'OL3W', 300)`)

	// The finishes, in completion order. b1 is the six-year-old doorstop;
	// b2 is this year's only acquisition, so finishes outrun buys early.
	addPlayed := func(id, bookID, created, finished string) {
		t.Helper()
		exec(`INSERT INTO library_entries (id, user_id, media_type, book_id, status, created_at, finished_at)
			VALUES (?, 'u1', 'book', ?, 'played', ?, ?)`, id, bookID, created, finished)
	}
	addPlayed("b1", "OL1W", ymd(-6, "01-01"), ymd(0, "01-10"))
	exec(`UPDATE library_entries SET edition_id = 'OL1M' WHERE id = 'b1'`)
	addPlayed("b2", "OL2W", ymd(0, "01-01"), ymd(0, "02-01"))
	addPlayed("b3", "OL3W", ymd(-1, "03-01"), ymd(0, "03-01"))
	addPlayed("b4", "OL4W", ymd(-1, "04-01"), ymd(0, "04-01"))
	addPlayed("b5", "OL5W", ymd(-1, "05-01"), ymd(0, "05-01"))
	addPlayed("b6", "OL6W", ymd(-1, "06-01"), ymd(0, "06-01"))
	addPlayed("b7", "OL7W", ymd(-1, "07-01"), ymd(0, "07-01"))
	addPlayed("b8", "OL8W", ymd(-1, "08-01"), ymd(0, "08-01"))
	addPlayed("b9", "OL9W", ymd(-1, "09-01"), ymd(0, "09-01"))
	addPlayed("b10", "OL10W", ymd(-1, "10-01"), ymd(0, "10-01"))

	// b10's two abandonments: the comeback arcs the charm predicate reads.
	for _, h := range []struct{ id, from, to, at string }{
		{"h1", models.StatusBacklog, models.StatusDropped, ymd(-2, "01-01")},
		{"h2", models.StatusDropped, models.StatusPlaying, ymd(-2, "02-01")},
		{"h3", models.StatusPlaying, models.StatusDropped, ymd(-1, "01-01")},
		{"h4", models.StatusDropped, models.StatusPlaying, ymd(-1, "02-01")},
	} {
		exec(`INSERT INTO entry_status_history (id, entry_id, user_id, from_status, to_status, changed_at)
			VALUES (?, 'b10', 'u1', ?, ?, ?)`, h.id, h.from, h.to, h.at)
	}

	// The honest drop: owned three years, 'reading' for most of them.
	exec(`INSERT INTO library_entries (id, user_id, media_type, book_id, status, created_at, started_at, finished_at)
		VALUES ('d1', 'u1', 'book', 'OL11W', 'dropped', ?, ?, ?)`,
		ymd(-3, "01-01"), ymd(-3, "06-01"), ymd(0, "05-15"))

	// Ten shelf warmers holding the unread pile's peak up.
	for i := 20; i < 30; i++ {
		exec(`INSERT INTO library_entries (id, user_id, media_type, book_id, status, created_at)
			VALUES (?, 'u1', 'book', ?, 'backlog', ?)`,
			fmt.Sprintf("p%d", i), fmt.Sprintf("OL%dW", i), ymd(-10, "01-01"))
	}

	// b3 in all three formats: a physical copy, an attached EPUB, and an
	// attached audiobook.
	exec(`INSERT INTO media_files (id, root, path, kind, size_bytes, mtime, book_id, scanned_at)
		VALUES (11, '/nas', 'b3/epub.epub', 'epub', 1, 1, 'OL3W', CURRENT_TIMESTAMP)`)
	exec(`INSERT INTO media_files (id, root, path, kind, size_bytes, mtime, book_id, scanned_at)
		VALUES (12, '/nas', 'b3/audio.m4b', 'audio', 1, 1, 'OL3W', CURRENT_TIMESTAMP)`)
	exec(`INSERT INTO physical_copies (id, user_id, entry_id, edition_id)
		VALUES ('pc1', 'u1', 'b3', 'OL3M')`)

	// The page map: 25 scanned pages of that copy — the cartographer's
	// whole journey, one anchor at a time.
	for page := 1; page <= 25; page++ {
		exec(`INSERT INTO page_anchors (physical_copy_id, printed_page, char_offset, source)
			VALUES ('pc1', ?, ?, 'ocr')`, page, page*1800)
	}

	// One finished game, so the arenas can be watched for cross-talk.
	exec(`INSERT INTO games (id, name) VALUES (1, 'Game A')`)
	exec(`INSERT INTO library_entries (id, user_id, media_type, game_id, status, created_at, finished_at)
		VALUES ('g1', 'u1', 'game', 1, 'played', ?, ?)`, ymd(-5, "01-01"), ymd(0, "01-15"))

	// Reading sessions: 10 pages read and 2 hours listened this year,
	// 20 pages read last year for the scoping check.
	exec(`INSERT INTO reading_sessions (id, user_id, entry_id, started_at, ended_at, mode, chars_advanced, seconds)
		VALUES ('rs1', 'u1', 'b2', ?, ?, 'read', 18000, 1800)`, ymd(0, "02-01"), ymd(0, "02-01"))
	exec(`INSERT INTO reading_sessions (id, user_id, entry_id, started_at, ended_at, mode, chars_advanced, seconds)
		VALUES ('rs2', 'u1', 'b2', ?, ?, 'listen', 0, 7200)`, ymd(0, "02-02"), ymd(0, "02-02"))
	exec(`INSERT INTO reading_sessions (id, user_id, entry_id, started_at, ended_at, mode, chars_advanced, seconds)
		VALUES ('rs3', 'u1', 'b1', ?, ?, 'read', 36000, 3600)`, ymd(-1, "02-01"), ymd(-1, "02-01"))

	return s
}

func TestBookAchievementsBackfill(t *testing.T) {
	s := newBooksStore(t)
	ctx := context.Background()

	if _, err := s.Achievements(ctx, "u1"); err != nil {
		t.Fatalf("Achievements: %v", err)
	}

	// The ledger's own book rows, straight from the domain annotation.
	database := s.DB()
	rows, err := database.QueryContext(ctx,
		`SELECT achievement_id, COALESCE(entry_id, '') FROM achievement_unlocks
		 WHERE user_id = 'u1' AND domain = 'book' ORDER BY achievement_id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	bookUnlocks := map[string]string{}
	for rows.Next() {
		var id, entryID string
		if err := rows.Scan(&id, &entryID); err != nil {
			t.Fatal(err)
		}
		bookUnlocks[id] = entryID
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}

	want := map[string]string{
		"b1":                    "b1", // placeholder overwritten below
		"first_edition":         "b1",
		"shelf_improvement":     "b5",
		"well_read":             "b10",
		"late_fine":             "b1",
		"doorstop":              "b1",
		"third_times_the_charm": "b10",
		"every_which_way":       "b3",
		// The line crossing lands on b9: by September ten of the pile's
		// members (eight finishes, the drop, b10 still open) have closed.
		"tbr_trim":      "b9",
		"breaking_even": "b2",
		"honest_dnf":    "d1",
		"cartographer":  "",
	}
	delete(want, "b1")
	if len(bookUnlocks) != len(want) {
		t.Fatalf("book unlocks %v, want exactly %v", bookUnlocks, want)
	}
	for id, entryID := range want {
		if bookUnlocks[id] != entryID {
			t.Errorf("%s attached to %q, want %q", id, bookUnlocks[id], entryID)
		}
	}

	// The arenas stay in their lanes: every game unlock belongs to a game
	// entry, every book unlock to a book entry.
	rows, err = database.QueryContext(ctx, `
		SELECT u.achievement_id, u.domain, COALESCE(u.entry_id, ''), e.media_type
		FROM achievement_unlocks u
		LEFT JOIN library_entries e ON e.id = u.entry_id
		WHERE u.user_id = 'u1'`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var id, domain, entryID string
		var mediaType *string
		if err := rows.Scan(&id, &domain, &entryID, &mediaType); err != nil {
			t.Fatal(err)
		}
		switch {
		case entryID == "":
			continue // lazy unlocks carry no entry
		case mediaType == nil || *mediaType != domain:
			t.Errorf("%s (domain %s) attached to entry %q of media type %v",
				id, domain, entryID, mediaType)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}

	// Idempotent: re-running the gallery adds nothing.
	var before, after int
	if err := database.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM achievement_unlocks WHERE user_id = 'u1'`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Achievements(ctx, "u1"); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM achievement_unlocks WHERE user_id = 'u1'`).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if before != after {
		t.Errorf("second gallery pass changed unlock count %d → %d", before, after)
	}

	// An empty shelf stays locked.
	u2, err := s.Achievements(ctx, "u2")
	if err != nil {
		t.Fatal(err)
	}
	for _, st := range u2 {
		if st.UnlockedAt != nil {
			t.Errorf("u2 has %s unlocked with an empty library", st.ID)
		}
	}
}

func TestBookFinishLiveUnlocks(t *testing.T) {
	ctx := context.Background()
	database, err := db.Open(filepath.Join(t.TempDir(), "book_live.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	if err := db.Migrate(database); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	s := New(database)

	// A fresh shelf: one six-year-old 700-page backlog book, nothing else.
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := database.Exec(query, args...); err != nil {
			t.Fatalf("seed %q: %v", query, err)
		}
	}
	exec(`INSERT INTO users (id, email, username, password_hash) VALUES ('u1', 'u1@example.com', 'u1', 'x')`)
	exec(`INSERT INTO books (id, title, authors_json) VALUES ('OL50W', 'Live', '[]')`)
	exec(`INSERT INTO book_editions (id, book_id, page_count) VALUES ('OL50M', 'OL50W', 700)`)
	year := time.Now().UTC().Year()
	exec(`INSERT INTO library_entries (id, user_id, media_type, book_id, edition_id, status, created_at)
		VALUES ('b50', 'u1', 'book', 'OL50W', 'OL50M', 'backlog', ?)`,
		fmt.Sprintf("%04d-01-01 12:00:00", year-6))

	_, unlocks, err := s.UpdateEntry(ctx, "u1", "b50", EntryUpdate{Status: strptr(models.StatusPlayed)})
	if err != nil {
		t.Fatalf("UpdateEntry: %v", err)
	}
	got := unlockedIDs(unlocks)
	want := map[string]string{
		"first_edition": "b50",
		"late_fine":     "b50",
		"doorstop":      "b50",
		// The book's only finish this year beats the shelf's zero buys.
		"breaking_even": "b50",
	}
	if len(got) != len(want) {
		t.Fatalf("live unlocks = %v, want %v", got, want)
	}
	for id, entryID := range want {
		if got[id] != entryID {
			t.Errorf("%s = %q, want %q", id, got[id], entryID)
		}
	}
	// No game achievement rides a book finish.
	for _, gameOnly := range []string{"first_blood", "speedrun", "dusty_relic"} {
		if _, ok := got[gameOnly]; ok {
			t.Errorf("%s fired on a book finish", gameOnly)
		}
	}
}

func TestReadingSeason(t *testing.T) {
	s := newBooksStore(t)
	ctx := context.Background()

	year := time.Now().UTC().Year()
	season, err := s.ReadingSeason(ctx, "u1", year)
	if err != nil {
		t.Fatalf("ReadingSeason: %v", err)
	}
	if season.Year != year {
		t.Errorf("Year = %d, want %d", season.Year, year)
	}
	if season.BooksFinished != 10 {
		t.Errorf("BooksFinished = %d, want 10", season.BooksFinished)
	}
	if season.PagesRead != 10 {
		t.Errorf("PagesRead = %d, want 10", season.PagesRead)
	}
	if season.HoursListened != 2 {
		t.Errorf("HoursListened = %v, want 2", season.HoursListened)
	}
	if season.Rescues != 1 {
		t.Errorf("Rescues = %d, want 1 (the six-year-old doorstop)", season.Rescues)
	}
	if season.AuthorsCleared != 1 {
		t.Errorf("AuthorsCleared = %d, want 1 (Author A, both books read)", season.AuthorsCleared)
	}

	prev, err := s.ReadingSeason(ctx, "u1", year-1)
	if err != nil {
		t.Fatalf("ReadingSeason prev: %v", err)
	}
	if prev.BooksFinished != 0 || prev.PagesRead != 20 || prev.HoursListened != 0 ||
		prev.Rescues != 0 || prev.AuthorsCleared != 0 {
		t.Errorf("prev season = %+v, want 0 finishes / 20 pages / 0h / 0 rescues / 0 authors", prev)
	}

	empty, err := s.ReadingSeason(ctx, "u2", year)
	if err != nil {
		t.Fatalf("ReadingSeason empty: %v", err)
	}
	if empty.BooksFinished != 0 || empty.PagesRead != 0 || empty.HoursListened != 0 ||
		empty.AuthorsCleared != 0 || empty.Rescues != 0 {
		t.Errorf("empty season = %+v, want all zeros", empty)
	}
}
