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
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/collinpendleton/backhog/align/internal/api"
	"github.com/collinpendleton/backhog/align/internal/config"
)

// fakeAPI stands in for the /internal alignment endpoints so the whole
// worker loop can be exercised without a database: it records what the
// worker uploaded and how it finished.
type fakeAPI struct {
	mu         sync.Mutex
	segments   []api.Segment
	anchors    []api.Anchor
	anchorPost int
	states     []string
	completed  struct {
		state      string
		model      string
		failure    string
		coverage   float64
		confidence float64
	}
	completeCalls int
	// claimLost makes every write answer 409, the way the API does once
	// a stale claim has been handed to another worker.
	claimLost bool
}

func (f *fakeAPI) server(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()

	mux.HandleFunc("POST /internal/align/{jobID}/progress", func(w http.ResponseWriter, r *http.Request) {
		if f.reject(w) {
			return
		}
		var body struct {
			State string `json:"state"`
		}
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		f.mu.Lock()
		if body.State != "" && (len(f.states) == 0 || f.states[len(f.states)-1] != body.State) {
			f.states = append(f.states, body.State)
		}
		f.mu.Unlock()
		writeTestJSON(w, map[string]any{"job": map[string]any{"id": r.PathValue("jobID")}})
	})

	mux.HandleFunc("POST /internal/align/{jobID}/segments", func(w http.ResponseWriter, r *http.Request) {
		if f.reject(w) {
			return
		}
		var body struct {
			Segments []api.Segment `json:"segments"`
		}
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		f.mu.Lock()
		f.segments = append(f.segments, body.Segments...)
		f.mu.Unlock()
		writeTestJSON(w, map[string]any{"job": map[string]any{"id": r.PathValue("jobID")}})
	})

	mux.HandleFunc("POST /internal/align/{jobID}/anchors", func(w http.ResponseWriter, r *http.Request) {
		if f.reject(w) {
			return
		}
		var body struct {
			Anchors []api.Anchor `json:"anchors"`
		}
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		f.mu.Lock()
		f.anchors = append(f.anchors, body.Anchors...)
		f.anchorPost++
		f.mu.Unlock()
		writeTestJSON(w, map[string]any{"job": map[string]any{"id": r.PathValue("jobID")}})
	})

	mux.HandleFunc("POST /internal/align/{jobID}/complete", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			State      string  `json:"state"`
			Model      string  `json:"model"`
			Error      string  `json:"error"`
			Coverage   float64 `json:"coverage"`
			Confidence float64 `json:"mean_confidence"`
		}
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		f.mu.Lock()
		f.completed.state = body.State
		f.completed.model = body.Model
		f.completed.failure = body.Error
		f.completed.coverage = body.Coverage
		f.completed.confidence = body.Confidence
		f.completeCalls++
		f.mu.Unlock()
		writeTestJSON(w, map[string]any{"job": map[string]any{"id": r.PathValue("jobID")}})
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func (f *fakeAPI) reject(w http.ResponseWriter) bool {
	f.mu.Lock()
	lost := f.claimLost
	f.mu.Unlock()
	if !lost {
		return false
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusConflict)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": "job is claimed by another worker"})
	return true
}

// writeCanonical drops a canonical text where a claim can point at it.
func writeCanonical(t *testing.T, text string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "text.txt")
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatalf("write canonical text: %v", err)
	}
	return path
}

func writeTestJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// stubTools writes shell stand-ins for ffmpeg, ffprobe and whisper-cli.
// The ffmpeg stub records the window it was asked for in the "WAV" it
// writes; the whisper stub reads that back and emits two segments at
// fixed offsets into the chunk, which is enough to prove the worker puts
// them on the right place of the global timeline.
func stubTools(t *testing.T) (ffmpeg, ffprobe, whisper string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("stub tools are shell scripts")
	}
	dir := t.TempDir()

	write := func(name, body string) string {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
		return path
	}

	ffmpeg = write("ffmpeg", `#!/bin/sh
out=""
for arg in "$@"; do out="$arg"; done
printf 'stub-pcm\n' > "$out"
`)
	ffprobe = write("ffprobe", `#!/bin/sh
echo "40.0"
`)
	whisper = write("whisper-cli", `#!/bin/sh
prefix=""
while [ $# -gt 0 ]; do
  case "$1" in
    -of) prefix="$2"; shift 2 ;;
    *) shift ;;
  esac
done
cat > "$prefix.json" <<'JSON'
{"transcription":[
 {"offsets":{"from":1000,"to":3000},"text":" first line"},
 {"offsets":{"from":4000,"to":6000},"text":" [BLANK_AUDIO]"},
 {"offsets":{"from":7000,"to":9000},"text":" second line"}
]}
JSON
`)
	return ffmpeg, ffprobe, whisper
}

