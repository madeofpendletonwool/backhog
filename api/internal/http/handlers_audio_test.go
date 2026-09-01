package http

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/collinpendleton/backhog/api/internal/backfill"
	"github.com/collinpendleton/backhog/api/internal/config"
	"github.com/collinpendleton/backhog/api/internal/db"
	"github.com/collinpendleton/backhog/api/internal/media"
	"github.com/collinpendleton/backhog/api/internal/metadata"
	"github.com/collinpendleton/backhog/api/internal/models"
	"github.com/collinpendleton/backhog/api/internal/store"
)

// audioTestApp is a booted router over a fixture NAS root holding real MP4
// audiobooks, plus a sibling directory outside the root that nothing served
// through the API may ever reach.
type audioTestApp struct {
	ts      *httptest.Server
	client  *http.Client
	store   *store.Store
	base    string
	root    string
	outside string
}

const (
	// The first track is deliberately over a megabyte so the range tests
	// exercise offsets a real seek would use, not toy ones.
	trackOnePad     = 1_200_000
	trackOneSeconds = 90.0
	trackTwoSeconds = 45.0
)

func newAudioTestApp(t *testing.T) *audioTestApp {
	t.Helper()

	base := t.TempDir()
	root := filepath.Join(base, "library")
	outside := filepath.Join(base, "outside")
	write := func(dir, rel string, body []byte) {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", rel, err)
		}
		if err := os.WriteFile(p, body, 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}
	write(root, "Neal Stephenson/Anathem/01 - Erasmas.m4b", apiM4BFixture("Erasmas", trackOneSeconds, trackOnePad))
	write(root, "Neal Stephenson/Anathem/02 - Apert.m4b", apiM4BFixture("Apert", trackTwoSeconds, 4096))
	// A file whose container headers carry no usable duration: the timeline
	// must report it rather than invent a length.
	write(root, "Andy Weir/Project Hail Mary.m4b", []byte("not actually an mp4 container"))
	// The file the path-containment checks must never serve.
	write(outside, "secret.m4b", []byte("private bytes that are not in the library"))

	database, err := db.Open(filepath.Join(t.TempDir(), "audio.db"))
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
	srv := NewServer(cfg, st, nil, nil, nil, nil, &backfill.Runner{}, media.NewRunner(st, []string{root}))
	ts := httptest.NewServer(srv.Routes())
	t.Cleanup(ts.Close)

	app := &audioTestApp{ts: ts, store: st, base: base, root: root, outside: outside}
	app.client = app.newClient(t, "listener@example.com", "listener")

	for _, b := range []metadata.Book{
		{ID: "OL1W", Title: "Anathem", Authors: []string{"Neal Stephenson"}},
		{ID: "OL2W", Title: "Project Hail Mary", Authors: []string{"Andy Weir"}},
		{ID: "OL3W", Title: "Dune", Authors: []string{"Frank Herbert"}},
		{ID: "OL4W", Title: "Escapes", Authors: []string{"Nobody"}},
		{ID: "OL5W", Title: "More Escapes", Authors: []string{"Nobody"}},
	} {
		if err := st.UpsertBook(t.Context(), b, ""); err != nil {
			t.Fatalf("seed book %s: %v", b.ID, err)
		}
	}
	app.scanAndWait(t)
	return app
}

// newClient registers a fresh user with its own cookie jar.
func (a *audioTestApp) newClient(t *testing.T, email, username string) *http.Client {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookie jar: %v", err)
	}
	client := &http.Client{Jar: jar, Timeout: 30 * time.Second}
	register(t, a.ts.URL, client, email, username, "hogwash123")
	return client
}

// api issues a JSON request as the default (owning) user.
func (a *audioTestApp) api(t *testing.T, method, path string, body any) (int, map[string]any) {
	t.Helper()
	return a.apiAs(t, a.client, method, path, body)
}

func (a *audioTestApp) apiAs(t *testing.T, client *http.Client, method, path string, body any) (int, map[string]any) {
	t.Helper()
	var reader io.Reader = bytes.NewReader(nil)
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		reader = bytes.NewReader(raw)
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
	var decoded map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		t.Fatalf("%s %s: decode: %v", method, path, err)
	}
	return resp.StatusCode, decoded
}

