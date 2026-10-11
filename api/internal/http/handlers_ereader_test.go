package http

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/collinpendleton/backhog/api/internal/ereader"
	"github.com/collinpendleton/backhog/api/internal/models"
)

// The e-reader routes (MAD-828, MAD-829) as the devices use them: an OPDS
// client downloading with Basic, KOReader's sync plugin authenticating with
// x-auth-user / x-auth-key, and nothing but those headers on the wire.

const downloadPath = "/api/opds/books/" + epubFixtureEntry + "/epub/fixture-book.epub"

// mintAs creates a token over the real API with the cookie client and
// returns the secret.
func (a *epubTestApp) mintAs(t *testing.T, client *http.Client, body map[string]any) string {
	t.Helper()
	raw, _ := json.Marshal(body)
	resp, err := client.Post(a.ts.URL+"/api/tokens", "application/json", bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	defer resp.Body.Close()
	out := map[string]any{}
	_ = json.NewDecoder(resp.Body).Decode(&out)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("mint %v = %d (%v)", body, resp.StatusCode, out)
	}
	secret, _ := out["token"].(string)
	if !strings.HasPrefix(secret, models.TokenPrefix) {
		t.Fatalf("minted secret lacks the %s prefix", models.TokenPrefix)
	}
	return secret
}

