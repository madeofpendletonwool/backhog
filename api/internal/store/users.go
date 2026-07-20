package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/collinpendleton/backhog/api/internal/models"
)

// CreateUser inserts a user and returns it. The caller supplies an already
// hashed password.
func (s *Store) CreateUser(ctx context.Context, email, username, passwordHash string) (models.User, error) {
	u := models.User{ID: newID(), Email: email, Username: username}
	err := s.db.QueryRowContext(ctx, `
		INSERT INTO users (id, email, username, password_hash)
		VALUES (?, ?, ?, ?)
		RETURNING created_at`, u.ID, email, username, passwordHash).Scan(&u.CreatedAt)
	if err != nil {
		if isUniqueViolation(err) {
			return models.User{}, ErrConflict
		}
		return models.User{}, err
	}
	return u, nil
}

// GetUserByEmail returns the user and their password hash for login.
func (s *Store) GetUserByEmail(ctx context.Context, email string) (models.User, string, error) {
	var u models.User
	var hash string
	err := s.db.QueryRowContext(ctx, `
		SELECT id, email, username, password_hash, created_at
		FROM users WHERE email = ? COLLATE NOCASE`, email).
		Scan(&u.ID, &u.Email, &u.Username, &hash, &u.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return models.User{}, "", ErrNotFound
	}
	return u, hash, err
}

// GetUser returns a user by id.
func (s *Store) GetUser(ctx context.Context, id string) (models.User, error) {
	var u models.User
	err := s.db.QueryRowContext(ctx, `
		SELECT id, email, username, created_at FROM users WHERE id = ?`, id).
		Scan(&u.ID, &u.Email, &u.Username, &u.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return models.User{}, ErrNotFound
	}
	return u, err
}

// GetPasswordHash returns the stored hash for a user, for password changes.
func (s *Store) GetPasswordHash(ctx context.Context, userID string) (string, error) {
	var hash string
	err := s.db.QueryRowContext(ctx, `SELECT password_hash FROM users WHERE id = ?`, userID).Scan(&hash)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return hash, err
}

// UpdatePassword sets a new password hash and invalidates all other sessions.
func (s *Store) UpdatePassword(ctx context.Context, userID, passwordHash, keepSessionID string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx,
		`UPDATE users SET password_hash = ? WHERE id = ?`, passwordHash, userID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM sessions WHERE user_id = ? AND id != ?`, userID, keepSessionID); err != nil {
		return err
	}
	return tx.Commit()
}

func isUniqueViolation(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed")
}
