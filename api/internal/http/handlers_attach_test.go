package http

import (
	"bytes"
	"encoding/json"
	"fmt"
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
	"github.com/collinpendleton/backhog/api/internal/store"
)

// attachTestApp is a booted router whose media runner points at a real
// fixture NAS tree, with the EPUB text dir configured so attaching an EPUB
// actually parses it.
type attachTestApp struct {
	ts     *httptest.Server
	client *http.Client
	store  *store.Store
	root   string
}

func newAttachTestApp(t *testing.T) *attachTestApp {
	t.Helper()

	root := t.TempDir()
	write := func(rel, body string) {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", rel, err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}
	// A directory-per-book audiobook, a single-file audiobook, a real EPUB,
	// and an Audible file the scanner must skip and explain.
	write("Neal Stephenson/Anathem/01 - Erasmas.m4b", "fake audio 1")
	write("Neal Stephenson/Anathem/02 - Apert.m4b", "fake audio 2")
	write("Andy Weir/Project Hail Mary.m4b", "fake single audio")
	write("books/Dune.epub", string(apiEpubFixture(t)))
	write("Audible/locked.aax", "audible DRM bytes")

	database, err := db.Open(filepath.Join(t.TempDir(), "attach.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	if err := db.Migrate(database); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	st := store.New(database)

	cfg := config.Config{EpubTextDir: filepath.Join(t.TempDir(), "epub_text")}
	srv := NewServer(cfg, st, nil, nil, nil, nil, &backfill.Runner{}, media.NewRunner(st, []string{root}))
	ts := httptest.NewServer(srv.Routes())
	t.Cleanup(ts.Close)

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookie jar: %v", err)
	}
	client := &http.Client{Jar: jar, Timeout: 10 * time.Second}
	register(t, ts.URL, client, "attach@example.com", "attacher", "hogwash123")

	// The metadata cache holds the works the fixtures imitate, so matching
	// and adding to the library both work offline.
	for _, b := range []metadata.Book{
		{ID: "OL1W", Title: "Anathem", Authors: []string{"Neal Stephenson"}},
		{ID: "OL2W", Title: "Project Hail Mary", Authors: []string{"Andy Weir"}},
		{ID: "OL3W", Title: "Dune", Authors: []string{"Frank Herbert"}},
	} {
		if err := st.UpsertBook(t.Context(), b, ""); err != nil {
			t.Fatalf("seed book %s: %v", b.ID, err)
		}
	}

	return &attachTestApp{ts: ts, client: client, store: st, root: root}
}

func (a *attachTestApp) req(t *testing.T, method, path string, body any) (int, map[string]any) {
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
	resp, err := a.client.Do(req)
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

func (a *attachTestApp) scanAndWait(t *testing.T) {
	t.Helper()
	_, kicked := a.req(t, http.MethodPost, "/api/media/scan", nil)
	if kicked["started"] != true {
		t.Fatal("scan did not start")
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		_, status := a.req(t, http.MethodGet, "/api/media/scan", nil)
		if status["running"] == false && status["last"] != nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("scan never finished: %v", status)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// candidateBy finds a candidate by its directory path.
func candidateBy(t *testing.T, cs []any, dirPath string) map[string]any {
	t.Helper()
	for _, c := range cs {
		cm := c.(map[string]any)
		if cm["dir_path"] == dirPath {
			return cm
		}
	}
	t.Fatalf("no candidate for dir %q", dirPath)
	return nil
}

func fileIDByPath(t *testing.T, files []any, path string) float64 {
	t.Helper()
	for _, f := range files {
		fm := f.(map[string]any)
		if fm["path"] == path {
			return fm["id"].(float64)
		}
	}
	t.Fatalf("no file row for path %q", path)
	return 0
}

func addBookEntry(t *testing.T, a *attachTestApp, bookID string) string {
	t.Helper()
	status, body := a.req(t, http.MethodPost, "/api/library", map[string]any{"book_id": bookID})
	if status != http.StatusCreated {
		t.Fatalf("add %s: status %d: %v", bookID, status, body)
	}
	return body["id"].(string)
}

// TestAttachFlow walks the whole review queue end to end: scan, match,
// attach in track order, EPUB parse on attach, detach leaving the file on
// disk untouched, and the skipped-file explanation.
func TestAttachFlow(t *testing.T) {
	app := newAttachTestApp(t)
	app.scanAndWait(t)

	// Anonymous candidates are rejected.
	// (covered for /media/files already; same auth group)

	entry := addBookEntry(t, app, "OL1W")

	status, body := app.req(t, http.MethodGet, "/api/media/candidates", nil)
	if status != http.StatusOK {
		t.Fatalf("candidates: status %d: %v", status, body)
	}
	candidates := body["candidates"].([]any)
	if len(candidates) != 3 {
		t.Fatalf("got %d candidates, want 3 (two audio groups, one epub): %v", len(candidates), body)
	}

	// The Anathem directory: grouped, ordered, confidently matched to the
	// library copy the user owns.
	anathem := candidateBy(t, candidates, "Neal Stephenson/Anathem")
	if anathem["kind"] != "audio" || anathem["title_guess"] != "Anathem" {
		t.Fatalf("anathem candidate = %v", anathem)
	}
	if anathem["high_confidence"] != true {
		t.Errorf("anathem not high confidence: %v", anathem["suggestions"])
	}
	sugg := anathem["suggestions"].([]any)[0].(map[string]any)
	if sugg["source"] != "library" || sugg["in_library"] != true {
		t.Errorf("anathem top suggestion = %v", sugg)
	}
	files := anathem["files"].([]any)
	if len(files) != 2 || fileOrder(files, 0) != "Neal Stephenson/Anathem/01 - Erasmas.m4b" {
		t.Errorf("anathem file order = %v", files)
	}

	// The EPUB: matched from its filename, not owned yet.
	epub := candidateBy(t, candidates, "books")
	if epub["kind"] != "epub" || epub["title_guess"] != "Dune" {
		t.Fatalf("epub candidate = %v", epub)
	}

	// The skipped file is explained, not silently missing.
	skipped := body["skipped"].([]any)
	if len(skipped) != 1 {
		t.Fatalf("skipped = %v, want one row", skipped)
	}
	skip := skipped[0].(map[string]any)
	if skip["path"] != "Audible/locked.aax" || skip["reason"] != "unsupported_extension" {
		t.Errorf("skip row = %v", skip)
	}

	// Attach the audio group in the candidate's order: the array is the
	// explicit track order.
	ids := []float64{fileID(files, 1), fileID(files, 0)} // deliberately reversed
	status, body = app.req(t, http.MethodPost, "/api/books/"+entry+"/files",
		map[string]any{"file_ids": ids, "kind": "audio"})
	if status != http.StatusCreated {
		t.Fatalf("attach audio: status %d: %v", status, body)
	}
	if body["attached"] != float64(2) {
		t.Errorf("attached = %v", body["attached"])
	}

	status, body = app.req(t, http.MethodGet, "/api/books/"+entry+"/files", nil)
	if status != http.StatusOK {
		t.Fatalf("entry files: status %d: %v", status, body)
	}
	entryFiles := body["files"].([]any)
	if len(entryFiles) != 2 || entryFiles[0].(map[string]any)["path"] != "Neal Stephenson/Anathem/02 - Apert.m4b" {
		t.Errorf("attached order = %v (array order must be track order)", entryFiles)
	}

	// Attaching an EPUB triggers the canonical-text parse.
	duneEntry := addBookEntry(t, app, "OL3W")
	status, body = app.req(t, http.MethodGet, "/api/media/files?kind=epub&unattached=true", nil)
	if status != http.StatusOK {
		t.Fatalf("files: status %d: %v", status, body)
	}
	epubID := fileIDByPath(t, body["files"].([]any), "books/Dune.epub")
	status, body = app.req(t, http.MethodPost, "/api/books/"+duneEntry+"/files",
		map[string]any{"file_ids": []float64{epubID}, "kind": "epub"})
	if status != http.StatusCreated {
		t.Fatalf("attach epub: status %d: %v", status, body)
	}
	var parsed int
	if err := app.store.DB().QueryRow(
		`SELECT COUNT(*) FROM epub_texts et JOIN media_files mf ON mf.id = et.media_file_id
		 WHERE mf.path = 'books/Dune.epub'`).Scan(&parsed); err != nil {
		t.Fatalf("probe epub_texts: %v", err)
	}
	if parsed != 1 {
		t.Errorf("epub parse rows = %d, want 1 (attach must trigger the parse)", parsed)
	}

	// Attached files leave the review queue.
	status, body = app.req(t, http.MethodGet, "/api/media/candidates", nil)
	if status != http.StatusOK {
		t.Fatalf("candidates again: %d", status)
	}
	if got := len(body["candidates"].([]any)); got != 1 {
		t.Errorf("%d candidates after attaching, want 1 (Project Hail Mary)", got)
	}

	// Detach: the row survives, the file on disk is untouched.
	onDisk := filepath.Join(app.root, "Neal Stephenson/Anathem/02 - Apert.m4b")
	before, err := os.ReadFile(onDisk)
	if err != nil {
		t.Fatalf("read before detach: %v", err)
	}
	status, body = app.req(t, http.MethodDelete,
		fmt.Sprintf("/api/books/%s/files/%d", entry, int(ids[0])), nil)
	if status != http.StatusOK {
		t.Fatalf("detach: status %d: %v", status, body)
	}
	after, err := os.ReadFile(onDisk)
	if err != nil || !bytes.Equal(before, after) {
		t.Errorf("detach touched the file on disk: %v", err)
	}
	var bookIDSQL, trackSQL *any
	if err := app.store.DB().QueryRow(
		`SELECT book_id, track_number FROM media_files WHERE id = ?`, int(ids[0])).
		Scan(&bookIDSQL, &trackSQL); err != nil {
		t.Fatalf("probe detached row: %v", err)
	}
	if bookIDSQL != nil || trackSQL != nil {
		t.Errorf("detached row = (%v, %v); want (nil, nil)", bookIDSQL, trackSQL)
	}

	// The detached file returns to the queue.
	status, body = app.req(t, http.MethodGet, "/api/media/candidates", nil)
	if status != http.StatusOK {
		t.Fatalf("candidates third: %d", status)
	}
	if got := len(body["candidates"].([]any)); got != 2 {
		t.Errorf("%d candidates after detach, want 2", got)
	}
}

func fileID(files []any, i int) float64 {
	return files[i].(map[string]any)["id"].(float64)
}

func fileOrder(files []any, i int) string {
	return files[i].(map[string]any)["path"].(string)
}

// TestAttachErrors pins the failure modes: unknown entry, foreign file,
// kind mismatch, wrong-owner entry, and the ignore round trip.
func TestAttachErrors(t *testing.T) {
	app := newAttachTestApp(t)
	app.scanAndWait(t)
	entry := addBookEntry(t, app, "OL1W")
	other := addBookEntry(t, app, "OL2W")

	status, body := app.req(t, http.MethodGet, "/api/media/files?kind=audio&unattached=true", nil)
	audioFiles := body["files"].([]any)
	anathem0 := fileIDByPath(t, audioFiles, "Neal Stephenson/Anathem/01 - Erasmas.m4b")
	anathem1 := fileIDByPath(t, audioFiles, "Neal Stephenson/Anathem/02 - Apert.m4b")

	if status, body = app.req(t, http.MethodPost, "/api/books/nope/files",
		map[string]any{"file_ids": []float64{anathem0}, "kind": "audio"}); status != http.StatusNotFound {
		t.Errorf("unknown entry status = %d: %v", status, body)
	}
	if status, body = app.req(t, http.MethodPost, "/api/books/"+entry+"/files",
		map[string]any{"file_ids": []float64{anathem0}, "kind": "epub"}); status != http.StatusBadRequest {
		t.Errorf("kind mismatch status = %d: %v", status, body)
	}
	if status, body = app.req(t, http.MethodPost, "/api/books/"+entry+"/files",
		map[string]any{"file_ids": []float64{anathem0, anathem0}, "kind": "audio"}); status != http.StatusBadRequest {
		t.Errorf("duplicate id status = %d: %v", status, body)
	}
	if status, body = app.req(t, http.MethodPost, "/api/books/"+entry+"/files",
		map[string]any{"file_ids": []float64{9999}, "kind": "audio"}); status != http.StatusNotFound {
		t.Errorf("unknown file status = %d: %v", status, body)
	}

	// Attach to one book, then attempt the same file on another book.
	if status, body = app.req(t, http.MethodPost, "/api/books/"+entry+"/files",
		map[string]any{"file_ids": []float64{anathem0, anathem1}, "kind": "audio"}); status != http.StatusCreated {
		t.Fatalf("attach: status %d: %v", status, body)
	}
	if status, body = app.req(t, http.MethodPost, "/api/books/"+other+"/files",
		map[string]any{"file_ids": []float64{anathem0}, "kind": "audio"}); status != http.StatusConflict {
		t.Errorf("cross-book attach status = %d: %v", status, body)
	}

	// Detaching through the wrong entry is a 404, not a cross-edit.
	if status, _ = app.req(t, http.MethodDelete,
		fmt.Sprintf("/api/books/%s/files/%d", other, int(anathem0)), nil); status != http.StatusNotFound {
		t.Errorf("detach through wrong entry status = %d", status)
	}

	// Ignore hides a candidate; unignore brings it back.
	status, body = app.req(t, http.MethodPost, "/api/media/ignore",
		map[string]any{"file_ids": []float64{fileIDByPath(t, audioFiles, "Andy Weir/Project Hail Mary.m4b")}})
	if status != http.StatusOK || body["ignored"] != float64(1) {
		t.Fatalf("ignore = (%d, %v)", status, body)
	}
	status, body = app.req(t, http.MethodGet, "/api/media/candidates", nil)
	if got := len(body["candidates"].([]any)); got != 1 {
		t.Fatalf("%d candidates after ignoring Project Hail Mary, want 1 (the epub): %v", got, body)
	}
	for _, c := range body["candidates"].([]any) {
		if c.(map[string]any)["dir_path"] == "Andy Weir" {
			t.Error("ignored candidate still in the queue")
		}
	}
	status, body = app.req(t, http.MethodGet, "/api/media/files?kind=audio&unattached=true", nil)
	phm := fileIDByPath(t, body["files"].([]any), "Andy Weir/Project Hail Mary.m4b")
	if status, _ = app.req(t, http.MethodDelete, fmt.Sprintf("/api/media/ignore/%d", int(phm)), nil); status != http.StatusOK {
		t.Errorf("unignore status = %d", status)
	}
	status, body = app.req(t, http.MethodGet, "/api/media/candidates", nil)
	if got := len(body["candidates"].([]any)); got != 2 {
		t.Errorf("%d candidates after unignore, want 2", got)
	}

	// Anonymous access is refused.
	resp, err := http.Get(app.ts.URL + "/api/media/candidates")
	if err != nil {
		t.Fatalf("anonymous candidates: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("anonymous candidates status = %d, want 401", resp.StatusCode)
	}
}
