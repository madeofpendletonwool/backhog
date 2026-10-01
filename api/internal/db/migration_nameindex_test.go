package db

import (
	"path/filepath"
	"testing"

	"github.com/pressly/goose/v3"
)

// TestNameIndexMigration verifies 00034: occurrences land keyed by media
// file, the currency column arrives empty on epub_texts, hidden names are
// per user per book, and the down half drops all three.
func TestNameIndexMigration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "name_index_test.db")
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
		`INSERT INTO books (id, title) VALUES ('OL1W', 'Mansfield Park')`,
		`INSERT INTO media_files (root, path, kind, size_bytes, mtime, book_id, scanned_at)
			VALUES ('/nas', 'book.epub', 'epub', 1, 0, 'OL1W', 0)`,
		`INSERT INTO name_occurrences (media_file_id, name, display, char_start, char_end)
			VALUES (1, 'elizabeth', 'Elizabeth', 10, 19)`,
		`INSERT INTO name_occurrences (media_file_id, name, display, char_start, char_end)
			VALUES (1, 'elizabeth', 'Elizabeth', 40, 49)`,
		`INSERT INTO name_occurrences (media_file_id, name, display, char_start, char_end)
			VALUES (1, 'mrs norris', 'Mrs Norris', 60, 70)`,
		`INSERT INTO epub_texts (id, media_file_id, char_count, word_count,
			normalized_sha256, parser_version)
			VALUES ('t1', 1, 100, 20, 'sha', '3')`,
	}
	for _, q := range seed {
		if _, err := database.Exec(q); err != nil {
			t.Fatalf("seed %q: %v", q, err)
		}
	}

	// The currency column: present, and empty until an extractor pass says
	// otherwise — a fresh parse has no name index at all.
	var namesVersion string
	if err := database.QueryRow(
		`SELECT names_version FROM epub_texts WHERE id = 't1'`).Scan(&namesVersion); err != nil {
		t.Fatalf("read names_version: %v", err)
	}
	if namesVersion != "" {
		t.Errorf("names_version = %q, want empty on a fresh parse", namesVersion)
	}

	// Occurrences group by their folded key and keep their display form.
	var mentions int
	if err := database.QueryRow(
		`SELECT count(*) FROM name_occurrences WHERE media_file_id = 1 AND name = 'elizabeth'`,
	).Scan(&mentions); err != nil {
		t.Fatalf("count occurrences: %v", err)
	}
	if mentions != 2 {
		t.Errorf("elizabeth occurrences = %d, want 2", mentions)
	}

	// A hidden name is one user's fact about one book: the same name can
	// be hidden for one reader and live for another.
	seedUsers := []string{
		`INSERT INTO users (id, email, username, password_hash) VALUES
			('u1', 'a@example.com', 'a', 'x')`,
		`INSERT INTO hidden_book_names (user_id, book_id, name) VALUES ('u1', 'OL1W', 'mrs norris')`,
	}
	for _, q := range seedUsers {
		if _, err := database.Exec(q); err != nil {
			t.Fatalf("seed %q: %v", q, err)
		}
	}
	if _, err := database.Exec(
		`INSERT INTO hidden_book_names (user_id, book_id, name) VALUES ('u1', 'OL1W', 'mrs norris')`,
	); err == nil {
		t.Error("duplicate hidden name inserted — the PK must hold one row per (user, book, name)")
	}

	// Down: all three landings go.
	if err := goose.DownTo(database, "migrations", 33); err != nil {
		t.Fatalf("migrate down to 33: %v", err)
	}
	for _, table := range []string{"name_occurrences", "hidden_book_names"} {
		var n int
		if err := database.QueryRow(
			`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, table,
		).Scan(&n); err != nil {
			t.Fatalf("probe %s: %v", table, err)
		}
		if n != 0 {
			t.Errorf("%s survived the down migration", table)
		}
	}
	var cols int
	if err := database.QueryRow(
		`SELECT COUNT(*) FROM pragma_table_info('epub_texts') WHERE name = 'names_version'`,
	).Scan(&cols); err != nil {
		t.Fatalf("probe column: %v", err)
	}
	if cols != 0 {
		t.Error("names_version survived the down migration")
	}
}
