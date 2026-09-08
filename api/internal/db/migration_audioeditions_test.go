package db

import (
	"path/filepath"
	"testing"

	"github.com/pressly/goose/v3"
)

// TestAudioEditionsMigration verifies 00027: the backfill groups each book's
// attached audio into one edition per directory, designates the edition that
// was actually playing, and the partial unique index refuses a second.
//
// The designation is the load-bearing part, for the same reason it was in
// 00024. A stored listening position names a file and an offset inside it,
// and the percentage beside it was computed against the timeline as it stood
// — ORDER BY track_number, path across every attached file. The edition
// holding the track that sorted first is the one the player opened on, so
// that is the one the stored position means.
func TestAudioEditionsMigration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audio_editions_test.db")
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
	if err := goose.UpTo(database, "migrations", 26); err != nil {
		t.Fatalf("migrate to 26: %v", err)
	}

	books := `INSERT INTO books (id, title) VALUES
		('OL1W', 'Anathem'), ('OL2W', 'Carrie'), ('OL3W', 'Cujo')`
	if _, err := database.Exec(books); err != nil {
		t.Fatalf("seed books: %v", err)
	}

	seed := `INSERT INTO media_files (id, root, path, kind, size_bytes, mtime, book_id, track_number, scanned_at) VALUES
		-- Two rips of one book, interleaved on the old timeline: the Dufris
		-- tracks sorted first, so that is the tape being listened to.
		(1, '/nas', 'Anathem [Dufris]/01.m4b', 'audio', 10, 1, 'OL1W', 1, '2026-09-01T00:00:00Z'),
		(2, '/nas', 'Anathem [Dufris]/02.m4b', 'audio', 10, 1, 'OL1W', 2, '2026-09-01T00:00:00Z'),
		(3, '/nas', 'Anathem [Guidall]/01.m4b', 'audio', 10, 1, 'OL1W', 1, '2026-09-01T00:00:00Z'),
		(4, '/nas', 'Anathem [Guidall]/02.m4b', 'audio', 10, 1, 'OL1W', 2, '2026-09-01T00:00:00Z'),
		-- A rip split across platters is still one directory's worth of one
		-- recording as far as the grouping is concerned: two editions here,
		-- one per disc, is the honest reading of a flat path list, and the
		-- attach flow is what says otherwise going forward.
		(5, '/nas', 'Carrie/01.mp3', 'audio', 10, 1, 'OL2W', 1, '2026-09-01T00:00:00Z'),
		(6, '/nas', 'Carrie/02.mp3', 'audio', 10, 1, 'OL2W', 2, '2026-09-01T00:00:00Z'),
		-- A lone m4b, and the same file name under a second root: different
		-- mounts are different files, so they are different recordings.
		(7, '/nas', 'Cujo.m4b', 'audio', 10, 1, 'OL3W', 1, '2026-09-01T00:00:00Z'),
		(8, '/backup', 'Cujo.m4b', 'audio', 10, 1, 'OL3W', 2, '2026-09-01T00:00:00Z'),
		-- Text-side and unattached rows must stay out of it entirely.
		(9, '/nas', 'Anathem.epub', 'epub', 10, 1, 'OL1W', NULL, '2026-09-01T00:00:00Z'),
		(10, '/nas', 'Loose/01.m4b', 'audio', 10, 1, NULL, NULL, '2026-09-01T00:00:00Z')`
	if _, err := database.Exec(seed); err != nil {
		t.Fatalf("seed: %v", err)
	}

	if err := Migrate(database); err != nil {
		t.Fatalf("migrate up: %v", err)
	}

	// Each book's files are grouped, and every attached audio row lands in
	// an edition of its own book.
	for _, want := range []struct {
		book     string
		editions int
	}{
		{"OL1W", 2},
		{"OL2W", 1},
		{"OL3W", 2},
	} {
		var got int
		if err := database.QueryRow(
			`SELECT COUNT(*) FROM audio_editions WHERE book_id = ?`, want.book).Scan(&got); err != nil {
			t.Fatalf("count editions of %s: %v", want.book, err)
		}
		if got != want.editions {
			t.Errorf("%s has %d editions, want %d", want.book, got, want.editions)
		}
	}

	var orphans int
	if err := database.QueryRow(`SELECT COUNT(*) FROM media_files
		WHERE kind = 'audio' AND book_id IS NOT NULL AND audio_edition_id IS NULL`).Scan(&orphans); err != nil {
		t.Fatalf("count orphans: %v", err)
	}
	if orphans != 0 {
		t.Errorf("%d attached audio files ended up in no edition", orphans)
	}

	// Nothing that was not attached audio was touched.
	var stray int
	if err := database.QueryRow(`SELECT COUNT(*) FROM media_files
		WHERE audio_edition_id IS NOT NULL AND (kind <> 'audio' OR book_id IS NULL)`).Scan(&stray); err != nil {
		t.Fatalf("count stray: %v", err)
	}
	if stray != 0 {
		t.Errorf("%d text-side or unattached rows were given an edition", stray)
	}

	// The designation follows the track that sorted first on the old
	// timeline, per book.
	for _, want := range []struct {
		book  string
		track int64
	}{
		{"OL1W", 1},
		{"OL2W", 5},
		{"OL3W", 7},
	} {
		var got int64
		if err := database.QueryRow(`
			SELECT f.id FROM media_files f
			JOIN audio_editions e ON e.id = f.audio_edition_id AND e.is_primary = 1
			WHERE e.book_id = ?
			ORDER BY f.track_number, f.path LIMIT 1`, want.book).Scan(&got); err != nil {
			t.Fatalf("designated tape of %s: %v", want.book, err)
		}
		if got != want.track {
			t.Errorf("%s plays file %d first, want %d", want.book, got, want.track)
		}
	}

	var primaries int
	if err := database.QueryRow(
		`SELECT COUNT(*) FROM audio_editions WHERE is_primary = 1`).Scan(&primaries); err != nil {
		t.Fatalf("count primaries: %v", err)
	}
	if primaries != 3 {
		t.Errorf("%d designated editions, want exactly one per book", primaries)
	}

	// The index, not a query, is what keeps it to one.
	if _, err := database.Exec(
		`INSERT INTO audio_editions (book_id, is_primary) VALUES ('OL1W', 1)`); err == nil {
		t.Error("a second designated edition was accepted for one book")
	}
}
