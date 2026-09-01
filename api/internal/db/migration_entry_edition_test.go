package db

import (
	"path/filepath"
	"testing"

	"github.com/pressly/goose/v3"
)

// TestEntryEditionMigration verifies 00014: the nullable edition_id lands on
// library_entries with its FK to book_editions, existing rows carry NULL, a
// book entry can anchor a printing, deleting the edition detaches rather than
// cascading, and the down half drops the column.
func TestEntryEditionMigration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "edition_test.db")
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

	if err := goose.UpTo(database, "migrations", 13); err != nil {
		t.Fatalf("migrate to 13: %v", err)
	}
	seed := []string{
		`INSERT INTO users (id, email, username, password_hash) VALUES ('u1', 'a@a.a', 'a', 'x')`,
		`INSERT INTO games (id, name) VALUES (1, 'Hollow Knight')`,
		`INSERT INTO library_entries (id, user_id, media_type, game_id, status)
			VALUES ('e1', 'u1', 'game', 1, 'backlog')`,
	}
	for _, q := range seed {
		if _, err := database.Exec(q); err != nil {
			t.Fatalf("seed %q: %v", q, err)
		}
	}

	if err := Migrate(database); err != nil {
		t.Fatalf("migrate up: %v", err)
	}

	// Existing rows survive with a NULL printing.
	var nulls int
	if err := database.QueryRow(
		`SELECT COUNT(*) FROM library_entries WHERE edition_id IS NULL`).Scan(&nulls); err != nil || nulls != 1 {
		t.Fatalf("NULL edition_id rows after up = %d (err %v), want 1", nulls, err)
	}

	// A book entry can anchor an edition; the FK holds.
	book := []string{
		`INSERT INTO books (id, title) VALUES ('OL1W', 'Dune')`,
		`INSERT INTO book_editions (id, book_id, page_count) VALUES ('OL1M', 'OL1W', 412)`,
		`INSERT INTO library_entries (id, user_id, media_type, book_id, edition_id, status)
			VALUES ('e2', 'u1', 'book', 'OL1W', 'OL1M', 'backlog')`,
	}
	for _, q := range book {
		if _, err := database.Exec(q); err != nil {
			t.Fatalf("seed book %q: %v", q, err)
		}
	}
	if _, err := database.Exec(
		`INSERT INTO library_entries (id, user_id, media_type, book_id, edition_id, status)
			VALUES ('bad', 'u1', 'book', 'OL1W', 'OL0M', 'backlog')`); err == nil {
		t.Error("edition FK did not reject an unknown printing")
	}

	// Deleting the edition detaches the entry (SET NULL), never deletes it.
	if _, err := database.Exec(`DELETE FROM book_editions WHERE id = 'OL1M'`); err != nil {
		t.Fatalf("delete edition: %v", err)
	}
	var stillThere int
	if err := database.QueryRow(
		`SELECT COUNT(*) FROM library_entries WHERE id = 'e2' AND edition_id IS NULL`).Scan(&stillThere); err != nil || stillThere != 1 {
		t.Errorf("entry after edition delete = %d (err %v), want detached but present", stillThere, err)
	}

	// Down drops the column.
	if err := goose.DownTo(database, "migrations", 13); err != nil {
		t.Fatalf("migrate down: %v", err)
	}
	if _, err := database.Exec(
		`SELECT edition_id FROM library_entries LIMIT 1`); err == nil {
		t.Error("edition_id still queryable after down")
	}
}
