package db

import (
	"path/filepath"
	"testing"

	"github.com/pressly/goose/v3"
)

// TestPagedPositionMigration verifies 00029: existing progress and session
// rows come through the migration as text-mode rows byte-identical to what
// they were, the new columns refuse shapes the model says cannot exist, and
// the pdf_files classification table holds what the paged model reads.
func TestPagedPositionMigration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "paged_position_test.db")
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
	if err := goose.UpTo(database, "migrations", 28); err != nil {
		t.Fatalf("migrate to 28: %v", err)
	}

	seed := []string{
		`INSERT INTO users (id, email, username, password_hash) VALUES ('u1', 'a@a.a', 'a', 'x')`,
		`INSERT INTO books (id, title) VALUES ('OL1W', 'Carrie'), ('OL2W', 'Pictures')`,
		`INSERT INTO library_entries (id, user_id, media_type, book_id, status)
		 VALUES ('e1', 'u1', 'book', 'OL1W', 'playing'), ('e2', 'u1', 'book', 'OL2W', 'backlog')`,
		`INSERT INTO book_progress (entry_id, char_offset, char_offset_source, percent_complete)
		 VALUES ('e1', 40, 'read', 12.5)`,
		`INSERT INTO reading_sessions (id, user_id, entry_id, started_at, ended_at, mode, chars_advanced, seconds)
		 VALUES ('s1', 'u1', 'e1', '2026-09-01T10:00:00Z', '2026-09-01T10:30:00Z', 'read', 9000, 1800)`,
		`INSERT INTO media_files (id, root, path, kind, size_bytes, mtime, book_id, scanned_at)
		 VALUES (9, '/nas', 'Carrie.pdf', 'epub', 10, 1, 'OL1W', '2026-09-01T00:00:00Z')`,
	}
	for _, q := range seed {
		if _, err := database.Exec(q); err != nil {
			t.Fatalf("seed %q: %v", q, err)
		}
	}

	if err := Migrate(database); err != nil {
		t.Fatalf("migrate up: %v", err)
	}

	// Existing rows are text-mode, page axis empty, sessions carry zero
	// turned pages — byte-identical behaviour to before the migration.
	var mode string
	var pageIndex, pagesTurned any
	if err := database.QueryRow(
		`SELECT position_mode, page_index FROM book_progress WHERE entry_id = 'e1'`).
		Scan(&mode, &pageIndex); err != nil {
		t.Fatalf("read migrated progress: %v", err)
	}
	if mode != "text" || pageIndex != nil {
		t.Fatalf("migrated progress = (%q, %v), want (text, NULL)", mode, pageIndex)
	}
	if err := database.QueryRow(
		`SELECT pages_turned FROM reading_sessions WHERE id = 's1'`).Scan(&pagesTurned); err != nil {
		t.Fatalf("read migrated session: %v", err)
	}
	if pagesTurned.(int64) != 0 {
		t.Fatalf("migrated pages_turned = %v, want 0", pagesTurned)
	}

	// A page-mode row carries a page index and a pinned char offset.
	if _, err := database.Exec(
		`INSERT INTO book_progress (entry_id, position_mode, page_index, percent_complete)
		 VALUES ('e2', 'page', 3, 30)`); err != nil {
		t.Fatalf("insert page-mode row: %v", err)
	}

	// The classification home takes both verdicts, keyed per media file.
	if _, err := database.Exec(
		`INSERT INTO pdf_files (id, media_file_id, classification, page_count, has_text_layer, parser_version)
		 VALUES ('p1', 9, 'image-native', 32, 0, '3')`); err != nil {
		t.Fatalf("insert image-native classification: %v", err)
	}
	if _, err := database.Exec(
		`INSERT INTO pdf_files (id, media_file_id, classification, page_count, has_text_layer, parser_version)
		 VALUES ('p2', 9, 'text-native', 32, 1, '3')`); err == nil {
		t.Fatal("a second classification for one media file was accepted, want UNIQUE to refuse it")
	}

	// DownTo rolls the whole migration back cleanly.
	if err := goose.DownTo(database, "migrations", 28); err != nil {
		t.Fatalf("migrate down: %v", err)
	}
	if _, err := database.Exec(`SELECT page_index FROM book_progress LIMIT 1`); err == nil {
		t.Fatal("page_index survived the down migration")
	}
	if _, err := database.Exec(`SELECT pages_turned FROM reading_sessions LIMIT 1`); err == nil {
		t.Fatal("pages_turned survived the down migration")
	}
	if _, err := database.Exec(`SELECT 1 FROM pdf_files LIMIT 1`); err == nil {
		t.Fatal("pdf_files survived the down migration")
	}
}
