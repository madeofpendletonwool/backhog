package store

import (
	"context"
	"database/sql"
	"errors"
	"math"

	"github.com/collinpendleton/backhog/api/internal/models"
)

// positionGap is the spacing used when appending to the end of a queue.
const positionGap = 1024.0

// minGap is the smallest gap we tolerate between neighbours before renormalising.
// Below this, repeated midpoint insertions would exhaust float64 precision.
const minGap = 1e-6

// ErrNeedsRenormalize signals that positions have converged and the caller
// should renormalise before retrying. It never escapes this package.
var errNeedsRenormalize = errors.New("positions converged")

// midpoint returns a position between two neighbours. A nil bound means "the
// edge of the list". Returns errNeedsRenormalize when the gap is too small to
// subdivide safely.
func midpoint(before, after *float64) (float64, error) {
	switch {
	case before == nil && after == nil:
		return positionGap, nil
	case before == nil:
		return *after - positionGap, nil
	case after == nil:
		return *before + positionGap, nil
	}
	if *after-*before < minGap {
		return 0, errNeedsRenormalize
	}
	return *before + (*after-*before)/2, nil
}

// Queue returns the ordered play queue: backlog entries, by queue position.
func (s *Store) Queue(ctx context.Context, userID string) ([]models.Entry, error) {
	return s.queryEntries(ctx, entrySelect+`
		WHERE e.user_id = ? AND e.status = 'backlog'
		ORDER BY e.queue_position ASC NULLS LAST, e.created_at ASC`, userID)
}

// MoveEntry repositions an entry in the play queue, between the entries
// identified by beforeID and afterID (either may be empty to mean an end of the
// list). Only the moved row is written; on precision exhaustion the whole queue
// is renormalised once and the move retried.
func (s *Store) MoveEntry(ctx context.Context, userID, entryID, beforeID, afterID string) error {
	err := s.moveEntryOnce(ctx, userID, entryID, beforeID, afterID)
	if errors.Is(err, errNeedsRenormalize) {
		if err := s.renormalizeQueue(ctx, userID); err != nil {
			return err
		}
		return s.moveEntryOnce(ctx, userID, entryID, beforeID, afterID)
	}
	return err
}

func (s *Store) moveEntryOnce(ctx context.Context, userID, entryID, beforeID, afterID string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Confirm the entry is ours before reading anything else.
	var exists int
	err = tx.QueryRowContext(ctx,
		`SELECT 1 FROM library_entries WHERE user_id = ? AND id = ?`, userID, entryID).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}

	before, err := queuePositionOf(ctx, tx, userID, beforeID)
	if err != nil {
		return err
	}
	after, err := queuePositionOf(ctx, tx, userID, afterID)
	if err != nil {
		return err
	}

	pos, err := midpoint(before, after)
	if err != nil {
		return err
	}

	if _, err := tx.ExecContext(ctx,
		`UPDATE library_entries SET queue_position = ?, updated_at = CURRENT_TIMESTAMP
		 WHERE user_id = ? AND id = ?`, pos, userID, entryID); err != nil {
		return err
	}
	return tx.Commit()
}

// queuePositionOf resolves a neighbour id to its position. An empty id means
// there is no neighbour on that side.
func queuePositionOf(ctx context.Context, tx *sql.Tx, userID, entryID string) (*float64, error) {
	if entryID == "" {
		return nil, nil
	}
	var pos sql.NullFloat64
	err := tx.QueryRowContext(ctx,
		`SELECT queue_position FROM library_entries WHERE user_id = ? AND id = ?`,
		userID, entryID).Scan(&pos)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if !pos.Valid {
		return nil, nil
	}
	return &pos.Float64, nil
}

// renormalizeQueue rewrites every position in the queue back to even spacing.
// This is O(n) but only runs after many thousands of midpoint insertions.
func (s *Store) renormalizeQueue(ctx context.Context, userID string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	rows, err := tx.QueryContext(ctx, `
		SELECT id FROM library_entries
		WHERE user_id = ? AND status = 'backlog'
		ORDER BY queue_position ASC NULLS LAST, created_at ASC`, userID)
	if err != nil {
		return err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	for i, id := range ids {
		if _, err := tx.ExecContext(ctx,
			`UPDATE library_entries SET queue_position = ? WHERE user_id = ? AND id = ?`,
			float64(i+1)*positionGap, userID, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// nextQueuePosition returns a position at the end of the user's queue.
func (s *Store) nextQueuePosition(ctx context.Context, userID string) (float64, error) {
	var maxPos sql.NullFloat64
	err := s.db.QueryRowContext(ctx,
		`SELECT MAX(queue_position) FROM library_entries WHERE user_id = ? AND status = 'backlog'`,
		userID).Scan(&maxPos)
	if err != nil {
		return 0, err
	}
	return nextAfter(maxPos), nil
}

func nextQueuePositionTx(ctx context.Context, tx *sql.Tx, userID string) (float64, error) {
	var maxPos sql.NullFloat64
	err := tx.QueryRowContext(ctx,
		`SELECT MAX(queue_position) FROM library_entries WHERE user_id = ? AND status = 'backlog'`,
		userID).Scan(&maxPos)
	if err != nil {
		return 0, err
	}
	return nextAfter(maxPos), nil
}

func nextAfter(maxPos sql.NullFloat64) float64 {
	if !maxPos.Valid || math.IsNaN(maxPos.Float64) {
		return positionGap
	}
	return maxPos.Float64 + positionGap
}
