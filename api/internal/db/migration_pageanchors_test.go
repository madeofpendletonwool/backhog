package db

import (
	"path/filepath"
	"testing"

	"github.com/pressly/goose/v3"
)

// TestPageAnchorsMigration verifies 00018: one copy per (user, entry,
// edition), anchors unique per printed page with their CHECK constraints,
// both tables cascading with the entry and the edition, and a down half
// that removes them.
func TestPageAnchorsMigration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "page_anchors_test.db")
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
		`INSERT INTO book_editions (id, book_id, page_count) VALUES ('OL1M', 'OL1W', 614)`,
		`INSERT INTO library_entries (id, user_id, media_type, book_id, edition_id, status)
			VALUES ('e1', 'u1', 'book', 'OL1W', 'OL1M', 'backlog')`,
	}
	for _, q := range seed {
		if _, err := database.Exec(q); err != nil {
			t.Fatalf("seed %q: %v", q, err)
		}
	}

	// A copy of a printing, and the same printing twice is a conflict:
	// the page map is a property of the printing, not of the lump of paper.
	if _, err := database.Exec(`
		INSERT INTO physical_copies (id, user_id, entry_id, edition_id, notes)
		VALUES ('c1', 'u1', 'e1', 'OL1M', 'paperback, water damage')`); err != nil {
		t.Fatalf("insert copy: %v", err)
	}
	if _, err := database.Exec(`
		INSERT INTO physical_copies (id, user_id, entry_id, edition_id)
		VALUES ('c2', 'u1', 'e1', 'OL1M')`); err == nil {
		t.Error("a second copy of the same printing for the same entry was accepted")
	}

	for _, bad := range []struct {
		what string
		q    string
	}{
		{"unknown entry", `INSERT INTO physical_copies (id, user_id, entry_id, edition_id)
			VALUES ('c3', 'u1', 'nope', 'OL1M')`},
		{"unknown edition", `INSERT INTO physical_copies (id, user_id, entry_id, edition_id)
			VALUES ('c3', 'u1', 'e1', 'OL9M')`},
		{"unknown user", `INSERT INTO physical_copies (id, user_id, entry_id, edition_id)
			VALUES ('c3', 'nobody', 'e1', 'OL1M')`},
	} {
		if _, err := database.Exec(bad.q); err == nil {
			t.Errorf("%s was accepted", bad.what)
		}
	}

	// Anchors: a page maps to one offset, and the CHECKs hold.
	if _, err := database.Exec(`
		INSERT INTO page_anchors (physical_copy_id, printed_page, char_offset, source, confidence)
		VALUES ('c1', 1, 0, 'ocr', 0.93)`); err != nil {
		t.Fatalf("insert anchor: %v", err)
	}
	if _, err := database.Exec(`
		INSERT INTO page_anchors (physical_copy_id, printed_page, char_offset, source)
		VALUES ('c1', 1, 500, 'manual')`); err == nil {
		t.Error("a second anchor for the same page was accepted")
	}
	for _, bad := range []struct {
		what string
		q    string
	}{
		{"page zero", `INSERT INTO page_anchors (physical_copy_id, printed_page, char_offset, source)
			VALUES ('c1', 0, 10, 'ocr')`},
		{"negative offset", `INSERT INTO page_anchors (physical_copy_id, printed_page, char_offset, source)
			VALUES ('c1', 2, -1, 'ocr')`},
		{"unknown source", `INSERT INTO page_anchors (physical_copy_id, printed_page, char_offset, source)
			VALUES ('c1', 2, 10, 'divination')`},
		{"confidence over 1", `INSERT INTO page_anchors (physical_copy_id, printed_page, char_offset, source, confidence)
			VALUES ('c1', 2, 10, 'ocr', 1.5)`},
		{"negative confidence", `INSERT INTO page_anchors (physical_copy_id, printed_page, char_offset, source, confidence)
			VALUES ('c1', 2, 10, 'ocr', -0.1)`},
		{"unknown copy", `INSERT INTO page_anchors (physical_copy_id, printed_page, char_offset, source)
			VALUES ('nope', 2, 10, 'ocr')`},
	} {
		if _, err := database.Exec(bad.q); err == nil {
			t.Errorf("%s was accepted", bad.what)
		}
	}

	// Deleting the entry takes the copies and their anchors with it.
	if _, err := database.Exec(`DELETE FROM library_entries WHERE id = 'e1'`); err != nil {
		t.Fatalf("delete entry: %v", err)
	}
	for _, table := range []string{"physical_copies", "page_anchors"} {
		var n int
		if err := database.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&n); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		if n != 0 {
			t.Errorf("%s kept %d rows after the entry was deleted", table, n)
		}
	}

	// The edition cascade is the defensive one: a copy without its
	// printing is page numbers of nothing.
	if _, err := database.Exec(`
		INSERT INTO library_entries (id, user_id, media_type, book_id, edition_id, status)
		VALUES ('e2', 'u1', 'book', 'OL1W', 'OL1M', 'backlog')`); err != nil {
		t.Fatalf("re-seed entry: %v", err)
	}
	if _, err := database.Exec(`
		INSERT INTO physical_copies (id, user_id, entry_id, edition_id)
		VALUES ('c9', 'u1', 'e2', 'OL1M')`); err != nil {
		t.Fatalf("insert copy 2: %v", err)
	}
	if _, err := database.Exec(`DELETE FROM book_editions WHERE id = 'OL1M'`); err != nil {
		t.Fatalf("delete edition: %v", err)
	}
	var copies int
	if err := database.QueryRow(`SELECT COUNT(*) FROM physical_copies`).Scan(&copies); err != nil {
		t.Fatalf("count copies after edition delete: %v", err)
	}
	if copies != 0 {
		t.Errorf("copies survived their edition being deleted")
	}

	// Down removes both tables and leaves the rest of the schema intact.
	if err := goose.DownTo(database, "migrations", 17); err != nil {
		t.Fatalf("migrate down: %v", err)
	}
	for _, table := range []string{"physical_copies", "page_anchors"} {
		var name string
		err := database.QueryRow(
			`SELECT name FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&name)
		if err == nil {
			t.Errorf("%s survived the down migration", table)
		}
	}
	var entries int
	if err := database.QueryRow(`SELECT COUNT(*) FROM library_entries`).Scan(&entries); err != nil {
		t.Errorf("library_entries after down: %v", err)
	}
}
