package http

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/collinpendleton/backhog/api/internal/models"
)

// seedQuizBook puts one book in the owner's library — no files attached,
// because quiz results are the reader's own rows about their own reading
// and must work on a bare entry.
func (a *accountsTestApp) seedQuizBook(t *testing.T) string {
	t.Helper()
	ctx := context.Background()
	if _, err := a.store.DB().ExecContext(ctx,
		`INSERT INTO books (id, title) VALUES ('OLQW', 'The Village Mystery')`); err != nil {
		t.Fatalf("seed book: %v", err)
	}
	entry, err := a.store.AddBookEntry(ctx, a.userID(t, "owner@example.com"), "OLQW", nil, models.StatusPlaying)
	if err != nil {
		t.Fatalf("add entry: %v", err)
	}
	return entry.ID
}

// mintScopedToken mints a token carrying exactly the given scopes, over
// the real API the settings page uses.
func (a *accountsTestApp) mintScopedToken(t *testing.T, name string, scopes []string) string {
	t.Helper()
	status, body := a.as(t, a.owner, http.MethodPost, "/api/tokens", map[string]any{
		"name": name, "scopes": scopes,
	})
	if status != http.StatusCreated {
		t.Fatalf("mint %s = %d (%v)", name, status, body)
	}
	secret, _ := body["token"].(string)
	return secret
}

// recordQuiz posts one quiz result and returns the raw response.
func (a *accountsTestApp) recordQuiz(t *testing.T, secret, entryID string, body map[string]any) (int, map[string]any) {
	t.Helper()
	if body == nil {
		body = map[string]any{"questions": 5, "correct": 4, "source": "mcp"}
	}
	return a.asToken(t, secret, http.MethodPost, "/api/books/"+entryID+"/quiz-results", body)
}

// TestQuizResultsEndToEnd is the acceptance core of MAD-471: a token with
// quiz:write records a result, the comprehension ladder answers through the
// existing achievements framework, and the season card counts what was
// reported. Everything rides the bearer path — no cookie in sight — because
// the MCP server is the client this exists for.
func TestQuizResultsEndToEnd(t *testing.T) {
	app := newAccountsTestApp(t)
	entry := app.seedQuizBook(t)

	quizToken := app.mintScopedToken(t, "quizzing", []string{"books:read", "quiz:write"})

	status, body := app.recordQuiz(t, quizToken, entry, map[string]any{
		"questions": 5, "correct": 4, "source": "mcp",
		"chapter_range": map[string]any{"from": 1, "to": 2},
	})
	if status != http.StatusCreated {
		t.Fatalf("record = %d (%v)", status, body)
	}
	result, _ := body["quiz_result"].(map[string]any)
	if result["questions"] != float64(5) || result["correct"] != float64(4) {
		t.Fatalf("stored result = %v", result)
	}
	if result["chapter_start"] != float64(1) || result["chapter_end"] != float64(2) {
		t.Fatalf("chapter range = %v", result)
	}

	// The first result tips Book Report through the ordinary framework:
	// the write answers with the toast payload…
	unlocked, _ := body["achievements"].([]any)
	if len(unlocked) != 1 {
		t.Fatalf("first result unlocked %d achievements, want Book Report: %v", len(unlocked), body)
	}
	first, _ := unlocked[0].(map[string]any)
	if first["id"] != "book_report" {
		t.Fatalf("first unlock = %v, want book_report", first)
	}

	// …and the gallery agrees, with the entry attached like any unlock.
	status, list := app.asToken(t, quizToken, http.MethodGet, "/api/achievements", nil)
	if status != http.StatusOK {
		t.Fatalf("achievements = %d", status)
	}
	byID := map[string]map[string]any{}
	items, _ := list["achievements"].([]any)
	for _, raw := range items {
		a, _ := raw.(map[string]any)
		byID[a["id"].(string)] = a
	}
	if byID["book_report"]["unlocked_at"] == nil {
		t.Error("book_report locked in the gallery after a recorded result")
	}
	if byID["gold_star"]["unlocked_at"] != nil {
		t.Error("gold_star unlocked off 4 correct answers")
	}

	// The season card counts what was reported — questions answered and
	// answered correctly, this year.
	status, season := app.asToken(t, quizToken, http.MethodGet, "/api/achievements/reading-season", nil)
	if status != http.StatusOK {
		t.Fatalf("reading season = %d (%v)", status, season)
	}
	if season["quiz_answered"] != float64(5) || season["quiz_correct"] != float64(4) {
		t.Fatalf("quiz stats = %v / %v, want 5 / 4", season["quiz_answered"], season["quiz_correct"])
	}

	// The ladder climbs: a 25-correct year tips Gold Star, this result or
	// a later one — the framework's running count decides, not the client.
	app.recordQuiz(t, quizToken, entry, map[string]any{
		"questions": 25, "correct": 25, "source": "mcp"})
	status, list = app.asToken(t, quizToken, http.MethodGet, "/api/achievements", nil)
	items, _ = list["achievements"].([]any)
	for _, raw := range items {
		a, _ := raw.(map[string]any)
		if a["id"] == "gold_star" && a["unlocked_at"] == nil {
			t.Error("gold_star still locked after 29 correct answers in the year")
		}
	}

	// A quiz is for the reader's own shelf: someone else's entry is a 404,
	// the same ownership wall every book route holds.
	other := newAccountsTestApp(t)
	otherEntry := other.seedQuizBook(t)
	if status, _ := app.recordQuiz(t, quizToken, otherEntry, nil); status != http.StatusNotFound {
		t.Errorf("recording against a stranger's book = %d, want 404", status)
	}
}

