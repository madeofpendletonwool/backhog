package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/collinpendleton/backhog/ocr/internal/api"
	"github.com/collinpendleton/backhog/ocr/internal/config"
)

// fakeAPI stands in for the whole /internal/ocr half of the queue: claim,
// pages, progress, complete, and the page image stream. It records what
// the worker did so assertions run against behaviour, not internals.
type fakeAPI struct {
	mu sync.Mutex
	ts *httptest.Server

	pageCount int
	// ledger is served with claims; pageHashes backs the image stream.
	ledger     map[string]api.LedgerEntry
	pageHashes map[int]string
	// refusePage serves a 422 for these pages (vector art).
	refusePage map[int]bool

	claims    int
	completed map[string]string // jobID -> model ("") means failed via error
	failure   string
	pages     []api.Page // every page row the worker uploaded
	jobID     string
}

func newFakeAPI(t *testing.T, pageCount int) *fakeAPI {
	t.Helper()
	f := &fakeAPI{
		pageCount:  pageCount,
		pageHashes: map[int]string{},
		refusePage: map[int]bool{},
		completed:  map[string]string{},
	}
	for page := 1; page <= pageCount; page++ {
		f.pageHashes[page] = fmt.Sprintf("hash-%d", page)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /internal/ocr/claim", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		if f.claims > 0 {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		f.claims++
		f.jobID = "job-1"
		replyJSON(w, api.Claim{Job: api.Job{ID: "job-1", EntryID: "e1"}, PageCount: f.pageCount, Ledger: f.ledger})
	})
	mux.HandleFunc("GET /internal/ocr/{jobID}/page/{page}", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		page := 0
		fmt.Sscanf(r.PathValue("page"), "%d", &page)
		if f.refusePage[page] {
			http.Error(w, "no image", http.StatusUnprocessableEntity)
			return
		}
		hash, ok := f.pageHashes[page]
		if !ok {
			http.Error(w, "gone", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("X-Backhog-Page-Sha256", hash)
		w.Write([]byte("pngbytes"))
	})
	mux.HandleFunc("POST /internal/ocr/{jobID}/pages", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Worker   string      `json:"worker"`
			Pages    []api.Page  `json:"pages"`
			OCRVer   string      `json:"ocr_version"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if body.OCRVer == "" {
			http.Error(w, "ocr_version required", http.StatusBadRequest)
			return
		}
		f.mu.Lock()
		defer f.mu.Unlock()
		f.pages = append(f.pages, body.Pages...)
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("POST /internal/ocr/{jobID}/progress", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("POST /internal/ocr/{jobID}/complete", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Worker string `json:"worker"`
			Model  string `json:"model"`
			Error  string `json:"error"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.mu.Lock()
		defer f.mu.Unlock()
		f.completed[r.PathValue("jobID")] = body.Model
		f.failure = body.Error
		w.WriteHeader(http.StatusOK)
	})
	f.ts = httptest.NewServer(mux)
	t.Cleanup(f.ts.Close)
	return f
}

// stubTesseract writes a shell script that records invocations and emits
// lettering, mirroring the tesseract package's own test stub.
func stubTesseract(t *testing.T, lettering string) (string, func() int) {
	t.Helper()
	dir := t.TempDir()
	log := filepath.Join(dir, "calls")
	script := `#!/bin/sh
[ "$1" = "--version" ] && { echo "tesseract 5.3.0"; exit 0; }
echo x >> "` + log + `"
cat <<'EOF'
level	page_num	block_num	par_num	line_num	word_num	left	top	width	height	conf	text
5	1	1	1	1	1	0	0	1	1	90.0	` + lettering + `
EOF`
	path := filepath.Join(dir, "tesseract")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	calls := func() int {
		data, _ := os.ReadFile(log)
		return len(strings.Split(strings.TrimSpace(string(data)), "\n"))
	}
	return path, calls
}

