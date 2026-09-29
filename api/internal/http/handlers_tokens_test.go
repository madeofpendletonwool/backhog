package http

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/collinpendleton/backhog/api/internal/models"
)

// tokenClient is a bare client with no cookie jar, which is the point: the
// whole feature must work with nothing but the Authorization header.
func tokenClient() *http.Client {
	return &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
}

// asToken runs one request authenticating only with the bearer secret.
func (a *accountsTestApp) asToken(t *testing.T, secret, method, path string, body any) (int, map[string]any) {
	t.Helper()
	var reader *bytes.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		reader = bytes.NewReader(raw)
	} else {
		reader = bytes.NewReader(nil)
	}
	req, err := http.NewRequest(method, a.ts.URL+path, reader)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+secret)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := tokenClient().Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()

	out := map[string]any{}
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

// mintToken creates a token over the real API and returns its secret.
func (a *accountsTestApp) mintToken(t *testing.T, client *http.Client, name string, days int) string {
	t.Helper()
	status, body := a.as(t, client, http.MethodPost, "/api/tokens", map[string]any{
		"name": name, "expires_days": days,
	})
	if status != http.StatusCreated {
		t.Fatalf("mint %s = %d (%v)", name, status, body)
	}
	secret, _ := body["token"].(string)
	if !strings.HasPrefix(secret, models.TokenPrefix) {
		t.Fatalf("minted secret %q lacks the %s prefix", secret, models.TokenPrefix)
	}
	return secret
}

// TestTokensEndToEnd walks the whole feature the way its two halves are
// used: the settings page mints and revokes, an external client carries
// nothing but the secret.
func TestTokensEndToEnd(t *testing.T) {
	app := newAccountsTestApp(t)

	secret := app.mintToken(t, app.owner, "mcp", 0)

	// The token acts as its user, with no cookie anywhere in sight.
	status, me := app.asToken(t, secret, http.MethodGet, "/api/auth/me", nil)
	if status != http.StatusOK || me["username"] != "owner" {
		t.Fatalf("token /auth/me = %d (%v)", status, me)
	}

	// Using it is visible: the list the settings page shows picked up a
	// last_used_at.
	status, list := app.as(t, app.owner, http.MethodGet, "/api/tokens", nil)
	if status != http.StatusOK {
		t.Fatalf("list = %d", status)
	}
	tokens, _ := list["tokens"].([]any)
	if len(tokens) != 1 {
		t.Fatalf("list has %d tokens, want 1", len(tokens))
	}
	first, _ := tokens[0].(map[string]any)
	if first["name"] != "mcp" || first["last_used_at"] == nil || first["status"] != "active" {
		t.Fatalf("listed token = %v", first)
	}
	if _, has := first["token"]; has {
		t.Error("the list handed back the plaintext secret")
	}

	// A read-only token cannot drive a write route, whatever it is.
	status, _ = app.asToken(t, secret, http.MethodPost, "/api/library/reorder",
		map[string]any{"ordered": []string{"a", "b"}})
	if status != http.StatusForbidden {
		t.Errorf("write over a read-only token = %d, want 403", status)
	}

	// And the token routes are cookie-only: a token cannot list or mint
	// tokens, including by GET.
	status, _ = app.asToken(t, secret, http.MethodGet, "/api/tokens", nil)
	if status != http.StatusForbidden {
		t.Errorf("token listing tokens = %d, want 403", status)
	}
	status, _ = app.asToken(t, secret, http.MethodPost, "/api/tokens",
		map[string]any{"name": "baby"})
	if status != http.StatusForbidden {
		t.Errorf("token minting a token = %d, want 403", status)
	}

	// A bad secret is a 401, not a pass-through to anonymous behaviour.
	if status, _ := app.asToken(t, models.TokenPrefix+"deadbeef", http.MethodGet, "/api/auth/me", nil); status != http.StatusUnauthorized {
		t.Errorf("bad secret = %d, want 401", status)
	}

	// Revoking from the settings page kills the credential immediately.
	status, _ = app.as(t, app.owner, http.MethodDelete, "/api/tokens/"+first["id"].(string), nil)
	if status != http.StatusNoContent {
		t.Fatalf("revoke = %d, want 204", status)
	}
	if status, _ := app.asToken(t, secret, http.MethodGet, "/api/auth/me", nil); status != http.StatusUnauthorized {
		t.Errorf("revoked token = %d, want 401", status)
	}

	// An expired token never worked in the first place. Mint one and run
	// its clock out with the same direct write the invite tests use.
	status, created := app.as(t, app.owner, http.MethodPost, "/api/tokens", map[string]any{
		"name": "short-lived", "expires_days": 1,
	})
	if status != http.StatusCreated {
		t.Fatalf("mint short-lived = %d (%v)", status, created)
	}
	if _, err := app.store.DB().ExecContext(context.Background(),
		`UPDATE api_tokens SET expires_at = ? WHERE name = 'short-lived'`,
		time.Now().Add(-time.Minute).UTC()); err != nil {
		t.Fatalf("expire: %v", err)
	}
	if status, _ := app.asToken(t, created["token"].(string), http.MethodGet, "/api/auth/me", nil); status != http.StatusUnauthorized {
		t.Errorf("expired token = %d, want 401", status)
	}

	// Validation: a nameless token or an unknown scope is refused before
	// anything is written.
	if status, _ := app.as(t, app.owner, http.MethodPost, "/api/tokens", map[string]any{
		"name": ""}); status != http.StatusBadRequest {
		t.Errorf("nameless token = %d, want 400", status)
	}
	if status, _ := app.as(t, app.owner, http.MethodPost, "/api/tokens", map[string]any{
		"name": "wizardry", "scopes": []string{"books:write"}}); status != http.StatusBadRequest {
		t.Errorf("unknown scope = %d, want 400", status)
	}
	if status, _ := app.as(t, app.owner, http.MethodPost, "/api/tokens", map[string]any{
		"name": "eternal", "expires_days": 99999}); status != http.StatusBadRequest {
		t.Errorf("absurd expiry = %d, want 400", status)
	}
}

