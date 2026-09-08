package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/collinpendleton/backhog/api/internal/models"
)

// ErrLastAdmin refuses the change that would leave the installation with no
// administrator. Demoting, disabling or deleting the only admin locks the
// account panel, the server settings and every future invite behind an
// account nobody can sign into any more, and the only repair is editing the
// database by hand.
var ErrLastAdmin = errors.New("this is the last administrator")

// ErrDisabled marks an account that has been suspended. Login reports it
// plainly rather than as a bad password: the person's credentials are fine
// and telling them otherwise sends them round a password reset that cannot
// help.
var ErrDisabled = errors.New("account disabled")

// userColumns is the projection every user read shares.
const userColumns = `id, email, username, role, disabled_at, created_at`

// CreateUser inserts a user and returns it. The caller supplies an already
// hashed password. An unrecognised role lands on member, which is what every
// account was before roles existed.
func (s *Store) CreateUser(ctx context.Context, email, username, passwordHash, role string) (models.User, error) {
	if !models.ValidRole(role) {
		role = models.RoleMember
	}
	u := models.User{ID: newID(), Email: email, Username: username, Role: role}
	err := s.db.QueryRowContext(ctx, `
		INSERT INTO users (id, email, username, password_hash, role)
		VALUES (?, ?, ?, ?, ?)
		RETURNING created_at`, u.ID, email, username, passwordHash, role).Scan(&u.CreatedAt)
	if err != nil {
		if isUniqueViolation(err) {
			return models.User{}, ErrConflict
		}
		return models.User{}, err
	}
	return u, nil
}

// GetUserByEmail returns the user and their password hash for login. A
// disabled account is returned alongside ErrDisabled so the caller can still
// verify the password first and answer in constant time.
func (s *Store) GetUserByEmail(ctx context.Context, email string) (models.User, string, error) {
	var u models.User
	var hash string
	err := s.db.QueryRowContext(ctx, `
		SELECT `+userColumns+`, password_hash
		FROM users WHERE email = ? COLLATE NOCASE`, email).
		Scan(&u.ID, &u.Email, &u.Username, &u.Role, &u.DisabledAt, &u.CreatedAt, &hash)
	if errors.Is(err, sql.ErrNoRows) {
		return models.User{}, "", ErrNotFound
	}
	return u, hash, err
}

// GetUser returns a user by id, disabled or not.
func (s *Store) GetUser(ctx context.Context, id string) (models.User, error) {
	var u models.User
	err := s.db.QueryRowContext(ctx,
		`SELECT `+userColumns+` FROM users WHERE id = ?`, id).
		Scan(&u.ID, &u.Email, &u.Username, &u.Role, &u.DisabledAt, &u.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return models.User{}, ErrNotFound
	}
	return u, err
}

// CountUsers reports how many accounts exist. Zero is the bootstrap case:
// the first registration on a fresh install is always allowed and always
// becomes the admin, whatever the registration setting says.
func (s *Store) CountUsers(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&n)
	return n, err
}

