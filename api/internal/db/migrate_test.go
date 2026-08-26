package db

import (
	"path/filepath"
	"testing"

	"github.com/pressly/goose/v3"
)

// TestMigrationsUpAndDown runs the full ladder forward, all the way back, and
// forward again — the round trip every future migration has to survive.
func TestMigrationsUpAndDown(t *testing.T) {
	path := filepath.Join(t.TempDir(), "migrate_test.db")
	database, err := Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer database.Close()

	if err := Migrate(database); err != nil {
		t.Fatalf("migrate up: %v", err)
	}

	goose.SetBaseFS(migrations)
	goose.SetLogger(goose.NopLogger())
	if err := goose.SetDialect("sqlite3"); err != nil {
		t.Fatalf("set dialect: %v", err)
	}
	if err := goose.DownTo(database, "migrations", 0); err != nil {
		t.Fatalf("migrate down: %v", err)
	}

	if err := Migrate(database); err != nil {
		t.Fatalf("migrate up again: %v", err)
	}
}
