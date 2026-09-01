package db

import (
	"path/filepath"
	"testing"

	"github.com/pressly/goose/v3"
)

// TestBooksMigration verifies 00011 on an existing database: the books and
// book_editions cache tables arrive, the deferred library_entries.book_id FK
// lands via the rebuild, game rows survive, and the down half collapses back
// to the 00010 shape with the cache tables dropped.
func TestBooksMigration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "books_test.db")
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

	// Stand up the pre-books schema the way a real database would look after
	// 00010: game entries, plus one hand-made book entry that can only be an
	// orphan (the books table does not exist yet).
	if err := goose.UpTo(database, "migrations", 10); err != nil {
		t.Fatalf("migrate to 10: %v", err)
	}
	seed := []string{
		`INSERT INTO users (id, email, username, password_hash) VALUES
			('u1', 'a@a.a', 'a', 'x'), ('u2', 'b@b.b', 'b', 'x')`,
		`INSERT INTO games (id, name) VALUES (1, 'Hollow Knight'), (2, 'Disco Elysium')`,
		`INSERT INTO library_entries (id, user_id, media_type, game_id, status)
			VALUES ('e1', 'u1', 'game', 1, 'backlog'), ('e2', 'u1', 'game', 2, 'played'),
			       ('e3', 'u2', 'game', 1, 'playing')`,
		`INSERT INTO library_entries (id, user_id, media_type, book_id, status)
			VALUES ('orphan', 'u2', 'book', 'OL1W', 'backlog')`,
		`INSERT INTO play_sessions (id, user_id, entry_id, played_on, minutes)
			VALUES ('s1', 'u1', 'e2', '2026-01-02', 90)`,
	}
	for _, q := range seed {
		if _, err := database.Exec(q); err != nil {
			t.Fatalf("seed %q: %v", q, err)
		}
	}

	if err := Migrate(database); err != nil {
		t.Fatalf("migrate up: %v", err)
	}

	// Game rows survive the rebuild untouched; the orphan book row is dropped
	// (documented in the migration: it can never have pointed at a real book).
	var games, orphans, sessions int
	if err := database.QueryRow(
		`SELECT COUNT(*) FROM library_entries WHERE media_type = 'game'`).Scan(&games); err != nil || games != 3 {
		t.Errorf("game rows after up = %d (err %v), want 3", games, err)
	}
	if err := database.QueryRow(
		`SELECT COUNT(*) FROM library_entries WHERE media_type = 'book'`).Scan(&orphans); err != nil || orphans != 0 {
		t.Errorf("book rows after up = %d (err %v), want 0 (orphans dropped)", orphans, err)
	}
	if err := database.QueryRow(
		`SELECT COUNT(*) FROM play_sessions`).Scan(&sessions); err != nil || sessions != 1 {
		t.Errorf("play_sessions after up = %d (err %v), want 1", sessions, err)
	}

	// The cache tables work: a book, an edition, and an entry pointing at the
	// book through the new FK.
	year := 1937
	pages := 310
	if _, err := database.Exec(
		`INSERT INTO books (id, title, first_publish_year) VALUES ('OL1168083W', 'The Hobbit', ?)`, year); err != nil {
		t.Fatalf("insert book: %v", err)
	}
	if _, err := database.Exec(
		`INSERT INTO book_editions (id, book_id, isbn13, publisher, page_count) VALUES ('OL7440402M', 'OL1168083W', '9780261102217', 'HarperCollins', ?)`, pages); err != nil {
		t.Fatalf("insert edition: %v", err)
	}
	if _, err := database.Exec(
		`INSERT INTO library_entries (id, user_id, media_type, book_id, status) VALUES ('b1', 'u2', 'book', 'OL1168083W', 'backlog')`); err != nil {
		t.Fatalf("insert book entry: %v", err)
	}

	// The FK actually enforces: an unknown book_id is rejected.
	if _, err := database.Exec(
		`INSERT INTO library_entries (id, user_id, media_type, book_id, status) VALUES ('bad', 'u1', 'book', 'OL999W', 'backlog')`); err == nil {
		t.Error("entry with unknown book_id was allowed")
	}
	// And editions cascade with their work.
	if _, err := database.Exec(`DELETE FROM books WHERE id = 'OL1168083W'`); err != nil {
		t.Fatalf("delete book: %v", err)
	}
	var editions int
	if err := database.QueryRow(
		`SELECT COUNT(*) FROM book_editions`).Scan(&editions); err != nil || editions != 0 {
		t.Errorf("editions after book delete = %d (err %v), want 0 (cascade)", editions, err)
	}

	if rows, err := database.Query(`PRAGMA foreign_key_check`); err != nil {
		t.Fatalf("foreign_key_check: %v", err)
	} else {
		for rows.Next() {
			t.Error("foreign_key_check reported a violation after the rebuild")
		}
		rows.Close()
	}

	// Down: the cache tables go away, game and book rows alike survive the
	// rebuild back to the 00010 shape (book_id logical again).
	if _, err := database.Exec(
		`INSERT INTO books (id, title) VALUES ('OL2W', 'Another')`); err != nil {
		t.Fatalf("insert second book: %v", err)
	}
	if _, err := database.Exec(
		`INSERT INTO library_entries (id, user_id, media_type, book_id, status) VALUES ('b2', 'u1', 'book', 'OL2W', 'wishlist')`); err != nil {
		t.Fatalf("insert second book entry: %v", err)
	}
	if err := goose.DownTo(database, "migrations", 10); err != nil {
		t.Fatalf("migrate down to 10: %v", err)
	}
	var total int
	if err := database.QueryRow(
		`SELECT COUNT(*) FROM library_entries`).Scan(&total); err != nil || total != 4 {
		t.Errorf("entries after down = %d (err %v), want 4 (3 games + 1 book)", total, err)
	}
	if _, err := database.Exec(
		`SELECT 1 FROM books`); err == nil {
		t.Error("books table still exists after down")
	}
	if _, err := database.Exec(
		`SELECT 1 FROM book_editions`); err == nil {
		t.Error("book_editions table still exists after down")
	}
	if rows, err := database.Query(`PRAGMA foreign_key_check`); err != nil {
		t.Fatalf("foreign_key_check after down: %v", err)
	} else {
		for rows.Next() {
			t.Error("foreign_key_check reported a violation after down")
		}
		rows.Close()
	}
}
