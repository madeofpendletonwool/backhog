package db

import (
	"path/filepath"
	"testing"

	"github.com/pressly/goose/v3"
)

// TestQuizResultsMigration verifies 00036: the table arrives with its
// guards (a result cannot claim more correct answers than questions, a
// chapter range cannot run backwards), and the down half drops it.
func TestQuizResultsMigration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "quiz_results_test.db")
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

	insert := func(cols string, args ...any) error {
		stmt, err := database.Prepare(`INSERT INTO book_quiz_results (` + cols + `) VALUES (` +
			placeholders(len(args)) + `)`)
		if err != nil {
			return err
		}
		defer stmt.Close()
		_, err = stmt.Exec(args...)
		return err
	}

	// The honest shape stores whole: counts, an optional chapter range,
	// a source.
	if err := insert(`id, user_id, entry_id, questions, correct, chapter_start, chapter_end, source`,
		"q1", "u1", "b1", 8, 6, 2, 3, "mcp"); err != nil {
		t.Fatalf("honest result rejected: %v", err)
	}
	if err := insert(`id, user_id, entry_id, questions, correct`,
		"q2", "u1", "b1", 8, 0); err != nil {
		t.Fatalf("a zero-correct quiz is still a quiz: %v", err)
	}

	// The guards: no perfect scores on questions nobody asked, no empty
	// quizzes, no backwards ranges.
	if err := insert(`id, user_id, entry_id, questions, correct`,
		"q3", "u1", "b1", 5, 6); err == nil {
		t.Error("correct above questions was accepted")
	}
	if err := insert(`id, user_id, entry_id, questions, correct`,
		"q4", "u1", "b1", 0, 0); err == nil {
		t.Error("a zero-question quiz was accepted")
	}
	if err := insert(`id, user_id, entry_id, questions, correct, chapter_start, chapter_end`,
		"q5", "u1", "b1", 5, 5, 4, 2); err == nil {
		t.Error("a backwards chapter range was accepted")
	}
	if err := insert(`id, user_id, entry_id, questions, correct, chapter_start`,
		"q6", "u1", "b1", 5, 5, 1); err == nil {
		t.Error("a half-present chapter range was accepted")
	}

	// The default source is the one client there is.
	var source string
	if err := database.QueryRow(
		`SELECT source FROM book_quiz_results WHERE id = 'q2'`).Scan(&source); err != nil {
		t.Fatalf("read default source: %v", err)
	}
	if source != "mcp" {
		t.Errorf("default source = %q, want mcp", source)
	}

	// Down: the table goes.
	if err := goose.DownTo(database, "migrations", 35); err != nil {
		t.Fatalf("migrate down to 35: %v", err)
	}
	var tables int
	if err := database.QueryRow(
		`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'book_quiz_results'`).
		Scan(&tables); err != nil {
		t.Fatalf("probe table: %v", err)
	}
	if tables != 0 {
		t.Errorf("book_quiz_results survived the down migration")
	}
}

// placeholders renders n SQL question marks for the hand-built inserts.
func placeholders(n int) string {
	out := ""
	for i := 0; i < n; i++ {
		if i > 0 {
			out += ","
		}
		out += "?"
	}
	return out
}
