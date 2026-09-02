package http

import (
	"bytes"
	"encoding/json"
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
	"github.com/collinpendleton/backhog/api/internal/store"
)

// The alignment fixture book: a real EPUB and two real measured tracks,
// so the full queue lifecycle — enqueue through the session API, claim
// and stream through the token API — runs against the same inputs a
// deployment would have.
const (
	alignEntry     = "ae1"
	alignAudioOnly = "ae2"
	alignEpubFile  = 201
	alignTrackOne  = 202
	alignTrackTwo  = 203
	alignLoneTrack = 204

	alignWorkerToken  = "test-worker-token"
	alignTrackSeconds = 90.0
)

type alignTestApp struct {
	ts     *httptest.Server
	user   *http.Client // the reader, with a session
	worker *http.Client // the alignment worker, token only
	store  *store.Store
	root   string
}

// newAlignTestApp boots the router with the worker token set. Pass an
// empty token to boot the alignment-disabled world.
func newAlignTestApp(t *testing.T, token string) *alignTestApp {
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
	oneSize := write("01.m4b", apiM4BFixture("Erasmas", alignTrackSeconds, 4096))
	twoSize := write("02.m4b", apiM4BFixture("Apert", alignTrackSeconds, 4096))
	loneSize := write("lone.m4b", apiM4BFixture("Lone", 60, 4096))

	database, err := db.Open(filepath.Join(t.TempDir(), "align.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	if err := db.Migrate(database); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	st := store.New(database)

	cfg := config.Config{
		EpubTextDir:      filepath.Join(t.TempDir(), "epub_text"),
		MediaDirs:        []string{root},
		AlignWorkerToken: token,
	}
	srv := NewServer(cfg, st, nil, nil, nil, nil, &backfill.Runner{}, nil)
	ts := httptest.NewServer(srv.Routes())
	t.Cleanup(ts.Close)

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookie jar: %v", err)
	}
	user := &http.Client{Jar: jar, Timeout: 30 * time.Second}
	register(t, ts.URL, user, "aligner@example.com", "aligner", "hogwash123")

	app := &alignTestApp{
		ts:     ts,
		user:   user,
		worker: &http.Client{Timeout: 30 * time.Second},
		store:  st,
		root:   root,
	}
	userID := app.whoami(t)

	now := time.Now()
	exec := func(q string, args ...any) {
		if _, err := st.DB().Exec(q, args...); err != nil {
			t.Fatalf("seed %q: %v", q, err)
		}
	}
	exec(`INSERT INTO books (id, title) VALUES ('OLAW', 'Anathem'), ('OLAT', 'Tape Only')`)
	exec(`INSERT INTO library_entries (id, user_id, media_type, book_id, status)
	      VALUES (?, ?, 'book', 'OLAW', 'backlog'), (?, ?, 'book', 'OLAT', 'backlog')`,
		alignEntry, userID, alignAudioOnly, userID)
	insertFile := func(id int, name, kind string, size int, bookID string, track any) {
		exec(`INSERT INTO media_files (id, root, path, kind, size_bytes, mtime, book_id, track_number, scanned_at)
		      VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			id, root, name, kind, size, now.UnixNano(), bookID, track, now.UTC())
	}
	insertFile(alignEpubFile, "book.epub", "epub", epubSize, "OLAW", nil)
	insertFile(alignTrackOne, "01.m4b", "audio", oneSize, "OLAW", 1)
	insertFile(alignTrackTwo, "02.m4b", "audio", twoSize, "OLAW", 2)
	insertFile(alignLoneTrack, "lone.m4b", "audio", loneSize, "OLAT", 1)
	return app
}

func (a *alignTestApp) whoami(t *testing.T) string {
	t.Helper()
	resp, err := a.user.Get(a.ts.URL + "/api/auth/me")
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

// call posts body as JSON with an optional bearer token and returns the
// status plus the decoded JSON, if any.
func (a *alignTestApp) call(t *testing.T, client *http.Client, method, path, token string, body any) (int, map[string]any) {
	t.Helper()
	var payload []byte
	if body != nil {
		var err error
		if payload, err = json.Marshal(body); err != nil {
			t.Fatalf("marshal: %v", err)
		}
	}
	req, err := http.NewRequest(method, a.ts.URL+path, bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	var out map[string]any
	if resp.StatusCode != http.StatusNoContent {
		json.NewDecoder(resp.Body).Decode(&out)
	}
	return resp.StatusCode, out
}

// charCount learns the fixture book's canonical text length the way a
// real client does (the enqueue parses the EPUB on demand).
func (a *alignTestApp) charCount(t *testing.T) int {
	t.Helper()
	status, body := a.call(t, a.user, http.MethodGet, "/api/books/"+alignEntry+"/text/chapters", "", nil)
	if status != http.StatusOK {
		t.Fatalf("chapters status = %d: %v", status, body)
	}
	return int(body["char_count"].(float64))
}

func TestAlignWorkerFlowEndToEnd(t *testing.T) {
	app := newAlignTestApp(t, alignWorkerToken)

	// Enqueue through the user API; a second enqueue is idempotent.
	status, body := app.call(t, app.user, http.MethodPost, "/api/books/"+alignEntry+"/align", "", nil)
	if status != http.StatusCreated {
		t.Fatalf("enqueue status = %d: %v", status, body)
	}
	job := body["job"].(map[string]any)
	if job["state"] != "queued" {
		t.Fatalf("new job state = %v, want queued", job["state"])
	}
	status, again := app.call(t, app.user, http.MethodPost, "/api/books/"+alignEntry+"/align", "", nil)
	if status != http.StatusOK || again["job"].(map[string]any)["id"] != job["id"] {
		t.Fatalf("re-enqueue = (%d, %v), want the same job with 200", status, again)
	}

	// The worker claims and receives everything it needs: the job, the
	// canonical text's path, and the ordered tracks with real paths.
	status, claim := app.call(t, app.worker, http.MethodPost, "/internal/align/claim",
		alignWorkerToken, map[string]any{"worker": "w1"})
	if status != http.StatusOK {
		t.Fatalf("claim status = %d: %v", status, claim)
	}
	claimed := claim["job"].(map[string]any)
	if claimed["id"] != job["id"] || claimed["state"] != "claimed" {
		t.Fatalf("claimed job = %v, want the queued one in state claimed", claimed)
	}
	if claimed["attempts"] != 1.0 {
		t.Errorf("attempts = %v, want 1", claimed["attempts"])
	}
	textPath := claim["epub_text_path"].(string)
	if textPath == "" {
		t.Fatal("claim omitted the epub text path")
	}
	if _, err := os.Stat(textPath); err != nil {
		t.Errorf("epub text path %s: %v", textPath, err)
	}
	tracks := claim["tracks"].([]any)
	if len(tracks) != 2 {
		t.Fatalf("claim returned %d tracks, want 2", len(tracks))
	}
	for i, id := range []float64{alignTrackOne, alignTrackTwo} {
		track := tracks[i].(map[string]any)
		if track["id"] != id {
			t.Errorf("track %d = %v, want %v in timeline order", i, track["id"], id)
		}
		if p := track["path"].(string); p == "" {
			t.Errorf("track %d has no resolved path", i)
		} else if _, err := os.Stat(p); err != nil {
			t.Errorf("track %d path %s: %v", i, p, err)
		}
		if track["missing"] != false {
			t.Errorf("track %d flagged missing", i)
		}
	}

	jobPath := "/internal/align/" + job["id"].(string)

	// Heartbeat through the pipeline, streaming batches as we go.
	status, body = app.call(t, app.worker, http.MethodPost, jobPath+"/progress",
		alignWorkerToken, map[string]any{
			"worker": "w1", "state": "transcribing", "progress": 0.4,
			"stage_detail": "whisper large-v3: chunk 2/5",
		})
	if status != http.StatusOK || body["job"].(map[string]any)["state"] != "transcribing" {
		t.Fatalf("progress = (%d, %v)", status, body)
	}
	status, body = app.call(t, app.worker, http.MethodPost, jobPath+"/segments",
		alignWorkerToken, map[string]any{"worker": "w1", "segments": []map[string]any{
			{"audio_start": 0, "audio_end": 4.2, "text": "the clock ticks"},
			{"audio_start": 4.2, "audio_end": 8.9, "text": "orth"},
		}})
	if status != http.StatusOK {
		t.Fatalf("segments = (%d, %v)", status, body)
	}
	status, body = app.call(t, app.worker, http.MethodPost, jobPath+"/anchors",
		alignWorkerToken, map[string]any{"worker": "w1", "anchors": []map[string]any{
			{"char_offset": 0, "audio_seconds": 0, "confidence": 0.9},
			{"char_offset": 40, "audio_seconds": 10, "confidence": 0.8},
		}})
	if status != http.StatusOK {
		t.Fatalf("anchors = (%d, %v)", status, body)
	}

	// While streaming, the user sees the pipeline but no usable
	// alignment yet.
	status, body = app.call(t, app.user, http.MethodGet, "/api/books/"+alignEntry+"/align", "", nil)
	if status != http.StatusOK || body["alignment"] != nil {
		t.Fatalf("mid-flight status = (%d, %v), want no usable alignment yet", status, body)
	}

	// Complete: the job and its alignment agree, and the position
	// endpoints start deriving through the streamed anchors.
	status, body = app.call(t, app.worker, http.MethodPost, jobPath+"/complete",
		alignWorkerToken, map[string]any{
			"worker": "w1", "state": "ready", "coverage": 0.94,
			"mean_confidence": 0.87, "model": "whisper large-v3 + aeneas",
		})
	if status != http.StatusOK {
		t.Fatalf("complete = (%d, %v)", status, body)
	}
	if done := body["job"].(map[string]any); done["state"] != "ready" || done["progress"] != 1.0 {
		t.Errorf("completed job = %v", done)
	}
	alignment := body["alignment"].(map[string]any)
	if alignment["state"] != "ready" || alignment["coverage"] != 0.94 || alignment["model"] != "whisper large-v3 + aeneas" {
		t.Errorf("completed alignment = %v", alignment)
	}

	// Terminal: the queue refuses further writes.
	status, _ = app.call(t, app.worker, http.MethodPost, jobPath+"/progress",
		alignWorkerToken, map[string]any{"worker": "w1"})
	if status != http.StatusConflict {
		t.Errorf("progress after complete = %d, want 409", status)
	}

	// The queue is empty again.
	status, _ = app.call(t, app.worker, http.MethodPost, "/internal/align/claim",
		alignWorkerToken, map[string]any{"worker": "w1"})
	if status != http.StatusNoContent {
		t.Errorf("claim on empty queue = %d, want 204", status)
	}
}

// The acceptance test for the translator wiring: anchors that arrive
// (here by hand, straight into the tables) make the position endpoint
// derive a real audio timestamp instead of reporting underived.
func TestAlignDerivedPositionFromHandInsertedAnchors(t *testing.T) {
	app := newAlignTestApp(t, alignWorkerToken)
	count := app.charCount(t)

	alignStatus, body := app.call(t, app.user, http.MethodGet, "/api/books/"+alignEntry+"/align", "", nil)
	if alignStatus != http.StatusOK || body["worker_enabled"] != true {
		t.Fatalf("status = (%d, %v), want worker_enabled true", alignStatus, body)
	}
	if body["job"] != nil {
		t.Fatalf("job = %v on a never-aligned book, want null", body["job"])
	}

	// Hand-insert the alignment and its anchors, the way Stage 7's
	// results look to everyone downstream: char 0 at the very start of
	// the tape, the last character at the very end.
	exec := func(q string, args ...any) {
		if _, err := app.store.DB().Exec(q, args...); err != nil {
			t.Fatalf("seed %q: %v", q, err)
		}
	}
	exec(`INSERT INTO alignments (id, entry_id, epub_text_id, state, coverage, mean_confidence, model)
	      SELECT 'hand', ?, id, 'ready', 0.9, 0.9, 'hand' FROM epub_texts LIMIT 1`, alignEntry)
	for _, a := range []struct {
		offset  int
		seconds float64
	}{
		{0, 0},
		{count, 2 * alignTrackSeconds},
	} {
		exec(`INSERT INTO alignment_anchors (alignment_id, char_offset, audio_seconds, confidence)
		      VALUES ('hand', ?, ?, 0.9)`, a.offset, a.seconds)
	}

	// A reading position is derived onto the tape through the anchors.
	status, put := app.call(t, app.user, http.MethodPut, "/api/books/"+alignEntry+"/position", "",
		map[string]any{"char_offset": count / 2})
	if status != http.StatusOK {
		t.Fatalf("put position = (%d, %v)", status, put)
	}

	status, got := app.call(t, app.user, http.MethodGet, "/api/books/"+alignEntry+"/position", "", nil)
	if status != http.StatusOK {
		t.Fatalf("get position = (%d, %v)", status, got)
	}
	audio := got["audio"].(map[string]any)
	if audio["derived"] != true {
		t.Fatalf("audio = %v, want derived through the alignment", audio)
	}
	if seconds := audio["seconds"].(float64); seconds < alignTrackSeconds-1 || seconds > alignTrackSeconds+1 {
		t.Errorf("derived seconds = %v, want ~%v (halfway through the tape)", seconds, alignTrackSeconds)
	}
	if audio["confidence"] != 0.9 {
		t.Errorf("derived confidence = %v, want the anchors'", audio["confidence"])
	}
	if got["derived"] != true {
		t.Errorf("top-level derived = %v, want true", got["derived"])
	}

	// And the reverse: a timestamp becomes a character offset.
	status, put = app.call(t, app.user, http.MethodPut, "/api/books/"+alignEntry+"/position", "",
		map[string]any{"audio_seconds": alignTrackSeconds, "audio_file_id": alignTrackOne})
	if status != http.StatusOK {
		t.Fatalf("put listening position = (%d, %v)", status, put)
	}
	pos := put["position"].(map[string]any)
	if pos["char_offset"] != float64(count/2) {
		t.Errorf("listen landed at char %v, want ~%d", pos["char_offset"], count/2)
	}
	if pos["source"] != "listen" {
		t.Errorf("source = %v, want listen", pos["source"])
	}
}

func TestAlignWorkerAuthentication(t *testing.T) {
	// No token configured: the whole internal half answers 503, and
	// everything user-facing still works.
	disabled := newAlignTestApp(t, "")
	status, body := disabled.call(t, disabled.worker, http.MethodPost, "/internal/align/claim",
		"anything", map[string]any{"worker": "w1"})
	if status != http.StatusServiceUnavailable {
		t.Fatalf("claim without a configured token = (%d, %v), want 503", status, body)
	}
	status, body = disabled.call(t, disabled.user, http.MethodPost, "/api/books/"+alignEntry+"/align", "", nil)
	if status != http.StatusCreated {
		t.Fatalf("enqueue with alignment disabled = (%d, %v), want it to queue anyway", status, body)
	}
	status, body = disabled.call(t, disabled.user, http.MethodGet, "/api/books/"+alignEntry+"/align", "", nil)
	if body["worker_enabled"] != false {
		t.Errorf("worker_enabled = %v, want false", body["worker_enabled"])
	}

	app := newAlignTestApp(t, alignWorkerToken)
	for _, tc := range []struct {
		name  string
		token string
	}{
		{"no token", ""},
		{"wrong token", "not-the-token"},
	} {
		status, body := app.call(t, app.worker, http.MethodPost, "/internal/align/claim",
			tc.token, map[string]any{"worker": "w1"})
		if status != http.StatusUnauthorized {
			t.Errorf("claim with %s = (%d, %v), want 401", tc.name, status, body)
		}
	}

	// A valid token with an empty queue is 204, and a claim without a
	// worker id is a 400, not a claim.
	status, _ = app.call(t, app.worker, http.MethodPost, "/internal/align/claim",
		alignWorkerToken, map[string]any{"worker": "w1"})
	if status != http.StatusNoContent {
		t.Errorf("empty claim = %d, want 204", status)
	}
	status, _ = app.call(t, app.worker, http.MethodPost, "/internal/align/claim",
		alignWorkerToken, map[string]any{})
	if status != http.StatusBadRequest {
		t.Errorf("claim without a worker = %d, want 400", status)
	}
}

func TestAlignJobBelongsToItsWorker(t *testing.T) {
	app := newAlignTestApp(t, alignWorkerToken)
	_, body := app.call(t, app.user, http.MethodPost, "/api/books/"+alignEntry+"/align", "", nil)
	job := body["job"].(map[string]any)

	_, claim := app.call(t, app.worker, http.MethodPost, "/internal/align/claim",
		alignWorkerToken, map[string]any{"worker": "w1"})
	if claim["job"].(map[string]any)["id"] != job["id"] {
		t.Fatalf("claim returned a different job than the queue showed")
	}

	jobPath := "/internal/align/" + job["id"].(string)
	for _, tc := range []struct {
		name string
		body map[string]any
	}{
		{"progress", map[string]any{"worker": "w2", "state": "transcribing"}},
		{"segments", map[string]any{"worker": "w2", "segments": []map[string]any{}}},
		{"anchors", map[string]any{"worker": "w2", "anchors": []map[string]any{}}},
		{"complete", map[string]any{"worker": "w2", "state": "ready", "model": "x"}},
	} {
		status, resp := app.call(t, app.worker, http.MethodPost, jobPath+"/"+tc.name,
			alignWorkerToken, tc.body)
		if status != http.StatusConflict {
			t.Errorf("%s from another worker = (%d, %v), want 409", tc.name, status, resp)
		}
	}

	// Unknown job ids answer 404.
	status, _ := app.call(t, app.worker, http.MethodPost, "/internal/align/nope/progress",
		alignWorkerToken, map[string]any{"worker": "w1"})
	if status != http.StatusNotFound {
		t.Errorf("unknown job = %d, want 404", status)
	}
}

func TestAlignEnqueueNeedsBothHalves(t *testing.T) {
	app := newAlignTestApp(t, alignWorkerToken)

	// Audio-only: no text to align.
	status, body := app.call(t, app.user, http.MethodPost, "/api/books/"+alignAudioOnly+"/align", "", nil)
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("audio-only enqueue = (%d, %v), want 422", status, body)
	}

	// An unknown or foreign entry is a plain 404.
	status, _ = app.call(t, app.user, http.MethodPost, "/api/books/who-knows/align", "", nil)
	if status != http.StatusNotFound {
		t.Errorf("unknown entry = %d, want 404", status)
	}

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookie jar: %v", err)
	}
	stranger := &http.Client{Jar: jar, Timeout: 30 * time.Second}
	register(t, app.ts.URL, stranger, "nosy@example.com", "nosy", "hogwash123")
	status, _ = app.call(t, stranger, http.MethodPost, "/api/books/"+alignEntry+"/align", "", nil)
	if status != http.StatusNotFound {
		t.Errorf("stranger enqueue = %d, want 404", status)
	}
}

func TestAlignDeleteClearsEverything(t *testing.T) {
	app := newAlignTestApp(t, alignWorkerToken)
	count := app.charCount(t)

	// Run one alignment to completion so anchors exist.
	_, body := app.call(t, app.user, http.MethodPost, "/api/books/"+alignEntry+"/align", "", nil)
	jobID := body["job"].(map[string]any)["id"].(string)
	app.call(t, app.worker, http.MethodPost, "/internal/align/claim",
		alignWorkerToken, map[string]any{"worker": "w1"})
	app.call(t, app.worker, http.MethodPost, "/internal/align/"+jobID+"/anchors",
		alignWorkerToken, map[string]any{"worker": "w1", "anchors": []map[string]any{
			{"char_offset": 0, "audio_seconds": 0, "confidence": 0.9},
			{"char_offset": count, "audio_seconds": 180, "confidence": 0.9},
		}})
	app.call(t, app.worker, http.MethodPost, "/internal/align/"+jobID+"/complete",
		alignWorkerToken, map[string]any{
			"worker": "w1", "state": "ready", "coverage": 0.9,
			"mean_confidence": 0.9, "model": "test",
		})

	status, got := app.call(t, app.user, http.MethodPut, "/api/books/"+alignEntry+"/position", "",
		map[string]any{"char_offset": count / 4})
	if pos := got["position"].(map[string]any); pos["audio"] == nil ||
		pos["audio"].(map[string]any)["derived"] != true {
		t.Fatalf("aligned position = %v, want a derived audio view first", pos)
	}

	// DELETE cancels/clears: no job, no alignment, no derived views —
	// and the stored character offset is still exactly what it was.
	status, _ = app.call(t, app.user, http.MethodDelete, "/api/books/"+alignEntry+"/align", "", nil)
	if status != http.StatusNoContent {
		t.Fatalf("delete = %d, want 204", status)
	}
	_, got = app.call(t, app.user, http.MethodGet, "/api/books/"+alignEntry+"/align", "", nil)
	if got["job"] != nil || got["alignment"] != nil {
		t.Errorf("after delete = (%v, %v), want nulls", got["job"], got["alignment"])
	}
	_, got = app.call(t, app.user, http.MethodGet, "/api/books/"+alignEntry+"/position", "", nil)
	if audio, ok := got["audio"].(map[string]any); !ok || audio["derived"] != false {
		t.Errorf("audio after clear = %v, want underived again", got["audio"])
	}
	if got["char_offset"] != float64(count/4) {
		t.Errorf("char_offset after clear = %v, want the stored truth untouched", got["char_offset"])
	}

	// The entry can be aligned again from scratch.
	status, body = app.call(t, app.user, http.MethodPost, "/api/books/"+alignEntry+"/align", "", nil)
	if status != http.StatusCreated {
		t.Errorf("re-enqueue after clear = (%d, %v), want a fresh 201", status, body)
	}
}
