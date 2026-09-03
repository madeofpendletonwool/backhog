package db

import (
	"path/filepath"
	"testing"

	"github.com/pressly/goose/v3"
)

// TestBookAchievementsMigration verifies 00019: the achievement ledger gains
// its domain annotation without touching a single unlock — the table's one
// hard rule is that history never breaks — existing rows backfill to 'game',
// the CHECK holds the line against bogus values, and the down half removes
// the column while keeping the rows.
func TestBookAchievementsMigration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "book_achievements_test.db")
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

	// Pre-migration state: two unlocks from the games era, one tied to the
	// entry that earned it, one free-floating (a time-window achievement).
	if err := goose.UpTo(database, "migrations", 18); err != nil {
		t.Fatalf("migrate to 18: %v", err)
	}
	seed := []string{
		`INSERT INTO users (id, email, username, password_hash) VALUES ('u1', 'a@a.a', 'a', 'x')`,
		`INSERT INTO games (id, name) VALUES (1, 'Game A')`,
		`INSERT INTO library_entries (id, user_id, media_type, game_id, status)
			VALUES ('e1', 'u1', 'game', 1, 'played')`,
		`INSERT INTO achievement_unlocks (id, user_id, achievement_id, entry_id)
			VALUES ('un1', 'u1', 'first_blood', 'e1')`,
		`INSERT INTO achievement_unlocks (id, user_id, achievement_id)
			VALUES ('un2', 'u1', 'restraint')`,
	}
	for _, q := range seed {
		if _, err := database.Exec(q); err != nil {
			t.Fatalf("seed %q: %v", q, err)
		}
	}

	if err := Migrate(database); err != nil {
		t.Fatalf("migrate up: %v", err)
	}

	// Both pre-migration unlocks survive, annotated 'game' by the backfill.
	type row struct {
		achievementID string
		domain        string
	}
	rows := map[string]row{}
	rrows, err := database.Query(
		`SELECT id, achievement_id, domain FROM achievement_unlocks WHERE user_id = 'u1'`)
	if err != nil {
		t.Fatalf("read unlocks: %v", err)
	}
	for rrows.Next() {
		var id string
		var r row
		if err := rrows.Scan(&id, &r.achievementID, &r.domain); err != nil {
			rrows.Close()
			t.Fatalf("scan unlock: %v", err)
		}
		rows[id] = r
	}
	rrows.Close()
	if len(rows) != 2 {
		t.Fatalf("unlock count after migration = %d, want 2 preserved", len(rows))
	}
	for id, want := range map[string]row{
		"un1": {achievementID: "first_blood", domain: "game"},
		"un2": {achievementID: "restraint", domain: "game"},
	} {
		if got := rows[id]; got != want {
			t.Errorf("%s = %+v, want %+v (history must survive intact)", id, got, want)
		}
	}

	// A book unlock stamps its own domain, and the CHECK refuses a value
	// outside the three the catalogue speaks.
	if _, err := database.Exec(`
		INSERT INTO achievement_unlocks (id, user_id, achievement_id, domain)
		VALUES ('un3', 'u1', 'first_edition', 'book')`); err != nil {
		t.Fatalf("insert book unlock: %v", err)
	}
	for _, bad := range []string{"novel", "GAME", ""} {
		if _, err := database.Exec(`
			INSERT INTO achievement_unlocks (id, user_id, achievement_id, domain)
			VALUES ('bad', 'u1', 'first_edition', ?)`, bad); err == nil {
			t.Errorf("domain %q was accepted", bad)
		}
	}

	// Down keeps the rows and drops the annotation.
	if err := goose.DownTo(database, "migrations", 18); err != nil {
		t.Fatalf("migrate down: %v", err)
	}
	var n int
	if err := database.QueryRow(
		`SELECT COUNT(*) FROM achievement_unlocks WHERE user_id = 'u1'`).Scan(&n); err != nil {
		t.Fatalf("count unlocks after down: %v", err)
	}
	if n != 3 {
		t.Errorf("unlock count after down = %d, want 3", n)
	}
	if _, err := database.Exec(
		`SELECT domain FROM achievement_unlocks LIMIT 1`); err == nil {
		t.Error("domain column survived the down migration")
	}
}
