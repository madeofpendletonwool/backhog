package db

import (
	"path/filepath"
	"testing"

	"github.com/pressly/goose/v3"
)

// TestAlignmentMigration verifies 00017: the job queue with its state
// vocabulary and liveness columns, one active job per entry, the
// alignments/anchors/segments tables with their CHECKs, everything
// cascading with the rows they belong to, and a down half that removes
// it all.
func TestAlignmentMigration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "alignment_test.db")
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
		`INSERT INTO library_entries (id, user_id, media_type, book_id, status)
			VALUES ('e1', 'u1', 'book', 'OL1W', 'backlog')`,
		`INSERT INTO media_files (id, root, path, kind, size_bytes, mtime, book_id, scanned_at)
			VALUES (7, '/nas', 'Anathem/01.m4b', 'epub', 1, 1, 'OL1W', CURRENT_TIMESTAMP)`,
		`INSERT INTO epub_texts (id, media_file_id, char_count, word_count, normalized_sha256, parser_version)
			VALUES ('t1', 7, 900000, 150000, 'abc', 'v1')`,
	}
	for _, q := range seed {
		if _, err := database.Exec(q); err != nil {
			t.Fatalf("seed %q: %v", q, err)
		}
	}

	// A job round-trips with its queue facts.
	if _, err := database.Exec(`
		INSERT INTO alignment_jobs (id, entry_id, epub_text_id, audio_timeline_hash)
		VALUES ('j1', 'e1', 't1', 'hash-of-the-tape')`); err != nil {
		t.Fatalf("insert job: %v", err)
	}
	var state string
	var attempts int
	if err := database.QueryRow(
		`SELECT state, attempts FROM alignment_jobs WHERE id = 'j1'`).Scan(&state, &attempts); err != nil {
		t.Fatalf("read back: %v", err)
	}
	if state != "queued" || attempts != 0 {
		t.Errorf("new job = (%q, %d attempts), want (queued, 0)", state, attempts)
	}

	for _, bad := range []struct {
		what string
		q    string
	}{
		{"unknown job state", `INSERT INTO alignment_jobs (id, entry_id, epub_text_id, audio_timeline_hash, state)
			VALUES ('bad', 'e1', 't1', 'h', 'thinking')`},
		{"progress over 1", `INSERT INTO alignment_jobs (id, entry_id, epub_text_id, audio_timeline_hash, progress)
			VALUES ('bad', 'e1', 't1', 'h', 1.5)`},
		{"negative attempts", `INSERT INTO alignment_jobs (id, entry_id, epub_text_id, audio_timeline_hash, attempts)
			VALUES ('bad', 'e1', 't1', 'h', -1)`},
		{"two active jobs on one entry", `INSERT INTO alignment_jobs (id, entry_id, epub_text_id, audio_timeline_hash)
			VALUES ('bad', 'e1', 't1', 'h')`},
		{"job for an unknown entry", `INSERT INTO alignment_jobs (id, entry_id, epub_text_id, audio_timeline_hash)
			VALUES ('bad', 'nope', 't1', 'h')`},
	} {
		if _, err := database.Exec(bad.q); err == nil {
			t.Errorf("%s was accepted", bad.what)
		}
	}

	// A finished job frees the entry for a fresh one: the unique index
	// only covers the active states.
	if _, err := database.Exec(
		`UPDATE alignment_jobs SET state = 'ready' WHERE id = 'j1'`); err != nil {
		t.Fatalf("finish job: %v", err)
	}
	if _, err := database.Exec(`
		INSERT INTO alignment_jobs (id, entry_id, epub_text_id, audio_timeline_hash)
		VALUES ('j2', 'e1', 't1', 'h')`); err != nil {
		t.Fatalf("re-enqueue after ready: %v", err)
	}

	// The alignment row (sharing the job's id) and its batches.
	if _, err := database.Exec(`
		INSERT INTO alignments (id, entry_id, epub_text_id, state, coverage, mean_confidence, model)
		VALUES ('j1', 'e1', 't1', 'ready', 0.94, 0.87, 'whisper large-v3')`); err != nil {
		t.Fatalf("insert alignment: %v", err)
	}
	for _, q := range []string{
		`INSERT INTO alignment_anchors (alignment_id, char_offset, audio_seconds, confidence)
			VALUES ('j1', 0, 0, 0.9), ('j1', 412900, 21600.5, 0.8)`,
		`INSERT INTO transcript_segments (alignment_id, audio_start, audio_end, text)
			VALUES ('j1', 0, 4.2, 'the clock ticks'), ('j1', 4.2, 8.9, 'orth')`,
	} {
		if _, err := database.Exec(q); err != nil {
			t.Fatalf("insert batch %q: %v", q, err)
		}
	}
	for _, bad := range []struct {
		what string
		q    string
	}{
		{"unknown alignment state", `INSERT INTO alignments (id, entry_id, epub_text_id, state)
			VALUES ('bad', 'e1', 't1', 'transcribing')`},
		{"coverage over 1", `INSERT INTO alignments (id, entry_id, epub_text_id, state, coverage)
			VALUES ('bad', 'e1', 't1', 'ready', 1.2)`},
		{"confidence under 0", `INSERT INTO alignments (id, entry_id, epub_text_id, state, mean_confidence)
			VALUES ('bad', 'e1', 't1', 'ready', -0.1)`},
		{"negative anchor offset", `INSERT INTO alignment_anchors (alignment_id, char_offset, audio_seconds)
			VALUES ('j1', -1, 0)`},
		{"negative anchor seconds", `INSERT INTO alignment_anchors (alignment_id, char_offset, audio_seconds)
			VALUES ('j1', 10, -1)`},
		{"anchor confidence over 1", `INSERT INTO alignment_anchors (alignment_id, char_offset, confidence)
			VALUES ('j1', 10, 1.5)`},
		{"duplicate anchor offset", `INSERT INTO alignment_anchors (alignment_id, char_offset, audio_seconds)
			VALUES ('j1', 412900, 21601)`},
		{"anchor for a missing alignment", `INSERT INTO alignment_anchors (alignment_id, char_offset, audio_seconds)
			VALUES ('nope', 0, 0)`},
		{"inverted segment", `INSERT INTO transcript_segments (alignment_id, audio_start, audio_end)
			VALUES ('j1', 8.9, 4.2)`},
	} {
		if _, err := database.Exec(bad.q); err == nil {
			t.Errorf("%s was accepted", bad.what)
		}
	}

	// Everything belongs to the entry and goes with it.
	if _, err := database.Exec(`DELETE FROM library_entries WHERE id = 'e1'`); err != nil {
		t.Fatalf("delete entry: %v", err)
	}
	for _, table := range []string{"alignment_jobs", "alignments", "alignment_anchors", "transcript_segments"} {
		var n int
		if err := database.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&n); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		if n != 0 {
			t.Errorf("%s kept %d rows after the entry was deleted", table, n)
		}
	}

	// Down removes the four tables and leaves the rest of the schema.
	if err := goose.DownTo(database, "migrations", 16); err != nil {
		t.Fatalf("migrate down: %v", err)
	}
	for _, table := range []string{"alignment_jobs", "alignments", "alignment_anchors", "transcript_segments"} {
		var name string
		err := database.QueryRow(
			`SELECT name FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&name)
		if err == nil {
			t.Errorf("%s survived the down migration", table)
		}
	}
	var texts int
	if err := database.QueryRow(`SELECT COUNT(*) FROM epub_texts`).Scan(&texts); err != nil {
		t.Errorf("epub_texts after down: %v", err)
	}
}
