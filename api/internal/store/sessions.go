package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/collinpendleton/backhog/api/internal/models"
)

// SessionTTL is how long a login lasts.
const SessionTTL = 30 * 24 * time.Hour

// CreateSession issues a new session token for a user.
func (s *Store) CreateSession(ctx context.Context, userID string) (string, time.Time, error) {
	id := newID() + newID() // 256 bits of entropy for the cookie value
	expires := time.Now().Add(SessionTTL)
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO sessions (id, user_id, expires_at) VALUES (?, ?, ?)`, id, userID, expires)
	if err != nil {
		return "", time.Time{}, err
	}
	return id, expires, nil
}

// UserForSession resolves a session token to its user, rejecting expired
// sessions. Returns ErrNotFound for unknown or expired tokens.
func (s *Store) UserForSession(ctx context.Context, sessionID string) (models.User, error) {
	var u models.User
	err := s.db.QueryRowContext(ctx, `
		SELECT u.id, u.email, u.username, u.created_at
		FROM sessions s JOIN users u ON u.id = s.user_id
		WHERE s.id = ? AND s.expires_at > CURRENT_TIMESTAMP`, sessionID).
		Scan(&u.ID, &u.Email, &u.Username, &u.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return models.User{}, ErrNotFound
	}
	return u, err
}

// DeleteSession revokes a single session.
func (s *Store) DeleteSession(ctx context.Context, sessionID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE id = ?`, sessionID)
	return err
}

// PurgeExpiredSessions removes sessions past their expiry.
func (s *Store) PurgeExpiredSessions(ctx context.Context) (int64, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE expires_at <= CURRENT_TIMESTAMP`)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
