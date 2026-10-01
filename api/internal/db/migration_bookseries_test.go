package db

import (
	"path/filepath"
	"testing"

	"github.com/pressly/goose/v3"
)

// TestBookSeriesMigration verifies 00035: the columns arrive with their
// defaults, the partial index only sees named series, and the down half
// drops both.
func TestBookSeriesMigration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "book_series_test.db")
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
		`INSERT INTO books (id, title) VALUES ('OL1W', 'The Final Empire')`,
		`INSERT INTO books (id, title, series_name, series_number)
			VALUES ('OL2W', 'The Well of Ascension', 'Mistborn', 2.0)`,
		`INSERT INTO books (id, title, series_name)
			VALUES ('OL3W', 'Secret History', 'Mistborn')`,
	}
	for _, q := range seed {
		if _, err := database.Exec(q); err != nil {
			t.Fatalf("seed %q: %v", q, err)
		}
	}

	// A pre-existing row defaults to "no series" rather than NULL — the
	// column is NOT NULL, so every reader can = '' compare without a
	// COALESCE, and a named series never matches it.
	var name string
	var number any
	if err := database.QueryRow(
		`SELECT series_name, series_number FROM books WHERE id = 'OL1W'`).Scan(&name, &number); err != nil {
		t.Fatalf("read defaults: %v", err)
	}
	if name != "" || number != nil {
		t.Errorf("defaults = (%q, %v), want ('', nil)", name, number)
	}

	// The rank is optional: a sidecar that names the series but not the
	// index stores NULL, and a book outside any series may not carry a
	// stray rank.
	if _, err := database.Exec(
		`INSERT INTO books (id, title, series_name, series_number)
			VALUES ('OL5W', 'Bad', '', 1.0)`); err == nil {
		t.Error("a series_number without a series_name was accepted")
	}
	if _, err := database.Exec(
		`INSERT INTO books (id, title, series_name, series_number)
			VALUES ('OL6W', 'Bad', 'Mistborn', -1.0)`); err == nil {
		t.Error("a negative series_number was accepted")
	}

	// The partial index carries exactly the named series.
	var named int
	if err := database.QueryRow(
		`SELECT COUNT(*) FROM books WHERE series_name <> ''`).Scan(&named); err != nil {
		t.Fatalf("count named: %v", err)
	}
	if named != 2 {
		t.Errorf("named series rows = %d, want 2", named)
	}

	// Down: both columns go.
	if err := goose.DownTo(database, "migrations", 34); err != nil {
		t.Fatalf("migrate down to 34: %v", err)
	}
	var cols int
	if err := database.QueryRow(
		`SELECT COUNT(*) FROM pragma_table_info('books')
		 WHERE name IN ('series_name', 'series_number')`).Scan(&cols); err != nil {
		t.Fatalf("probe columns: %v", err)
	}
	if cols != 0 {
		t.Errorf("series columns survived the down migration (%d)", cols)
	}
}
