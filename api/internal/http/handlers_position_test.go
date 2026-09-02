package http

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/collinpendleton/backhog/api/internal/backfill"
	"github.com/collinpendleton/backhog/api/internal/books/position"
	"github.com/collinpendleton/backhog/api/internal/config"
	"github.com/collinpendleton/backhog/api/internal/db"
	"github.com/collinpendleton/backhog/api/internal/store"
)

// The fixture book: an EPUB plus two audio tracks, so all three coordinate
// spaces are reachable from one entry.
const (
	positionEntry     = "pe1"
	positionAudioOnly = "pe2"
	positionEpubFile  = 101
	positionTrackOne  = 102
	positionTrackTwo  = 103
	positionLoneTrack = 104

	positionTrackOneSeconds = 90.0
	positionTrackTwoSeconds = 45.0
)

// fixedAnchors is the Stage 7 / Stage 9 seam standing in for real alignment
// data, so the derived path can be exercised before either exists.
type fixedAnchors struct {
	audio, pages []position.Anchor
}

func (f fixedAnchors) AudioAnchors(context.Context, string) ([]position.Anchor, error) {
	return f.audio, nil
}
func (f fixedAnchors) PageAnchors(context.Context, string) ([]position.Anchor, error) {
	return f.pages, nil
}

type positionTestApp struct {
	ts     *httptest.Server
	client *http.Client
	store  *store.Store
	userID string
}