func testWorker(t *testing.T, bin string) (*Worker, config.Config) {
	t.Helper()
	cfg := config.Config{
		APIURL:            "",
		Token:             "secret",
		WorkerID:          "w-test",
		TesseractBin:      bin,
		Language:          "eng",
		PSM:               11,
		PageBatch:         8,
		PageTimeout:       time.Minute,
		WorkDir:           t.TempDir(),
		PollInterval:      time.Millisecond,
		HeartbeatInterval: time.Hour, // the ticker stays quiet; the batch writes beat
		StatusAddr:        "127.0.0.1:0",
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	w := New(cfg, "tesseract 5.3.0 (eng)", log)
	return w, cfg
}

// RunOnce drives one claim to completion on the fake API.
func RunOnce(ctx context.Context, t *testing.T, w *Worker, f *fakeAPI) {
	t.Helper()
	w.cfg.APIURL = f.ts.URL
	w.api = api.New(f.ts.URL, "secret", "w-test")
	go func() { _ = w.Run(ctx) }()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		f.mu.Lock()
		done := len(f.completed) > 0
		f.mu.Unlock()
		if done {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("worker never completed the job")
}

func TestWorkerReadsEveryPage(t *testing.T) {
	f := newFakeAPI(t, 3)
	bin, calls := stubTesseract(t, "HELLO")
	w, _ := testWorker(t, bin)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	RunOnce(ctx, t, w, f)

	if got := calls(); got != 3 {
		t.Fatalf("tesseract ran %d times, want 3 (one per page)", got)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.pages) != 3 {
		t.Fatalf("uploaded pages = %d, want 3", len(f.pages))
	}
	for i, p := range f.pages {
		if p.PageNumber != i+1 || p.ImageSHA256 != fmt.Sprintf("hash-%d", i+1) {
			t.Errorf("page = %#v", p)
		}
		if !strings.Contains(p.Text, "HELLO") || p.MeanConfidence != 0.9 {
			t.Errorf("reading = %#v", p)
		}
	}
	if f.completed["job-1"] != "tesseract 5.3.0 (eng)" {
		t.Errorf("completed model = %q", f.completed["job-1"])
	}
	if f.failure != "" {
		t.Errorf("failure = %q, want none", f.failure)
	}
}

func TestWorkerLedgerSkipsUnchangedPages(t *testing.T) {
	f := newFakeAPI(t, 3)
	// The ledger already holds pages 1 and 3, read by this pipeline
	// version from these exact images.
	f.ledger = map[string]api.LedgerEntry{
		"1": {ImageSHA256: "hash-1", OCRVersion: ocrVersion},
		"3": {ImageSHA256: "hash-3", OCRVersion: ocrVersion},
	}
	bin, calls := stubTesseract(t, "HELLO")
	w, _ := testWorker(t, bin)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	RunOnce(ctx, t, w, f)

	if got := calls(); got != 1 {
		t.Fatalf("tesseract ran %d times, want 1 (pages 1 and 3 are ledger hits)", got)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.pages) != 1 || f.pages[0].PageNumber != 2 {
		t.Fatalf("uploaded pages = %#v, want only page 2", f.pages)
	}
}

func TestWorkerLedgerVersionMismatchRebills(t *testing.T) {
	f := newFakeAPI(t, 1)
	f.ledger = map[string]api.LedgerEntry{
		"1": {ImageSHA256: "hash-1", OCRVersion: "an-older-pipeline"},
	}
	bin, calls := stubTesseract(t, "HELLO")
	w, _ := testWorker(t, bin)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	RunOnce(ctx, t, w, f)

	if got := calls(); got != 1 {
		t.Fatalf("tesseract ran %d times, want 1 (the old version does not count)", got)
	}
}

func TestWorkerSkipsUnreadablePages(t *testing.T) {
	f := newFakeAPI(t, 3)
	f.refusePage[2] = true // vector art: a named refusal, not a failure
	bin, calls := stubTesseract(t, "HELLO")
	w, _ := testWorker(t, bin)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	RunOnce(ctx, t, w, f)

	if got := calls(); got != 2 {
		t.Fatalf("tesseract ran %d times, want 2", got)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.completed) != 1 || f.failure != "" {
		t.Fatalf("completed = %v failure = %q, want a clean completion", f.completed, f.failure)
	}
}

func TestWorkerTesseractFailureFailsTheJob(t *testing.T) {
	f := newFakeAPI(t, 1)
	dir := t.TempDir()
	script := `#!/bin/sh
[ "$1" = "--version" ] && { echo "tesseract 5.3.0"; exit 0; }
echo "cannot read" >&2
exit 1`
	bin := filepath.Join(dir, "tesseract")
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	w, _ := testWorker(t, bin)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	RunOnce(ctx, t, w, f)

	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failure == "" {
		t.Fatal("job completed without a failure message")
	}
	if !strings.Contains(f.failure, "ocr_failed") && !strings.Contains(f.failure, "tesseract") {
		t.Errorf("failure = %q, want a named read failure", f.failure)
	}
}

func TestWorkerPreflight(t *testing.T) {
	bin, _ := stubTesseract(t, "HELLO")
	w, cfg := testWorker(t, bin)
	if err := w.Preflight(); err != nil {
		t.Fatalf("preflight: %v", err)
	}
	cfg.WorkDir = "/definitely/not/writable"
	w2, _ := testWorker(t, bin)
	w2.cfg.WorkDir = cfg.WorkDir
	if err := w2.Preflight(); err == nil {
		t.Fatal("unwritable work dir accepted")
	}
}

func TestWorkerEngineIsWiredFromConfig(t *testing.T) {
	bin, _ := stubTesseract(t, "HELLO")
	w, _ := testWorker(t, bin)
	if w.ocr.PSM != 11 || w.ocr.Language != "eng" || w.ocr.Bin != bin {
		t.Errorf("engine = %+v", w.ocr)
	}
}

// replyJSON is the test-local JSON writer (the package's own writeJSON
// stays in status.go).
func replyJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
