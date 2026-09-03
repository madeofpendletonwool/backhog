package db

import (
	"path/filepath"
	"testing"

	"github.com/pressly/goose/v3"
)

// TestMediaFormatsMigration verifies 00020: the two new skip reasons are
// accepted while a bogus one is still refused, existing skip rows survive the
// rebuild, media_sidecars lands with its uniqueness constraint, meta_version
// defaults to 0 on rows written before it existed, and the down half returns
// to the pre-00020 shape without dropping the rows it can still express.
func TestMediaFormatsMigration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "media_formats_test.db")
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

	// Pre-state at version 19: an inventoried file and a skipped one, both
	// written by a scanner that had never heard of meta_version.
	if err := goose.UpTo(database, "migrations", 19); err != nil {
		t.Fatalf("migrate to 19: %v", err)
	}
	seed := []string{
		`INSERT INTO media_files (root, path, kind, size_bytes, mtime, scanned_at)
			VALUES ('/r', 'novel.epub', 'epub', 1, 1, '2026-08-31T12:00:00Z')`,
		`INSERT INTO media_skipped (root, path, ext, reason, size_bytes, mtime, seen_at)
			VALUES ('/r', 'locked.aax', '.aax', 'unsupported_extension', 1, 1, '2026-08-31T12:00:00Z')`,
	}
	for _, q := range seed {
		if _, err := database.Exec(q); err != nil {
			t.Fatalf("seed %q: %v", q, err)
		}
	}

	if err := Migrate(database); err != nil {
		t.Fatalf("migrate up: %v", err)
	}

	// A pre-existing row reads as version 0, which is what makes the
	// scanner's fast path re-read it exactly once.
	var metaVersion int
	if err := database.QueryRow(
		`SELECT meta_version FROM media_files WHERE path = 'novel.epub'`).Scan(&metaVersion); err != nil {
		t.Fatalf("probe meta_version: %v", err)
	}
	if metaVersion != 0 {
		t.Errorf("meta_version on a pre-00020 row = %d, want 0", metaVersion)
	}

	// The skipped row survived the CHECK rebuild.
	var reason string
	if err := database.QueryRow(
		`SELECT reason FROM media_skipped WHERE path = 'locked.aax'`).Scan(&reason); err != nil {
		t.Fatalf("probe skipped: %v", err)
	}
	if reason != "unsupported_extension" {
		t.Errorf("skipped reason did not survive the rebuild: %q", reason)
	}

	// The new reasons are accepted; an invented one is still refused.
	for _, r := range []string{"format_unhandled", "sidecar_metadata", "drm_epub"} {
		if _, err := database.Exec(`INSERT INTO media_skipped (root, path, ext, reason, size_bytes, mtime, seen_at)
			VALUES ('/r', ?, '.x', ?, 1, 1, '2026-08-31T12:00:00Z')`, "probe-"+r, r); err != nil {
			t.Errorf("reason %q was rejected: %v", r, err)
		}
	}
	if _, err := database.Exec(`INSERT INTO media_skipped (root, path, ext, reason, size_bytes, mtime, seen_at)
		VALUES ('/r', 'bogus', '.x', 'made-up-reason', 1, 1, '2026-08-31T12:00:00Z')`); err == nil {
		t.Error("unknown skip reason was allowed after the rebuild")
	}

	// media_sidecars: one row per file, replaceable per root.
	if _, err := database.Exec(`INSERT INTO media_sidecars (root, path, title, author, isbn, seen_at)
		VALUES ('/r', 'Book/metadata.opf', 'Breakfast of Champions', 'Kurt Vonnegut', '9780385334204', '2026-08-31T12:00:00Z')`); err != nil {
		t.Fatalf("insert sidecar: %v", err)
	}
	if _, err := database.Exec(`INSERT INTO media_sidecars (root, path, title, seen_at)
		VALUES ('/r', 'Book/metadata.opf', 'Duplicate', '2026-08-31T12:00:00Z')`); err == nil {
		t.Error("duplicate (root, path) sidecar was allowed")
	}
	// The columns the matcher reads are NOT NULL with a default, so a row
	// written with only a title still scans cleanly.
	if _, err := database.Exec(`INSERT INTO media_sidecars (root, path, title, seen_at)
		VALUES ('/r', 'Other/metadata.opf', 'Just A Title', '2026-08-31T12:00:00Z')`); err != nil {
		t.Fatalf("insert sparse sidecar: %v", err)
	}
	var author, workKey string
	if err := database.QueryRow(
		`SELECT author, work_key FROM media_sidecars WHERE path = 'Other/metadata.opf'`).Scan(&author, &workKey); err != nil {
		t.Fatalf("probe sparse sidecar: %v", err)
	}
	if author != "" || workKey != "" {
		t.Errorf("sparse sidecar defaults = %q / %q, want empty strings", author, workKey)
	}

	// Down: the sidecar table goes, the new reasons go with the rows that
	// carried them, and the rows the old CHECK can still express stay.
	if err := goose.DownTo(database, "migrations", 19); err != nil {
		t.Fatalf("migrate down to 19: %v", err)
	}
	var count int
	if err := database.QueryRow(
		`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'media_sidecars'`).Scan(&count); err != nil {
		t.Fatalf("probe media_sidecars: %v", err)
	}
	if count != 0 {
		t.Error("media_sidecars survived the down migration")
	}
	if err := database.QueryRow(
		`SELECT COUNT(*) FROM media_skipped WHERE reason IN ('format_unhandled','sidecar_metadata')`).Scan(&count); err != nil {
		t.Fatalf("probe new reasons after down: %v", err)
	}
	if count != 0 {
		t.Errorf("%d rows with post-00020 reasons survived the down migration", count)
	}
	if err := database.QueryRow(
		`SELECT COUNT(*) FROM media_skipped WHERE path = 'locked.aax'`).Scan(&count); err != nil {
		t.Fatalf("probe surviving skip row: %v", err)
	}
	if count != 1 {
		t.Error("the down migration dropped a row the old shape can express")
	}
	if _, err := database.Exec(`SELECT meta_version FROM media_files`); err == nil {
		t.Error("meta_version survived the down migration")
	}
}
