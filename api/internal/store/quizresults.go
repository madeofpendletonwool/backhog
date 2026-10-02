package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/collinpendleton/backhog/api/internal/achievements"
	"github.com/collinpendleton/backhog/api/internal/models"
)

// maxQuizQuestions caps one recorded quiz: beyond this is a client
// malfunctioning, not a quiz. The cap also caps what one request can add
// to the comprehension ladder, which is the most honesty a self-reported
// count can afford.
const maxQuizQuestions = 100

// RecordQuizResult stores one self-reported quiz outcome against a book in
// the caller's library and runs the comprehension ladder over it (MAD-471).
// Backhog stores and counts; it does not grade and does not generate — the
// questions, the answers and the score are all the client's own report, and
// the achievements they feed say so in their descriptions. The handler owns
// validation; here the row is written and the event evaluated in one
// transaction, so a result and its unlocks land together. Newly unlocked
// achievements are returned for the toast.
func (s *Store) RecordQuizResult(ctx context.Context, userID, entryID string, res models.QuizResult) (models.QuizResult, []models.AchievementStatus, error) {
	if res.Questions < 1 || res.Questions > maxQuizQuestions {
		return models.QuizResult{}, nil, fmt.Errorf("questions must be between 1 and %d", maxQuizQuestions)
	}
	if res.Correct < 0 || res.Correct > res.Questions {
		return models.QuizResult{}, nil, fmt.Errorf("correct must be between 0 and the questions asked")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return models.QuizResult{}, nil, err
	}
	defer tx.Rollback()

	var mediaType string
	err = tx.QueryRowContext(ctx,
		`SELECT media_type FROM library_entries WHERE user_id = ? AND id = ?`,
		userID, entryID).Scan(&mediaType)
	if errors.Is(err, sql.ErrNoRows) {
		return models.QuizResult{}, nil, ErrNotFound
	}
	if err != nil {
		return models.QuizResult{}, nil, err
	}
	if mediaType != models.MediaBook {
		return models.QuizResult{}, nil, fmt.Errorf("quiz results are recorded against books")
	}

	res.ID = newID()
	res.UserID = userID
	res.Entry = entryID
	err = tx.QueryRowContext(ctx, `
		INSERT INTO book_quiz_results
			(id, user_id, entry_id, questions, correct, chapter_start, chapter_end, source)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?) RETURNING created_at`,
		res.ID, userID, entryID, res.Questions, res.Correct,
		res.ChapterStart, res.ChapterEnd, res.Source).Scan(&res.CreatedAt)
	if err != nil {
		return models.QuizResult{}, nil, err
	}

	newly, err := evaluateQuizEventTx(ctx, tx, userID, entryID, res.ID)
	if err != nil {
		return models.QuizResult{}, nil, err
	}

	if err := tx.Commit(); err != nil {
		return models.QuizResult{}, nil, err
	}
	return res, s.hydrateUnlocks(ctx, userID, newly), nil
}

// evaluateQuizEventTx runs the catalogue against one recorded quiz result,
// inside the caller's transaction so the row and its unlocks land together.
// The snapshot carries only what a quiz predicate can read: the entry's
// domain and status, and the comprehension aggregate — correct answers in
// the result's calendar year, this result included because the insert has
// already happened.
func evaluateQuizEventTx(ctx context.Context, tx *sql.Tx, userID, entryID, resultID string) ([]unlockStub, error) {
	var e achievements.Entry
	var createdAt string
	err := tx.QueryRowContext(ctx, `
		SELECT e.status, e.created_at, q.created_at
		FROM library_entries e
		JOIN book_quiz_results q ON q.entry_id = e.id AND q.user_id = e.user_id
		WHERE e.user_id = ? AND e.id = ? AND q.id = ?`,
		userID, entryID, resultID).
		Scan(&e.Status, &e.CreatedAt, &createdAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	e.ID = entryID
	e.MediaType = models.MediaBook
	e.At = time.Now()
	if at, ok := parseDBTime(createdAt); ok {
		e.At = at
	}

	if err := snapshotQuizAggregatesTx(ctx, tx, userID, &e); err != nil {
		return nil, err
	}

	return unlockMatchingTx(ctx, tx, userID, entryID, achievements.EventQuiz, e, models.MediaBook)
}

// snapshotQuizAggregatesTx fills the quiz predicate's one aggregate: the
// correct answers of At's calendar year, the just-recorded result counted
// because it shares the transaction.
func snapshotQuizAggregatesTx(ctx context.Context, tx *sql.Tx, userID string, e *achievements.Entry) error {
	year := fmt.Sprintf("%04d", e.At.Year())
	return tx.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(correct), 0) FROM book_quiz_results
		WHERE user_id = ? AND strftime('%Y', created_at) = ?`,
		userID, year).Scan(&e.YearQuizCorrect)
}

// backfillQuizAchievementsTx replays the user's quiz history in record
// order so the comprehension ladder attaches to the result that crossed
// each line — the same pass the finishes replay makes over books. Like
// every backfill it is idempotent: only gaps fill.
func backfillQuizAchievementsTx(ctx context.Context, tx *sql.Tx, userID string) error {
	rows, err := tx.QueryContext(ctx, `
		SELECT q.entry_id, q.created_at, q.correct
		FROM book_quiz_results q
		JOIN library_entries e ON e.id = q.entry_id AND e.user_id = q.user_id
		WHERE q.user_id = ?
		ORDER BY q.created_at, q.id`, userID)
	if err != nil {
		return err
	}
	type replay struct {
		entryID string
		at      time.Time
		correct int
	}
	results := []replay{}
	for rows.Next() {
		var r replay
		var raw string
		if err := rows.Scan(&r.entryID, &raw, &r.correct); err != nil {
			rows.Close()
			return err
		}
		if at, ok := parseDBTime(raw); ok {
			r.at = at
			results = append(results, r)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	yearCorrect := map[int]int{}
	for _, r := range results {
		var e achievements.Entry
		e.ID = r.entryID
		e.MediaType = models.MediaBook
		e.At = r.at
		yearCorrect[r.at.Year()] += r.correct
		e.YearQuizCorrect = yearCorrect[r.at.Year()]
		if _, err := unlockMatchingTx(ctx, tx, userID, r.entryID,
			achievements.EventQuiz, e, models.MediaBook); err != nil {
			return err
		}
	}
	return nil
}

// QuizStats totals the caller's quiz activity for one calendar year: the
// questions answered and answered correctly. Reading Season's card reads
// them; like everything quiz-shaped they are the clients' own report.
func quizStats(ctx context.Context, q rowQuerier, userID, start, end string) (answered, correct int, err error) {
	row := q.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(questions), 0), COALESCE(SUM(correct), 0)
		FROM book_quiz_results
		WHERE user_id = ? AND created_at >= ? AND created_at < ?`,
		userID, start, end)
	if err := row.Scan(&answered, &correct); err != nil {
		return 0, 0, err
	}
	return answered, correct, nil
}
