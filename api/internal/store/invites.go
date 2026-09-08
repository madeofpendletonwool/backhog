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

// ErrInviteInvalid marks a token that does not resolve to a redeemable
// invite: unknown, already used, revoked or expired. The four are one error
// on purpose — distinguishing them tells an attacker holding a guessed token
// whether it was ever real.
var ErrInviteInvalid = errors.New("invite is not valid")

// InviteTTL is how long a new invite stays redeemable. A week is long enough
// to reach someone who checks their messages on the weekend and short enough
// that a link forgotten in a chat log stops working.
const InviteTTL = 7 * 24 * time.Hour

// inviteColumns is the projection every invite read shares, with the
// creator's and redeemer's usernames joined in for display.
const inviteColumns = `
	i.id, i.email, i.role, i.note, i.created_by, COALESCE(cu.username, ''),
	i.expires_at, i.accepted_at, COALESCE(i.accepted_by, ''), COALESCE(au.username, ''),
	i.revoked_at, i.created_at
	FROM invites i
	LEFT JOIN users cu ON cu.id = i.created_by
	LEFT JOIN users au ON au.id = i.accepted_by`

// hashInviteToken is the one-way transform between the link an admin hands
// out and the row that recognises it. SHA-256 with no salt is right here and
// would not be for a password: the token is 256 bits of uniform randomness,
// so there is no dictionary to precompute and no work factor worth paying.
func hashInviteToken(token string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(token)))
	return hex.EncodeToString(sum[:])
}

// newInviteToken returns 256 bits of URL-safe randomness.
func newInviteToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic("crypto/rand failed: " + err.Error())
	}
	return hex.EncodeToString(b)
}

// CreateInvite issues a single-use sign-up link carrying a role. The
// returned invite is the only time the token exists in plaintext — the row
// holds its hash — so the caller must show it to the admin immediately.
func (s *Store) CreateInvite(ctx context.Context, createdBy, email, role, note string, ttl time.Duration) (models.Invite, error) {
	if !models.ValidRole(role) {
		return models.Invite{}, errors.New("unknown role: " + role)
	}
	if ttl <= 0 {
		ttl = InviteTTL
	}

	inv := models.Invite{
		ID:        newID(),
		Email:     strings.TrimSpace(email),
		Role:      role,
		Note:      strings.TrimSpace(note),
		Token:     newInviteToken(),
		CreatedBy: createdBy,
		ExpiresAt: time.Now().Add(ttl).UTC(),
	}
	err := s.db.QueryRowContext(ctx, `
		INSERT INTO invites (id, token_hash, email, role, note, created_by, expires_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		RETURNING created_at`,
		inv.ID, hashInviteToken(inv.Token), inv.Email, inv.Role, inv.Note,
		createdBy, inv.ExpiresAt).Scan(&inv.CreatedAt)
	if err != nil {
		return models.Invite{}, err
	}
	return inv, nil
}

// ListInvites returns every invite, redeemable ones first and newest first
// within each group. Tokens are never included.
func (s *Store) ListInvites(ctx context.Context) ([]models.Invite, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT `+inviteColumns+`
		ORDER BY (i.accepted_at IS NULL AND i.revoked_at IS NULL
		          AND i.expires_at > CURRENT_TIMESTAMP) DESC,
		         i.created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []models.Invite{}
	for rows.Next() {
		inv, err := scanInvite(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, inv)
	}
	return out, rows.Err()
}

// RevokeInvite makes an unredeemed invite stop working. An invite that was
// already accepted is left alone — the account it created is the thing to
// disable, not the paper trail that records it.
func (s *Store) RevokeInvite(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `
		UPDATE invites SET revoked_at = CURRENT_TIMESTAMP
		WHERE id = ? AND accepted_at IS NULL AND revoked_at IS NULL`, id)
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

// DeleteInvite removes an invite row outright, for tidying a list of spent
// and expired links.
func (s *Store) DeleteInvite(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM invites WHERE id = ?`, id)
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

// InviteForToken resolves a redeemable token to its invite, so the sign-up
// page can show who it is for and what it grants before anyone types a
// password. It never returns the token back.
func (s *Store) InviteForToken(ctx context.Context, token string) (models.Invite, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT `+inviteColumns+`
		WHERE i.token_hash = ? AND i.accepted_at IS NULL AND i.revoked_at IS NULL
		  AND i.expires_at > CURRENT_TIMESTAMP`, hashInviteToken(token))
	if err != nil {
		return models.Invite{}, err
	}
	defer rows.Close()

	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return models.Invite{}, err
		}
		return models.Invite{}, ErrInviteInvalid
	}
	return scanInvite(rows)
}

// CreateUserFromInvite redeems a token and creates the account it grants, in
// one transaction. Both halves belong together: an invite is single-use, and
// checking it in one statement and spending it in another is exactly the
// window two people clicking the same link at once would slip through.
func (s *Store) CreateUserFromInvite(ctx context.Context, token, email, username, passwordHash string) (models.User, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return models.User{}, err
	}
	defer tx.Rollback()

	var inviteID, role string
	err = tx.QueryRowContext(ctx, `
		SELECT id, role FROM invites
		WHERE token_hash = ? AND accepted_at IS NULL AND revoked_at IS NULL
		  AND expires_at > CURRENT_TIMESTAMP`, hashInviteToken(token)).Scan(&inviteID, &role)
	if errors.Is(err, sql.ErrNoRows) {
		return models.User{}, ErrInviteInvalid
	}
	if err != nil {
		return models.User{}, err
	}
	if !models.ValidRole(role) {
		role = models.RoleReader
	}

	u := models.User{ID: newID(), Email: email, Username: username, Role: role}
	err = tx.QueryRowContext(ctx, `
		INSERT INTO users (id, email, username, password_hash, role)
		VALUES (?, ?, ?, ?, ?)
		RETURNING created_at`, u.ID, email, username, passwordHash, role).Scan(&u.CreatedAt)
	if err != nil {
		if isUniqueViolation(err) {
			return models.User{}, ErrConflict
		}
		return models.User{}, err
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE invites SET accepted_at = CURRENT_TIMESTAMP, accepted_by = ?
		WHERE id = ?`, u.ID, inviteID); err != nil {
		return models.User{}, err
	}
	if err := tx.Commit(); err != nil {
		return models.User{}, err
	}
	return u, nil
}

// scanInvite reads one row of the inviteColumns projection.
func scanInvite(row interface{ Scan(...any) error }) (models.Invite, error) {
	var inv models.Invite
	var createdBy sql.NullString
	err := row.Scan(&inv.ID, &inv.Email, &inv.Role, &inv.Note, &createdBy, &inv.CreatedByAs,
		&inv.ExpiresAt, &inv.AcceptedAt, &inv.AcceptedBy, &inv.AcceptedAs,
		&inv.RevokedAt, &inv.CreatedAt)
	if err != nil {
		return models.Invite{}, err
	}
	inv.CreatedBy = createdBy.String
	return inv, nil
}
