package db

import (
	"path/filepath"
	"testing"

	"github.com/pressly/goose/v3"
)

// TestBookClaimsMigration verifies 00037: the claims store arrives with its
// guards (no inverted spans, no reveal ahead of its evidence, no blank
// statements or quotes), claim identity is unique, versions and aliases
// follow their parents away, and the down half drops the whole family.
func TestBookClaimsMigration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "book_claims_test.db")
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
		`INSERT INTO users (id, email, username, password_hash) VALUES ('u1', 'u1@example.com', 'u1', 'x')`,
		`INSERT INTO books (id, title) VALUES ('OL1W', 'The Village Mystery')`,
		`INSERT INTO library_entries (id, user_id, media_type, book_id, status, created_at)
			VALUES ('b1', 'u1', 'book', 'OL1W', 'playing', '2026-01-01 00:00:00')`,
	}
	for _, q := range seed {
		if _, err := database.Exec(q); err != nil {
			t.Fatalf("seed %q: %v", q, err)
		}
	}

	insert := func(table, cols string, args ...any) error {
		stmt, err := database.Prepare(`INSERT INTO ` + table + ` (` + cols + `) VALUES (` +
			placeholders(len(args)) + `)`)
		if err != nil {
			return err
		}
		defer stmt.Close()
		_, err = stmt.Exec(args...)
		return err
	}

	claim := `id, user_id, entry_id, statement, subject, predicate, object,
		char_start, char_end, quote, chapter_index, chapter_hash, source`
	// The honest shape: a triple anchored in one chapter, hashed against it.
	if err := insert(`book_claims`, claim,
		"c1", "u1", "b1", "the butler is polite", "the butler", "is", "polite",
		12, 30, "The butler bowed.", 1, "hash-alpha", "my-extractor"); err != nil {
		t.Fatalf("honest claim rejected: %v", err)
	}
	// A claim may be a bare statement with no triple.
	if err := insert(`book_claims`, `id, user_id, entry_id, statement, char_start, char_end, quote, chapter_index, chapter_hash, source`,
		"c2", "u1", "b1", "the manor is old", 40, 55, "The manor is old.", 1, "hash-alpha", "my-extractor"); err != nil {
		t.Fatalf("bare claim rejected: %v", err)
	}
	// Claim identity is deterministic: the same evidence and statement
	// cannot land twice.
	if err := insert(`book_claims`, `id, user_id, entry_id, statement, char_start, char_end, quote, chapter_index, chapter_hash, source`,
		"c3", "u1", "b1", "the manor is old", 40, 55, "a different quote", 1, "hash-alpha", "my-extractor"); err == nil {
		t.Error("a duplicate claim identity was accepted")
	}

	// The guards: inverted or empty spans, blank statements and quotes.
	if err := insert(`book_claims`, `id, user_id, entry_id, statement, char_start, char_end, quote, chapter_index, chapter_hash, source`,
		"c4", "u1", "b1", "inverted", 60, 50, "x", 1, "h", "s"); err == nil {
		t.Error("an inverted span was accepted")
	}
	if err := insert(`book_claims`, `id, user_id, entry_id, statement, char_start, char_end, quote, chapter_index, chapter_hash, source`,
		"c5", "u1", "b1", "   ", 10, 20, "x", 1, "h", "s"); err == nil {
		t.Error("a blank statement was accepted")
	}
	if err := insert(`book_claims`, `id, user_id, entry_id, statement, char_start, char_end, quote, chapter_index, chapter_hash, source`,
		"c6", "u1", "b1", "no quote", 10, 20, " ", 1, "h", "s"); err == nil {
		t.Error("a blank quote was accepted")
	}

	version := `id, claim_id, statement, char_start, char_end, quote,
		chapter_index, chapter_hash, reveal_offset, source`
	// A version reveals at or after its evidence.
	if err := insert(`claim_versions`, version,
		"v1", "c1", "the butler did it", 200, 220, "The butler did it.",
		2, "hash-beta", 205, "my-extractor"); err != nil {
		t.Fatalf("honest version rejected: %v", err)
	}
	if err := insert(`claim_versions`, version,
		"v2", "c1", "impossible", 200, 220, "q", 2, "hash-beta", 150, "my-extractor"); err == nil {
		t.Error("a reveal preceding its evidence was accepted")
	}
	if err := insert(`claim_versions`, `id, claim_id, statement, char_start, char_end, quote, chapter_index, chapter_hash, reveal_offset`,
		"v3", "c1", "dupe", 200, 220, "q", 2, "hash-beta", 205); err == nil {
		t.Error("a duplicate version identity was accepted")
	}

	// Entities and aliases: unique per reader and book, aliases follow
	// their entity away.
	if err := insert(`book_entities`, `id, user_id, book_id, name, kind`,
		"e1", "u1", "OL1W", "The butler", "person"); err != nil {
		t.Fatalf("honest entity rejected: %v", err)
	}
	if err := insert(`book_entities`, `id, user_id, book_id, name, kind`,
		"e2", "u1", "OL1W", "The butler", "person"); err == nil {
		t.Error("a duplicate entity was accepted")
	}
	if err := insert(`entity_aliases`, `entity_id, alias`, "e1", "Mr. Stevens"); err != nil {
		t.Fatalf("honest alias rejected: %v", err)
	}
	if err := insert(`entity_aliases`, `entity_id, alias`, "e1", "Mr. Stevens"); err == nil {
		t.Error("a duplicate alias was accepted")
	}

	// Claim versions are their claim's children: the parent going takes
	// them, and aliases follow entities.
	if _, err := database.Exec(`DELETE FROM book_claims WHERE id = 'c1'`); err != nil {
		t.Fatalf("delete claim: %v", err)
	}
	var versions int
	if err := database.QueryRow(
		`SELECT COUNT(*) FROM claim_versions WHERE claim_id = 'c1'`).Scan(&versions); err != nil {
		t.Fatalf("probe versions: %v", err)
	}
	if versions != 0 {
		t.Errorf("%d versions survived their claim", versions)
	}
	if _, err := database.Exec(`DELETE FROM book_entities WHERE id = 'e1'`); err != nil {
		t.Fatalf("delete entity: %v", err)
	}
	var aliases int
	if err := database.QueryRow(
		`SELECT COUNT(*) FROM entity_aliases WHERE entity_id = 'e1'`).Scan(&aliases); err != nil {
		t.Fatalf("probe aliases: %v", err)
	}
	if aliases != 0 {
		t.Errorf("%d aliases survived their entity", aliases)
	}

	// Down: the whole family goes.
	if err := goose.DownTo(database, "migrations", 36); err != nil {
		t.Fatalf("migrate down to 36: %v", err)
	}
	for _, table := range []string{"book_claims", "claim_versions", "book_entities", "entity_aliases"} {
		var n int
		if err := database.QueryRow(
			`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?`,
			table).Scan(&n); err != nil {
			t.Fatalf("probe %s: %v", table, err)
		}
		if n != 0 {
			t.Errorf("%s survived the down migration", table)
		}
	}
}
