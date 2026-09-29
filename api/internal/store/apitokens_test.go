package store

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/collinpendleton/backhog/api/internal/models"
)

func TestAPITokenLifecycle(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	user := newTestUser(t, s, "owner@example.com", "owner")
	other := newTestUser(t, s, "friend@example.com", "friend")

	token, err := s.CreateAPIToken(ctx, user, "mcp", nil, nil)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if !strings.HasPrefix(token.Token, models.TokenPrefix) {
		t.Errorf("secret %q lacks the %s prefix", token.Token, models.TokenPrefix)
	}
	if len(token.Scopes) != 1 || token.Scopes[0] != models.ScopeBooksRead {
		t.Errorf("default scopes = %v, want read-only", token.Scopes)
	}

	// The row recognises the secret and nothing else.
	got, scopes, err := s.UserForAPIToken(ctx, token.Token)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if got.ID != user || len(scopes) != 1 || scopes[0] != models.ScopeBooksRead {
		t.Errorf("resolve = %v %v, want %s with books:read", got.ID, scopes, user)
	}
	if _, _, err := s.UserForAPIToken(ctx, token.Token+"nope"); !errors.Is(err, ErrNotFound) {
		t.Errorf("wrong secret = %v, want ErrNotFound", err)
	}

	// First use stamps last_used_at.
	mine, err := s.ListAPITokens(ctx, user)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(mine) != 1 || mine[0].LastUsedAt == nil {
		t.Fatalf("after use, tokens = %+v, want one with last_used_at set", mine)
	}
	theirs, err := s.ListAPITokens(ctx, other)
	if err != nil {
		t.Fatalf("list other: %v", err)
	}
	if len(theirs) != 0 {
		t.Errorf("someone else's list showed my token: %+v", theirs)
	}

	// Revocation is scoped to the owner, single-shot, and terminal.
	if err := s.RevokeAPIToken(ctx, other, token.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("foreign revoke = %v, want ErrNotFound", err)
	}
	if err := s.RevokeAPIToken(ctx, user, token.ID); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if err := s.RevokeAPIToken(ctx, user, token.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("second revoke = %v, want ErrNotFound", err)
	}
	if _, _, err := s.UserForAPIToken(ctx, token.Token); !errors.Is(err, ErrNotFound) {
		t.Errorf("revoked token resolves = %v, want ErrNotFound", err)
	}

	// The revoked row stays in the owner's list, marked.
	mine, err = s.ListAPITokens(ctx, user)
	if err != nil {
		t.Fatalf("list after revoke: %v", err)
	}
	if len(mine) != 1 || mine[0].Status(time.Now()) != "revoked" {
		t.Errorf("revoked row = %+v, want it kept and marked", mine)
	}
}

func TestAPITokenExpiryAndDisabledAccount(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	user := newTestUser(t, s, "owner@example.com", "owner")

	past := time.Now().Add(-time.Hour).UTC()
	expired, err := s.CreateAPIToken(ctx, user, "stale", nil, &past)
	if err != nil {
		t.Fatalf("create expired: %v", err)
	}
	if _, _, err := s.UserForAPIToken(ctx, expired.Token); !errors.Is(err, ErrNotFound) {
		t.Errorf("expired token resolves = %v, want ErrNotFound", err)
	}
	if got := expired.Status(time.Now()); got != "expired" {
		t.Errorf("status = %q, want expired", got)
	}

	live, err := s.CreateAPIToken(ctx, user, "live", nil, nil)
	if err != nil {
		t.Fatalf("create live: %v", err)
	}
	if _, _, err := s.UserForAPIToken(ctx, live.Token); err != nil {
		t.Fatalf("live token before disable: %v", err)
	}
	// Disabling the account retires the token on its next use, exactly as
	// it does every live session.
	if _, err := s.SetUserDisabled(ctx, user, true); err != nil {
		t.Fatalf("disable: %v", err)
	}
	if _, _, err := s.UserForAPIToken(ctx, live.Token); !errors.Is(err, ErrNotFound) {
		t.Errorf("disabled account's token resolves = %v, want ErrNotFound", err)
	}
}

func TestAPITokenLastUsedThrottles(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	user := newTestUser(t, s, "owner@example.com", "owner")

	token, err := s.CreateAPIToken(ctx, user, "busy", nil, nil)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, _, err := s.UserForAPIToken(ctx, token.Token); err != nil {
		t.Fatalf("first use: %v", err)
	}
	rows, err := s.ListAPITokens(ctx, user)
	if err != nil || len(rows) != 1 || rows[0].LastUsedAt == nil {
		t.Fatalf("after first use: %v %+v", err, rows)
	}
	first := *rows[0].LastUsedAt

	// A second use inside the interval must not rewrite the timestamp.
	if _, _, err := s.UserForAPIToken(ctx, token.Token); err != nil {
		t.Fatalf("second use: %v", err)
	}
	rows, err = s.ListAPITokens(ctx, user)
	if err != nil || len(rows) != 1 {
		t.Fatalf("list: %v %+v", err, rows)
	}
	if !rows[0].LastUsedAt.Equal(first) {
		t.Errorf("last_used_at moved from %v to %v inside the interval", first, *rows[0].LastUsedAt)
	}

	// Backdate it past the interval (the same direct-SQL trick the invite
	// expiry test uses) and the next use touches it again.
	stale := time.Now().Add(-2 * time.Hour).UTC()
	if _, err := s.db.ExecContext(ctx,
		`UPDATE api_tokens SET last_used_at = ? WHERE id = ?`, stale, token.ID); err != nil {
		t.Fatalf("backdate: %v", err)
	}
	if _, _, err := s.UserForAPIToken(ctx, token.Token); err != nil {
		t.Fatalf("use after idle: %v", err)
	}
	rows, err = s.ListAPITokens(ctx, user)
	if err != nil || len(rows) != 1 || rows[0].LastUsedAt == nil {
		t.Fatalf("list after idle use: %v %+v", err, rows)
	}
	if !rows[0].LastUsedAt.After(stale) {
		t.Errorf("last_used_at = %v, want it refreshed after the idle window", *rows[0].LastUsedAt)
	}
}

func TestAPITokenScopeValidation(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	user := newTestUser(t, s, "owner@example.com", "owner")

	// Unknown scopes were rejected at the handler; the store drops them
	// rather than storing a credential that grants nothing it can name.
	token, err := s.CreateAPIToken(ctx, user, "odd", []string{"wizardry"}, nil)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if len(token.Scopes) != 1 || token.Scopes[0] != models.ScopeBooksRead {
		t.Errorf("unknown scope stored as %v, want the read-only default", token.Scopes)
	}
}
