package db

import (
	"path/filepath"
	"testing"

	"github.com/pressly/goose/v3"
)

// TestMediaEntriesMigration verifies 00010 on an existing database: game rows
// survive the rebuild as media_type='game', the subject-column CHECK and the
// partial unique indexes hold, and the down half collapses back to game-only
// rows.
func TestMediaEntriesMigration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "media_entries_test.db")
	database, err := Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer database.Close()

	goose.SetBaseFS(migrations)
	goose.SetLogger(goose.NopLogger())
	if err := goose.SetDialect("sqlite3"); err != nil {
		t.Fatalf("set dialect: %v", err)
	}

	// Stand up the pre-media schema and populate it the way a real database
	// would look: two users sharing the games cache, entries in various
	// statuses, and rows in the tables that reference library_entries.
	if err := goose.UpTo(database, "migrations", 9); err != nil {
		t.Fatalf("migrate to 9: %v", err)
	}
	seed := []string{
		`INSERT INTO users (id, email, username, password_hash) VALUES
			('u1', 'a@a.a', 'a', 'x'), ('u2', 'b@b.b', 'b', 'x')`,
		`INSERT INTO games (id, name) VALUES (1, 'Hollow Knight'), (2, 'Disco Elysium')`,
		`INSERT INTO library_entries (id, user_id, game_id, status, queue_position)
			VALUES ('e1', 'u1', 1, 'backlog', 1024), ('e2', 'u1', 2, 'played', NULL),
			       ('e3', 'u2', 1, 'playing', NULL)`,
		`INSERT INTO play_sessions (id, user_id, entry_id, played_on, minutes)
			VALUES ('s1', 'u1', 'e2', '2026-01-02', 90)`,
		`INSERT INTO lists (id, user_id, name, kind) VALUES ('l1', 'u1', 'Mine', 'manual')`,
		`INSERT INTO list_items (list_id, entry_id, position) VALUES ('l1', 'e1', 1024)`,
	}
	for _, q := range seed {
		if _, err := database.Exec(q); err != nil {
			t.Fatalf("seed %q: %v", q, err)
		}
	}

	if err := Migrate(database); err != nil {
		t.Fatalf("migrate up: %v", err)
	}

	// Every pre-existing row must have landed as a game, still visible to the
	// tables that reference entries.
	var media string
	var sessions, items, bad int
	if err := database.QueryRow(
		`SELECT media_type FROM library_entries WHERE id = 'e1'`).Scan(&media); err != nil {
		t.Fatalf("read e1: %v", err)
	}
	if media != "game" {
		t.Errorf("e1 media_type = %q, want %q", media, "game")
	}
	if err := database.QueryRow(
		`SELECT COUNT(*) FROM library_entries WHERE media_type <> 'game' OR game_id IS NULL OR book_id IS NOT NULL`).Scan(&bad); err != nil {
		t.Fatalf("count bad rows: %v", err)
	}
	if bad != 0 {
		t.Errorf("%d migrated rows are not clean game rows", bad)
	}
	if err := database.QueryRow(
		`SELECT COUNT(*) FROM play_sessions`).Scan(&sessions); err != nil || sessions != 1 {
		t.Errorf("play_sessions after up = %d (err %v), want 1", sessions, err)
	}
	if err := database.QueryRow(
		`SELECT COUNT(*) FROM list_items`).Scan(&items); err != nil || items != 1 {
		t.Errorf("list_items after up = %d (err %v), want 1", items, err)
	}
	if rows, err := database.Query(`PRAGMA foreign_key_check`); err != nil {
		t.Fatalf("foreign_key_check: %v", err)
	} else {
		for rows.Next() {
			t.Error("foreign_key_check reported a violation after the rebuild")
		}
		rows.Close()
	}

	// The subject CHECK: a game row needs game_id, a book row needs book_id,
	// and media_type must match whichever is set.
	for _, q := range []string{
		`INSERT INTO library_entries (id, user_id, media_type, game_id, status) VALUES ('bad1', 'u1', 'game', NULL, 'backlog')`,
		`INSERT INTO library_entries (id, user_id, media_type, book_id, status) VALUES ('bad2', 'u1', 'book', NULL, 'backlog')`,
		`INSERT INTO library_entries (id, user_id, media_type, book_id, status) VALUES ('bad3', 'u1', 'game', 'oliver', 'backlog')`,
		`INSERT INTO library_entries (id, user_id, media_type, game_id, status) VALUES ('bad4', 'u1', 'zine', 1, 'backlog')`,
	} {
		if _, err := database.Exec(q); err == nil {
			t.Errorf("constraint rejected nothing for %q", q)
		}
	}

	// The partial unique indexes are the real per-media guard: the same game
	// twice for one user fails, the same game for another user is fine.
	if _, err := database.Exec(
		`INSERT INTO library_entries (id, user_id, media_type, game_id, status) VALUES ('dup', 'u1', 'game', 1, 'wishlist')`); err == nil {
		t.Error("duplicate (user, game) was allowed")
	}

	// Down: game rows survive, a book row is dropped rather than folded.
	if _, err := database.Exec(
		`INSERT INTO library_entries (id, user_id, media_type, book_id, status) VALUES ('b1', 'u2', 'book', 'isbn-1', 'backlog')`); err != nil {
		t.Fatalf("insert book row: %v", err)
	}
	if err := goose.DownTo(database, "migrations", 9); err != nil {
		t.Fatalf("migrate down to 9: %v", err)
	}
	var gameRows int
	if err := database.QueryRow(
		`SELECT COUNT(*) FROM library_entries`).Scan(&gameRows); err != nil {
		t.Fatalf("count after down: %v", err)
	}
	if gameRows != 3 {
		t.Errorf("game rows after down = %d, want 3 (book row dropped)", gameRows)
	}
	if err := database.QueryRow(
		`SELECT COUNT(*) FROM library_entries WHERE game_id IS NULL`).Scan(&bad); err != nil {
		t.Fatalf("count nulls after down: %v", err)
	}
	if bad != 0 {
		t.Errorf("%d rows with NULL game_id after down", bad)
	}
}