// deviceRequest sends one request with exactly the headers given — no
// cookie jar, no redirects — and returns the response with its body read.
func (a *epubTestApp) deviceRequest(t *testing.T, method, path string, headers map[string]string, body []byte) (*http.Response, []byte) {
	t.Helper()
	req, err := http.NewRequest(method, a.ts.URL+path, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := tokenClient().Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	return resp, data
}

func basic(user, password string) map[string]string {
	req, _ := http.NewRequest(http.MethodGet, "/", nil)
	req.SetBasicAuth(user, password)
	return map[string]string{"Authorization": req.Header.Get("Authorization")}
}

func xauth(user, token string) map[string]string {
	sum := md5.Sum([]byte(token))
	return map[string]string{
		"Accept":      "application/vnd.koreader.v1+json",
		"x-auth-user": user,
		"x-auth-key":  hex.EncodeToString(sum[:]),
	}
}

func TestEpubDownloadServesOriginalBytes(t *testing.T) {
	app := newEpubTestApp(t)
	fixture := apiEpubFixture(t)
	app.attachEpub(t, fixture)
	device := app.mintAs(t, app.client, map[string]any{"name": "kindle", "device": true})

	resp, body := app.deviceRequest(t, http.MethodGet, downloadPath, basic("reader", device), nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("download = %d (%s)", resp.StatusCode, body)
	}
	if !bytes.Equal(body, fixture) {
		t.Fatalf("downloaded %d bytes that differ from the %d-byte original", len(body), len(fixture))
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/epub+zip" {
		t.Errorf("Content-Type = %q", ct)
	}
	if cd := resp.Header.Get("Content-Disposition"); !strings.Contains(cd, `filename="Fixture Book.epub"`) {
		t.Errorf("Content-Disposition = %q", cd)
	}
	if resp.Header.Get("ETag") == "" || resp.Header.Get("Accept-Ranges") != "bytes" {
		t.Errorf("missing ETag/Accept-Ranges: %v", resp.Header)
	}

	// A dropped Wi-Fi connection resumes rather than restarts.
	h := basic("reader", device)
	h["Range"] = "bytes=0-99"
	resp, body = app.deviceRequest(t, http.MethodGet, downloadPath, h, nil)
	if resp.StatusCode != http.StatusPartialContent || !bytes.Equal(body, fixture[:100]) {
		t.Errorf("range = %d, %d bytes", resp.StatusCode, len(body))
	}

	// HEAD answers with the size and no body.
	resp, body = app.deviceRequest(t, http.MethodHead, downloadPath, basic("reader", device), nil)
	if resp.StatusCode != http.StatusOK || len(body) != 0 || resp.ContentLength != int64(len(fixture)) {
		t.Errorf("HEAD = %d, %d body bytes, length %d", resp.StatusCode, len(body), resp.ContentLength)
	}

	// A bearer token works too: the route is the API's, not only Basic's.
	resp, _ = app.deviceRequest(t, http.MethodGet, downloadPath,
		map[string]string{"Authorization": "Bearer " + device}, nil)
	if resp.StatusCode != http.StatusOK {
		t.Errorf("bearer download = %d", resp.StatusCode)
	}
}

func TestEpubDownloadRecordsKosyncDocumentIDs(t *testing.T) {
	app := newEpubTestApp(t)
	fixture := apiEpubFixture(t)
	app.attachEpub(t, fixture)
	device := app.mintAs(t, app.client, map[string]any{"name": "kindle", "device": true})

	if resp, _ := app.deviceRequest(t, http.MethodGet, downloadPath, basic("reader", device), nil); resp.StatusCode != http.StatusOK {
		t.Fatalf("download = %d", resp.StatusCode)
	}

	ctx := context.Background()
	binary, err := ereader.PartialMD5(bytes.NewReader(fixture), int64(len(fixture)))
	if err != nil {
		t.Fatal(err)
	}
	// The fixture book has no author, so every CrossPoint format names it
	// the same — one filename hash covers all three.
	for label, hash := range map[string]string{
		"binary":   binary,
		"filename": ereader.FilenameMD5("Fixture Book.epub"),
	} {
		entry, err := app.store.KosyncEntryForHash(ctx, app.userID, hash)
		if err != nil || entry != epubFixtureEntry {
			t.Errorf("%s hash %s → %q, %v; want %s", label, hash, entry, err, epubFixtureEntry)
		}
	}

	// The lookup is per user: a stranger holding the same hash finds nothing.
	if _, err := app.store.KosyncEntryForHash(ctx, "someone-else", binary); err == nil {
		t.Error("another user's lookup resolved this user's entry")
	}
}

func TestEreaderAuthMatrix(t *testing.T) {
	app := newEpubTestApp(t)
	app.attachEpub(t, apiEpubFixture(t))
	device := app.mintAs(t, app.client, map[string]any{"name": "kindle", "device": true})
	plain := app.mintAs(t, app.client, map[string]any{"name": "mcp"})

	cases := []struct {
		name    string
		path    string
		headers map[string]string
		want    int
	}{
		{"anonymous download", downloadPath, nil, http.StatusUnauthorized},
		{"basic, device token", downloadPath, basic("reader", device), http.StatusOK},
		{"basic, plain token", downloadPath, basic("reader", plain), http.StatusOK},
		{"basic, username case-insensitive", downloadPath, basic("READER", device), http.StatusOK},
		{"basic, wrong username", downloadPath, basic("snoop", device), http.StatusUnauthorized},
		{"basic, account password", downloadPath, basic("reader", "hogwash123"), http.StatusUnauthorized},
		{"basic, unknown token", downloadPath, basic("reader", models.TokenPrefix+"nope"), http.StatusUnauthorized},
		// Basic is read only on the e-reader routes.
		{"basic elsewhere", "/api/auth/me", basic("reader", device), http.StatusUnauthorized},
		{"x-auth elsewhere", "/api/auth/me", xauth("reader", device), http.StatusUnauthorized},

		{"kosync anonymous", "/api/kosync/users/auth", nil, http.StatusUnauthorized},
		{"kosync x-auth, device token", "/api/kosync/users/auth", xauth("reader", device), http.StatusOK},
		{"kosync x-auth, case-insensitive user", "/api/kosync/users/auth", xauth("Reader", device), http.StatusOK},
		{"kosync x-auth, wrong user", "/api/kosync/users/auth", xauth("snoop", device), http.StatusUnauthorized},
		// Only device tokens carry a kosync key.
		{"kosync x-auth, plain token", "/api/kosync/users/auth", xauth("reader", plain), http.StatusUnauthorized},
		{"kosync x-auth, account password", "/api/kosync/users/auth", xauth("reader", "hogwash123"), http.StatusUnauthorized},
		{"kosync basic (CrossPoint)", "/api/kosync/users/auth", basic("reader", device), http.StatusOK},
	}
	for _, c := range cases {
		resp, body := app.deviceRequest(t, http.MethodGet, c.path, c.headers, nil)
		if resp.StatusCode != c.want {
			t.Errorf("%s: %s = %d (%s), want %d", c.name, c.path, resp.StatusCode, body, c.want)
			continue
		}
		if c.want == http.StatusUnauthorized && strings.HasPrefix(c.path, "/api/opds") &&
			!strings.HasPrefix(resp.Header.Get("WWW-Authenticate"), "Basic ") {
			t.Errorf("%s: 401 without a Basic challenge (%q)", c.name, resp.Header.Get("WWW-Authenticate"))
		}
	}

	// CrossPoint sends both forms at once; a good Basic carries it even
	// beside a stale x-auth pair.
	both := basic("reader", device)
	for k, v := range xauth("reader", plain) {
		both[k] = v
	}
	if resp, _ := app.deviceRequest(t, http.MethodGet, "/api/kosync/users/auth", both, nil); resp.StatusCode != http.StatusOK {
		t.Errorf("basic + x-auth = %d", resp.StatusCode)
	}
}

func TestEreaderRevokedTokenFailsEveryScheme(t *testing.T) {
	app := newEpubTestApp(t)
	app.attachEpub(t, apiEpubFixture(t))
	device := app.mintAs(t, app.client, map[string]any{"name": "kindle", "device": true})

	ctx := context.Background()
	tokens, err := app.store.ListAPITokens(ctx, app.userID)
	if err != nil || len(tokens) != 1 {
		t.Fatalf("list tokens: %v (%d)", err, len(tokens))
	}
	if !tokens[0].Device {
		t.Error("a device token lists as device: false")
	}
	if err := app.store.RevokeAPIToken(ctx, app.userID, tokens[0].ID); err != nil {
		t.Fatal(err)
	}
	for name, h := range map[string]map[string]string{
		"basic":  basic("reader", device),
		"x-auth": xauth("reader", device),
		"bearer": {"Authorization": "Bearer " + device},
	} {
		if resp, _ := app.deviceRequest(t, http.MethodGet, "/api/kosync/users/auth", h, nil); resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("revoked %s = %d, want 401", name, resp.StatusCode)
		}
	}
}

