package db

import (
	"path/filepath"
	"testing"

	"github.com/pressly/goose/v3"
)

// TestEpubTextMigration verifies 00013: the canonical-text index tables land
// on top of the media inventory, uniqueness and CHECK constraints hold, the
// cascades from media_files work, and the down half removes both tables.
func TestEpubTextMigration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "epub_text_test.db")
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

	if err := Migrate(database); err != nil {
		t.Fatalf("migrate up: %v", err)
	}

	seed := `INSERT INTO media_files (id, root, path, kind, size_bytes, mtime, scanned_at)
		VALUES (7, '/media/ebooks', 'book.epub', 'epub', 42, 1, '2026-09-01T12:00:00Z')`
	if _, err := database.Exec(seed); err != nil {
		t.Fatalf("seed media file: %v", err)
	}

	insert := `INSERT INTO epub_texts (id, media_file_id, char_count, word_count,
			normalized_sha256, parsed_at, parser_version)
		VALUES ('t1', 7, 1000, 180, 'deadbeef', '2026-09-01T12:00:00Z', '1')`
	if _, err := database.Exec(insert); err != nil {
		t.Fatalf("insert epub text: %v", err)
	}

	chapter := `INSERT INTO epub_chapters (id, epub_text_id, spine_index, href, title,
			char_start, char_end, depth)
		VALUES ('c0', 't1', 0, 'ch1.xhtml', 'One', 0, 500, 0),
		       ('c1', 't1', 1, 'ch2.xhtml', 'Two', 500, 1000, 1)`
	if _, err := database.Exec(chapter); err != nil {
		t.Fatalf("insert chapters: %v", err)
	}

	// One canonical text per media file; re-attaching must go through upsert.
	dup := `INSERT INTO epub_texts (id, media_file_id, char_count, word_count,
			normalized_sha256, parser_version)
		VALUES ('t2', 7, 1, 1, 'x', '1')`
	if _, err := database.Exec(dup); err == nil {
		t.Error("duplicate media_file_id was allowed")
	}

	// One chapter row per spine position.
	dupChapter := `INSERT INTO epub_chapters (id, epub_text_id, spine_index, href,
			char_start, char_end)
		VALUES ('c2', 't1', 1, 'ch2b.xhtml', 0, 0)`
	if _, err := database.Exec(dupChapter); err == nil {
		t.Error("duplicate (epub_text_id, spine_index) was allowed")
	}

	// Ranges are half-open and can be empty, but never inverted.
	bad := `INSERT INTO epub_chapters (id, epub_text_id, spine_index, href,
			char_start, char_end)
		VALUES ('c3', 't1', 2, 'ch3.xhtml', 10, 9)`
	if _, err := database.Exec(bad); err == nil {
		t.Error("inverted char range was allowed")
	}

	// Cascades: removing the inventory row removes its canonical text and
	// chapters; removing the text row removes its chapters.
	if _, err := database.Exec(`DELETE FROM media_files WHERE id = 7`); err != nil {
		t.Fatalf("delete media file: %v", err)
	}
	for _, q := range []string{
		`SELECT COUNT(*) FROM epub_texts`,
		`SELECT COUNT(*) FROM epub_chapters`,
	} {
		var n int
		if err := database.QueryRow(q).Scan(&n); err != nil {
			t.Fatalf("count: %v", err)
		}
		if n != 0 {
			t.Errorf("cascade left rows behind (%q): %d", q, n)
		}
	}

	if rows, err := database.Query(`PRAGMA foreign_key_check`); err != nil {
		t.Fatalf("foreign_key_check: %v", err)
	} else {
		for rows.Next() {
			t.Error("foreign_key_check reported a violation")
		}
		rows.Close()
	}

	// Down: the whole index is derived data and drops clean.
	if err := goose.DownTo(database, "migrations", 12); err != nil {
		t.Fatalf("migrate down to 12: %v", err)
	}
	for _, table := range []string{"epub_texts", "epub_chapters"} {
		var count int
		if err := database.QueryRow(
			`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&count); err != nil {
			t.Fatalf("probe %s: %v", table, err)
		}
		if count != 0 {
			t.Errorf("%s still exists after down", table)
		}
	}
}
