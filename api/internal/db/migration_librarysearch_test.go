package db

import (
	"path/filepath"
	"testing"

	"github.com/pressly/goose/v3"
)

// TestLibraryTextSearchMigration verifies 00033: the FTS5 table lands,
// matches chapter rows by phrase, deletes by its unindexed keys, and the
// down half drops it whole.
func TestLibraryTextSearchMigration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "library_text_search_test.db")
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
	if err := Migrate(database); err != nil {
		t.Fatalf("migrate up: %v", err)
	}

	seed := []string{
		`INSERT INTO book_text_fts (body, book_id, media_file_id)
			VALUES ('the quiet village slept under the hill', 'OL1W', 1)`,
		`INSERT INTO book_text_fts (body, book_id, media_file_id)
			VALUES ('the butler did it by morning', 'OL1W', 1)`,
		`INSERT INTO book_text_fts (body, book_id, media_file_id)
			VALUES ('another village entirely', 'OL2W', 2)`,
	}
	for _, q := range seed {
		if _, err := database.Exec(q); err != nil {
			t.Fatalf("seed %q: %v", q, err)
		}
	}

	var n int
	if err := database.QueryRow(
		`SELECT count(*) FROM book_text_fts WHERE book_text_fts MATCH ?`, `"quiet village"`).Scan(&n); err != nil {
		t.Fatalf("phrase match: %v", err)
	}
	if n != 1 {
		t.Errorf("phrase match = %d rows, want 1", n)
	}
	// The exact fold: diacritics kept means café is not cafe, matching the
	// canonical text the rows were sliced from.
	if _, err := database.Exec(
		`INSERT INTO book_text_fts (body) VALUES ('café au lait')`); err != nil {
		t.Fatalf("seed café: %v", err)
	}
	if err := database.QueryRow(
		`SELECT count(*) FROM book_text_fts WHERE book_text_fts MATCH ?`, `cafe`).Scan(&n); err != nil {
		t.Fatalf("cafe match: %v", err)
	}
	if n != 0 {
		t.Error("cafe matched café — the tokenizer must keep diacritics")
	}

	// Replacement is delete-by-key: a re-parse empties its file's rows
	// before the new chapter text lands.
	if _, err := database.Exec(
		`DELETE FROM book_text_fts WHERE media_file_id = 1`); err != nil {
		t.Fatalf("delete by media file: %v", err)
	}
	if err := database.QueryRow(
		`SELECT count(*) FROM book_text_fts WHERE book_id = 'OL1W'`).Scan(&n); err != nil {
		t.Fatalf("count after delete: %v", err)
	}
	if n != 0 {
		t.Errorf("rows survived their media file's delete: %d", n)
	}

	// Down: the table goes whole.
	if err := goose.DownTo(database, "migrations", 32); err != nil {
		t.Fatalf("migrate down to 32: %v", err)
	}
	var tables int
	if err := database.QueryRow(
		`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'book_text_fts'`).Scan(&tables); err != nil {
		t.Fatalf("probe table: %v", err)
	}
	if tables != 0 {
		t.Error("book_text_fts survived the down migration")
	}
}
