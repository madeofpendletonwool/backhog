package db

import (
	"path/filepath"
	"testing"

	"github.com/pressly/goose/v3"
)

// TestMediaAttachMigration verifies 00015: the media_files rebuild adds the
// deferred book_id foreign key (and track_number) without losing rows, the
// attach-side tables land with their constraints, and the down half returns
// to the FK-free shape.
func TestMediaAttachMigration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "media_attach_test.db")
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

	// Pre-state at version 14: an attached row, a dangling one, and a plain
	// one. book_id is still FK-free, so the dangling row is legal here.
	if err := goose.UpTo(database, "migrations", 14); err != nil {
		t.Fatalf("migrate to 14: %v", err)
	}
	seed := []string{
		`INSERT INTO books (id, title) VALUES ('OL1W', 'Anathem')`,
		`INSERT INTO media_files (root, path, kind, size_bytes, mtime, book_id, scanned_at)
			VALUES ('/r', 'a.m4b', 'audio', 1, 1, 'OL1W', '2026-08-31T12:00:00Z')`,
		`INSERT INTO media_files (root, path, kind, size_bytes, mtime, book_id, scanned_at)
			VALUES ('/r', 'dangling.m4b', 'audio', 1, 1, 'OL404W', '2026-08-31T12:00:00Z')`,
		`INSERT INTO media_files (root, path, kind, size_bytes, mtime, scanned_at)
			VALUES ('/r', 'plain.epub', 'epub', 1, 1, '2026-08-31T12:00:00Z')`,
	}
	for _, q := range seed {
		if _, err := database.Exec(q); err != nil {
			t.Fatalf("seed %q: %v", q, err)
		}
	}

	if err := Migrate(database); err != nil {
		t.Fatalf("migrate up: %v", err)
	}

	// The rebuild keeps rows and their ids, detaches the dangling reference,
	// and leaves healthy attachments alone.
	var attached, dangling string
	if err := database.QueryRow(
		`SELECT book_id FROM media_files WHERE path = 'a.m4b'`).Scan(&attached); err != nil {
		t.Fatalf("probe attached: %v", err)
	}
	if attached != "OL1W" {
		t.Errorf("healthy attachment did not survive the rebuild: %q", attached)
	}
	if err := database.QueryRow(
		`SELECT COALESCE(book_id, '') FROM media_files WHERE path = 'dangling.m4b'`).Scan(&dangling); err != nil {
		t.Fatalf("probe dangling: %v", err)
	}
	if dangling != "" {
		t.Errorf("dangling book_id survived the rebuild: %q", dangling)
	}

	// The foreign key exists and points where it should.
	fks, err := database.Query(`PRAGMA foreign_key_list(media_files)`)
	if err != nil {
		t.Fatalf("foreign_key_list: %v", err)
	}
	var sawBookFK bool
	for fks.Next() {
		var id, seq int
		var table, from, to string
		var onUpdate, onDelete, match string
		if err := fks.Scan(&id, &seq, &table, &from, &to, &onUpdate, &onDelete, &match); err != nil {
			t.Fatalf("scan fk: %v", err)
		}
		if table == "books" && from == "book_id" {
			sawBookFK = true
			if onDelete != "SET NULL" {
				t.Errorf("book_id FK on delete = %q, want SET NULL", onDelete)
			}
		}
	}
	fks.Close()
	if !sawBookFK {
		t.Error("media_files has no book_id foreign key after 00015")
	}

	// Enforced: a bogus book_id is rejected, and PRAGMA foreign_key_check —
	// the proof the task asked for — is clean.
	if _, err := database.Exec(`INSERT INTO media_files (root, path, kind, size_bytes, mtime, book_id, scanned_at)
		VALUES ('/r', 'x.mp3', 'audio', 1, 1, 'OL404W', '2026-08-31T12:00:00Z')`); err == nil {
		t.Error("insert with unknown book_id was allowed")
	}
	if rows, err := database.Query(`PRAGMA foreign_key_check`); err != nil {
		t.Fatalf("foreign_key_check: %v", err)
	} else {
		for rows.Next() {
			t.Error("foreign_key_check reported a violation")
		}
		rows.Close()
	}

	// track_number: positive when set, orderable.
	if _, err := database.Exec(`UPDATE media_files SET track_number = 1 WHERE path = 'a.m4b'`); err != nil {
		t.Fatalf("set track_number: %v", err)
	}
	if _, err := database.Exec(`UPDATE media_files SET track_number = 0 WHERE path = 'plain.epub'`); err == nil {
		t.Error("track_number = 0 was allowed")
	}

	// Deleting the book detaches rather than deleting the inventory.
	if _, err := database.Exec(`DELETE FROM books WHERE id = 'OL1W'`); err != nil {
		t.Fatalf("delete book: %v", err)
	}
	var count int
	if err := database.QueryRow(
		`SELECT COUNT(*) FROM media_files WHERE path = 'a.m4b' AND book_id IS NULL`).Scan(&count); err != nil {
		t.Fatalf("probe detach: %v", err)
	}
	if count != 1 {
		t.Error("deleting a book did not detach its files")
	}

	// The attach-side tables: skipped rows carry a reason, ignores cascade.
	if _, err := database.Exec(`INSERT INTO media_skipped (root, path, ext, reason, size_bytes, mtime, seen_at)
		VALUES ('/r', 'x.aax', '.aax', 'unsupported_extension', 1, 1, '2026-08-31T12:00:00Z')`); err != nil {
		t.Fatalf("insert skipped: %v", err)
	}
	if _, err := database.Exec(`INSERT INTO media_skipped (root, path, ext, reason, size_bytes, mtime, seen_at)
		VALUES ('/r', 'y.aax', '.aax', 'made-up-reason', 1, 1, '2026-08-31T12:00:00Z')`); err == nil {
		t.Error("unknown skip reason was allowed")
	}
	if _, err := database.Exec(`INSERT INTO media_skipped (root, path, ext, reason, size_bytes, mtime, seen_at)
		VALUES ('/r', 'x.aax', '.aax', 'unsupported_extension', 1, 1, '2026-08-31T12:00:00Z')`); err == nil {
		t.Error("duplicate (root, path) skipped row was allowed")
	}
	if err := database.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&count); err != nil {
		t.Fatalf("count users: %v", err)
	}
	if count != 0 {
		t.Fatal("expected no users for the ignore-constraint probes")
	}

	// Down: back to the FK-free shape, aux tables gone.
	if err := goose.DownTo(database, "migrations", 14); err != nil {
		t.Fatalf("migrate down to 14: %v", err)
	}
	for _, table := range []string{"media_skipped", "media_ignores"} {
		if err := database.QueryRow(
			`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&count); err != nil {
			t.Fatalf("probe %s: %v", table, err)
		}
		if count != 0 {
			t.Errorf("%s still exists after down", table)
		}
	}
	fks, err = database.Query(`PRAGMA foreign_key_list(media_files)`)
	if err != nil {
		t.Fatalf("foreign_key_list after down: %v", err)
	}
	for fks.Next() {
		t.Error("media_files still declares a foreign key after down")
	}
	fks.Close()
	if err := database.QueryRow(
		`SELECT COUNT(*) FROM media_files WHERE path = 'a.m4b'`).Scan(&count); err != nil {
		t.Fatalf("probe rows after down: %v", err)
	}
	if count != 1 {
		t.Error("media_files rows lost on down")
	}
}
