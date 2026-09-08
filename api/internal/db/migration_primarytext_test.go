package db

import (
	"path/filepath"
	"testing"

	"github.com/pressly/goose/v3"
)

// TestPrimaryTextMigration verifies 00024: the backfill designates exactly
// one text file per book, it designates the one yesterday's resolver was
// already handing out, and the partial unique index refuses a second.
//
// The backfill's choice is the load-bearing part. Every offset in
// book_progress and page_anchors was measured against whatever
// EpubMediaFileForBook returned before this migration — the lowest-id
// present row — so promoting a better-ranked format here would move every
// existing reader's position by the difference between two canonicalizations
// of the same book. The new format preference applies to attachments made
// after the migration; existing books keep the text their offsets mean.
func TestPrimaryTextMigration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "primary_text_test.db")
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
	if err := goose.UpTo(database, "migrations", 23); err != nil {
		t.Fatalf("migrate to 23: %v", err)
	}

	books := `INSERT INTO books (id, title) VALUES
		('OL1W', 'Carrie'), ('OL2W', 'It'), ('OL3W', 'Cujo')`
	if _, err := database.Exec(books); err != nil {
		t.Fatalf("seed books: %v", err)
	}

	seed := `INSERT INTO media_files (id, root, path, kind, size_bytes, mtime, book_id, scanned_at, missing_at) VALUES
		-- A book scanned as epub first, mobi later: the epub is what the
		-- old resolver returned and what the reader's offsets refer to.
		(1, '/nas', 'King/Carrie.epub', 'epub', 10, 1, 'OL1W', '2026-09-01T00:00:00Z', NULL),
		(2, '/nas', 'King/Carrie.mobi', 'epub', 10, 1, 'OL1W', '2026-09-01T00:00:00Z', NULL),
		-- A book whose mobi was scanned first. The epub outranks it on
		-- format, but the stored offsets are the mobi's, so the mobi keeps
		-- the designation until someone switches it deliberately.
		(3, '/nas', 'King/It.mobi', 'epub', 10, 1, 'OL2W', '2026-09-01T00:00:00Z', NULL),
		(4, '/nas', 'King/It.epub', 'epub', 10, 1, 'OL2W', '2026-09-01T00:00:00Z', NULL),
		-- A book whose lowest-id file is missing: the old resolver skipped
		-- it, so the present one is what was being read.
		(5, '/nas', 'King/Cujo.epub', 'epub', 10, 1, 'OL3W', '2026-09-01T00:00:00Z', '2026-09-02T00:00:00Z'),
		(6, '/nas', 'King/Cujo.mobi', 'epub', 10, 1, 'OL3W', '2026-09-01T00:00:00Z', NULL),
		-- Audio and unattached rows must stay out of it entirely.
		(7, '/nas', 'King/Carrie/01.mp3', 'audio', 10, 1, 'OL1W', '2026-09-01T00:00:00Z', NULL),
		(8, '/nas', 'King/Misery.epub', 'epub', 10, 1, NULL, '2026-09-01T00:00:00Z', NULL)`
	if _, err := database.Exec(seed); err != nil {
		t.Fatalf("seed: %v", err)
	}

	if err := Migrate(database); err != nil {
		t.Fatalf("migrate up: %v", err)
	}

	for _, want := range []struct {
		book string
		id   int64
	}{
		{"OL1W", 1},
		{"OL2W", 3},
		{"OL3W", 6},
	} {
		var got int64
		var count int
		if err := database.QueryRow(`SELECT COUNT(*), COALESCE(MAX(id), 0) FROM media_files
			WHERE book_id = ? AND kind = 'epub' AND is_primary_text = 1`, want.book).
			Scan(&count, &got); err != nil {
			t.Fatalf("primary of %s: %v", want.book, err)
		}
		if count != 1 || got != want.id {
			t.Fatalf("%s has %d primaries (id %d), want exactly file %d", want.book, count, got, want.id)
		}
	}

	var stray int
	if err := database.QueryRow(
		`SELECT COUNT(*) FROM media_files WHERE is_primary_text = 1 AND (kind = 'audio' OR book_id IS NULL)`).
		Scan(&stray); err != nil {
		t.Fatalf("stray: %v", err)
	}
	if stray != 0 {
		t.Fatalf("%d audio or unattached rows were designated, want 0", stray)
	}

	if _, err := database.Exec(`UPDATE media_files SET is_primary_text = 1 WHERE id = 2`); err == nil {
		t.Fatal("a second primary for OL1W was accepted, want the unique index to refuse it")
	}

	// DownTo, not Down: Down reverts one migration, so a plain Down would
	// only undo whichever migration happens to be newest. This test is
	// about 00024, so it names the version to roll back past.
	if err := goose.DownTo(database, "migrations", 23); err != nil {
		t.Fatalf("migrate down: %v", err)
	}
	if _, err := database.Exec(`SELECT is_primary_text FROM media_files LIMIT 1`); err == nil {
		t.Fatal("is_primary_text survived the down migration")
	}
}
