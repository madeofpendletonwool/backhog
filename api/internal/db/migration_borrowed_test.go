package db

import (
	"path/filepath"
	"testing"

	"github.com/pressly/goose/v3"
)

// TestBorrowedCopiesMigration verifies 00022: physical_copies learns
// provenance — acquisition owned/borrowed with a CHECK, a nullable due
// date, a nullable return stamp — existing rows default to owned, all
// three columns round-trip, and the down half removes them.
func TestBorrowedCopiesMigration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "borrowed_copies_test.db")
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
		`INSERT INTO book_editions (id, book_id) VALUES ('OL2M', 'OL1W')`,
		`INSERT INTO library_entries (id, user_id, media_type, book_id, edition_id, status)
			VALUES ('e1', 'u1', 'book', 'OL1W', 'OL1M', 'backlog')`,
	}
	for _, q := range seed {
		if _, err := database.Exec(q); err != nil {
			t.Fatalf("seed %q: %v", q, err)
		}
	}

	// Pre-existing rows are owned printings, and say so now.
	if _, err := database.Exec(`
		INSERT INTO physical_copies (id, user_id, entry_id, edition_id)
		VALUES ('c1', 'u1', 'e1', 'OL1M')`); err != nil {
		t.Fatalf("insert legacy copy: %v", err)
	}
	var acquisition string
	var dueAt, returnedAt any
	if err := database.QueryRow(
		`SELECT acquisition, due_at, returned_at FROM physical_copies WHERE id = 'c1'`).
		Scan(&acquisition, &dueAt, &returnedAt); err != nil {
		t.Fatalf("read legacy copy: %v", err)
	}
	if acquisition != "owned" || dueAt != nil || returnedAt != nil {
		t.Errorf("legacy copy = (%q, %v, %v), want (owned, NULL, NULL)",
			acquisition, dueAt, returnedAt)
	}

	// A library checkout of a second printing round-trips: borrowed, due
	// on a date, in hand.
	if _, err := database.Exec(`
		INSERT INTO physical_copies (id, user_id, entry_id, edition_id, acquisition, due_at, returned_at)
		VALUES ('c2', 'u1', 'e1', 'OL2M', 'borrowed', '2026-09-12 00:00:00', '2026-08-30 16:00:00')`); err != nil {
		t.Fatalf("insert borrowed copy: %v", err)
	}
	var gotDue, gotReturned string
	if err := database.QueryRow(
		`SELECT due_at, returned_at FROM physical_copies WHERE id = 'c2'`).
		Scan(&gotDue, &gotReturned); err != nil {
		t.Fatalf("read borrowed copy: %v", err)
	}
	if gotDue != "2026-09-12T00:00:00Z" || gotReturned != "2026-08-30T16:00:00Z" {
		t.Errorf("borrowed copy round-trip = (%q, %q)", gotDue, gotReturned)
	}

	// The value set is closed: 'lent_out' is documentation, not a value.
	if _, err := database.Exec(`
		INSERT INTO physical_copies (id, user_id, entry_id, edition_id, acquisition)
		VALUES ('c3', 'u1', 'e1', 'OL2M', 'lent_out')`); err == nil {
		t.Error("an unknown acquisition was accepted")
	}

	// Down drops all three columns and leaves the copies themselves.
	if err := goose.DownTo(database, "migrations", 21); err != nil {
		t.Fatalf("migrate down: %v", err)
	}
	for _, column := range []string{"acquisition", "due_at", "returned_at"} {
		var n int
		if err := database.QueryRow(
			`SELECT COUNT(*) FROM pragma_table_info('physical_copies') WHERE name = ?`,
			column).Scan(&n); err != nil {
			t.Fatalf("table_info %s: %v", column, err)
		}
		if n != 0 {
			t.Errorf("column %s survived the down migration", column)
		}
	}
	var copies int
	if err := database.QueryRow(`SELECT COUNT(*) FROM physical_copies`).Scan(&copies); err != nil {
		t.Fatalf("count copies after down: %v", err)
	}
	if copies != 2 {
		t.Errorf("copies after down = %d, want the 2 rows untouched", copies)
	}
}
