package db

import (
	"path/filepath"
	"testing"

	"github.com/pressly/goose/v3"
)

// TestMediaFilesMigration verifies 00012 as landed: the media_files
// inventory table, kind and (root, path) uniqueness, and book_id plain
// nullable TEXT with no foreign key — the deferred-FK contract at version
// 12. (00015 later adds the FK; migration_attach_test.go covers that.)
func TestMediaFilesMigration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "media_files_test.db")
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

	if err := goose.UpTo(database, "migrations", 12); err != nil {
		t.Fatalf("migrate to 12: %v", err)
	}

	insert := `INSERT INTO media_files (root, path, kind, size_bytes, mtime, scanned_at)
		VALUES ('/media/audiobooks', 'Book.m4b', 'audio', 42, 1700000000000000000, '2026-08-31T12:00:00Z')`
	if _, err := database.Exec(insert); err != nil {
		t.Fatalf("insert valid row: %v", err)
	}

	// The kind CHECK: only epub and audio are tracked.
	for _, q := range []string{
		`INSERT INTO media_files (root, path, kind, size_bytes, mtime, scanned_at)
			VALUES ('/r', 'x.mp4', 'video', 1, 1, '2026-08-31T12:00:00Z')`,
	} {
		if _, err := database.Exec(q); err == nil {
			t.Errorf("constraint rejected nothing for %q", q)
		}
	}

	// Upserts key on (root, path); the same path under another root is fine.
	dup := `INSERT INTO media_files (root, path, kind, size_bytes, mtime, scanned_at)
		VALUES ('/media/audiobooks', 'Book.m4b', 'audio', 99, 2, '2026-08-31T12:00:00Z')`
	if _, err := database.Exec(dup); err == nil {
		t.Error("duplicate (root, path) was allowed")
	}
	other := `INSERT INTO media_files (root, path, kind, size_bytes, mtime, scanned_at)
		VALUES ('/media/ebooks', 'Book.m4b', 'audio', 42, 1, '2026-08-31T12:00:00Z')`
	if _, err := database.Exec(other); err != nil {
		t.Errorf("same path under another root rejected: %v", err)
	}

	// book_id must be plain nullable TEXT with no foreign key: the books
	// table arrives in 00011 on a concurrent branch and may not exist yet.
	fks, err := database.Query(`PRAGMA foreign_key_list(media_files)`)
	if err != nil {
		t.Fatalf("foreign_key_list: %v", err)
	}
	for fks.Next() {
		t.Error("media_files declares a foreign key; book_id must stay FK-free until 00011 lands")
	}
	fks.Close()

	attached := `INSERT INTO media_files (root, path, kind, size_bytes, mtime, book_id, scanned_at)
		VALUES ('/r', 'y.epub', 'epub', 1, 1, 'isbn-does-not-exist-yet', '2026-08-31T12:00:00Z')`
	if _, err := database.Exec(attached); err != nil {
		t.Errorf("attaching to a not-yet-existing book id failed: %v", err)
	}

	if rows, err := database.Query(`PRAGMA foreign_key_check`); err != nil {
		t.Fatalf("foreign_key_check: %v", err)
	} else {
		for rows.Next() {
			t.Error("foreign_key_check reported a violation")
		}
		rows.Close()
	}

	// Down: the inventory is derived data; it drops whole.
	if err := goose.DownTo(database, "migrations", 11); err != nil {
		t.Fatalf("migrate down to 11: %v", err)
	}
	var count int
	if err := database.QueryRow(
		`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'media_files'`).Scan(&count); err != nil {
		t.Fatalf("probe table: %v", err)
	}
	if count != 0 {
		t.Error("media_files still exists after down")
	}
}