// stream fetches a track's bytes with optional request headers, returning the
// response and its full body.
func (a *audioTestApp) stream(t *testing.T, client *http.Client, path string, headers map[string]string) (*http.Response, []byte) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, a.ts.URL+path, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return resp, body
}

func (a *audioTestApp) scanAndWait(t *testing.T) {
	t.Helper()
	if _, kicked := a.api(t, http.MethodPost, "/api/media/scan", nil); kicked["started"] != true {
		t.Fatal("scan did not start")
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		_, status := a.api(t, http.MethodGet, "/api/media/scan", nil)
		if status["running"] == false && status["last"] != nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("scan never finished: %v", status)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// entryWithAudio adds a book to the library and attaches the given scanned
// paths as its tracks, in the order given.
func (a *audioTestApp) entryWithAudio(t *testing.T, bookID string, paths ...string) string {
	t.Helper()
	status, body := a.api(t, http.MethodPost, "/api/library", map[string]any{"book_id": bookID})
	if status != http.StatusCreated {
		t.Fatalf("add %s: status %d: %v", bookID, status, body)
	}
	entry := body["id"].(string)

	status, body = a.api(t, http.MethodGet, "/api/media/files?kind=audio", nil)
	if status != http.StatusOK {
		t.Fatalf("media files: status %d: %v", status, body)
	}
	ids := make([]float64, 0, len(paths))
	for _, p := range paths {
		ids = append(ids, fileIDByPath(t, body["files"].([]any), p))
	}

	status, body = a.api(t, http.MethodPost, "/api/books/"+entry+"/files",
		map[string]any{"file_ids": ids, "kind": "audio"})
	if status != http.StatusCreated {
		t.Fatalf("attach %v: status %d: %v", paths, status, body)
	}
	return entry
}

// insertMediaRow puts a row straight into the inventory, bypassing the
// scanner — the only way to model a database whose paths do not come from a
// walk of the configured roots.
func (a *audioTestApp) insertMediaRow(t *testing.T, relPath string) float64 {
	t.Helper()
	err := a.store.InsertMediaFiles(t.Context(), []models.MediaFile{{
		Root: a.root, Path: relPath, Kind: models.MediaFileAudio,
		SizeBytes: 41, Mtime: time.Now().UnixNano(), ScannedAt: time.Now().UTC(),
	}})
	if err != nil {
		t.Fatalf("insert media row %s: %v", relPath, err)
	}
	_, body := a.api(t, http.MethodGet, "/api/media/files?kind=audio", nil)
	return fileIDByPath(t, body["files"].([]any), relPath)
}

func timelineTracks(t *testing.T, body map[string]any) []map[string]any {
	t.Helper()
	raw, ok := body["tracks"].([]any)
	if !ok {
		t.Fatalf("no tracks in %v", body)
	}
	out := make([]map[string]any, len(raw))
	for i, tr := range raw {
		out[i] = tr.(map[string]any)
	}
	return out
}

// TestAudioTimeline checks the shape the player consumes: tracks in attach
// order, each with the global second it starts at, and a total that is the
// sum of them.
func TestAudioTimeline(t *testing.T) {
	app := newAudioTestApp(t)
	entry := app.entryWithAudio(t, "OL1W",
		"Neal Stephenson/Anathem/01 - Erasmas.m4b",
		"Neal Stephenson/Anathem/02 - Apert.m4b")

	status, body := app.api(t, http.MethodGet, "/api/books/"+entry+"/audio", nil)
	if status != http.StatusOK {
		t.Fatalf("timeline: status %d: %v", status, body)
	}
	if body["degraded"] != false {
		t.Errorf("degraded = %v, want false: %v", body["degraded"], body)
	}
	if total := body["total_duration"].(float64); math.Abs(total-135) > 0.01 {
		t.Errorf("total_duration = %v, want 135", total)
	}

	tracks := timelineTracks(t, body)
	if len(tracks) != 2 {
		t.Fatalf("got %d tracks, want 2: %v", len(tracks), tracks)
	}
	if tracks[0]["title"] != "Erasmas" || tracks[1]["title"] != "Apert" {
		t.Errorf("titles = %v, %v", tracks[0]["title"], tracks[1]["title"])
	}
	for i, want := range []struct{ number, start, duration float64 }{
		{1, 0, trackOneSeconds},
		{2, trackOneSeconds, trackTwoSeconds},
	} {
		got := tracks[i]
		if got["track_number"] != want.number {
			t.Errorf("track %d numbered %v, want %v", i, got["track_number"], want.number)
		}
		if math.Abs(got["global_start"].(float64)-want.start) > 0.01 {
			t.Errorf("track %d global_start = %v, want %v", i, got["global_start"], want.start)
		}
		if math.Abs(got["duration_seconds"].(float64)-want.duration) > 0.01 {
			t.Errorf("track %d duration = %v, want %v", i, got["duration_seconds"], want.duration)
		}
		if got["measured"] != true {
			t.Errorf("track %d not measured: %v", i, got)
		}
	}
}

// A file nothing can measure leaves the timeline degraded and says so,
// instead of quietly guessing a length that would displace every later track.
func TestAudioTimelineDegraded(t *testing.T) {
	app := newAudioTestApp(t)
	entry := app.entryWithAudio(t, "OL2W", "Andy Weir/Project Hail Mary.m4b")

	status, body := app.api(t, http.MethodGet, "/api/books/"+entry+"/audio", nil)
	if status != http.StatusOK {
		t.Fatalf("timeline: status %d: %v", status, body)
	}
	if body["degraded"] != true {
		t.Errorf("degraded = %v, want true: %v", body["degraded"], body)
	}
	if body["total_duration"] != float64(0) {
		t.Errorf("total_duration = %v, want 0", body["total_duration"])
	}
	track := timelineTracks(t, body)[0]
	if track["measured"] != false || track["duration_seconds"] != float64(0) {
		t.Errorf("unmeasurable track = %v", track)
	}
	if track["track_number"] != float64(1) {
		t.Errorf("unmeasurable track lost its slot: %v", track)
	}
}

// duration_seconds is NULL for plenty of real files: the timeline derives it
// from the container and writes it back, so the parse happens once.
func TestAudioTimelineDerivesAndPersistsDuration(t *testing.T) {
	app := newAudioTestApp(t)
	entry := app.entryWithAudio(t, "OL1W", "Neal Stephenson/Anathem/01 - Erasmas.m4b")

	files, err := app.store.ListMediaFiles(t.Context(), store.MediaFileFilter{Kind: "audio"})
	if err != nil {
		t.Fatalf("list files: %v", err)
	}
	var trackID int64
	for _, f := range files {
		if f.Path == "Neal Stephenson/Anathem/01 - Erasmas.m4b" {
			trackID = f.ID
		}
	}
	// Wipe what the scanner measured so the timeline has to do it itself.
	if err := app.store.SetMediaFileDuration(t.Context(), trackID, 0); err != nil {
		t.Fatalf("clear duration: %v", err)
	}

	status, body := app.api(t, http.MethodGet, "/api/books/"+entry+"/audio", nil)
	if status != http.StatusOK {
		t.Fatalf("timeline: status %d: %v", status, body)
	}
	if body["degraded"] != false {
		t.Errorf("degraded = %v; the container carries a duration: %v", body["degraded"], body)
	}
	if d := timelineTracks(t, body)[0]["duration_seconds"].(float64); math.Abs(d-trackOneSeconds) > 0.01 {
		t.Errorf("derived duration = %v, want %v", d, trackOneSeconds)
	}

	// And it landed in the row, so the next request does not re-open the file.
	files, err = app.store.ListMediaFiles(t.Context(), store.MediaFileFilter{Kind: "audio"})
	if err != nil {
		t.Fatalf("list files: %v", err)
	}
	for _, f := range files {
		if f.ID != trackID {
			continue
		}
		if f.DurationSeconds == nil || math.Abs(*f.DurationSeconds-trackOneSeconds) > 0.01 {
			t.Errorf("duration not persisted: %v", f.DurationSeconds)
		}
	}
}

func TestAudioTimelineWithoutAudio(t *testing.T) {
	app := newAudioTestApp(t)
	status, body := app.api(t, http.MethodPost, "/api/library", map[string]any{"book_id": "OL3W"})
	if status != http.StatusCreated {
		t.Fatalf("add book: %d %v", status, body)
	}
	entry := body["id"].(string)

	if status, body := app.api(t, http.MethodGet, "/api/books/"+entry+"/audio", nil); status != http.StatusNotFound {
		t.Errorf("timeline for a book with no audio: status %d: %v", status, body)
	}
	if status, _ := app.api(t, http.MethodGet, "/api/books/does-not-exist/audio", nil); status != http.StatusNotFound {
		t.Errorf("timeline for an unknown entry: status %d", status)
	}
}

// TestAudioTrackRanges is the streaming contract: whole file, open-ended,
// mid-file, suffix and unsatisfiable ranges, plus the validators that let a
// browser seek without re-downloading.
func TestAudioTrackRanges(t *testing.T) {
	app := newAudioTestApp(t)
	entry := app.entryWithAudio(t, "OL1W",
		"Neal Stephenson/Anathem/01 - Erasmas.m4b",
		"Neal Stephenson/Anathem/02 - Apert.m4b")

	_, body := app.api(t, http.MethodGet, "/api/books/"+entry+"/audio", nil)
	trackID := int64(timelineTracks(t, body)[0]["id"].(float64))
	url := fmt.Sprintf("/api/books/%s/audio/%d", entry, trackID)

	onDisk, err := os.ReadFile(filepath.Join(app.root, "Neal Stephenson", "Anathem", "01 - Erasmas.m4b"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	size := int64(len(onDisk))

	// The unranged request: the whole file, with the headers that make the
	// ranged requests below possible.
	resp, full := app.stream(t, app.client, url, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("full GET: status %d", resp.StatusCode)
	}
	if !bytes.Equal(full, onDisk) {
		t.Errorf("full GET returned %d bytes, want the %d on disk", len(full), size)
	}
	if got := resp.Header.Get("Content-Type"); got != "audio/mp4" {
		t.Errorf("content-type = %q, want audio/mp4", got)
	}
	if resp.Header.Get("Accept-Ranges") != "bytes" {
		t.Errorf("accept-ranges = %q", resp.Header.Get("Accept-Ranges"))
	}
	if resp.Header.Get("Cache-Control") != audioCacheControl {
		t.Errorf("cache-control = %q", resp.Header.Get("Cache-Control"))
	}
	etag := resp.Header.Get("ETag")
	if etag == "" || etag[0] != '"' {
		t.Fatalf("etag = %q, want a quoted strong validator", etag)
	}

	t.Run("open ended", func(t *testing.T) {
		resp, body := app.stream(t, app.client, url, map[string]string{"Range": "bytes=1000000-"})
		if resp.StatusCode != http.StatusPartialContent {
			t.Fatalf("status %d, want 206", resp.StatusCode)
		}
		wantRange := fmt.Sprintf("bytes 1000000-%d/%d", size-1, size)
		if got := resp.Header.Get("Content-Range"); got != wantRange {
			t.Errorf("content-range = %q, want %q", got, wantRange)
		}
		if !bytes.Equal(body, onDisk[1000000:]) {
			t.Errorf("body is %d bytes, want the trailing %d", len(body), size-1000000)
		}
	})

	t.Run("mid file", func(t *testing.T) {
		resp, body := app.stream(t, app.client, url, map[string]string{"Range": "bytes=1000000-1001000"})
		if resp.StatusCode != http.StatusPartialContent {
			t.Fatalf("status %d, want 206", resp.StatusCode)
		}
		if len(body) != 1001 {
			t.Errorf("got %d bytes, want exactly 1001", len(body))
		}
		if !bytes.Equal(body, onDisk[1000000:1001001]) {
			t.Error("mid-file range returned the wrong bytes")
		}
		wantRange := fmt.Sprintf("bytes 1000000-1001000/%d", size)
		if got := resp.Header.Get("Content-Range"); got != wantRange {
			t.Errorf("content-range = %q, want %q", got, wantRange)
		}
	})

	t.Run("suffix", func(t *testing.T) {
		resp, body := app.stream(t, app.client, url, map[string]string{"Range": "bytes=-500"})
		if resp.StatusCode != http.StatusPartialContent {
			t.Fatalf("status %d, want 206", resp.StatusCode)
		}
		if !bytes.Equal(body, onDisk[size-500:]) {
			t.Errorf("suffix range returned %d bytes, want the last 500", len(body))
		}
	})

	t.Run("unsatisfiable", func(t *testing.T) {
		resp, _ := app.stream(t, app.client, url,
			map[string]string{"Range": fmt.Sprintf("bytes=%d-", size+10)})
		if resp.StatusCode != http.StatusRequestedRangeNotSatisfiable {
			t.Fatalf("status %d, want 416", resp.StatusCode)
		}
		wantRange := fmt.Sprintf("bytes */%d", size)
		if got := resp.Header.Get("Content-Range"); got != wantRange {
			t.Errorf("content-range = %q, want %q", got, wantRange)
		}
	})

	// A seek re-issues the range against the validator it already holds.
	t.Run("if-range honours a matching etag", func(t *testing.T) {
		resp, body := app.stream(t, app.client, url, map[string]string{
			"Range": "bytes=10-19", "If-Range": etag,
		})
		if resp.StatusCode != http.StatusPartialContent || len(body) != 10 {
			t.Errorf("status %d, %d bytes; want 206 and 10", resp.StatusCode, len(body))
		}
	})

	t.Run("if-range with a stale etag returns the whole file", func(t *testing.T) {
		resp, body := app.stream(t, app.client, url, map[string]string{
			"Range": "bytes=10-19", "If-Range": `"0-0"`,
		})
		if resp.StatusCode != http.StatusOK || len(body) != len(onDisk) {
			t.Errorf("status %d, %d bytes; want 200 and the whole file", resp.StatusCode, len(body))
		}
	})

	t.Run("conditional get", func(t *testing.T) {
		resp, body := app.stream(t, app.client, url, map[string]string{"If-None-Match": etag})
		if resp.StatusCode != http.StatusNotModified || len(body) != 0 {
			t.Errorf("status %d with %d bytes; want 304 and no body", resp.StatusCode, len(body))
		}
	})
}

// A media_files row whose path leaves the configured roots — via ".." or via
// a symlink out of the mount — is refused, even though the bytes it points at
// are readable by the process. Without this the endpoint is an arbitrary-file
// read.
func TestAudioTrackPathContainment(t *testing.T) {
	app := newAudioTestApp(t)

	// The escape target really is there and really is readable: a 404 below
	// is containment, not a missing file.
	secret := filepath.Join(app.outside, "secret.m4b")
	if _, err := os.ReadFile(secret); err != nil {
		t.Fatalf("fixture unreadable, test would pass for the wrong reason: %v", err)
	}
	if err := os.Symlink(secret, filepath.Join(app.root, "Neal Stephenson", "escape.m4b")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	for _, tc := range []struct {
		name   string
		path   string
		bookID string
	}{
		{"parent traversal", "../outside/secret.m4b", "OL4W"},
		{"symlink out of the mount", "Neal Stephenson/escape.m4b", "OL5W"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fileID := app.insertMediaRow(t, tc.path)

			status, body := app.api(t, http.MethodPost, "/api/library", map[string]any{"book_id": tc.bookID})
			if status != http.StatusCreated {
				t.Fatalf("add book: %d %v", status, body)
			}
			entry := body["id"].(string)
			status, body = app.api(t, http.MethodPost, "/api/books/"+entry+"/files",
				map[string]any{"file_ids": []float64{fileID}, "kind": "audio"})
			if status != http.StatusCreated {
				t.Fatalf("attach: status %d: %v", status, body)
			}

			url := fmt.Sprintf("/api/books/%s/audio/%d", entry, int64(fileID))
			resp, streamed := app.stream(t, app.client, url, nil)
			if resp.StatusCode != http.StatusNotFound {
				t.Fatalf("status %d, want 404; body: %q", resp.StatusCode, streamed)
			}
			if bytes.Contains(streamed, []byte("private bytes")) {
				t.Fatal("the endpoint served a file from outside the media roots")
			}
		})
	}
}

// The session cookie is the only thing that grants access. Another user
// holding the exact URL gets the same 404 an unknown entry gets.
func TestAudioCrossUserAccess(t *testing.T) {
	app := newAudioTestApp(t)
	entry := app.entryWithAudio(t, "OL1W", "Neal Stephenson/Anathem/01 - Erasmas.m4b")

	_, body := app.api(t, http.MethodGet, "/api/books/"+entry+"/audio", nil)
	trackID := int64(timelineTracks(t, body)[0]["id"].(float64))
	trackURL := fmt.Sprintf("/api/books/%s/audio/%d", entry, trackID)

	// The owner can read it, so the intruder's 404 is about them and not
	// about the file.
	if resp, _ := app.stream(t, app.client, trackURL, nil); resp.StatusCode != http.StatusOK {
		t.Fatalf("owner GET: status %d", resp.StatusCode)
	}

	intruder := app.newClient(t, "intruder@example.com", "intruder")
	if status, _ := app.apiAs(t, intruder, http.MethodGet, "/api/books/"+entry+"/audio", nil); status != http.StatusNotFound {
		t.Errorf("intruder timeline: status %d, want 404", status)
	}
	resp, streamed := app.stream(t, intruder, trackURL, nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("intruder stream: status %d, want 404", resp.StatusCode)
	}
	if len(streamed) > 1024 {
		t.Errorf("intruder received %d bytes", len(streamed))
	}

	// Range follow-ups are re-authorized too: a URL is not a bearer token.
	resp, _ = app.stream(t, intruder, trackURL, map[string]string{"Range": "bytes=0-99"})
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("intruder ranged stream: status %d, want 404", resp.StatusCode)
	}

	// And so is an anonymous request.
	resp, _ = app.stream(t, &http.Client{Timeout: 10 * time.Second}, trackURL, nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("anonymous stream: status %d, want 401", resp.StatusCode)
	}
}

// A track id that exists but hangs off a different book is a 404 through
// this entry, not someone else's audio.
func TestAudioTrackFromAnotherBook(t *testing.T) {
	app := newAudioTestApp(t)
	anathem := app.entryWithAudio(t, "OL1W", "Neal Stephenson/Anathem/01 - Erasmas.m4b")
	hail := app.entryWithAudio(t, "OL2W", "Andy Weir/Project Hail Mary.m4b")

	_, body := app.api(t, http.MethodGet, "/api/books/"+anathem+"/audio", nil)
	anathemTrack := int64(timelineTracks(t, body)[0]["id"].(float64))

	resp, _ := app.stream(t, app.client, fmt.Sprintf("/api/books/%s/audio/%d", hail, anathemTrack), nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status %d, want 404", resp.StatusCode)
	}

	if status, _ := app.api(t, http.MethodGet, "/api/books/"+hail+"/audio/nonsense", nil); status != http.StatusBadRequest {
		t.Errorf("non-numeric track id: status %d, want 400", status)
	}
}

// apiM4BFixture writes a minimal MP4 audiobook: ftyp + moov(mvhd) + an mdat
// of pad bytes, so a fixture can be large enough for real byte ranges.
func apiM4BFixture(title string, seconds float64, pad int) []byte {
	box := func(typ string, body ...[]byte) []byte {
		var payload bytes.Buffer
		for _, b := range body {
			payload.Write(b)
		}
		out := make([]byte, 4, payload.Len()+8)
		binary.BigEndian.PutUint32(out, uint32(payload.Len()+8))
		out = append(out, typ...)
		return append(out, payload.Bytes()...)
	}
	be32 := func(v uint32) []byte {
		b := make([]byte, 4)
		binary.BigEndian.PutUint32(b, v)
		return b
	}
	textAtom := func(typ, text string) []byte {
		return box(typ, box("data", append([]byte{0, 0, 0, 1, 0, 0, 0, 0}, []byte(text)...)))
	}

	mvhd := box("mvhd", []byte{0, 0, 0, 0}, be32(0), be32(0), be32(1000), be32(uint32(seconds*1000)))
	udta := box("udta", box("meta", append([]byte{0, 0, 0, 0}, box("ilst", textAtom("\xa9nam", title))...)))
	moov := box("moov", mvhd, udta)
	ftyp := box("ftyp", append(append([]byte("M4B "), be32(0)...), []byte("M4B mp42")...))
	mdat := box("mdat", bytes.Repeat([]byte{0xAB}, pad))
	return append(append(ftyp, moov...), mdat...)
}