// ListUsers returns every account for the admin panel, admins first and then
// alphabetically, each with the counts that say what removing it would cost.
func (s *Store) ListUsers(ctx context.Context) ([]models.AdminUser, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT u.id, u.email, u.username, u.role, u.disabled_at, u.created_at,
		       (SELECT COUNT(*) FROM library_entries e WHERE e.user_id = u.id),
		       (SELECT COUNT(*) FROM book_shares bs WHERE bs.owner_id = u.id),
		       (SELECT COUNT(*) FROM book_shares bs WHERE bs.shared_with_id = u.id),
		       (SELECT MAX(se.created_at) FROM sessions se
		         WHERE se.user_id = u.id AND se.expires_at > CURRENT_TIMESTAMP)
		FROM users u
		ORDER BY (u.role = 'admin') DESC, u.username COLLATE NOCASE`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []models.AdminUser{}
	for rows.Next() {
		var u models.AdminUser
		if err := rows.Scan(&u.ID, &u.Email, &u.Username, &u.Role, &u.DisabledAt, &u.CreatedAt,
			&u.EntryCount, &u.SharedCount, &u.ReceivedCount, &u.LastSeenAt); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// SetUserRole changes an account's role, refusing to demote the last admin.
func (s *Store) SetUserRole(ctx context.Context, userID, role string) (models.User, error) {
	if !models.ValidRole(role) {
		return models.User{}, errors.New("unknown role: " + role)
	}
	return s.mutateUser(ctx, userID, func(ctx context.Context, tx *sql.Tx, current models.User) error {
		if current.Role == models.RoleAdmin && role != models.RoleAdmin {
			if err := guardLastAdminTx(ctx, tx, userID); err != nil {
				return err
			}
		}
		_, err := tx.ExecContext(ctx, `UPDATE users SET role = ? WHERE id = ?`, role, userID)
		return err
	})
}

// SetUserDisabled suspends or restores an account. Disabling drops its
// sessions too: the resolver already refuses a disabled user, so this is
// only housekeeping, but it keeps the sessions table honest about who is
// actually signed in.
func (s *Store) SetUserDisabled(ctx context.Context, userID string, disabled bool) (models.User, error) {
	return s.mutateUser(ctx, userID, func(ctx context.Context, tx *sql.Tx, current models.User) error {
		if disabled {
			if current.Role == models.RoleAdmin {
				if err := guardLastAdminTx(ctx, tx, userID); err != nil {
					return err
				}
			}
			if _, err := tx.ExecContext(ctx,
				`UPDATE users SET disabled_at = CURRENT_TIMESTAMP WHERE id = ? AND disabled_at IS NULL`,
				userID); err != nil {
				return err
			}
			_, err := tx.ExecContext(ctx, `DELETE FROM sessions WHERE user_id = ?`, userID)
			return err
		}
		_, err := tx.ExecContext(ctx, `UPDATE users SET disabled_at = NULL WHERE id = ?`, userID)
		return err
	})
}

// AdminSetPassword replaces an account's password without knowing the old
// one, for the admin who has to rescue someone who is locked out. Every
// session belonging to that account is revoked: a password they did not
// choose should not leave their old logins running.
func (s *Store) AdminSetPassword(ctx context.Context, userID, passwordHash string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	res, err := tx.ExecContext(ctx,
		`UPDATE users SET password_hash = ? WHERE id = ?`, passwordHash, userID)
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err != nil {
		return err
	} else if n == 0 {
		return ErrNotFound
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM sessions WHERE user_id = ?`, userID); err != nil {
		return err
	}
	return tx.Commit()
}

// DeleteUser removes an account and everything cascading off it: its
// library, progress, reading sessions, page anchors, achievements, lists,
// projects and shares. The file inventory survives — media_files.attached_by
// is SET NULL, so the rows stay pointed at their books and simply stop
// having an owner.
func (s *Store) DeleteUser(ctx context.Context, userID string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var role string
	err = tx.QueryRowContext(ctx, `SELECT role FROM users WHERE id = ?`, userID).Scan(&role)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if role == models.RoleAdmin {
		if err := guardLastAdminTx(ctx, tx, userID); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM users WHERE id = ?`, userID); err != nil {
		return err
	}
	return tx.Commit()
}

// mutateUser runs fn inside a transaction with the current row loaded,
// then returns the account as it now stands.
func (s *Store) mutateUser(ctx context.Context, userID string, fn func(context.Context, *sql.Tx, models.User) error) (models.User, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return models.User{}, err
	}
	defer tx.Rollback()

	var current models.User
	err = tx.QueryRowContext(ctx,
		`SELECT `+userColumns+` FROM users WHERE id = ?`, userID).
		Scan(&current.ID, &current.Email, &current.Username, &current.Role,
			&current.DisabledAt, &current.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return models.User{}, ErrNotFound
	}
	if err != nil {
		return models.User{}, err
	}
	if err := fn(ctx, tx, current); err != nil {
		return models.User{}, err
	}

	var updated models.User
	if err := tx.QueryRowContext(ctx,
		`SELECT `+userColumns+` FROM users WHERE id = ?`, userID).
		Scan(&updated.ID, &updated.Email, &updated.Username, &updated.Role,
			&updated.DisabledAt, &updated.CreatedAt); err != nil {
		return models.User{}, err
	}
	if err := tx.Commit(); err != nil {
		return models.User{}, err
	}
	return updated, nil
}

// guardLastAdminTx returns ErrLastAdmin when excludeID is the only enabled
// administrator left. A disabled admin does not count: an account that
// cannot sign in cannot administer anything.
func guardLastAdminTx(ctx context.Context, tx *sql.Tx, excludeID string) error {
	var others int
	err := tx.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM users
		WHERE role = 'admin' AND disabled_at IS NULL AND id != ?`, excludeID).Scan(&others)
	if err != nil {
		return err
	}
	if others == 0 {
		return ErrLastAdmin
	}
	return nil
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
