package db

import (
	"path/filepath"
	"testing"

	"github.com/pressly/goose/v3"
)

// TestBookProgressMigration verifies 00016: one progress row per entry with
// the canonical offset as its truth, the raw-audio fallback pair enforced as
// all-or-nothing, reading_sessions mirroring play_sessions, both tables
// cascading with the entry, and a down half that removes them.
func TestBookProgressMigration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "progress_test.db")
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
			VALUES (7, '/nas', 'Anathem/01.m4b', 'audio', 1, 1, 'OL1W', CURRENT_TIMESTAMP)`,
	}
	for _, q := range seed {
		if _, err := database.Exec(q); err != nil {
			t.Fatalf("seed %q: %v", q, err)
		}
	}

	// The canonical offset round-trips: what goes in comes back out.
	if _, err := database.Exec(`
		INSERT INTO book_progress (entry_id, char_offset, char_offset_source, percent_complete)
		VALUES ('e1', 412900, 'read', 61.4)`); err != nil {
		t.Fatalf("insert progress: %v", err)
	}
	var offset int
	var source string
	if err := database.QueryRow(
		`SELECT char_offset, char_offset_source FROM book_progress WHERE entry_id = 'e1'`).
		Scan(&offset, &source); err != nil {
		t.Fatalf("read back: %v", err)
	}
	if offset != 412900 || source != "read" {
		t.Errorf("stored position = (%d, %q), want (412900, \"read\")", offset, source)
	}

	// One row per entry.
	if _, err := database.Exec(
		`INSERT INTO book_progress (entry_id, char_offset, char_offset_source) VALUES ('e1', 5, 'read')`); err == nil {
		t.Error("a second progress row for the same entry was accepted")
	}

	for _, bad := range []struct {
		what string
		q    string
	}{
		{"negative offset", `INSERT INTO book_progress (entry_id, char_offset, char_offset_source)
			VALUES ('e1', -1, 'read')`},
		{"unknown source", `INSERT INTO book_progress (entry_id, char_offset, char_offset_source)
			VALUES ('e1', 0, 'telepathy')`},
		{"percent over 100", `INSERT INTO book_progress (entry_id, char_offset, char_offset_source, percent_complete)
			VALUES ('e1', 0, 'read', 101)`},
		{"raw file without seconds", `INSERT INTO book_progress (entry_id, char_offset, char_offset_source, raw_audio_file_id)
			VALUES ('e1', 0, 'listen', 7)`},
		{"raw file that is not a real file", `INSERT INTO book_progress (entry_id, char_offset, char_offset_source, raw_audio_seconds, raw_audio_file_id)
			VALUES ('e1', 0, 'listen', 12.0, 999)`},
		{"unknown entry", `INSERT INTO book_progress (entry_id, char_offset, char_offset_source)
			VALUES ('nope', 0, 'read')`},
	} {
		if _, err := database.Exec(bad.q); err == nil {
			t.Errorf("%s was accepted", bad.what)
		}
	}

	// The raw fallback is accepted as a complete pair, and detaching the
	// audio file clears the pointer without deleting the position.
	if _, err := database.Exec(`
		UPDATE book_progress SET raw_audio_seconds = 120.5, raw_audio_file_id = 7 WHERE entry_id = 'e1'`); err != nil {
		t.Fatalf("store raw audio fallback: %v", err)
	}
	if _, err := database.Exec(`DELETE FROM media_files WHERE id = 7`); err != nil {
		t.Fatalf("delete media file: %v", err)
	}
	var fileID *int64
	if err := database.QueryRow(
		`SELECT raw_audio_file_id FROM book_progress WHERE entry_id = 'e1'`).Scan(&fileID); err != nil {
		t.Fatalf("read raw file id: %v", err)
	}
	if fileID != nil {
		t.Errorf("raw_audio_file_id = %v after the file was deleted, want NULL", *fileID)
	}
	// The timestamp survives as an orphan rather than the detach failing on
	// a constraint — it is the read path's job to notice it no longer
	// resolves onto any timeline.
	var seconds *float64
	if err := database.QueryRow(
		`SELECT raw_audio_seconds FROM book_progress WHERE entry_id = 'e1'`).Scan(&seconds); err != nil {
		t.Fatalf("read raw seconds: %v", err)
	}
	if seconds == nil || *seconds != 120.5 {
		t.Errorf("raw_audio_seconds = %v after the detach, want 120.5 kept", seconds)
	}

	// reading_sessions: both modes, endpoints that must not invert, and a
	// duration cap matching play_sessions'.
	for _, q := range []string{
		`INSERT INTO reading_sessions (id, user_id, entry_id, started_at, ended_at, mode, chars_advanced, seconds)
			VALUES ('rs1', 'u1', 'e1', '2026-09-01T10:00:00Z', '2026-09-01T10:45:00Z', 'read', 18000, 2700)`,
		`INSERT INTO reading_sessions (id, user_id, entry_id, started_at, ended_at, mode, chars_advanced, seconds)
			VALUES ('rs2', 'u1', 'e1', '2026-09-01T18:00:00Z', '2026-09-01T18:30:00Z', 'listen', 0, 1800)`,
	} {
		if _, err := database.Exec(q); err != nil {
			t.Fatalf("insert session %q: %v", q, err)
		}
	}
	for _, bad := range []struct {
		what string
		q    string
	}{
		{"unknown mode", `INSERT INTO reading_sessions (id, user_id, entry_id, started_at, ended_at, mode, seconds)
			VALUES ('bad', 'u1', 'e1', '2026-09-01T10:00:00Z', '2026-09-01T10:45:00Z', 'skim', 60)`},
		{"inverted endpoints", `INSERT INTO reading_sessions (id, user_id, entry_id, started_at, ended_at, mode, seconds)
			VALUES ('bad', 'u1', 'e1', '2026-09-01T11:00:00Z', '2026-09-01T10:00:00Z', 'read', 60)`},
		{"negative chars", `INSERT INTO reading_sessions (id, user_id, entry_id, started_at, ended_at, mode, chars_advanced, seconds)
			VALUES ('bad', 'u1', 'e1', '2026-09-01T10:00:00Z', '2026-09-01T10:45:00Z', 'read', -5, 60)`},
		{"over a day", `INSERT INTO reading_sessions (id, user_id, entry_id, started_at, ended_at, mode, seconds)
			VALUES ('bad', 'u1', 'e1', '2026-09-01T10:00:00Z', '2026-09-03T10:00:00Z', 'read', 172800)`},
	} {
		if _, err := database.Exec(bad.q); err == nil {
			t.Errorf("%s was accepted", bad.what)
		}
	}

	// Both tables belong to the entry and go with it.
	if _, err := database.Exec(`DELETE FROM library_entries WHERE id = 'e1'`); err != nil {
		t.Fatalf("delete entry: %v", err)
	}
	for _, table := range []string{"book_progress", "reading_sessions"} {
		var n int
		if err := database.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&n); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		if n != 0 {
			t.Errorf("%s kept %d rows after the entry was deleted", table, n)
		}
	}

	// Down removes both tables and leaves the rest of the schema intact.
	if err := goose.Down(database, "migrations"); err != nil {
		t.Fatalf("migrate down: %v", err)
	}
	for _, table := range []string{"book_progress", "reading_sessions"} {
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
