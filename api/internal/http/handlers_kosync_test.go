package http

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/collinpendleton/backhog/api/internal/ereader"
)

// The KOReader sync protocol (MAD-831) as KOReader sends it: x-auth
// headers, JSON bodies, and nothing else on the wire.

type kosyncApp struct {
	*epubTestApp
	device  string
	fixture []byte
	binary  string
}

func newKosyncApp(t *testing.T) kosyncApp {
	t.Helper()
	app := newEpubTestApp(t)
	fixture := apiEpubFixture(t)
	app.attachEpub(t, fixture)
	binary, err := ereader.PartialMD5(bytes.NewReader(fixture), int64(len(fixture)))
	if err != nil {
		t.Fatal(err)
	}
	return kosyncApp{epubTestApp: app, fixture: fixture, binary: binary,
		device: app.mintAs(t, app.client, map[string]any{"name": "kindle", "device": true})}
}

func (a kosyncApp) push(t *testing.T, body map[string]any) (int, map[string]any) {
	t.Helper()
	raw, _ := json.Marshal(body)
	h := xauth("reader", a.device)
	h["Content-Type"] = "application/json"
	resp, data := a.deviceRequest(t, http.MethodPut, "/api/kosync/syncs/progress", h, raw)
	out := map[string]any{}
	_ = json.Unmarshal(data, &out)
	return resp.StatusCode, out
}

func (a kosyncApp) pull(t *testing.T, doc string) (int, map[string]any) {
	t.Helper()
	resp, data := a.deviceRequest(t, http.MethodGet,
		"/api/kosync/syncs/progress/"+doc+"?position_kinds=locator,percentage", xauth("reader", a.device), nil)
	out := map[string]any{}
	_ = json.Unmarshal(data, &out)
	return resp.StatusCode, out
}

// position reads the entry's Backhog position through the app's own API.
func (a kosyncApp) position(t *testing.T) int {
	t.Helper()
	status, body := a.get(t, "/api/books/"+epubFixtureEntry+"/position")
	if status != http.StatusOK {
		t.Fatalf("position = %d (%v)", status, body)
	}
	return int(body["char_offset"].(float64))
}

// offsetOf is the canonical offset of a block of the fixture.
func (a kosyncApp) offsetOf(t *testing.T, folded string) int {
	t.Helper()
	status, body := a.get(t, "/api/books/"+epubFixtureEntry+"/text?until=none")
	if status != http.StatusOK {
		t.Fatalf("text = %d", status)
	}
	i := strings.Index(body["text"].(string), folded)
	if i < 0 {
		t.Fatalf("%q not in the canonical text %q", folded, body["text"])
	}
	return i
}

func TestKosyncPushMovesBackhog(t *testing.T) {
	app := newKosyncApp(t)

	// KOReader on a book it has never downloaded from us: the binary hash
	// is unknown until the server hashes the library on demand.
	status, body := app.push(t, map[string]any{
		"document": app.binary, "progress": "/body/DocFragment[2]/body/p[2]/text().3",
		"percentage": 0.62, "device": "Kindle", "device_id": "KT3",
	})
	if status != http.StatusOK || body["document"] != app.binary || body["timestamp"] == nil {
		t.Fatalf("push = %d (%v)", status, body)
	}
	if got, want := app.position(t), app.offsetOf(t, "more text"); got != want {
		t.Errorf("Backhog position = %d, want the block of the XPointer (%d)", got, want)
	}

	// Backhog has not moved since: the pull hands back the push verbatim.
	status, body = app.pull(t, app.binary)
	if status != http.StatusOK || body["progress"] != "/body/DocFragment[2]/body/p[2]/text().3" ||
		body["device"] != "Kindle" || body["percentage"] != 0.62 {
		t.Errorf("pull after push = %d (%v)", status, body)
	}
}

func TestKosyncPullFollowsTheWebReader(t *testing.T) {
	app := newKosyncApp(t)

	// The web reader moves to "The end.".
	want := app.offsetOf(t, "the end")
	raw, _ := json.Marshal(map[string]any{"char_offset": want, "source": "read"})
	req, _ := http.NewRequest(http.MethodPut, app.ts.URL+"/api/books/"+epubFixtureEntry+"/position", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.client.Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("web position: %v %v", err, resp)
	}
	resp.Body.Close()

	status, body := app.pull(t, app.binary)
	if status != http.StatusOK || body["device"] != "Backhog" {
		t.Fatalf("pull = %d (%v)", status, body)
	}
	xp, _ := body["progress"].(string)
	if xp != "/body/DocFragment[3]/body/p/text().0" {
		t.Errorf("pull progress = %q", xp)
	}
	if p, _ := body["percentage"].(float64); p <= 0.5 || p > 1 {
		t.Errorf("pull percentage = %v", body["percentage"])
	}

	// Pushing that pointer back lands exactly where the web reader was.
	app.push(t, map[string]any{"document": app.binary, "progress": xp, "percentage": 0.9,
		"device": "Kindle", "device_id": "KT3"})
	if got := app.position(t); got != want {
		t.Errorf("round trip landed at %d, want %d", got, want)
	}
}

func TestKosyncFallsBackToPercentage(t *testing.T) {
	app := newKosyncApp(t)
	// A pointer this book cannot follow (another engine's, or garbage).
	status, _ := app.push(t, map[string]any{"document": app.binary, "progress": "#_doc_fragment_9",
		"percentage": 1.0, "device": "Kindle", "device_id": "KT3"})
	if status != http.StatusOK {
		t.Fatalf("push = %d", status)
	}
	if got, want := app.position(t), app.offsetOf(t, "the end"); got != want {
		t.Errorf("100%% landed at %d, want the last block %d", got, want)
	}
}