func TestProgressWriteScope(t *testing.T) {
	app := newEpubTestApp(t)
	app.attachEpub(t, apiEpubFixture(t))
	device := app.mintAs(t, app.client, map[string]any{"name": "kindle", "device": true})
	readOnly := app.mintAs(t, app.client, map[string]any{"name": "mcp"})

	position := []byte(`{"char_offset": 0, "source": "read"}`)
	bearer := func(secret string) map[string]string {
		return map[string]string{"Authorization": "Bearer " + secret, "Content-Type": "application/json"}
	}

	// The position write: allowed for progress:write, refused for books:read.
	for _, method := range []string{http.MethodPut, http.MethodPost} {
		if resp, body := app.deviceRequest(t, method, "/api/books/"+epubFixtureEntry+"/position", bearer(device), position); resp.StatusCode != http.StatusOK {
			t.Errorf("device %s position = %d (%s), want 200", method, resp.StatusCode, body)
		}
		if resp, _ := app.deviceRequest(t, method, "/api/books/"+epubFixtureEntry+"/position", bearer(readOnly), position); resp.StatusCode != http.StatusForbidden {
			t.Errorf("read-only %s position = %d, want 403", method, resp.StatusCode)
		}
	}

	// The kosync progress PUT passes the gate for the device (the route
	// itself lands with MAD-831) and is refused for a read-only token.
	kosync := xauth("reader", device)
	if resp, _ := app.deviceRequest(t, http.MethodPut, "/api/kosync/syncs/progress", kosync, []byte(`{}`)); resp.StatusCode == http.StatusForbidden {
		t.Error("device kosync progress PUT hit the read-only wall")
	}
	if resp, _ := app.deviceRequest(t, http.MethodPut, "/api/kosync/syncs/progress", bearer(readOnly), []byte(`{}`)); resp.StatusCode != http.StatusForbidden {
		t.Errorf("read-only kosync progress PUT = %d, want 403", resp.StatusCode)
	}

	// progress:write opens nothing else.
	for _, w := range []struct{ method, path string }{
		{http.MethodPost, "/api/library"},
		{http.MethodPatch, "/api/library/" + epubFixtureEntry},
		{http.MethodPost, "/api/books/" + epubFixtureEntry + "/sessions"},
		{http.MethodPost, "/api/tokens"},
	} {
		if resp, _ := app.deviceRequest(t, w.method, w.path, bearer(device), []byte(`{}`)); resp.StatusCode != http.StatusForbidden {
			t.Errorf("device %s %s = %d, want 403", w.method, w.path, resp.StatusCode)
		}
	}
}

