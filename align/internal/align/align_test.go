package align_test

import (
	"testing"

	"github.com/collinpendleton/backhog/align/internal/align"
	"github.com/collinpendleton/backhog/align/internal/bench"
)

// The worker's shipped thresholds, restated here so these tests fail if
// the aligner ever stops separating the cases the worker relies on it to
// separate. They are config.Config defaults, not align defaults.
const (
	workerMinCoverage   = 0.80
	workerMinConfidence = 0.60
)

func caseNamed(t *testing.T, name string) bench.Case {
	t.Helper()
	for _, p := range bench.Cases() {
		if p.Name == name {
			return bench.Generate(p)
		}
	}
	t.Fatalf("no bench case named %q", name)
	return bench.Case{}
}

func TestAlignsACleanReading(t *testing.T) {
	c := caseNamed(t, "clean")
	res := align.Align(c.Canonical, c.Segments, align.DefaultOptions())

	if res.Coverage < workerMinCoverage {
		t.Errorf("coverage = %.3f, want at least %.2f", res.Coverage, workerMinCoverage)
	}
	if res.MeanConfidence < workerMinConfidence {
		t.Errorf("mean confidence = %.3f, want at least %.2f", res.MeanConfidence, workerMinConfidence)
	}
	if len(res.Anchors) < len(c.Segments)/2 {
		t.Errorf("anchored %d of %d segments", len(res.Anchors), len(c.Segments))
	}

	for i := 1; i < len(res.Anchors); i++ {
		if res.Anchors[i].CharOffset <= res.Anchors[i-1].CharOffset {
			t.Fatalf("anchors go backwards through the book at %d: %+v", i, res.Anchors[i-1:i+1])
		}
		if res.Anchors[i].AudioSeconds < res.Anchors[i-1].AudioSeconds {
			t.Fatalf("anchors go backwards through the tape at %d", i)
		}
	}
}

func TestSkipsTheAudiobooksOwnFrontMatter(t *testing.T) {
	// The publisher's intro is in no EPUB. Anchoring it would drag the
	// whole map forward by a couple of minutes, so those segments must
	// produce no anchors at all and the first real anchor must land at
	// the top of the book.
	c := caseNamed(t, "clean")
	res := align.Align(c.Canonical, c.Segments, align.DefaultOptions())
	if len(res.Anchors) == 0 {
		t.Fatal("no anchors")
	}

	firstTruth := c.Truth[0]
	if got := res.Anchors[0].AudioSeconds; got < firstTruth.AudioSeconds-60 {
		t.Errorf("first anchor at %.1fs, before the narration of the book itself starts at %.1fs",
			got, firstTruth.AudioSeconds)
	}
	if got := res.Anchors[0].CharOffset; got > len(c.Canonical)/50 {
		t.Errorf("first anchor at char %d of %d; the book should be anchored from near its start",
			got, len(c.Canonical))
	}
	// And the outro, symmetrically: nothing may be anchored past the end.
	last := res.Anchors[len(res.Anchors)-1]
	if last.CharOffset >= len(c.Canonical) {
		t.Errorf("last anchor at char %d, past the end of a %d char book", last.CharOffset, len(c.Canonical))
	}
}

func TestAnchorsLandWhereTheTruthIs(t *testing.T) {
	// Interpolating the anchor map at a known moment must give back very
	// nearly the known character offset. This is the whole product: stop
	// listening, open the book, be on the right sentence.
	report := bench.Measure(caseNamed(t, "clean"), align.DefaultOptions())
	if report.TruthPoints < 100 {
		t.Fatalf("only %d truth points were inside the anchored span", report.TruthPoints)
	}
	if report.P95ErrChars > 200 {
		t.Errorf("95th percentile error is %d chars (~%.1fs), want under 200",
			report.P95ErrChars, float64(report.P95ErrChars)/report.CharsPerSecond)
	}
}

func TestABadTranscriptStillAligns(t *testing.T) {
	// A poor transcript should degrade, not collapse: the anchors get
	// looser and the confidence drops, but the book is still mapped.
	res := align.Align(caseNamed(t, "very-noisy").Canonical,
		caseNamed(t, "very-noisy").Segments, align.DefaultOptions())
	if res.Coverage < workerMinCoverage {
		t.Errorf("coverage = %.3f at 32%% WER, want at least %.2f", res.Coverage, workerMinCoverage)
	}
}

func TestAnAbridgedReadingIsNotPublishable(t *testing.T) {
	// An abridgement maps the part it read perfectly well, and would look
	// entirely healthy on confidence alone. Coverage is what catches it.
	c := caseNamed(t, "abridged")
	res := align.Align(c.Canonical, c.Segments, align.DefaultOptions())
	if len(res.Anchors) == 0 {
		t.Fatal("an abridgement still maps the part it read; want anchors")
	}
	if res.Coverage >= workerMinCoverage {
		t.Errorf("coverage = %.3f, want it below the %.2f publishing threshold",
			res.Coverage, workerMinCoverage)
	}
}

func TestTheWrongBookIsNotPublishable(t *testing.T) {
	// The failure that matters: the audio and the ebook are different
	// books. Nothing may pass the thresholds, whatever it managed to
	// match by chance.
	c := caseNamed(t, "wrong-book")
	res := align.Align(c.Canonical, c.Segments, align.DefaultOptions())
	if res.Coverage >= workerMinCoverage && res.MeanConfidence >= workerMinConfidence {
		t.Fatalf("a different book passed both thresholds: coverage %.3f, confidence %.3f",
			res.Coverage, res.MeanConfidence)
	}
	if res.Coverage > 0.05 {
		t.Errorf("coverage = %.3f against a different book entirely", res.Coverage)
	}
}

func TestEmptyInputsAreNotAnError(t *testing.T) {
	if got := align.Align("", []align.Segment{{Text: "hello"}}, align.DefaultOptions()); len(got.Anchors) != 0 {
		t.Errorf("anchors from an empty book: %+v", got.Anchors)
	}
	if got := align.Align("some book text here", nil, align.DefaultOptions()); len(got.Anchors) != 0 {
		t.Errorf("anchors from an empty transcript: %+v", got.Anchors)
	}
}