// TestQuizResultsNeedWriteScope is the other acceptance line: a
// books:read-only token cannot write quiz results. The wall is the auth
// middleware's, before any route is resolved — so the read-only token gets
// the same 403 every write gets, and no other POST shape opens for the
// quiz:write token either.
func TestQuizResultsNeedWriteScope(t *testing.T) {
	app := newAccountsTestApp(t)
	entry := app.seedQuizBook(t)

	readOnly := app.mintScopedToken(t, "reader", []string{"books:read"})
	if status, body := app.recordQuiz(t, readOnly, entry, nil); status != http.StatusForbidden {
		t.Fatalf("read-only token recording a quiz = %d (%v), want 403", status, body)
	}

	// The quiz:write scope opens exactly one POST. Its sibling writes —
	// the library, the queue, a stranger's route shape — stay shut.
	quizOnly := app.mintScopedToken(t, "quizzing", []string{"books:read", "quiz:write"})
	if status, _ := app.asToken(t, quizOnly, http.MethodPost, "/api/library/reorder",
		map[string]any{"ordered": []string{"a"}}); status != http.StatusForbidden {
		t.Errorf("quiz token driving the library = %d, want 403", status)
	}
	if status, _ := app.asToken(t, quizOnly, http.MethodPost, "/api/library",
		map[string]any{"media_type": "book", "query": "x"}); status != http.StatusForbidden {
		t.Errorf("quiz token adding a book = %d, want 403", status)
	}
	// And the write stays cookie-capable: the app itself may record a
	// result the same way, for the day the reader quizzes in-app.
	status, _ := app.as(t, app.owner, http.MethodPost, "/api/books/"+entry+"/quiz-results",
		map[string]any{"questions": 3, "correct": 3, "source": "mcp"})
	if status != http.StatusCreated {
		t.Errorf("session recording a quiz = %d, want 201", status)
	}
}

