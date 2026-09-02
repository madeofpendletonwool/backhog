package worker

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/collinpendleton/backhog/align/internal/api"
	"github.com/collinpendleton/backhog/align/internal/bench"
)

// benchClaim turns one synthetic book/transcript pair into the two things
// alignTranscript takes: a claim pointing at a canonical text on disk, and
// the transcript the transcription half would have produced.
func benchClaim(t *testing.T, name string) (*api.Claim, []api.Segment) {
	t.Helper()
	var c bench.Case
	for _, p := range bench.Cases() {
		if p.Name == name {
			c = bench.Generate(p)
		}
	}
	if c.Canonical == "" {
		t.Fatalf("no bench case named %q", name)
	}
	segments := make([]api.Segment, len(c.Segments))
	for i, s := range c.Segments {
		segments[i] = api.Segment{AudioStart: s.AudioStart, AudioEnd: s.AudioEnd, Text: s.Text}
	}
	return &api.Claim{
		Job:          api.Job{ID: "job-1", EntryID: "entry-1"},
		EpubTextPath: writeCanonical(t, c.Canonical),
	}, segments
}

func discardLog() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestAlignTranscriptPublishesAGoodReading(t *testing.T) {
	fake := &fakeAPI{}
	srv := fake.server(t)
	w, _ := testWorker(t, srv.URL)
	claim, segments := benchClaim(t, "clean")

	out, err := w.alignTranscript(context.Background(), discardLog(), claim, segments)
	if err != nil {
		t.Fatalf("alignTranscript: %v", err)
	}
	if out.State != api.StateReady {
		t.Fatalf("state = %q (%s), want ready", out.State, out.Detail)
	}
	if out.Coverage < 0.8 || out.Confidence < 0.6 {
		t.Errorf("coverage %.3f, confidence %.3f", out.Coverage, out.Confidence)
	}

	fake.mu.Lock()
	defer fake.mu.Unlock()
	if len(fake.anchors) != out.Anchors {
		t.Fatalf("uploaded %d anchors, reported %d", len(fake.anchors), out.Anchors)
	}
	if len(fake.anchors) == 0 {
		t.Fatal("no anchors uploaded")
	}
	// Batched, not sent in one lump: the API writes each batch in a
	// transaction and each one refreshes the claim's heartbeat.
	if fake.anchorPost < 2 {
		t.Errorf("uploaded %d anchors in %d requests, want them batched",
			len(fake.anchors), fake.anchorPost)
	}
	for i := 1; i < len(fake.anchors); i++ {
		if fake.anchors[i].CharOffset <= fake.anchors[i-1].CharOffset {
			t.Fatalf("anchors arrived out of order at %d: %+v", i, fake.anchors[i-1:i+1])
		}
	}
}

func TestAlignTranscriptKeepsAWrongBookOutOfTheLibrary(t *testing.T) {
	// The audio is a different book. There is no honest alignment to be
	// had, so the job must finish as low_confidence rather than
	// publishing a map that would send a reader to the wrong page.
	fake := &fakeAPI{}
	srv := fake.server(t)
	w, _ := testWorker(t, srv.URL)
	claim, segments := benchClaim(t, "wrong-book")

	out, err := w.alignTranscript(context.Background(), discardLog(), claim, segments)
	if err != nil {
		t.Fatalf("alignTranscript: %v", err)
	}
	if out.State != api.StateLowConfidence {
		t.Fatalf("state = %q, want low_confidence", out.State)
	}
	if out.Detail == "" {
		t.Error("a low-confidence result must say why")
	}
}

func TestAlignTranscriptLabelsAnAbridgement(t *testing.T) {
	// An abridgement maps its own half of the book perfectly well. It is
	// still not a whole map, so the anchors are kept and the result is
	// labelled rather than thrown away.
	fake := &fakeAPI{}
	srv := fake.server(t)
	w, _ := testWorker(t, srv.URL)
	claim, segments := benchClaim(t, "abridged")

	out, err := w.alignTranscript(context.Background(), discardLog(), claim, segments)
	if err != nil {
		t.Fatalf("alignTranscript: %v", err)
	}
	if out.State != api.StateLowConfidence {
		t.Fatalf("state = %q (coverage %.3f), want low_confidence", out.State, out.Coverage)
	}
	if out.Anchors == 0 {
		t.Error("the anchors of an abridgement are still worth keeping")
	}
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if len(fake.anchors) == 0 {
		t.Error("a low-confidence alignment must still upload its anchors")
	}
}

