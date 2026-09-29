package db

import (
	"path/filepath"
	"testing"

	"github.com/pressly/goose/v3"
)

// TestAPITokensMigration verifies 00032: the token table lands with its
// constraints, secrets are unique, and ownership cascades when the account
// goes. The down half drops the table whole.
func TestAPITokensMigration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "api_tokens_test.db")
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
		`INSERT INTO users (id, email, username, password_hash, role)
			VALUES ('u1', 'a@example.com', 'aaa', 'hash', 'member')`,
		`INSERT INTO api_tokens (id, user_id, name, token_hash, scopes)
			VALUES ('t1', 'u1', 'mcp', 'hash-one', 'books:read')`,
	}
	for _, q := range seed {
		if _, err := database.Exec(q); err != nil {
			t.Fatalf("seed %q: %v", q, err)
		}
	}

	// A second token with the same secret is refused: the hash is the
	// credential's identity.
	if _, err := database.Exec(
		`INSERT INTO api_tokens (id, user_id, name, token_hash) VALUES ('t2', 'u1', 'dupe', 'hash-one')`); err == nil {
		t.Error("duplicate token_hash was allowed")
	}
	// A token cannot point at nobody.
	if _, err := database.Exec(
		`INSERT INTO api_tokens (id, user_id, name, token_hash) VALUES ('t3', 'ghost', 'x', 'hash-two')`); err == nil {
		t.Error("unknown user_id was allowed")
	}
	// A revoked row keeps its place: revocation is a state, not a deletion.
	if _, err := database.Exec(
		`UPDATE api_tokens SET revoked_at = CURRENT_TIMESTAMP WHERE id = 't1'`); err != nil {
		t.Fatalf("revoke: %v", err)
	}

	// Deleting the account takes its tokens with it — a credential that
	// outlived its person would be a ghost key.
	if _, err := database.Exec(`DELETE FROM users WHERE id = 'u1'`); err != nil {
		t.Fatalf("delete user: %v", err)
	}
	var count int
	if err := database.QueryRow(`SELECT COUNT(*) FROM api_tokens`).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 0 {
		t.Error("tokens survived the account's deletion")
	}

	// Down: the table goes whole.
	if err := goose.DownTo(database, "migrations", 31); err != nil {
		t.Fatalf("migrate down to 31: %v", err)
	}
	if err := database.QueryRow(
		`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'api_tokens'`).Scan(&count); err != nil {
		t.Fatalf("probe table: %v", err)
	}
	if count != 0 {
		t.Error("api_tokens still exists after down")
	}
}