func TestKosyncUnknownDocumentIsParkedAndLinkable(t *testing.T) {
	app := newKosyncApp(t)
	const stray = "0123456789abcdef0123456789abcdef"

	if status, _ := app.pull(t, stray); status != http.StatusOK {
		t.Errorf("pull of an unknown document = %d, want 200 {}", status)
	}
	status, _ := app.push(t, map[string]any{"document": stray, "progress": "/body/DocFragment[2]/body/p/text().0",
		"percentage": 0.3, "device": "CrossPoint", "device_id": "crosspoint-reader",
		"metadata": map[string]any{"title": "Some Other Book", "authors": "Nobody", "filename": "Nobody - Some Other Book.epub"}})
	if status != http.StatusOK {
		t.Fatalf("unknown push = %d, want 200 (no error on the device)", status)
	}

	status, body := app.get(t, "/api/ereader/unmatched")
	list, _ := body["unmatched"].([]any)
	if status != http.StatusOK || len(list) != 1 || list[0].(map[string]any)["title"] != "Some Other Book" {
		t.Fatalf("unmatched = %d (%v)", status, body)
	}

	raw, _ := json.Marshal(map[string]string{"entry_id": epubFixtureEntry})
	resp, err := app.client.Post(app.ts.URL+"/api/ereader/unmatched/"+stray+"/link", "application/json", bytes.NewReader(raw))
	if err != nil || resp.StatusCode != http.StatusNoContent {
		t.Fatalf("link = %v %v", err, resp)
	}
	resp.Body.Close()

	// Linked: the next push lands on the book, and the list is empty.
	app.push(t, map[string]any{"document": stray, "progress": "/body/DocFragment[3]/body/p/text().0",
		"percentage": 0.95, "device": "CrossPoint", "device_id": "crosspoint-reader"})
	if got, want := app.position(t), app.offsetOf(t, "the end"); got != want {
		t.Errorf("linked push landed at %d, want %d", got, want)
	}
	if _, body := app.get(t, "/api/ereader/unmatched"); len(body["unmatched"].([]any)) != 0 {
		t.Errorf("unmatched after linking = %v", body)
	}
}

func TestKosyncMatchesByMetadata(t *testing.T) {
	app := newKosyncApp(t)
	// CrossPoint's filename-mode hash of a name we never generated, but the
	// push carries the title: one book has it, so the push lands there.
	status, _ := app.push(t, map[string]any{"document": ereader.FilenameMD5("renamed.epub"),
		"progress": "/body/DocFragment[3]/body/p/text().0", "percentage": 0.95,
		"device": "CrossPoint", "device_id": "crosspoint-reader",
		"metadata": map[string]any{"title": "fixture book", "authors": "", "filename": "renamed.epub"}})
	if status != http.StatusOK {
		t.Fatalf("push = %d", status)
	}
	if got, want := app.position(t), app.offsetOf(t, "the end"); got != want {
		t.Errorf("metadata-matched push landed at %d, want %d", got, want)
	}
}

func TestKosyncRecordsReadingSessions(t *testing.T) {
	app := newKosyncApp(t)
	push := func(progress string) {
		if status, _ := app.push(t, map[string]any{"document": app.binary, "progress": progress,
			"percentage": 0.5, "device": "Kindle", "device_id": "KT3"}); status != http.StatusOK {
			t.Fatalf("push = %d", status)
		}
	}
	push("/body/DocFragment[2]/body/h1/text().0")
	// Twenty minutes pass on the device.
	if _, err := app.store.DB().Exec(`UPDATE kosync_progress SET pushed_at = ?`,
		time.Now().UTC().Add(-20*time.Minute)); err != nil {
		t.Fatal(err)
	}
	push("/body/DocFragment[3]/body/p/text().0")

	status, body := app.get(t, "/api/books/"+epubFixtureEntry+"/sessions")
	sessions, _ := body["sessions"].([]any)
	if status != http.StatusOK || len(sessions) != 1 {
		t.Fatalf("sessions = %d (%v)", status, body)
	}
	s := sessions[0].(map[string]any)
	if s["mode"] != "read" || s["chars_advanced"].(float64) <= 0 || s["seconds"].(float64) < 1100 {
		t.Errorf("session = %v", s)
	}
}

func TestKosyncRegistrationNeverCreates(t *testing.T) {
	app := newKosyncApp(t)
	post := func(user string) int {
		raw, _ := json.Marshal(map[string]string{"username": user, "password": "d41d8cd98f00b204e9800998ecf8427e"})
		resp, _ := app.deviceRequest(t, http.MethodPost, "/api/kosync/users/create",
			map[string]string{"Content-Type": "application/json"}, raw)
		return resp.StatusCode
	}
	if got := post("reader"); got != http.StatusPaymentRequired {
		t.Errorf("existing username = %d, want 402 (already registered)", got)
	}
	if got := post("newcomer"); got != http.StatusForbidden {
		t.Errorf("new username = %d, want 403", got)
	}
	if ok, _ := app.store.UsernameExists(t.Context(), "newcomer"); ok {
		t.Error("kosync registration created an account")
	}
}

func TestKosyncWriteNeedsProgressScope(t *testing.T) {
	app := newKosyncApp(t)
	readOnly := app.mintAs(t, app.client, map[string]any{"name": "mcp"})
	raw, _ := json.Marshal(map[string]any{"document": app.binary, "progress": "x", "percentage": 0.1})
	resp, _ := app.deviceRequest(t, http.MethodPut, "/api/kosync/syncs/progress",
		map[string]string{"Authorization": "Bearer " + readOnly, "Content-Type": "application/json"}, raw)
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("read-only push = %d, want 403", resp.StatusCode)
	}
	if got := app.position(t); got != 0 {
		t.Errorf("a refused push moved the position to %d", got)
	}
}
