package store

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/collinpendleton/backhog/api/internal/models"
)

// unlockedIDs reads the user's achievement ledger as a set.
func quizUnlockSet(t *testing.T, s *Store, userID string) map[string]bool {
	t.Helper()
	rows, err := s.db.Query(`SELECT achievement_id FROM achievement_unlocks WHERE user_id = ?`, userID)
	if err != nil {
		t.Fatalf("read unlocks: %v", err)
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("scan unlock: %v", err)
		}
		out[id] = true
	}
	return out
}

// recordQuiz is the store call the HTTP handler makes.
func recordQuiz(t *testing.T, s *Store, userID, entryID string, questions, correct int) []models.AchievementStatus {
	t.Helper()
	_, unlocked, err := s.RecordQuizResult(context.Background(), userID, entryID, models.QuizResult{
		Questions: questions, Correct: correct, Source: models.QuizSourceMCP,
	})
	if err != nil {
		t.Fatalf("record quiz %d/%d: %v", correct, questions, err)
	}
	return unlocked
}

// TestQuizLadderLive walks the comprehension ladder the way a quizzing
// season actually happens: one result at a time, each able to tip the rung
// its correct answers crossed. The unlock lands with the write, inside the
// same transaction, and the second call for an already-unlocked rung
// reports nothing new.
func TestQuizLadderLive(t *testing.T) {
	s := newBooksStore(t)

	// b2 is one of u1's finished books; any book entry serves.
	first := recordQuiz(t, s, "u1", "b2", 5, 5)
	if len(first) != 1 || first[0].ID != "book_report" {
		t.Fatalf("first result unlocked %v, want book_report alone", first)
	}

	// Climb to 24 in the year, then cross the line.
	recordQuiz(t, s, "u1", "b2", 9, 9)
	recordQuiz(t, s, "u1", "b2", 10, 10)
	unlocked := quizUnlockSet(t, s, "u1")
	if unlocked["gold_star"] {
		t.Fatal("gold_star unlocked at 24 correct")
	}
	tipped := recordQuiz(t, s, "u1", "b2", 1, 1)
	if !quizUnlockSet(t, s, "u1")["gold_star"] {
		t.Fatal("the 25th correct answer did not tip gold_star")
	}
	found := false
	for _, a := range tipped {
		if a.ID == "gold_star" {
			found = true
		}
		if a.ID == "book_report" {
			t.Error("book_report re-announced on a later result")
		}
	}
	if !found {
		t.Fatal("the tipping result's toast lacked gold_star")
	}

	// The honor roll is reachable in one honest sitting of a long quiz.
	honor := recordQuiz(t, s, "u1", "b2", 100, 100)
	ids := map[string]bool{}
	for _, a := range honor {
		ids[a.ID] = true
	}
	if !ids["honor_roll"] || !quizUnlockSet(t, s, "u1")["honor_roll"] {
		t.Fatal("a 100-correct year did not tip honor_roll")
	}
}