func testWorker(t *testing.T, apiURL string) (*Worker, config.Config) {
	t.Helper()
	ffmpeg, ffprobe, whisper := stubTools(t)

	model := filepath.Join(t.TempDir(), "model.bin")
	if err := os.WriteFile(model, []byte("ggml"), 0o644); err != nil {
		t.Fatalf("write model: %v", err)
	}

	cfg := config.Config{
		APIURL:            apiURL,
		Token:             "s3cret",
		WorkerID:          "worker-1",
		FFmpegBin:         ffmpeg,
		FFprobeBin:        ffprobe,
		WhisperBin:        whisper,
		WhisperModel:      model,
		ModelName:         "base.en",
		Language:          "en",
		Threads:           1,
		BeamSize:          1,
		WhisperTimeout:    30 * time.Second,
		ChunkSeconds:      60,
		OverlapSeconds:    0,
		SegmentBatch:      2,
		AnchorBatch:       128,
		MinCoverage:       0.80,
		MinConfidence:     0.60,
		WorkDir:           t.TempDir(),
		PollInterval:      time.Millisecond,
		HeartbeatInterval: 20 * time.Millisecond,
	}

	w := New(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	w.api.Backoff = time.Millisecond
	return w, cfg
}

func TestRunClaimTranscribesOntoTheGlobalTimeline(t *testing.T) {
	fake := &fakeAPI{}
	srv := fake.server(t)
	w, _ := testWorker(t, srv.URL)

	media := t.TempDir()
	one := filepath.Join(media, "one.mp3")
	two := filepath.Join(media, "two.mp3")
	for _, p := range []string{one, two} {
		if err := os.WriteFile(p, []byte("audio"), 0o644); err != nil {
			t.Fatalf("write media: %v", err)
		}
	}

	// 100 seconds of track one is two chunks (0-60, 60-100); track two
	// is 30 seconds in one chunk and starts at global second 100.
	w.runClaim(context.Background(), &api.Claim{
		Job:          api.Job{ID: "job-1", EntryID: "entry-1"},
		EpubTextPath: writeCanonical(t, "first line second line and then some other words entirely"),
		Tracks: []api.TrackFile{
			{Path: one, Duration: 100},
			{Path: two, Duration: 30},
		},
	})

	fake.mu.Lock()
	defer fake.mu.Unlock()

	// Two chunks on track one plus one on track two, each yielding the
	// stub's two speech segments (the blank one is dropped).
	if len(fake.segments) != 6 {
		t.Fatalf("stored %d segments, want 6: %+v", len(fake.segments), fake.segments)
	}
	wantStarts := []float64{1, 7, 61, 67, 101, 107}
	for i, want := range wantStarts {
		if fake.segments[i].AudioStart != want {
			t.Errorf("segment %d starts at %v, want %v", i, fake.segments[i].AudioStart, want)
		}
	}
	if fake.segments[0].Text != "first line" || fake.segments[1].Text != "second line" {
		t.Errorf("text = %q / %q", fake.segments[0].Text, fake.segments[1].Text)
	}

	// Timestamps must never run backwards across a track boundary.
	for i := 1; i < len(fake.segments); i++ {
		if fake.segments[i].AudioStart < fake.segments[i-1].AudioStart {
			t.Fatalf("timeline goes backwards at %d: %+v", i, fake.segments)
		}
	}

	if len(fake.states) == 0 || fake.states[0] != api.StateTranscribing {
		t.Errorf("states = %v, want it to start transcribing", fake.states)
	}
	if fake.completed.state != api.StateLowConfidence {
		t.Errorf("completed state = %q", fake.completed.state)
	}
	if fake.completed.model != "base.en" {
		t.Errorf("completed model = %q", fake.completed.model)
	}
	// Six repetitions of two stub sentences are far too little narration
	// to locate in a book, so the aligner finds nothing and the job
	// finishes honestly labelled rather than published.
	if fake.completed.coverage != 0 {
		t.Errorf("coverage = %v, want 0 from a transcript that maps nowhere", fake.completed.coverage)
	}
	if len(fake.anchors) != 0 {
		t.Errorf("uploaded %d anchors from a transcript that maps nowhere", len(fake.anchors))
	}
}

func TestRunClaimProbesAnUnmeasuredFinalTrack(t *testing.T) {
	fake := &fakeAPI{}
	srv := fake.server(t)
	w, _ := testWorker(t, srv.URL)

	media := t.TempDir()
	one := filepath.Join(media, "one.mp3")
	two := filepath.Join(media, "two.mp3")
	for _, p := range []string{one, two} {
		if err := os.WriteFile(p, []byte("audio"), 0o644); err != nil {
			t.Fatalf("write media: %v", err)
		}
	}

	// The ffprobe stub reports 40 seconds, so the final track is one
	// chunk starting at global second 60.
	w.runClaim(context.Background(), &api.Claim{
		Job:          api.Job{ID: "job-1", EntryID: "entry-1"},
		EpubTextPath: writeCanonical(t, "first line second line and then some other words entirely"),
		Tracks: []api.TrackFile{
			{Path: one, Duration: 60},
			{Path: two, Duration: 0},
		},
	})

	fake.mu.Lock()
	defer fake.mu.Unlock()
	if len(fake.segments) != 4 {
		t.Fatalf("stored %d segments, want 4: %+v", len(fake.segments), fake.segments)
	}
	if fake.segments[2].AudioStart != 61 {
		t.Errorf("final track starts at %v, want 61", fake.segments[2].AudioStart)
	}
	if fake.completed.state != api.StateLowConfidence {
		t.Errorf("completed state = %q", fake.completed.state)
	}
}

func TestRunClaimFailsLoudlyOnAMissingMount(t *testing.T) {
	fake := &fakeAPI{}
	srv := fake.server(t)
	w, _ := testWorker(t, srv.URL)

	w.runClaim(context.Background(), &api.Claim{
		Job:    api.Job{ID: "job-1", EntryID: "entry-1"},
		Tracks: []api.TrackFile{{Path: "/media/gone.mp3", Missing: true}},
	})

	fake.mu.Lock()
	defer fake.mu.Unlock()
	if fake.completed.state != api.StateFailed {
		t.Fatalf("completed state = %q, want failed", fake.completed.state)
	}
	if !strings.HasPrefix(fake.completed.failure, failMediaMissing+":") {
		t.Errorf("failure = %q, want it coded %q", fake.completed.failure, failMediaMissing)
	}
}

func TestRunClaimFailsDistinguishablyOnADegradedTimeline(t *testing.T) {
	fake := &fakeAPI{}
	srv := fake.server(t)
	w, _ := testWorker(t, srv.URL)

	w.runClaim(context.Background(), &api.Claim{
		Job: api.Job{ID: "job-1", EntryID: "entry-1"},
		Tracks: []api.TrackFile{
			{Path: "/media/one.mp3", Duration: 0},
			{Path: "/media/two.mp3", Duration: 10},
		},
	})

	fake.mu.Lock()
	defer fake.mu.Unlock()
	if !strings.HasPrefix(fake.completed.failure, failTimelineDegraded+":") {
		t.Errorf("failure = %q, want it coded %q", fake.completed.failure, failTimelineDegraded)
	}
}

func TestRunClaimAbandonsAJobItNoLongerHolds(t *testing.T) {
	// A stale claim reclaimed by the API answers 409. Pushing on would
	// interleave two workers' transcripts into one alignment, so the
	// only correct move is to drop the work without completing it.
	fake := &fakeAPI{claimLost: true}
	srv := fake.server(t)
	w, _ := testWorker(t, srv.URL)

	media := t.TempDir()
	one := filepath.Join(media, "one.mp3")
	if err := os.WriteFile(one, []byte("audio"), 0o644); err != nil {
		t.Fatalf("write media: %v", err)
	}

	w.runClaim(context.Background(), &api.Claim{
		Job:    api.Job{ID: "job-1", EntryID: "entry-1"},
		Tracks: []api.TrackFile{{Path: one, Duration: 30}},
	})

	fake.mu.Lock()
	defer fake.mu.Unlock()
	if fake.completeCalls != 0 {
		t.Errorf("completed a job this worker did not hold (%d calls)", fake.completeCalls)
	}
	if len(fake.segments) != 0 {
		t.Errorf("uploaded %d segments after losing the claim", len(fake.segments))
	}
	if got := w.tracker.Get(); got.State != Idle {
		t.Errorf("worker state = %q, want idle", got.State)
	}
}

func TestRunClaimLeavesTheClaimAloneOnShutdown(t *testing.T) {
	// Killing the worker mid-book must leave the job reclaimable rather
	// than failing it: the attempt is not wasted, and a restarted worker
	// picks the book back up once the heartbeat goes stale.
	fake := &fakeAPI{}
	srv := fake.server(t)
	w, _ := testWorker(t, srv.URL)

	media := t.TempDir()
	one := filepath.Join(media, "one.mp3")
	if err := os.WriteFile(one, []byte("audio"), 0o644); err != nil {
		t.Fatalf("write media: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	w.runClaim(ctx, &api.Claim{
		Job:    api.Job{ID: "job-1", EntryID: "entry-1"},
		Tracks: []api.TrackFile{{Path: one, Duration: 600}},
	})

	fake.mu.Lock()
	defer fake.mu.Unlock()
	if fake.completeCalls != 0 {
		t.Errorf("a shutdown completed the job (%d calls); it should be left to reclaim", fake.completeCalls)
	}
}

func TestPreflightRejectsAMissingModel(t *testing.T) {
	w, cfg := testWorker(t, "http://api:8080")
	if err := w.Preflight(); err != nil {
		t.Fatalf("Preflight: %v", err)
	}

	cfg.WhisperModel = filepath.Join(t.TempDir(), "absent.bin")
	broken := New(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err := broken.Preflight(); err == nil {
		t.Fatal("want an error for a missing model file")
	}
}

func TestStatusEndpointReportsTheCurrentJob(t *testing.T) {
	w, _ := testWorker(t, "http://api:8080")
	w.tracker.startJob("job-1", "entry-1")
	w.tracker.setStage(api.StateTranscribing, "transcribing track 1/2, chunk 3/7", 0.25)

	rec := httptest.NewRecorder()
	w.StatusHandler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/status", nil))

	var got Status
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.JobID != "job-1" || got.Progress != 0.25 {
		t.Errorf("status = %+v", got)
	}
	if !strings.Contains(got.Stage, "chunk 3/7") {
		t.Errorf("stage = %q", got.Stage)
	}
}

func TestScratchIsCleanedUpAfterAJob(t *testing.T) {
	fake := &fakeAPI{}
	srv := fake.server(t)
	w, cfg := testWorker(t, srv.URL)

	media := t.TempDir()
	one := filepath.Join(media, "one.mp3")
	if err := os.WriteFile(one, []byte("audio"), 0o644); err != nil {
		t.Fatalf("write media: %v", err)
	}

	w.runClaim(context.Background(), &api.Claim{
		Job:    api.Job{ID: "job-1", EntryID: "entry-1"},
		Tracks: []api.TrackFile{{Path: one, Duration: 180}},
	})

	entries, err := os.ReadDir(cfg.WorkDir)
	if err != nil {
		t.Fatalf("read work dir: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("scratch left behind: %v", names(entries))
	}
}

func names(entries []os.DirEntry) []string {
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		out = append(out, fmt.Sprintf("%s (dir=%v)", e.Name(), e.IsDir()))
	}
	return out
}