func TestEpubDownloadIsOwnerScoped(t *testing.T) {
	app := newEpubTestApp(t)
	app.attachEpub(t, apiEpubFixture(t))

	jar, _ := cookiejar.New(nil)
	other := &http.Client{Jar: jar, Timeout: 10 * time.Second}
	register(t, app.ts.URL, other, "snoop@example.com", "snoop", "hogwash123")
	stranger := app.mintAs(t, other, map[string]any{"name": "their kindle", "device": true})

	// Not a 403: whether the entry exists is none of their business.
	if resp, _ := app.deviceRequest(t, http.MethodGet, downloadPath, basic("snoop", stranger), nil); resp.StatusCode != http.StatusNotFound {
		t.Errorf("stranger download = %d, want 404", resp.StatusCode)
	}

	// An entry with no EPUB is the same 404.
	app2 := newEpubTestApp(t)
	app2.attachEpub(t, nil)
	device := app2.mintAs(t, app2.client, map[string]any{"name": "kindle", "device": true})
	if resp, _ := app2.deviceRequest(t, http.MethodGet, downloadPath, basic("reader", device), nil); resp.StatusCode != http.StatusNotFound {
		t.Errorf("no-epub download = %d, want 404", resp.StatusCode)
	}
}

func TestDeviceTokenDefaults(t *testing.T) {
	app := newEpubTestApp(t)
	raw, _ := json.Marshal(map[string]any{"name": "kindle", "device": true})
	resp, err := app.client.Post(app.ts.URL+"/api/tokens", "application/json", bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out struct {
		Scopes []string `json:"scopes"`
		Device bool     `json:"device"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&out)
	if !out.Device || strings.Join(out.Scopes, ",") != "books:read,progress:write" {
		t.Errorf("device token = %+v, want device with books:read,progress:write", out)
	}
}

func TestLongTransferSkipsTimeout(t *testing.T) {
	var deadline bool
	h := timeoutExcept(time.Second, isLongTransfer)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, deadline = r.Context().Deadline()
	}))
	for path, wantDeadline := range map[string]bool{
		downloadPath:                    false,
		"/api/opds/books/e1/epub/x.epub": false,
		"/api/books/e1/text":             true,
		"/api/opds/books/e1/cover":       true,
		"/api/kosync/users/auth":         true,
	} {
		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, path, nil))
		if deadline != wantDeadline {
			t.Errorf("%s: deadline = %v, want %v", path, deadline, wantDeadline)
		}
	}
}
