package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/collinpendleton/backhog/api/internal/models"
)

// maxSessionMinutes caps a single logged session at 24 hours, matching the
// database CHECK. Anything longer is a typo, not a session.
const maxSessionMinutes = 1440

// AddSession records a stretch of play against an entry.
//
// Logging time on something still sitting in the backlog implies you've started
// it, so the status moves to 'playing' and it leaves the queue — otherwise the
// queue would keep suggesting a game you're already partway through.
func (s *Store) AddSession(ctx context.Context, userID, entryID, playedOn string, minutes int, note string) (models.Session, error) {
	if minutes <= 0 || minutes > maxSessionMinutes {
		return models.Session{}, fmt.Errorf("minutes must be between 1 and %d", maxSessionMinutes)
	}
	if _, err := time.Parse("2006-01-02", playedOn); err != nil {
		return models.Session{}, fmt.Errorf("played_on must be a date like 2026-07-20")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return models.Session{}, err
	}
	defer tx.Rollback()

	var status string
	err = tx.QueryRowContext(ctx,
		`SELECT status FROM library_entries WHERE user_id = ? AND id = ?`, userID, entryID).Scan(&status)
	if errors.Is(err, sql.ErrNoRows) {
		return models.Session{}, ErrNotFound
	}
	if err != nil {
		return models.Session{}, err
	}

	session := models.Session{
		ID: newID(), EntryID: entryID, PlayedOn: playedOn, Minutes: minutes, Note: note,
	}
	err = tx.QueryRowContext(ctx, `
		INSERT INTO play_sessions (id, user_id, entry_id, played_on, minutes, note)
		VALUES (?, ?, ?, ?, ?, ?) RETURNING created_at`,
		session.ID, userID, entryID, playedOn, minutes, note).Scan(&session.CreatedAt)
	if err != nil {
		return models.Session{}, err
	}

	if status == models.StatusBacklog || status == models.StatusWishlist {
		if _, err := tx.ExecContext(ctx, `
			UPDATE library_entries
			SET status = 'playing',
			    queue_position = NULL,
			    started_at = COALESCE(started_at, CURRENT_TIMESTAMP),
			    updated_at = CURRENT_TIMESTAMP
			WHERE user_id = ? AND id = ?`, userID, entryID); err != nil {
			return models.Session{}, err
		}
	}

	if err := tx.Commit(); err != nil {
		return models.Session{}, err
	}
	return session, nil
}

// Sessions returns an entry's logged sessions, newest first.
func (s *Store) Sessions(ctx context.Context, userID, entryID string) ([]models.Session, error) {
	// date() forces TEXT: the driver otherwise hands back a DATE column as a
	// full timestamp, and the client wants a plain 2026-07-20.
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, entry_id, date(played_on), minutes, note, created_at
		FROM play_sessions
		WHERE user_id = ? AND entry_id = ?
		ORDER BY played_on DESC, created_at DESC`, userID, entryID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	sessions := []models.Session{}
	for rows.Next() {
		var s models.Session
		if err := rows.Scan(&s.ID, &s.EntryID, &s.PlayedOn, &s.Minutes, &s.Note, &s.CreatedAt); err != nil {
			return nil, err
		}
		sessions = append(sessions, s)
	}
	return sessions, rows.Err()
}

// DeletePlaySession removes a logged playtime session. Named to avoid colliding
// with DeleteSession, which revokes an auth session.
func (s *Store) DeletePlaySession(ctx context.Context, userID, sessionID string) error {
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM play_sessions WHERE user_id = ? AND id = ?`, userID, sessionID)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}