// newPositionTestApp boots the router over a NAS root holding a real EPUB and
// two real MP4 tracks, all attached to one book entry sitting in the backlog.
// anchors is the provider the position translator reads; pass nil for the
// unaligned world every book lives in today.
func newPositionTestApp(t *testing.T, anchors position.Provider) *positionTestApp {
	t.Helper()

	root := filepath.Join(t.TempDir(), "library")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir root: %v", err)
	}
	write := func(name string, body []byte) int {
		if err := os.WriteFile(filepath.Join(root, name), body, 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
		return len(body)
	}
	epubSize := write("book.epub", apiEpubFixture(t))
	trackOneSize := write("01.m4b", apiM4BFixture("Erasmas", positionTrackOneSeconds, 4096))
	trackTwoSize := write("02.m4b", apiM4BFixture("Apert", positionTrackTwoSeconds, 4096))
	loneSize := write("lone.m4b", apiM4BFixture("Lone", 60, 4096))

	database, err := db.Open(filepath.Join(t.TempDir(), "position.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	if err := db.Migrate(database); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	st := store.New(database)

	cfg := config.Config{
		EpubTextDir: filepath.Join(t.TempDir(), "epub_text"),
		MediaDirs:   []string{root},
	}
	srv := NewServer(cfg, st, nil, nil, nil, nil, &backfill.Runner{}, nil)
	if anchors != nil {
		srv.anchors = anchors
	}
	ts := httptest.NewServer(srv.Routes())
	t.Cleanup(ts.Close)

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookie jar: %v", err)
	}
	client := &http.Client{Jar: jar, Timeout: 30 * time.Second}
	register(t, ts.URL, client, "reader@example.com", "reader", "hogwash123")

	app := &positionTestApp{ts: ts, client: client, store: st}
	app.userID = app.whoami(t, client)

	now := time.Now()
	exec := func(q string, args ...any) {
		if _, err := st.DB().Exec(q, args...); err != nil {
			t.Fatalf("seed %q: %v", q, err)
		}
	}
	exec(`INSERT INTO books (id, title) VALUES ('OL1W', 'Anathem'), ('OL2W', 'Tape Only')`)
	exec(`INSERT INTO library_entries (id, user_id, media_type, book_id, status)
	      VALUES (?, ?, 'book', 'OL1W', 'backlog'), (?, ?, 'book', 'OL2W', 'backlog')`,
		positionEntry, app.userID, positionAudioOnly, app.userID)
	insertFile := func(id int, name, kind string, size int, bookID string, track any) {
		exec(`INSERT INTO media_files (id, root, path, kind, size_bytes, mtime, book_id, track_number, scanned_at)
		      VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			id, root, name, kind, size, now.UnixNano(), bookID, track, now.UTC())
	}
	insertFile(positionEpubFile, "book.epub", "epub", epubSize, "OL1W", nil)
	insertFile(positionTrackOne, "01.m4b", "audio", trackOneSize, "OL1W", 1)
	insertFile(positionTrackTwo, "02.m4b", "audio", trackTwoSize, "OL1W", 2)
	insertFile(positionLoneTrack, "lone.m4b", "audio", loneSize, "OL2W", 1)

	return app
}

func (a *positionTestApp) whoami(t *testing.T, client *http.Client) string {
	t.Helper()
	resp, err := client.Get(a.ts.URL + "/api/auth/me")
	if err != nil {
		t.Fatalf("me: %v", err)
	}
	defer resp.Body.Close()
	var me struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&me); err != nil {
		t.Fatalf("me: decode: %v", err)
	}
	return me.ID
}

func (a *positionTestApp) do(t *testing.T, client *http.Client, method, path string, body any) (int, map[string]any) {
	t.Helper()
	var payload []byte
	if body != nil {
		var err error
		if payload, err = json.Marshal(body); err != nil {
			t.Fatalf("marshal: %v", err)
		}
	}
	req, err := http.NewRequestWithContext(t.Context(), method, a.ts.URL+path, bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	var out map[string]any
	json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

func (a *positionTestApp) api(t *testing.T, method, path string, body any) (int, map[string]any) {
	t.Helper()
	return a.do(t, a.client, method, path, body)
}

// charCount parses the fixture EPUB through the text endpoint, which is how a
// real client learns the denominator before it ever writes a position.
func (a *positionTestApp) charCount(t *testing.T) int {
	t.Helper()
	status, body := a.api(t, http.MethodGet, "/api/books/"+positionEntry+"/text/chapters", nil)
	if status != http.StatusOK {
		t.Fatalf("chapters status = %d: %v", status, body)
	}
	return int(body["char_count"].(float64))
}

func TestBookPositionRoundTripsCharOffset(t *testing.T) {
	app := newPositionTestApp(t, nil)
	count := app.charCount(t)

	status, body := app.api(t, http.MethodPut, "/api/books/"+positionEntry+"/position",
		map[string]any{"char_offset": 40})
	if status != http.StatusOK {
		t.Fatalf("put status = %d: %v", status, body)
	}

	// The first write starts the book: it is no longer sitting in the backlog.
	if body["status"] != "playing" || body["status_changed"] != true {
		t.Errorf("first write left status %v (changed %v), want playing/true",
			body["status"], body["status_changed"])
	}

	status, got := app.api(t, http.MethodGet, "/api/books/"+positionEntry+"/position", nil)
	if status != http.StatusOK {
		t.Fatalf("get status = %d: %v", status, got)
	}
	if got["char_offset"] != 40.0 {
		t.Errorf("char_offset = %v, want the 40 that was written", got["char_offset"])
	}
	if got["source"] != "read" {
		t.Errorf("source = %v, want read", got["source"])
	}
	if got["char_count"] != float64(count) {
		t.Errorf("char_count = %v, want %d", got["char_count"], count)
	}
	wantPercent := 40.0 / float64(count) * 100
	if p := got["percent"].(float64); p < wantPercent-0.01 || p > wantPercent+0.01 {
		t.Errorf("percent = %v, want ~%v", p, wantPercent)
	}
	chapter, ok := got["chapter"].(map[string]any)
	if !ok {
		t.Fatalf("chapter = %#v, want the spine document holding offset 40", got["chapter"])
	}
	if start, end := chapter["char_start"].(float64), chapter["char_end"].(float64); start > 40 || end <= 40 {
		t.Errorf("chapter [%v,%v) does not contain offset 40", start, end)
	}

	// A second write is no longer a status change.
	_, body = app.api(t, http.MethodPut, "/api/books/"+positionEntry+"/position",
		map[string]any{"char_offset": 42})
	if body["status_changed"] != false {
		t.Errorf("second write reported a status change")
	}
}

// The player's last write of a session goes out through
// navigator.sendBeacon, which can only POST — so POST must be the same write
// as PUT, session cookie and all.
func TestBookPositionAcceptsBeaconPost(t *testing.T) {
	app := newPositionTestApp(t, nil)

	status, body := app.api(t, http.MethodPost, "/api/books/"+positionEntry+"/position",
		map[string]any{"audio_seconds": 10, "audio_file_id": positionTrackOne})
	if status != http.StatusOK {
		t.Fatalf("post status = %d: %v", status, body)
	}

	status, got := app.api(t, http.MethodGet, "/api/books/"+positionEntry+"/position", nil)
	if status != http.StatusOK {
		t.Fatalf("get status = %d: %v", status, got)
	}
	audio, ok := got["audio"].(map[string]any)
	if !ok {
		t.Fatalf("audio = %#v, want the derived audio view", got["audio"])
	}
	if audio["seconds"] != 10.0 || audio["track_id"] != float64(positionTrackOne) {
		t.Errorf("audio = %v seconds in track %v, want 10 in track %d",
			audio["seconds"], audio["track_id"], positionTrackOne)
	}
}

func TestBookPositionUnalignedAudioIsHonest(t *testing.T) {
	app := newPositionTestApp(t, nil)
	app.charCount(t)

	// A book with no alignment: the listening position cannot become a
	// character offset, so it is stored raw and reported as underived.
	status, body := app.api(t, http.MethodPut, "/api/books/"+positionEntry+"/position",
		map[string]any{"audio_seconds": 30.0, "audio_file_id": positionTrackTwo})
	if status != http.StatusOK {
		t.Fatalf("put audio status = %d: %v", status, body)
	}

	_, got := app.api(t, http.MethodGet, "/api/books/"+positionEntry+"/position", nil)
	audio, ok := got["audio"].(map[string]any)
	if !ok {
		t.Fatalf("audio = %#v, want the raw fallback", got["audio"])
	}
	if audio["derived"] != false {
		t.Error("an unaligned audio position claimed to be derived")
	}
	// Track two starts where track one ends, so 30s into it is global 120s.
	if audio["seconds"] != positionTrackOneSeconds+30 {
		t.Errorf("global seconds = %v, want %v", audio["seconds"], positionTrackOneSeconds+30)
	}
	if audio["track_id"] != float64(positionTrackTwo) || audio["track_seconds"] != 30.0 {
		t.Errorf("track = (%v, %vs), want (%d, 30s)", audio["track_id"], audio["track_seconds"], positionTrackTwo)
	}
	if got["derived"] != false || got["confidence"] != 0.0 {
		t.Errorf("top level derived/confidence = %v/%v, want false/0", got["derived"], got["confidence"])
	}
	if got["page"] != nil {
		t.Errorf("page = %v with no page map, want null", got["page"])
	}
	// The reader's own position was left alone rather than reset.
	if got["char_offset"] != 0.0 {
		t.Errorf("char_offset = %v, want the untouched 0", got["char_offset"])
	}

	// Writing a real text position supersedes the raw fallback.
	app.api(t, http.MethodPut, "/api/books/"+positionEntry+"/position", map[string]any{"char_offset": 10})
	_, got = app.api(t, http.MethodGet, "/api/books/"+positionEntry+"/position", nil)
	audio = got["audio"].(map[string]any)
	if audio["seconds"] != 0.0 {
		t.Errorf("audio seconds = %v after a text write, want the cleared 0", audio["seconds"])
	}
}

func TestBookPositionDerivesFromAlignment(t *testing.T) {
	// The fixture book is small, so the maps are scaled to it: 4 characters
	// per second of audio, and 4 characters per printed page.
	app := newPositionTestApp(t, fixedAnchors{
		audio: []position.Anchor{
			{CharOffset: 0, Value: 0, Confidence: 0.9},
			{CharOffset: 40, Value: 10, Confidence: 0.8},
		},
		pages: []position.Anchor{
			{CharOffset: 0, Value: 1, Confidence: 0.6},
			{CharOffset: 40, Value: 11, Confidence: 0.6},
		},
	})
	app.charCount(t)

	if _, body := app.api(t, http.MethodPut, "/api/books/"+positionEntry+"/position",
		map[string]any{"char_offset": 20}); body["status"] != "playing" {
		t.Fatalf("put: %v", body)
	}

	_, got := app.api(t, http.MethodGet, "/api/books/"+positionEntry+"/position", nil)
	audio := got["audio"].(map[string]any)
	if audio["derived"] != true || audio["seconds"] != 5.0 {
		t.Errorf("audio = %v, want 5s derived", audio)
	}
	if audio["confidence"] != 0.8 {
		t.Errorf("audio confidence = %v, want the weaker anchor's 0.8", audio["confidence"])
	}
	page := got["page"].(map[string]any)
	if page["page"] != 6.0 {
		t.Errorf("page = %v, want 6", page["page"])
	}
	// Both views derived, so the whole position is; confidence is the
	// weakest link across them.
	if got["derived"] != true || got["confidence"] != 0.6 {
		t.Errorf("top level = %v/%v, want true/0.6", got["derived"], got["confidence"])
	}

	// The car: a timestamp comes back as a character offset, with nothing
	// stored raw.
	status, body := app.api(t, http.MethodPut, "/api/books/"+positionEntry+"/position",
		map[string]any{"audio_seconds": 5.0, "audio_file_id": positionTrackOne})
	if status != http.StatusOK {
		t.Fatalf("put audio status = %d: %v", status, body)
	}
	pos := body["position"].(map[string]any)
	if pos["char_offset"] != 20.0 || pos["source"] != "listen" {
		t.Errorf("listening position = %v chars from %v, want 20 from listen",
			pos["char_offset"], pos["source"])
	}
	if pos["audio"].(map[string]any)["derived"] != true {
		t.Error("an aligned listening position was stored as a raw fallback")
	}

	// A page is translatable too, once a page map exists.
	status, body = app.api(t, http.MethodPut, "/api/books/"+positionEntry+"/position",
		map[string]any{"page": 6})
	if status != http.StatusOK {
		t.Fatalf("put page status = %d: %v", status, body)
	}
	if pos := body["position"].(map[string]any); pos["char_offset"] != 20.0 {
		t.Errorf("page 6 landed at %v, want char 20", pos["char_offset"])
	}
}

func TestBookPositionOffersFinishNearTheEnd(t *testing.T) {
	app := newPositionTestApp(t, nil)
	count := app.charCount(t)

	_, body := app.api(t, http.MethodPut, "/api/books/"+positionEntry+"/position",
		map[string]any{"char_offset": count / 2})
	if body["offer_finished"] != false {
		t.Error("halfway through offered to finish the book")
	}

	_, body = app.api(t, http.MethodPut, "/api/books/"+positionEntry+"/position",
		map[string]any{"char_offset": count})
	if body["offer_finished"] != true {
		t.Errorf("reaching the end did not offer to finish: %v", body)
	}
	// Offered, never taken: the entry is still being read.
	if body["status"] != "playing" {
		t.Errorf("status = %v, want the book left playing until the user says so", body["status"])
	}
}

func TestBookPositionRejectsBadWrites(t *testing.T) {
	app := newPositionTestApp(t, nil)
	count := app.charCount(t)

	for _, tc := range []struct {
		name string
		body map[string]any
		want int
	}{
		{"no shape at all", map[string]any{}, http.StatusBadRequest},
		{"two shapes at once", map[string]any{"char_offset": 1, "page": 2}, http.StatusBadRequest},
		{"offset past the text", map[string]any{"char_offset": count + 1}, http.StatusBadRequest},
		{"negative offset", map[string]any{"char_offset": -1}, http.StatusBadRequest},
		{"unknown source", map[string]any{"char_offset": 1, "source": "osmosis"}, http.StatusBadRequest},
		{"audio without a file", map[string]any{"audio_seconds": 5.0}, http.StatusBadRequest},
		{"audio past the track", map[string]any{"audio_seconds": 9999.0, "audio_file_id": positionTrackOne}, http.StatusBadRequest},
		{"someone else's track", map[string]any{"audio_seconds": 5.0, "audio_file_id": positionLoneTrack}, http.StatusNotFound},
		{"page with no page map", map[string]any{"page": 42}, http.StatusUnprocessableEntity},
	} {
		if status, body := app.api(t, http.MethodPut, "/api/books/"+positionEntry+"/position", tc.body); status != tc.want {
			t.Errorf("%s: status = %d (want %d): %v", tc.name, status, tc.want, body)
		}
	}
}

func TestBookPositionIsScopedToItsOwner(t *testing.T) {
	app := newPositionTestApp(t, nil)

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookie jar: %v", err)
	}
	stranger := &http.Client{Jar: jar, Timeout: 30 * time.Second}
	register(t, app.ts.URL, stranger, "nosy@example.com", "nosy", "hogwash123")

	for _, tc := range []struct {
		method string
		path   string
		body   any
	}{
		{http.MethodGet, "/api/books/" + positionEntry + "/position", nil},
		{http.MethodPut, "/api/books/" + positionEntry + "/position", map[string]any{"char_offset": 1}},
		{http.MethodGet, "/api/books/" + positionEntry + "/sessions", nil},
	} {
		if status, body := app.do(t, stranger, tc.method, tc.path, tc.body); status != http.StatusNotFound {
			t.Errorf("%s %s as a stranger = %d, want 404: %v", tc.method, tc.path, status, body)
		}
	}
}

func TestBookPositionOnAnUnopenedBook(t *testing.T) {
	app := newPositionTestApp(t, nil)

	// A book that has never been opened reads as page one, not as a 404.
	status, got := app.api(t, http.MethodGet, "/api/books/"+positionEntry+"/position", nil)
	if status != http.StatusOK {
		t.Fatalf("status = %d: %v", status, got)
	}
	if got["char_offset"] != 0.0 || got["percent"] != 0.0 {
		t.Errorf("unopened book = %v chars / %v percent, want 0/0", got["char_offset"], got["percent"])
	}
	if got["updated_at"] != nil {
		t.Errorf("updated_at = %v on a book never opened, want null", got["updated_at"])
	}
	// The text has not been parsed, so there is no denominator and no
	// chapter — and the endpoint does not go and parse one.
	if got["char_count"] != 0.0 || got["chapter"] != nil {
		t.Errorf("unparsed text reported char_count %v / chapter %v", got["char_count"], got["chapter"])
	}
	// The audiobook is attached, so the player still has somewhere to start.
	if audio, ok := got["audio"].(map[string]any); !ok || audio["seconds"] != 0.0 {
		t.Errorf("audio = %#v, want the start of the timeline", got["audio"])
	}
}

func TestBookPositionAudioOnlyBookUsesDurationForPercent(t *testing.T) {
	app := newPositionTestApp(t, nil)

	// No EPUB on this one: percent has to come from the tape.
	status, body := app.api(t, http.MethodPut, "/api/books/"+positionAudioOnly+"/position",
		map[string]any{"audio_seconds": 30.0, "audio_file_id": positionLoneTrack})
	if status != http.StatusOK {
		t.Fatalf("put status = %d: %v", status, body)
	}
	pos := body["position"].(map[string]any)
	if p := pos["percent"].(float64); p < 49 || p > 51 {
		t.Errorf("percent = %v, want ~50 of a 60 second book", p)
	}
	if pos["char_count"] != 0.0 {
		t.Errorf("char_count = %v with no ebook attached, want 0", pos["char_count"])
	}
}

func TestReadingSessions(t *testing.T) {
	app := newPositionTestApp(t, nil)

	start := time.Date(2026, 9, 1, 20, 0, 0, 0, time.UTC)
	status, body := app.api(t, http.MethodPost, "/api/books/"+positionEntry+"/sessions", map[string]any{
		"started_at":     start,
		"ended_at":       start.Add(45 * time.Minute),
		"mode":           "read",
		"chars_advanced": 18000,
	})
	if status != http.StatusCreated {
		t.Fatalf("post session status = %d: %v", status, body)
	}
	session := body["session"].(map[string]any)
	// seconds was omitted, so the wall-clock span stands in for it.
	if session["seconds"] != 2700.0 {
		t.Errorf("seconds = %v, want the 2700 between the endpoints", session["seconds"])
	}

	// Logging time starts the book, exactly like a play session does.
	_, entry := app.api(t, http.MethodGet, "/api/library/"+positionEntry, nil)
	if entry["status"] != "playing" {
		t.Errorf("entry status after a session = %v, want playing", entry["status"])
	}

	if status, body := app.api(t, http.MethodPost, "/api/books/"+positionEntry+"/sessions", map[string]any{
		"started_at": start.Add(time.Hour),
		"ended_at":   start.Add(90 * time.Minute),
		"mode":       "listen",
		"seconds":    1200,
	}); status != http.StatusCreated {
		t.Fatalf("post listen session status = %d: %v", status, body)
	}

	status, body = app.api(t, http.MethodGet, "/api/books/"+positionEntry+"/sessions", nil)
	if status != http.StatusOK {
		t.Fatalf("get sessions status = %d: %v", status, body)
	}
	sessions := body["sessions"].([]any)
	if len(sessions) != 2 {
		t.Fatalf("sessions = %d, want 2", len(sessions))
	}
	// Newest first.
	if sessions[0].(map[string]any)["mode"] != "listen" {
		t.Errorf("first session mode = %v, want the newer listen", sessions[0])
	}
	totals := body["seconds_by_mode"].(map[string]any)
	if totals["read"] != 2700.0 || totals["listen"] != 1200.0 {
		t.Errorf("totals = %v, want read 2700 / listen 1200", totals)
	}

	for _, tc := range []struct {
		name string
		body map[string]any
	}{
		{"unknown mode", map[string]any{"started_at": start, "ended_at": start.Add(time.Minute), "mode": "skim"}},
		{"inverted endpoints", map[string]any{"started_at": start, "ended_at": start.Add(-time.Minute), "mode": "read"}},
		{"missing endpoints", map[string]any{"mode": "read"}},
		{"over a day", map[string]any{"started_at": start, "ended_at": start.Add(48 * time.Hour), "mode": "read"}},
	} {
		if status, body := app.api(t, http.MethodPost, "/api/books/"+positionEntry+"/sessions", tc.body); status != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400: %v", tc.name, status, body)
		}
	}
}
