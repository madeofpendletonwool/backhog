package http

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/collinpendleton/backhog/api/internal/backfill"
	"github.com/collinpendleton/backhog/api/internal/config"
	"github.com/collinpendleton/backhog/api/internal/db"
	"github.com/collinpendleton/backhog/api/internal/media"
	"github.com/collinpendleton/backhog/api/internal/models"
	"github.com/collinpendleton/backhog/api/internal/store"
)

type accountsTestApp struct {
	ts    *httptest.Server
	store *store.Store
	// owner is the first account, and therefore the administrator.
	owner *http.Client
}

func newAccountsTestApp(t *testing.T) *accountsTestApp {
	t.Helper()

	database, err := db.Open(filepath.Join(t.TempDir(), "accounts.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	if err := db.Migrate(database); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	st := store.New(database)

	cfg := config.Config{EpubTextDir: filepath.Join(t.TempDir(), "epub_text")}
	srv := NewServer(cfg, st, nil, nil, nil, nil, &backfill.Runner{}, media.NewRunner(st, nil))
	ts := httptest.NewServer(srv.Routes())
	t.Cleanup(ts.Close)

	app := &accountsTestApp{ts: ts, store: st}
	app.owner = app.signUp(t, "owner@example.com", "owner", "")
	return app
}

// signUp registers an account and returns a client holding its session. An
// empty token is a plain self-service sign-up.
func (a *accountsTestApp) signUp(t *testing.T, email, username, invite string) *http.Client {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookie jar: %v", err)
	}
	client := &http.Client{Jar: jar, Timeout: 10 * time.Second}
	status, body := a.as(t, client, http.MethodPost, "/api/auth/register", map[string]string{
		"email": email, "username": username, "password": "hogwash123", "invite": invite,
	})
	if status != http.StatusCreated {
		t.Fatalf("register %s: status %d (%v)", email, status, body)
	}
	return client
}

// as runs one request and decodes the JSON envelope, whatever the status.
func (a *accountsTestApp) as(t *testing.T, client *http.Client, method, path string, body any) (int, map[string]any) {
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
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()

	out := map[string]any{}
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

func (a *accountsTestApp) role(t *testing.T, client *http.Client) string {
	t.Helper()
	status, body := a.as(t, client, http.MethodGet, "/api/auth/me", nil)
	if status != http.StatusOK {
		t.Fatalf("/auth/me = %d", status)
	}
	role, _ := body["role"].(string)
	return role
}

func (a *accountsTestApp) userID(t *testing.T, email string) string {
	t.Helper()
	user, _, err := a.store.GetUserByEmail(context.Background(), email)
	if err != nil {
		t.Fatalf("look up %s: %v", email, err)
	}
	return user.ID
}

// TestFirstAccountAdminsAndTheRestRead covers the bootstrap rule and the
// cautious default in one pass: whoever sets the server up administers it,
// and anyone who signs themselves up afterwards lands on `reader`.
func TestFirstAccountAdminsAndTheRestRead(t *testing.T) {
	app := newAccountsTestApp(t)
	if got := app.role(t, app.owner); got != models.RoleAdmin {
		t.Errorf("first account role = %q, want admin", got)
	}
	friend := app.signUp(t, "friend@example.com", "friend", "")
	if got := app.role(t, friend); got != models.RoleReader {
		t.Errorf("self-service account role = %q, want reader", got)
	}
}

func TestRegistrationClosesAndInvitesStillWork(t *testing.T) {
	app := newAccountsTestApp(t)

	status, cfg := app.as(t, app.owner, http.MethodGet, "/api/auth/config", nil)
	if status != http.StatusOK || cfg["registration_enabled"] != true {
		t.Fatalf("auth config = %d %v, want registration on", status, cfg)
	}

	status, _ = app.as(t, app.owner, http.MethodPut, "/api/admin/settings", map[string]any{
		"registration_enabled": false, "default_role": models.RoleReader,
	})
	if status != http.StatusOK {
		t.Fatalf("close registration = %d", status)
	}

	// The front door is shut, and says so to a logged-out caller.
	anon := &http.Client{Timeout: 10 * time.Second}
	status, cfg = app.as(t, anon, http.MethodGet, "/api/auth/config", nil)
	if status != http.StatusOK || cfg["registration_enabled"] != false {
		t.Fatalf("auth config after closing = %d %v", status, cfg)
	}
	status, _ = app.as(t, anon, http.MethodPost, "/api/auth/register", map[string]string{
		"email": "nosy@example.com", "username": "nosy", "password": "hogwash123",
	})
	if status != http.StatusForbidden {
		t.Fatalf("uninvited sign-up = %d, want 403", status)
	}

	// An invite still gets in, and carries the role it was cut for.
	status, invite := app.as(t, app.owner, http.MethodPost, "/api/admin/invites", map[string]any{
		"email": "friend@example.com", "role": models.RoleMember, "note": "the book guy",
	})
	if status != http.StatusCreated {
		t.Fatalf("create invite = %d (%v)", status, invite)
	}
	token, _ := invite["token"].(string)
	if token == "" {
		t.Fatal("invite came back with no token")
	}

	// The sign-up page can read the offer before anyone types a password.
	status, cfg = app.as(t, anon, http.MethodGet, "/api/auth/config?invite="+token, nil)
	if status != http.StatusOK {
		t.Fatalf("resolve invite = %d", status)
	}
	offer, _ := cfg["invite"].(map[string]any)
	if offer == nil || offer["role"] != models.RoleMember || offer["invited_by"] != "owner" {
		t.Fatalf("invite offer = %v", cfg["invite"])
	}

	friend := app.signUp(t, "friend@example.com", "friend", token)
	if got := app.role(t, friend); got != models.RoleMember {
		t.Errorf("invited account role = %q, want member", got)
	}

	// Single use: the same link cannot seat a second person.
	status, _ = app.as(t, anon, http.MethodPost, "/api/auth/register", map[string]string{
		"email": "second@example.com", "username": "second",
		"password": "hogwash123", "invite": token,
	})
	if status != http.StatusForbidden {
		t.Fatalf("reused invite = %d, want 403", status)
	}
	// And a bad token is refused without saying whether it was ever real.
	status, cfg = app.as(t, anon, http.MethodGet, "/api/auth/config?invite=nonsense", nil)
	if status != http.StatusOK || cfg["invite"] != nil {
		t.Fatalf("unknown token = %d %v, want no offer", status, cfg)
	}
}

func TestAdminRoutesRefuseEveryoneElse(t *testing.T) {
	app := newAccountsTestApp(t)
	friend := app.signUp(t, "friend@example.com", "friend", "")
	friendID := app.userID(t, "friend@example.com")

	for _, tc := range []struct {
		method, path string
		body         any
	}{
		{http.MethodGet, "/api/admin/users", nil},
		{http.MethodGet, "/api/admin/settings", nil},
		{http.MethodGet, "/api/admin/invites", nil},
		{http.MethodPost, "/api/admin/invites", map[string]any{"role": models.RoleAdmin}},
		{http.MethodPatch, "/api/admin/users/" + friendID, map[string]any{"role": models.RoleAdmin}},
		{http.MethodDelete, "/api/admin/users/" + friendID, nil},
	} {
		status, _ := app.as(t, friend, tc.method, tc.path, tc.body)
		if status != http.StatusForbidden {
			t.Errorf("%s %s as a reader = %d, want 403", tc.method, tc.path, status)
		}
	}

	// Promoting to member is not promoting to admin.
	if _, err := app.store.SetUserRole(context.Background(), friendID, models.RoleMember); err != nil {
		t.Fatalf("promote: %v", err)
	}
	if status, _ := app.as(t, friend, http.MethodGet, "/api/admin/users", nil); status != http.StatusForbidden {
		t.Errorf("member reading the account panel = %d, want 403", status)
	}
}

func TestAdminCannotLockThemselvesOut(t *testing.T) {
	app := newAccountsTestApp(t)
	ownerID := app.userID(t, "owner@example.com")

	for _, body := range []any{
		map[string]any{"role": models.RoleReader},
		map[string]any{"disabled": true},
	} {
		status, _ := app.as(t, app.owner, http.MethodPatch, "/api/admin/users/"+ownerID, body)
		if status != http.StatusConflict {
			t.Errorf("self-edit %v = %d, want 409", body, status)
		}
	}
	if status, _ := app.as(t, app.owner, http.MethodDelete, "/api/admin/users/"+ownerID, nil); status != http.StatusConflict {
		t.Errorf("self-delete = %d, want 409", status)
	}
}

func TestDisabledAccountIsTurnedAwayAtEveryDoor(t *testing.T) {
	app := newAccountsTestApp(t)
	friend := app.signUp(t, "friend@example.com", "friend", "")
	friendID := app.userID(t, "friend@example.com")

	status, _ := app.as(t, app.owner, http.MethodPatch, "/api/admin/users/"+friendID,
		map[string]any{"disabled": true})
	if status != http.StatusOK {
		t.Fatalf("disable = %d", status)
	}

	// The live session dies on its very next request, without anyone having
	// to hunt down a cookie.
	if status, _ := app.as(t, friend, http.MethodGet, "/api/auth/me", nil); status != http.StatusUnauthorized {
		t.Errorf("disabled session = %d, want 401", status)
	}
	// And signing in again says why, rather than sending them round a
	// password reset that cannot help.
	anon := &http.Client{Timeout: 10 * time.Second}
	status, body := app.as(t, anon, http.MethodPost, "/api/auth/login", map[string]string{
		"email": "friend@example.com", "password": "hogwash123",
	})
	if status != http.StatusForbidden {
		t.Fatalf("disabled login = %d (%v), want 403", status, body)
	}

	// Restoring lets them back in.
	if status, _ := app.as(t, app.owner, http.MethodPatch, "/api/admin/users/"+friendID,
		map[string]any{"disabled": false}); status != http.StatusOK {
		t.Fatalf("re-enable = %d", status)
	}
	if status, _ = app.as(t, anon, http.MethodPost, "/api/auth/login", map[string]string{
		"email": "friend@example.com", "password": "hogwash123",
	}); status != http.StatusOK {
		t.Errorf("restored login = %d, want 200", status)
	}
}

func TestAdminPasswordResetSignsThemOut(t *testing.T) {
	app := newAccountsTestApp(t)
	friend := app.signUp(t, "friend@example.com", "friend", "")
	friendID := app.userID(t, "friend@example.com")

	if status, _ := app.as(t, app.owner, http.MethodPost, "/api/admin/users/"+friendID+"/password",
		map[string]string{"new_password": "brandnewpassword"}); status != http.StatusOK {
		t.Fatalf("reset = %d", status)
	}
	if status, _ := app.as(t, friend, http.MethodGet, "/api/auth/me", nil); status != http.StatusUnauthorized {
		t.Errorf("session after a password they did not choose = %d, want 401", status)
	}

	anon := &http.Client{Timeout: 10 * time.Second}
	if status, _ := app.as(t, anon, http.MethodPost, "/api/auth/login", map[string]string{
		"email": "friend@example.com", "password": "brandnewpassword",
	}); status != http.StatusOK {
		t.Errorf("login with the new password = %d, want 200", status)
	}
	// Too short is refused before anything is written.
	if status, _ := app.as(t, app.owner, http.MethodPost, "/api/admin/users/"+friendID+"/password",
		map[string]string{"new_password": "short"}); status != http.StatusBadRequest {
		t.Errorf("short reset = %d, want 400", status)
	}
}

// seedSharedBook puts one book in the owner's library with an EPUB and an
// audio track attached, and gives the friend their own entry for the same
// work. Files are inventoried through the store because the point of these
// tests is the HTTP gate, not the scanner.
func (a *accountsTestApp) seedSharedBook(t *testing.T, friendEmail string) (ownerEntry, friendEntry string) {
	t.Helper()
	ctx := context.Background()

	if _, err := a.store.DB().ExecContext(ctx,
		`INSERT INTO books (id, title) VALUES ('OL1W', 'Anathem')`); err != nil {
		t.Fatalf("seed book: %v", err)
	}
	// A printing for a physical copy to point at: page maps key off the
	// edition, never the work.
	if _, err := a.store.DB().ExecContext(ctx,
		`INSERT INTO book_editions (id, book_id, page_count) VALUES ('OL1M', 'OL1W', 981)`); err != nil {
		t.Fatalf("seed edition: %v", err)
	}
	files := []models.MediaFile{
		{Root: "/nas", Path: "Neal Stephenson/Anathem/01.m4b", Kind: models.MediaFileAudio,
			SizeBytes: 10, Mtime: 1, ScannedAt: time.Now()},
		{Root: "/nas", Path: "books/Anathem.epub", Kind: models.MediaFileEpub,
			SizeBytes: 10, Mtime: 1, ScannedAt: time.Now()},
	}
	if err := a.store.InsertMediaFiles(ctx, files); err != nil {
		t.Fatalf("inventory: %v", err)
	}

	ownerID := a.userID(t, "owner@example.com")
	friendID := a.userID(t, friendEmail)
	owner, err := a.store.AddBookEntry(ctx, ownerID, "OL1W", nil, models.StatusBacklog)
	if err != nil {
		t.Fatalf("owner entry: %v", err)
	}
	friend, err := a.store.AddBookEntry(ctx, friendID, "OL1W", nil, models.StatusBacklog)
	if err != nil {
		t.Fatalf("friend entry: %v", err)
	}

	rows, err := a.store.ListMediaFiles(ctx, store.MediaFileFilter{})
	if err != nil {
		t.Fatalf("list files: %v", err)
	}
	for _, f := range rows {
		if _, err := a.store.AttachMediaFiles(ctx, ownerID, owner.ID, []int64{f.ID}, f.Kind); err != nil {
			t.Fatalf("attach %d: %v", f.ID, err)
		}
	}
	return owner.ID, friend.ID
}

// TestReaderIsKeptOutOfTheFileLayer is the role gate from the outside: a
// reader uses the whole app and touches none of the plumbing under it.
func TestReaderIsKeptOutOfTheFileLayer(t *testing.T) {
	app := newAccountsTestApp(t)
	friend := app.signUp(t, "friend@example.com", "friend", "")
	_, friendEntry := app.seedSharedBook(t, "friend@example.com")

	for _, tc := range []struct {
		method, path string
		body         any
	}{
		{http.MethodGet, "/api/media/files", nil},
		{http.MethodGet, "/api/media/candidates", nil},
		{http.MethodPost, "/api/media/scan", nil},
		{http.MethodGet, "/api/media/scan", nil},
		{http.MethodPost, "/api/books/" + friendEntry + "/files", map[string]any{
			"file_ids": []int64{1}, "kind": models.MediaFileAudio}},
		{http.MethodDelete, "/api/books/" + friendEntry + "/files/1", nil},
		{http.MethodPut, "/api/books/" + friendEntry + "/files/2/primary", nil},
		{http.MethodPost, "/api/books/" + friendEntry + "/align", nil},
		{http.MethodDelete, "/api/books/" + friendEntry + "/align", nil},
	} {
		status, _ := app.as(t, friend, tc.method, tc.path, tc.body)
		if status != http.StatusForbidden {
			t.Errorf("%s %s as a reader = %d, want 403", tc.method, tc.path, status)
		}
	}

	// The same calls are ordinary work for the owner. Attaching an already
	// attached file is a conflict, not a refusal — what matters is that the
	// role gate let the request through to the handler at all.
	if status, _ := app.as(t, app.owner, http.MethodGet, "/api/media/files", nil); status != http.StatusOK {
		t.Errorf("owner listing files = %d, want 200", status)
	}
	if status, _ := app.as(t, app.owner, http.MethodPost, "/api/media/scan", nil); status != http.StatusOK {
		t.Errorf("owner kicking the scan = %d, want 200", status)
	}
}

// TestSharingEndToEnd walks the whole feature the way the two people do:
// the friend adds a book and finds nothing, the owner shares it, and the
// files appear — with the NAS paths blanked, because that is the owner's
// directory layout and not part of the loan.
func TestSharingEndToEnd(t *testing.T) {
	app := newAccountsTestApp(t)
	friend := app.signUp(t, "friend@example.com", "friend", "")
	ownerEntry, friendEntry := app.seedSharedBook(t, "friend@example.com")
	friendID := app.userID(t, "friend@example.com")

	// Adding the same work is not enough to read somebody else's copy.
	status, body := app.as(t, friend, http.MethodGet, "/api/books/"+friendEntry+"/files", nil)
	if status != http.StatusNotFound {
		t.Fatalf("unshared file list = %d (%v), want 404", status, body)
	}
	if status, _ := app.as(t, friend, http.MethodGet, "/api/books/"+friendEntry+"/audio", nil); status != http.StatusNotFound {
		t.Errorf("unshared audio timeline = %d, want 404", status)
	}

	// The owner's picker offers the friend, unshared.
	status, body = app.as(t, app.owner, http.MethodGet, "/api/books/"+ownerEntry+"/shares", nil)
	if status != http.StatusOK {
		t.Fatalf("share picker = %d (%v)", status, body)
	}
	candidates, _ := body["candidates"].([]any)
	if len(candidates) != 1 {
		t.Fatalf("picker offered %d accounts, want 1", len(candidates))
	}
	if first, _ := candidates[0].(map[string]any); first["username"] != "friend" || first["shared"] != false {
		t.Fatalf("candidate = %v", candidates[0])
	}

	if status, body = app.as(t, app.owner, http.MethodPost, "/api/books/"+ownerEntry+"/shares",
		map[string]string{"user_id": friendID}); status != http.StatusOK {
		t.Fatalf("share = %d (%v)", status, body)
	}

	// Now the friend can see what is attached — but not where it lives.
	status, body = app.as(t, friend, http.MethodGet, "/api/books/"+friendEntry+"/files", nil)
	if status != http.StatusOK {
		t.Fatalf("shared file list = %d (%v)", status, body)
	}
	files, _ := body["files"].([]any)
	if len(files) != 2 {
		t.Fatalf("shared file list has %d files, want 2", len(files))
	}
	for _, raw := range files {
		f, _ := raw.(map[string]any)
		if f["root"] != "" && f["root"] != nil {
			t.Errorf("reader was handed the NAS root %v", f["root"])
		}
		if path, _ := f["path"].(string); path != "01.m4b" && path != "Anathem.epub" {
			t.Errorf("reader was handed the directory layout: %q", path)
		}
	}

	// The owner's own listing is unredacted — they are the one who has to
	// recognise which file on disk this is.
	status, body = app.as(t, app.owner, http.MethodGet, "/api/books/"+ownerEntry+"/files", nil)
	if status != http.StatusOK {
		t.Fatalf("owner file list = %d", status)
	}
	files, _ = body["files"].([]any)
	if first, _ := files[0].(map[string]any); first["root"] != "/nas" {
		t.Errorf("owner's own listing was redacted: %v", first["root"])
	}

	// Both sides of "who has what".
	status, body = app.as(t, friend, http.MethodGet, "/api/shares", nil)
	if status != http.StatusOK {
		t.Fatalf("shares overview = %d", status)
	}
	received, _ := body["received"].([]any)
	if len(received) != 1 {
		t.Fatalf("friend received %d shares, want 1", len(received))
	}
	if got, _ := received[0].(map[string]any); got["owner_username"] != "owner" || got["book_title"] != "Anathem" {
		t.Fatalf("received share = %v", received[0])
	}

	// Revoking takes back the files and leaves the friend's own entry —
	// and their progress — exactly where it was.
	if status, _ := app.as(t, app.owner, http.MethodDelete,
		"/api/books/"+ownerEntry+"/shares/"+friendID, nil); status != http.StatusOK {
		t.Fatalf("revoke = %d", status)
	}
	if status, _ := app.as(t, friend, http.MethodGet, "/api/books/"+friendEntry+"/files", nil); status != http.StatusNotFound {
		t.Errorf("revoked file list = %d, want 404", status)
	}
	if status, _ := app.as(t, friend, http.MethodGet, "/api/library/"+friendEntry, nil); status != http.StatusOK {
		t.Errorf("friend lost their entry to a revoke: %d", status)
	}
}

// TestSharingIsScopedToYourOwnEntries: the share endpoints are not a way to
// reach into somebody else's library, and a foreign entry id stays a 404.
func TestSharingIsScopedToYourOwnEntries(t *testing.T) {
	app := newAccountsTestApp(t)
	friend := app.signUp(t, "friend@example.com", "friend", "")
	ownerEntry, _ := app.seedSharedBook(t, "friend@example.com")
	ownerID := app.userID(t, "owner@example.com")

	if status, _ := app.as(t, friend, http.MethodGet, "/api/books/"+ownerEntry+"/shares", nil); status != http.StatusNotFound {
		t.Errorf("picker on a foreign entry = %d, want 404", status)
	}
	if status, _ := app.as(t, friend, http.MethodPost, "/api/books/"+ownerEntry+"/shares",
		map[string]string{"user_id": ownerID}); status != http.StatusNotFound {
		t.Errorf("share of a foreign entry = %d, want 404", status)
	}
	if status, _ := app.as(t, app.owner, http.MethodPost, "/api/books/"+ownerEntry+"/shares",
		map[string]string{"user_id": ownerID}); status != http.StatusBadRequest {
		t.Errorf("self-share = %d, want 400", status)
	}
}

// TestUnsharedBookKeepsItsOwnRows draws the line between the two resolvers.
// A book whose ebook belongs to someone else is still a book you can be
// reading on paper: your position, your reading sessions and the printing
// you own are your own rows and keep working. What you do not get is
// anything derived from their files — no chapter, no percentage, no audio,
// and no search inside a text you were never given.
func TestUnsharedBookKeepsItsOwnRows(t *testing.T) {
	app := newAccountsTestApp(t)
	friend := app.signUp(t, "friend@example.com", "friend", "")
	_, friendEntry := app.seedSharedBook(t, "friend@example.com")

	// Their own rows: readable and writable.
	status, body := app.as(t, friend, http.MethodGet, "/api/books/"+friendEntry+"/position", nil)
	if status != http.StatusOK {
		t.Fatalf("position on an unshared book = %d (%v), want 200", status, body)
	}
	// Nothing about the owner's text leaks into the answer.
	if count, _ := body["char_count"].(float64); count != 0 {
		t.Errorf("char_count = %v, want 0 for a book whose text is not ours", count)
	}
	if body["chapter"] != nil {
		t.Errorf("chapter = %v, want none", body["chapter"])
	}
	if body["audio"] != nil {
		t.Errorf("audio = %v, want none", body["audio"])
	}

	if status, body = app.as(t, friend, http.MethodPost, "/api/books/"+friendEntry+"/copies",
		map[string]any{"edition_id": "OL1M", "notes": "the fat paperback", "acquisition": "owned"}); status != http.StatusOK &&
		status != http.StatusCreated {
		t.Fatalf("register a physical copy = %d (%v)", status, body)
	}
	if status, _ := app.as(t, friend, http.MethodGet, "/api/books/"+friendEntry+"/copies", nil); status != http.StatusOK {
		t.Errorf("list physical copies = %d, want 200", status)
	}
	if status, _ := app.as(t, friend, http.MethodGet, "/api/books/"+friendEntry+"/sessions", nil); status != http.StatusOK {
		t.Errorf("reading sessions = %d, want 200", status)
	}

	// The file-backed doors stay shut.
	for _, path := range []string{
		"/api/books/" + friendEntry + "/text/chapters",
		"/api/books/" + friendEntry + "/text",
		"/api/books/" + friendEntry + "/audio",
		"/api/books/" + friendEntry + "/search?q=erasmas",
		"/api/books/" + friendEntry + "/align",
	} {
		if status, _ := app.as(t, friend, http.MethodGet, path, nil); status != http.StatusNotFound {
			t.Errorf("GET %s = %d, want 404", path, status)
		}
	}
}
