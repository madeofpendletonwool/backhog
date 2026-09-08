package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/collinpendleton/backhog/api/internal/models"
)

func TestLastAdminCannotBeRemoved(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	admin, err := s.CreateUser(ctx, "admin@example.com", "admin", "hash", models.RoleAdmin)
	if err != nil {
		t.Fatalf("create admin: %v", err)
	}
	member := newTestUser(t, s, "member@example.com", "member")

	// All three doors out of "there is an administrator" are shut.
	if _, err := s.SetUserRole(ctx, admin.ID, models.RoleReader); !errors.Is(err, ErrLastAdmin) {
		t.Errorf("demote last admin = %v, want ErrLastAdmin", err)
	}
	if _, err := s.SetUserDisabled(ctx, admin.ID, true); !errors.Is(err, ErrLastAdmin) {
		t.Errorf("disable last admin = %v, want ErrLastAdmin", err)
	}
	if err := s.DeleteUser(ctx, admin.ID); !errors.Is(err, ErrLastAdmin) {
		t.Errorf("delete last admin = %v, want ErrLastAdmin", err)
	}

	// With a second administrator in place, the first may step down.
	if _, err := s.SetUserRole(ctx, member, models.RoleAdmin); err != nil {
		t.Fatalf("promote: %v", err)
	}
	if _, err := s.SetUserRole(ctx, admin.ID, models.RoleReader); err != nil {
		t.Fatalf("demote with a second admin present: %v", err)
	}

	// A disabled administrator does not count as one: an account that
	// cannot sign in cannot administer anything.
	if _, err := s.SetUserRole(ctx, admin.ID, models.RoleAdmin); err != nil {
		t.Fatalf("re-promote: %v", err)
	}
	if _, err := s.SetUserDisabled(ctx, admin.ID, true); err != nil {
		t.Fatalf("disable one of two admins: %v", err)
	}
	if _, err := s.SetUserDisabled(ctx, member, true); !errors.Is(err, ErrLastAdmin) {
		t.Errorf("disable the only enabled admin = %v, want ErrLastAdmin", err)
	}
}

func TestDisablingRetiresEverySession(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	newTestUser(t, s, "admin@example.com", "keeper")
	if _, err := s.SetUserRole(ctx, newTestUser(t, s, "boss@example.com", "boss"), models.RoleAdmin); err != nil {
		t.Fatalf("promote: %v", err)
	}

	friend := newTestUser(t, s, "friend@example.com", "friend")
	session, _, err := s.CreateSession(ctx, friend)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	if _, err := s.UserForSession(ctx, session); err != nil {
		t.Fatalf("session before disable: %v", err)
	}

	if _, err := s.SetUserDisabled(ctx, friend, true); err != nil {
		t.Fatalf("disable: %v", err)
	}
	if _, err := s.UserForSession(ctx, session); !errors.Is(err, ErrNotFound) {
		t.Errorf("session after disable = %v, want ErrNotFound", err)
	}

	// Restoring the account does not restore the old session — signing back
	// in is the only way through.
	if _, err := s.SetUserDisabled(ctx, friend, false); err != nil {
		t.Fatalf("re-enable: %v", err)
	}
	if _, err := s.UserForSession(ctx, session); !errors.Is(err, ErrNotFound) {
		t.Errorf("session after re-enable = %v, want it to stay dead", err)
	}
}

