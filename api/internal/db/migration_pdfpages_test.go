package db

import (
	"path/filepath"
	"testing"

	"github.com/pressly/goose/v3"
)

// TestPDFPageSeedMigration verifies 00030: pdf_pages carries a text-native
// PDF's per-page ranges keyed by media file with its CHECKs and cascade,
// and page_anchors learns the 'pdf' seed source through a rebuild that
// keeps every existing row. The down half drops the page rows and folds
// the seed source back out.
func TestPDFPageSeedMigration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pdf_page_seed_test.db")
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
		`INSERT INTO users (id, email, username, password_hash) VALUES ('u1', 'a@a.a', 'a', 'x')`,
		`INSERT INTO books (id, title) VALUES ('OL1W', 'Anathem')`,
		`INSERT INTO book_editions (id, book_id) VALUES ('OL1M', 'OL1W')`,
		`INSERT INTO library_entries (id, user_id, media_type, book_id, status)
			VALUES ('e1', 'u1', 'book', 'OL1W', 'backlog')`,
		`INSERT INTO media_files (id, root, path, kind, size_bytes, mtime, scanned_at)
			VALUES (50, '/nas', 'book.pdf', 'epub', 10, 0, CURRENT_TIMESTAMP)`,
		`INSERT INTO physical_copies (id, user_id, entry_id, edition_id)
			VALUES ('c1', 'u1', 'e1', 'OL1M')`,
	}
	for _, q := range seed {
		if _, err := database.Exec(q); err != nil {
			t.Fatalf("seed %q: %v", q, err)
		}
	}

	// Page ranges: one row per (file, page), ranges never inverted.
	for _, q := range []string{
		`INSERT INTO pdf_pages (media_file_id, page_number, char_start, char_end)
			VALUES (50, 1, 0, 100)`,
		`INSERT INTO pdf_pages (media_file_id, page_number, char_start, char_end)
			VALUES (50, 2, 100, 100)`,
	} {
		if _, err := database.Exec(q); err != nil {
			t.Fatalf("insert pdf page: %v", err)
		}
	}
	for _, bad := range []struct {
		what string
		q    string
	}{
		{"page zero", `INSERT INTO pdf_pages (media_file_id, page_number, char_start, char_end)
			VALUES (50, 0, 0, 5)`},
		{"negative start", `INSERT INTO pdf_pages (media_file_id, page_number, char_start, char_end)
			VALUES (50, 3, -1, 5)`},
		{"inverted range", `INSERT INTO pdf_pages (media_file_id, page_number, char_start, char_end)
			VALUES (50, 3, 50, 40)`},
		{"duplicate page", `INSERT INTO pdf_pages (media_file_id, page_number, char_start, char_end)
			VALUES (50, 1, 0, 99)`},
		{"unknown file", `INSERT INTO pdf_pages (media_file_id, page_number, char_start, char_end)
			VALUES (99, 1, 0, 5)`},
	} {
		if _, err := database.Exec(bad.q); err == nil {
			t.Errorf("%s was accepted", bad.what)
		}
	}

	// The seed source is writable next to the scan sources, and the
	// rebuild kept the table's other rules intact.
	if _, err := database.Exec(`
		INSERT INTO page_anchors (physical_copy_id, printed_page, char_offset, source, confidence)
		VALUES ('c1', 3, 120, 'pdf', 0.3)`); err != nil {
		t.Fatalf("insert pdf-seeded anchor: %v", err)
	}
	if _, err := database.Exec(`
		INSERT INTO page_anchors (physical_copy_id, printed_page, char_offset, source)
		VALUES ('c1', 4, 160, 'manual')`); err != nil {
		t.Fatalf("insert manual anchor: %v", err)
	}
	if _, err := database.Exec(`
		INSERT INTO page_anchors (physical_copy_id, printed_page, char_offset, source)
		VALUES ('c1', 5, 200, 'divination')`); err == nil {
		t.Error("an unknown source was accepted after the rebuild")
	}

	// The page rows go when the file row goes — they are that parse's.
	if _, err := database.Exec(`DELETE FROM media_files WHERE id = 50`); err != nil {
		t.Fatalf("delete file: %v", err)
	}
	var n int
	if err := database.QueryRow(`SELECT COUNT(*) FROM pdf_pages`).Scan(&n); err != nil {
		t.Fatalf("count pdf_pages: %v", err)
	}
	if n != 0 {
		t.Errorf("pdf_pages kept %d rows after the file was deleted", n)
	}

	// Down drops the page table and folds the seed source back out,
	// keeping every scan-made anchor.
	if err := goose.DownTo(database, "migrations", 29); err != nil {
		t.Fatalf("migrate down: %v", err)
	}
	var name string
	if err := database.QueryRow(
		`SELECT name FROM sqlite_master WHERE type = 'table' AND name = ?`, "pdf_pages").Scan(&name); err == nil {
		t.Error("pdf_pages survived the down migration")
	}
	var anchors int
	if err := database.QueryRow(`SELECT COUNT(*) FROM page_anchors`).Scan(&anchors); err != nil {
		t.Fatalf("count anchors after down: %v", err)
	}
	if anchors != 1 {
		t.Errorf("anchors after down = %d, want the manual row alone (seeds fold out)", anchors)
	}
}