func TestAlignTranscriptRespectsTheConfiguredThresholds(t *testing.T) {
	fake := &fakeAPI{}
	srv := fake.server(t)
	w, _ := testWorker(t, srv.URL)
	w.cfg.MinCoverage = 0.999999
	claim, segments := benchClaim(t, "clean")

	out, err := w.alignTranscript(context.Background(), discardLog(), claim, segments)
	if err != nil {
		t.Fatalf("alignTranscript: %v", err)
	}
	if out.State != api.StateLowConfidence {
		t.Errorf("state = %q at a coverage threshold of %.6f", out.State, w.cfg.MinCoverage)
	}
}

func TestReadCanonicalRefusesWhatIsNotABook(t *testing.T) {
	dir := t.TempDir()
	empty := filepath.Join(dir, "empty.txt")
	if err := os.WriteFile(empty, nil, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	for _, tc := range []struct{ name, path string }{
		{"no path at all", ""},
		{"missing file", filepath.Join(dir, "gone.txt")},
		{"empty file", empty},
	} {
		if _, err := readCanonical(tc.path); err == nil {
			t.Errorf("%s: readCanonical succeeded", tc.name)
		}
	}
}

func TestAlignTranscriptFailsWhenTheBookIsNotThere(t *testing.T) {
	// The EPUB was never parsed, or the volume is not mounted. That is a
	// failure with a code of its own, not a low-confidence alignment:
	// nothing about the transcript is wrong.
	fake := &fakeAPI{}
	srv := fake.server(t)
	w, _ := testWorker(t, srv.URL)

	_, err := w.alignTranscript(context.Background(), discardLog(),
		&api.Claim{Job: api.Job{ID: "job-1"}, EpubTextPath: filepath.Join(t.TempDir(), "gone.txt")},
		[]api.Segment{{AudioStart: 0, AudioEnd: 5, Text: "hello"}})
	if err == nil {
		t.Fatal("alignTranscript succeeded without a canonical text")
	}
	if got := toFailure(err, "alignment"); got.Code != failEpubText {
		t.Errorf("failure code = %q, want %q", got.Code, failEpubText)
	}
}

func TestAlignTranscriptRefusesAnEmptyTranscript(t *testing.T) {
	fake := &fakeAPI{}
	srv := fake.server(t)
	w, _ := testWorker(t, srv.URL)

	_, err := w.alignTranscript(context.Background(), discardLog(),
		&api.Claim{Job: api.Job{ID: "job-1"}, EpubTextPath: writeCanonical(t, "a book with words in it")},
		nil)
	if err == nil {
		t.Fatal("alignTranscript succeeded with no transcript")
	}
	if got := toFailure(err, "alignment"); got.Code != failTranscribe {
		t.Errorf("failure code = %q, want %q", got.Code, failTranscribe)
	}
}

func TestRunClaimPassesThroughTheAligningStage(t *testing.T) {
	// The whole loop with the aligner in it: one claim, one heartbeat,
	// transcription and then alignment, ending in a terminal state.
	fake := &fakeAPI{}
	srv := fake.server(t)
	w, _ := testWorker(t, srv.URL)

	// The whisper stub emits two short sentences per chunk, which is far
	// less narration than the aligner needs to locate anything in a book,
	// so this run ends low_confidence on purpose. What it proves is the
	// wiring: the job reaches the aligning stage and finishes there. The
	// ready case is covered by alignTranscript above, on a whole book.
	media := t.TempDir()
	one := filepath.Join(media, "one.mp3")
	if err := os.WriteFile(one, []byte("audio"), 0o644); err != nil {
		t.Fatalf("write media: %v", err)
	}

	w.runClaim(context.Background(), &api.Claim{
		Job:          api.Job{ID: "job-1", EntryID: "entry-1"},
		EpubTextPath: writeCanonical(t, "first line second line"),
		Tracks:       []api.TrackFile{{Path: one, Duration: 30}},
	})

	fake.mu.Lock()
	defer fake.mu.Unlock()
	if fake.completed.state != api.StateLowConfidence {
		t.Errorf("completed state = %q", fake.completed.state)
	}
	if !containsState(fake.states, api.StateAligning) {
		t.Errorf("states = %v, want the job to pass through aligning", fake.states)
	}
}

func containsState(states []string, want string) bool {
	for _, s := range states {
		if s == want {
			return true
		}
	}
	return false
}
