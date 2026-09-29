package store

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"github.com/collinpendleton/backhog/api/internal/models"
)

// tokenTouchInterval is how often one token's last_used_at is refreshed.
// Resolving the token is the gate on every request; writing the database on
// every one of them would be, so the timestamp moves at most once a minute —
// still an honest "is this credential in use" signal for the settings page.
const tokenTouchInterval = time.Minute

// hashAPIToken is the one-way transform between the bh_… secret a client
// holds and the row that recognises it. SHA-256 with no salt, exactly like
// invites: the secret is 256 bits of uniform randomness, so there is no
// dictionary to precompute and no work factor worth paying.
func hashAPIToken(token string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(token)))
	return hex.EncodeToString(sum[:])
}

// newAPITokenSecret returns 256 bits of randomness with the greppable
// prefix: a leaked credential is easier to spot in a log or a chat when
// every one of them starts the same way.
func newAPITokenSecret() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic("crypto/rand failed: " + err.Error())
	}
	return models.TokenPrefix + hex.EncodeToString(b)
}

// apiTokenColumns is the projection every token read shares.
const apiTokenColumns = `id, user_id, name, scopes, created_at, last_used_at, expires_at, revoked_at`

// CreateAPIToken issues a personal bearer credential. The returned token is
// the only time the secret exists in plaintext — the row holds its hash —
// so the caller must show it to the user immediately. An empty scope list
// lands on the read-only default; expiresAt nil means the token never
// expires.
func (s *Store) CreateAPIToken(ctx context.Context, userID, name string, scopes []string, expiresAt *time.Time) (models.APIToken, error) {
	scopes = normalizeScopes(scopes)
	t := models.APIToken{
		ID:        newID(),
		UserID:    userID,
		Name:      name,
		Scopes:    scopes,
		Token:     newAPITokenSecret(),
		ExpiresAt: expiresAt,
	}
	err := s.db.QueryRowContext(ctx, `
		INSERT INTO api_tokens (id, user_id, name, token_hash, scopes, expires_at)
		VALUES (?, ?, ?, ?, ?, ?)
		RETURNING created_at`,
		t.ID, userID, name, hashAPIToken(t.Token), strings.Join(scopes, ","), expiresAt).
		Scan(&t.CreatedAt)
	if err != nil {
		return models.APIToken{}, err
	}
	return t, nil
}

// ListAPITokens returns a user's tokens, live ones first and newest first
// within each group. Secrets are never included.
func (s *Store) ListAPITokens(ctx context.Context, userID string) ([]models.APIToken, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT `+apiTokenColumns+`
		FROM api_tokens WHERE user_id = ?
		ORDER BY (revoked_at IS NULL AND (expires_at IS NULL OR expires_at > CURRENT_TIMESTAMP)) DESC,
		         created_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []models.APIToken{}
	for rows.Next() {
		t, err := scanAPIToken(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// RevokeAPIToken makes one of the caller's own tokens stop working. The row
// stays: it is the record that this token existed and was killed.
func (s *Store) RevokeAPIToken(ctx context.Context, userID, id string) error {
	res, err := s.db.ExecContext(ctx, `
		UPDATE api_tokens SET revoked_at = CURRENT_TIMESTAMP
		WHERE id = ? AND user_id = ? AND revoked_at IS NULL`, id, userID)
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

// UserForAPIToken resolves a bearer secret to its user and scopes, rejecting
// revoked and expired tokens and disabled accounts — the same single gate
// every authenticated request passes through, so suspending an account
// retires its tokens on their very next use. last_used_at is refreshed when
// stale, at most once a minute.
func (s *Store) UserForAPIToken(ctx context.Context, token string) (models.User, []string, error) {
	var u models.User
	var scopes string
	var id string
	var lastUsed sql.NullTime
	err := s.db.QueryRowContext(ctx, `
		SELECT u.id, u.email, u.username, u.role, u.disabled_at, u.created_at,
		       t.id, t.scopes, t.last_used_at
		FROM api_tokens t JOIN users u ON u.id = t.user_id
		WHERE t.token_hash = ? AND t.revoked_at IS NULL
		  AND (t.expires_at IS NULL OR t.expires_at > CURRENT_TIMESTAMP)
		  AND u.disabled_at IS NULL`, hashAPIToken(token)).
		Scan(&u.ID, &u.Email, &u.Username, &u.Role, &u.DisabledAt, &u.CreatedAt,
			&id, &scopes, &lastUsed)
	if errors.Is(err, sql.ErrNoRows) {
		return models.User{}, nil, ErrNotFound
	}
	if err != nil {
		return models.User{}, nil, err
	}

	// A token freshly minted or idle past the interval gets a touch; the
	// busy common case stays a pure read.
	if !lastUsed.Valid || time.Since(lastUsed.Time) >= tokenTouchInterval {
		if _, err := s.db.ExecContext(ctx,
			`UPDATE api_tokens SET last_used_at = ? WHERE id = ?`, time.Now().UTC(), id); err != nil {
			return models.User{}, nil, err
		}
	}
	return u, strings.Split(scopes, ","), nil
}

// normalizeScopes validates a requested scope list, dropping empties and
// defaulting an empty list to read-only. Unknown scopes were already
// rejected at the handler; this is the belt under those braces.
func normalizeScopes(scopes []string) []string {
	out := make([]string, 0, len(scopes))
	for _, s := range scopes {
		if s != "" && models.ValidScope(s) && !contains(out, s) {
			out = append(out, s)
		}
	}
	if len(out) == 0 {
		out = []string{models.ScopeBooksRead}
	}
	return out
}

// scanAPIToken reads one row of the apiTokenColumns projection.
func scanAPIToken(row interface{ Scan(...any) error }) (models.APIToken, error) {
	var t models.APIToken
	var scopes string
	err := row.Scan(&t.ID, &t.UserID, &t.Name, &scopes, &t.CreatedAt,
		&t.LastUsedAt, &t.ExpiresAt, &t.RevokedAt)
	if err != nil {
		return models.APIToken{}, err
	}
	t.Scopes = strings.Split(scopes, ",")
	return t, nil
}
