package db

import (
	"path/filepath"
	"testing"

	"github.com/pressly/goose/v3"
)

// TestBookTextFTSFilesMigration verifies 00041: an install that already has
// FTS rows comes out of the migration with every indexed file recorded in
// the side table — and only those — so the first boot after it does not
// re-index the whole library.
func TestBookTextFTSFilesMigration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fts_files_test.db")
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
	if err := goose.UpTo(database, "migrations", 40); err != nil {
		t.Fatalf("migrate up to 40: %v", err)
	}

	seed := []string{
		`INSERT INTO media_files (id, root, path, kind, size_bytes, mtime, scanned_at)
		 VALUES (1, '/m', 'a.epub', 'epub', 1, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP),
		        (2, '/m', 'b.epub', 'epub', 1, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`,
		`INSERT INTO book_text_fts (body, book_id, media_file_id) VALUES ('one', 'OL1W', 1)`,
		`INSERT INTO book_text_fts (body, book_id, media_file_id) VALUES ('two', 'OL1W', 1)`,
		// An orphan left by a media file deleted before the side table
		// existed: it must not trip the foreign key.
		`INSERT INTO book_text_fts (body, book_id, media_file_id) VALUES ('gone', 'OL9W', 99)`,
	}
	for _, stmt := range seed {
		if _, err := database.Exec(stmt); err != nil {
			t.Fatalf("seed %q: %v", stmt, err)
		}
	}

	if err := goose.UpTo(database, "migrations", 41); err != nil {
		t.Fatalf("migrate up to 41: %v", err)
	}

	rows, err := database.Query(`SELECT media_file_id FROM book_text_fts_files ORDER BY 1`)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer rows.Close()
	var got []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("scan: %v", err)
		}
		got = append(got, id)
	}
	if len(got) != 1 || got[0] != 1 {
		t.Fatalf("book_text_fts_files = %v, want [1]", got)
	}

	if err := goose.DownTo(database, "migrations", 40); err != nil {
		t.Fatalf("migrate down to 40: %v", err)
	}
}