func TestUserRolesRoundTrip(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	admin, err := s.CreateUser(ctx, "a@example.com", "aaa", "hash", models.RoleAdmin)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	reader, err := s.CreateUser(ctx, "r@example.com", "rrr", "hash", models.RoleReader)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	// An unrecognised role lands on member, which is what every account was
	// before roles existed.
	odd, err := s.CreateUser(ctx, "o@example.com", "ooo", "hash", "wizard")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if odd.Role != models.RoleMember {
		t.Errorf("unknown role stored as %q, want member", odd.Role)
	}

	if !admin.IsAdmin() || !admin.CanManageMedia() {
		t.Error("admin should administer and manage media")
	}
	if reader.IsAdmin() || reader.CanManageMedia() {
		t.Error("reader should do neither")
	}
	if odd.IsAdmin() || !odd.CanManageMedia() {
		t.Error("member should manage media but not administer")
	}

	users, err := s.ListUsers(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(users) != 3 || users[0].Username != "aaa" {
		t.Fatalf("ListUsers = %+v, want the admin first of three", users)
	}
}

func TestDeleteUserLeavesTheInventoryStanding(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	f := seedSharedFixture(t, s)

	// A second admin, so the owner is not the last one standing.
	if _, err := s.SetUserRole(ctx, f.friend, models.RoleAdmin); err != nil {
		t.Fatalf("promote friend: %v", err)
	}
	if err := s.DeleteUser(ctx, f.owner); err != nil {
		t.Fatalf("delete owner: %v", err)
	}

	// The files are still inventoried and still attached to their book —
	// they live on the NAS, not in the account — but nobody owns them any
	// more, so nobody is refused.
	var attached int
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM media_files WHERE book_id = ? AND attached_by IS NULL`,
		f.book).Scan(&attached); err != nil {
		t.Fatalf("count: %v", err)
	}
	if attached != 3 {
		t.Fatalf("%d ownerless attached files, want 3", attached)
	}
	mustAccess(t, s, f.friend, f.friendEntry, f.book, true)
}

func TestSettingsRoundTrip(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	// The migration's defaults: open door, cautious role.
	settings, err := s.Settings(ctx)
	if err != nil {
		t.Fatalf("settings: %v", err)
	}
	if !settings.RegistrationEnabled || settings.DefaultRole != models.RoleReader {
		t.Fatalf("defaults = %+v, want registration on and reader", settings)
	}

	saved, err := s.SaveSettings(ctx, models.ServerSettings{
		RegistrationEnabled: false, DefaultRole: models.RoleMember,
	})
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	if saved.RegistrationEnabled || saved.DefaultRole != models.RoleMember {
		t.Fatalf("saved = %+v", saved)
	}
	reread, err := s.Settings(ctx)
	if err != nil {
		t.Fatalf("re-read: %v", err)
	}
	if reread != saved {
		t.Fatalf("re-read %+v, saved %+v", reread, saved)
	}

	if _, err := s.SaveSettings(ctx, models.ServerSettings{DefaultRole: "wizard"}); err == nil {
		t.Error("saving an unknown default role succeeded, want a rejection")
	}
}

func TestInviteIsSingleUse(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	admin, err := s.CreateUser(ctx, "admin@example.com", "admin", "hash", models.RoleAdmin)
	if err != nil {
		t.Fatalf("create admin: %v", err)
	}

	invite, err := s.CreateInvite(ctx, admin.ID, "friend@example.com", models.RoleReader, "the book guy", 0)
	if err != nil {
		t.Fatalf("create invite: %v", err)
	}
	if invite.Token == "" {
		t.Fatal("invite came back without a token")
	}

	// The plaintext token is never stored — only something derived from it.
	var stored string
	if err := s.db.QueryRowContext(ctx,
		`SELECT token_hash FROM invites WHERE id = ?`, invite.ID).Scan(&stored); err != nil {
		t.Fatalf("read hash: %v", err)
	}
	if stored == invite.Token {
		t.Error("the invite token is stored in plaintext")
	}

	found, err := s.InviteForToken(ctx, invite.Token)
	if err != nil {
		t.Fatalf("resolve token: %v", err)
	}
	if found.Role != models.RoleReader || found.CreatedByAs != "admin" {
		t.Fatalf("resolved = %+v", found)
	}
	if found.Token != "" {
		t.Error("a token read handed the token back")
	}

	user, err := s.CreateUserFromInvite(ctx, invite.Token, "friend@example.com", "friend", "hash")
	if err != nil {
		t.Fatalf("redeem: %v", err)
	}
	if user.Role != models.RoleReader {
		t.Errorf("redeemed account role = %q, want the invite's reader", user.Role)
	}

	// Spent. The second click gets nothing, and cannot tell whether the
	// token was ever real.
	if _, err := s.CreateUserFromInvite(ctx, invite.Token, "other@example.com", "other", "hash"); !errors.Is(err, ErrInviteInvalid) {
		t.Errorf("second redemption = %v, want ErrInviteInvalid", err)
	}
	if _, err := s.InviteForToken(ctx, invite.Token); !errors.Is(err, ErrInviteInvalid) {
		t.Errorf("spent token resolves = %v, want ErrInviteInvalid", err)
	}
	if _, err := s.InviteForToken(ctx, "not-a-token"); !errors.Is(err, ErrInviteInvalid) {
		t.Errorf("unknown token = %v, want ErrInviteInvalid", err)
	}

	listed, err := s.ListInvites(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(listed) != 1 || listed[0].Status(time.Now()) != "accepted" || listed[0].AcceptedAs != "friend" {
		t.Fatalf("listed = %+v, want one accepted invite naming the friend", listed)
	}
}

func TestInviteRevokedAndExpired(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	admin, err := s.CreateUser(ctx, "admin@example.com", "admin", "hash", models.RoleAdmin)
	if err != nil {
		t.Fatalf("create admin: %v", err)
	}

	revoked, err := s.CreateInvite(ctx, admin.ID, "", models.RoleMember, "", 0)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := s.RevokeInvite(ctx, revoked.ID); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if _, err := s.CreateUserFromInvite(ctx, revoked.Token, "a@example.com", "aaa", "hash"); !errors.Is(err, ErrInviteInvalid) {
		t.Errorf("revoked redemption = %v, want ErrInviteInvalid", err)
	}
	if err := s.RevokeInvite(ctx, revoked.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("second revoke = %v, want ErrNotFound", err)
	}

	expired, err := s.CreateInvite(ctx, admin.ID, "", models.RoleReader, "", 0)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := s.db.ExecContext(ctx,
		`UPDATE invites SET expires_at = ? WHERE id = ?`,
		time.Now().Add(-time.Hour), expired.ID); err != nil {
		t.Fatalf("age the invite: %v", err)
	}
	if _, err := s.CreateUserFromInvite(ctx, expired.Token, "b@example.com", "bbb", "hash"); !errors.Is(err, ErrInviteInvalid) {
		t.Errorf("expired redemption = %v, want ErrInviteInvalid", err)
	}

	if err := s.DeleteInvite(ctx, expired.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if err := s.DeleteInvite(ctx, expired.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("second delete = %v, want ErrNotFound", err)
	}
}