// TestQuizResultValidation pins the honesty of the row itself: the counts
// have to be a quiz that could have happened, and the source names who
// gave it. Nothing here grades — the guards are just arithmetic.
func TestQuizResultValidation(t *testing.T) {
	app := newAccountsTestApp(t)
	entry := app.seedQuizBook(t)
	quizToken := app.mintScopedToken(t, "quizzing", []string{"books:read", "quiz:write"})

	cases := []struct {
		name string
		body map[string]any
	}{
		{"more correct than questions", map[string]any{"questions": 5, "correct": 6, "source": "mcp"}},
		{"no questions", map[string]any{"questions": 0, "correct": 0, "source": "mcp"}},
		{"negative correct", map[string]any{"questions": 5, "correct": -1, "source": "mcp"}},
		{"an absurd quiz", map[string]any{"questions": 500, "correct": 500, "source": "mcp"}},
		{"unnamed source", map[string]any{"questions": 5, "correct": 5, "source": "web"}},
		{"backwards range", map[string]any{"questions": 5, "correct": 5, "source": "mcp",
			"chapter_range": map[string]any{"from": 4, "to": 2}}},
		{"half a range", map[string]any{"questions": 5, "correct": 5, "source": "mcp",
			"chapter_range": map[string]any{"from": 2}}},
	}
	for _, tc := range cases {
		if status, _ := app.recordQuiz(t, quizToken, entry, tc.body); status != http.StatusBadRequest {
			t.Errorf("%s = %d, want 400", tc.name, status)
		}
	}

	// A quiz against a game entry is not this route's business: the book
	// door resolves {entryID} through the caller's book entries, so a game
	// is the same 404 as a stranger's book — no category error leaks.
	ctx := context.Background()
	if _, err := app.store.DB().ExecContext(ctx,
		`INSERT INTO games (id, name) VALUES (7, 'Game Q')`); err != nil {
		t.Fatalf("seed game: %v", err)
	}
	gameEntry, err := app.store.AddEntry(ctx, app.userID(t, "owner@example.com"), 7, models.StatusPlaying, nil)
	if err != nil {
		t.Fatalf("add game entry: %v", err)
	}
	if status, _ := app.recordQuiz(t, quizToken, gameEntry.ID, nil); status != http.StatusNotFound {
		t.Errorf("quiz against a game = %d, want 404", status)
	}
}

// TestQuizResultsScopedToTheirUser checks the ledger's walls: one user's
// quiz year is not another's, and last year's answers never tip this
// year's ladder.
func TestQuizResultsScopedToTheirUser(t *testing.T) {
	app := newAccountsTestApp(t)
	entry := app.seedQuizBook(t)
	quizToken := app.mintScopedToken(t, "quizzing", []string{"books:read", "quiz:write"})

	// Backdate a 30-correct year so the ladder's year scoping is visible:
	// the historical rows replay through the backfill, but only rows in
	// the event's own year count toward it.
	past := time.Now().AddDate(-1, 0, 0).UTC().Format("2006-01-02 15:04:05")
	if _, err := app.store.DB().ExecContext(context.Background(),
		`INSERT INTO book_quiz_results (id, user_id, entry_id, questions, correct, source, created_at)
			VALUES ('q-old', ?, ?, 30, 30, 'mcp', ?)`,
		app.userID(t, "owner@example.com"), entry, past); err != nil {
		t.Fatalf("seed old result: %v", err)
	}

	// The backfill replays the old year and unlocks what it earned then.
	status, list := app.asToken(t, quizToken, http.MethodGet, "/api/achievements", nil)
	if status != http.StatusOK {
		t.Fatalf("achievements = %d", status)
	}
	items, _ := list["achievements"].([]any)
	byID := map[string]map[string]any{}
	for _, raw := range items {
		a, _ := raw.(map[string]any)
		byID[a["id"].(string)] = a
	}
	if byID["gold_star"]["unlocked_at"] == nil {
		t.Error("last year's 30 correct answers never unlocked gold_star — the backfill must replay them")
	}

	// But this year starts at zero: the season card and a fresh result's
	// ladder see only this year's rows.
	status, season := app.asToken(t, quizToken, http.MethodGet, "/api/achievements/reading-season", nil)
	if status != http.StatusOK {
		t.Fatalf("reading season = %d", status)
	}
	if season["quiz_answered"] != float64(0) || season["quiz_correct"] != float64(0) {
		t.Fatalf("this year's stats = %v / %v, want 0 / 0", season["quiz_answered"], season["quiz_correct"])
	}
}