// TestQuizLadderBackfillReplaysHistory pins the gallery's completeness: a
// season of recorded results that predates nothing (no unlocks in the
// ledger) still counts once the backfill replays it, and replaying twice
// changes nothing — the same idempotence every backfill holds.
func TestQuizLadderBackfillReplaysHistory(t *testing.T) {
	s := newBooksStore(t)

	year := time.Now().UTC().Year()
	stamp := func(month int) string {
		return fmt.Sprintf("%04d-%02d-01 12:00:00", year, month)
	}
	// 30 correct this year across three results, one last year that must
	// not move this year's ladder.
	for _, q := range []struct {
		id                 string
		questions, correct int
		when               string
	}{
		{"q1", 10, 8, stamp(1)},
		{"q2", 10, 10, stamp(2)},
		{"q3", 10, 12 - 2, stamp(3)},
		{"q-old", 10, 10, fmt.Sprintf("%04d-03-01 12:00:00", year-1)},
	} {
		if _, err := s.db.Exec(`INSERT INTO book_quiz_results
			(id, user_id, entry_id, questions, correct, source, created_at)
			VALUES (?, 'u1', 'b2', ?, ?, 'mcp', ?)`,
			q.id, q.questions, q.correct, q.when); err != nil {
			t.Fatalf("seed %s: %v", q.id, err)
		}
	}

	if err := s.BackfillAchievements(context.Background(), "u1"); err != nil {
		t.Fatalf("backfill: %v", err)
	}
	unlocked := quizUnlockSet(t, s, "u1")
	if !unlocked["book_report"] || !unlocked["gold_star"] {
		t.Fatalf("backfilled ladder = %v, want book_report and gold_star", unlocked)
	}
	if unlocked["honor_roll"] {
		t.Fatal("honor_roll unlocked off 30 correct")
	}

	// Idempotent: the replay only fills gaps.
	if err := s.BackfillAchievements(context.Background(), "u1"); err != nil {
		t.Fatalf("backfill again: %v", err)
	}
	var count int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM achievement_unlocks
		WHERE user_id = 'u1' AND achievement_id = 'gold_star'`).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Errorf("gold_star unlocked %d times, want 1", count)
	}
}

// TestQuizLadderStaysInTheBookArena is the cross-talk check: u1 has a
// finished game, and a quiz result must not wake any games predicate —
// nor may a game finish ever tip the quiz ladder. The routing is by
// domain and event kind; this pins it.
func TestQuizLadderStaysInTheBookArena(t *testing.T) {
	s := newBooksStore(t)

	// The baseline: run the backfill so the fixture's history — including
	// the finished game — has unlocked what it earned, then quiz.
	if err := s.BackfillAchievements(context.Background(), "u1"); err != nil {
		t.Fatalf("backfill: %v", err)
	}
	before := quizUnlockSet(t, s, "u1")

	recordQuiz(t, s, "u1", "b2", 100, 100)

	// The quiz results unlocked only quiz-shaped achievements: the games
	// ladders hold no quiz additions.
	after := quizUnlockSet(t, s, "u1")
	if !before["first_blood"] {
		t.Error("fixture sanity: the finished game should hold first_blood")
	}
	for id := range after {
		if before[id] {
			continue
		}
		switch id {
		case "book_report", "gold_star", "honor_roll":
		default:
			t.Errorf("a quiz result unlocked the non-quiz achievement %q", id)
		}
	}

	// And the ledger's quiz rungs exist only through quiz events: u2, who
	// owns nothing and quizzed never, has no quiz unlocks.
	if got := quizUnlockSet(t, s, "u2"); len(got) != 0 {
		t.Errorf("u2 unlocked %v off u1's quizzes", got)
	}
}

// TestReadingSeasonCountsQuizResults extends the season's story: the
// comprehension half totals questions and correct answers per year, scoped
// to the caller.
func TestReadingSeasonCountsQuizResults(t *testing.T) {
	s := newBooksStore(t)
	year := time.Now().UTC().Year()

	recordQuiz(t, s, "u1", "b2", 7, 5)
	recordQuiz(t, s, "u1", "b3", 3, 3)
	if _, err := s.db.Exec(`INSERT INTO book_quiz_results
		(id, user_id, entry_id, questions, correct, source, created_at)
		VALUES ('q-old', 'u1', 'b2', 50, 50, 'mcp', ?)`,
		fmt.Sprintf("%04d-06-01 12:00:00", year-1)); err != nil {
		t.Fatalf("seed old result: %v", err)
	}

	season, err := s.ReadingSeason(context.Background(), "u1", year)
	if err != nil {
		t.Fatalf("ReadingSeason: %v", err)
	}
	if season.QuizAnswered != 10 || season.QuizCorrect != 8 {
		t.Fatalf("quiz stats = %d/%d, want 10/8 (this year only)",
			season.QuizAnswered, season.QuizCorrect)
	}

	last, err := s.ReadingSeason(context.Background(), "u1", year-1)
	if err != nil {
		t.Fatalf("ReadingSeason last year: %v", err)
	}
	if last.QuizAnswered != 50 || last.QuizCorrect != 50 {
		t.Fatalf("last year's quiz stats = %d/%d, want 50/50", last.QuizAnswered, last.QuizCorrect)
	}

	if other, err := s.ReadingSeason(context.Background(), "u2", year); err != nil {
		t.Fatalf("ReadingSeason u2: %v", err)
	} else if other.QuizAnswered != 0 || other.QuizCorrect != 0 {
		t.Fatalf("u2's quiz stats = %d/%d, want 0/0", other.QuizAnswered, other.QuizCorrect)
	}
}