// TestTokenSeesExactlyItsUser is the acceptance core: a token-authenticated
// request sees what its user's cookie sees — and nothing another user owns.
// It replays the sharing scenarios against the bearer path.
func TestTokenSeesExactlyItsUser(t *testing.T) {
	app := newAccountsTestApp(t)
	friend := app.signUp(t, "friend@example.com", "friend", "")
	ownerEntry, friendEntry := app.seedSharedBook(t, "friend@example.com")
	friendID := app.userID(t, "friend@example.com")

	// The friend's own rows, over the token, match the cookie answer.
	cookieStatus, cookieBody := app.as(t, friend, http.MethodGet, "/api/library/"+friendEntry, nil)
	tokenStatus, tokenBody := app.asToken(t, app.mintToken(t, friend, "mine", 0),
		http.MethodGet, "/api/library/"+friendEntry, nil)
	if tokenStatus != cookieStatus || tokenStatus != http.StatusOK {
		t.Fatalf("own entry: cookie %d, token %d", cookieStatus, tokenStatus)
	}
	if tokenBody["id"] != cookieBody["id"] {
		t.Errorf("own entry: cookie and token answered differently")
	}

	// The owner's unshared book stays a 404 for the friend's token — the
	// same ownership wall the cookie tests already hold.
	ownerSecret := app.mintToken(t, app.owner, "owner", 0)
	friendSecret := app.mintToken(t, friend, "friend", 0)
	if status, _ := app.asToken(t, friendSecret, http.MethodGet, "/api/books/"+ownerEntry+"/files", nil); status != http.StatusNotFound {
		t.Errorf("friend's token reading the owner's unshared files = %d, want 404", status)
	}
	if status, _ := app.asToken(t, ownerSecret, http.MethodGet, "/api/books/"+ownerEntry+"/files", nil); status != http.StatusOK {
		t.Errorf("owner's own token reading their files = %d, want 200", status)
	}

	// Once shared, the same token sees exactly what the friend's cookie
	// sees: the files, none of the NAS paths.
	if status, _ := app.as(t, app.owner, http.MethodPost, "/api/books/"+ownerEntry+"/shares",
		map[string]string{"user_id": friendID}); status != http.StatusOK {
		t.Fatalf("share = %d", status)
	}
	status, body := app.asToken(t, friendSecret, http.MethodGet, "/api/books/"+friendEntry+"/files", nil)
	if status != http.StatusOK {
		t.Fatalf("shared files over token = %d (%v)", status, body)
	}
	files, _ := body["files"].([]any)
	if len(files) != 2 {
		t.Fatalf("shared file list over token has %d files, want 2", len(files))
	}
	for _, raw := range files {
		f, _ := raw.(map[string]any)
		if root, _ := f["root"].(string); root != "" {
			t.Errorf("friend's token was handed the NAS root %q", root)
		}
	}

	// And revoking the share closes the door for the token too.
	if status, _ := app.as(t, app.owner, http.MethodDelete,
		"/api/books/"+ownerEntry+"/shares/"+friendID, nil); status != http.StatusOK {
		t.Fatalf("revoke share = %d", status)
	}
	if status, _ := app.asToken(t, friendSecret, http.MethodGet, "/api/books/"+friendEntry+"/files", nil); status != http.StatusNotFound {
		t.Errorf("revoked share over token = %d, want 404", status)
	}
}

// TestBearerWithoutPrefixIsNotOurs pins the coexistence rule: the /internal
// worker gates authenticate their own Bearer tokens, and those requests pass
// through the personal-token middleware untouched. The test app enables no
// worker token, so a request that *reaches* the gate answers 503 — if the
// middleware had claimed it, the answer would be our 401.
func TestBearerWithoutPrefixIsNotOurs(t *testing.T) {
	app := newAccountsTestApp(t)

	req, err := http.NewRequest(http.MethodPost, app.ts.URL+"/internal/ocr/claim", strings.NewReader("{}"))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer some-worker-token")
	req.Header.Set("Content-Type", "application/json")
	resp, err := tokenClient().Do(req)
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("worker claim = %d, want the worker gate's 503 (not our 401)", resp.StatusCode)
	}

	// The flip side: a bh_ secret on the same route is ours to judge.
	if status, _ := app.asToken(t, models.TokenPrefix+"deadbeef",
		http.MethodGet, "/api/healthz", nil); status != http.StatusUnauthorized {
		t.Errorf("bad personal secret on a public route = %d, want 401", status)
	}
}
