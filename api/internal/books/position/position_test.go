package position

import (
	"context"
	"errors"
	"math"
	"testing"
)

// anchors for a book whose audiobook runs slightly faster than linear in the
// middle: three anchors, two segments with different slopes, so a wrong
// bracket shows up as a wrong answer rather than a coincidentally right one.
func audioFixture() []Anchor {
	return []Anchor{
		{CharOffset: 1000, Value: 60, Confidence: 0.9},
		{CharOffset: 3000, Value: 160, Confidence: 0.8},
		{CharOffset: 9000, Value: 460, Confidence: 0.95},
	}
}

func closeTo(t *testing.T, label string, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > 1e-9 {
		t.Errorf("%s = %v, want %v", label, got, want)
	}
}

func TestCharToAudioEmptyAnchorSet(t *testing.T) {
	tr := New(nil, nil)
	if tr.HasAudio() || tr.HasPages() {
		t.Fatal("a translator with no anchors reports a map")
	}
	for _, tc := range []struct {
		name string
		call func() (float64, bool)
	}{
		{"CharToAudio", func() (float64, bool) { v, _, ok := tr.CharToAudio(500); return v, ok }},
		{"AudioToChar", func() (float64, bool) { v, _, ok := tr.AudioToChar(90); return float64(v), ok }},
		{"CharToPage", func() (float64, bool) { v, _, ok := tr.CharToPage(500); return float64(v), ok }},
		{"PageToChar", func() (float64, bool) { v, _, ok := tr.PageToChar(25); return float64(v), ok }},
	} {
		v, ok := tc.call()
		if ok {
			t.Errorf("%s reported ok with no anchors", tc.name)
		}
		if v != 0 {
			t.Errorf("%s = %v with no anchors, want the zero value", tc.name, v)
		}
	}
}

func TestCharToAudioExactlyOnAnchor(t *testing.T) {
	tr := New(audioFixture(), nil)
	for _, a := range audioFixture() {
		got, conf, ok := tr.CharToAudio(a.CharOffset)
		if !ok {
			t.Fatalf("char %d: not ok", a.CharOffset)
		}
		closeTo(t, "seconds", got, a.Value)
		// An exact hit carries no interpolation error, so it keeps the
		// anchor's own confidence rather than a blended one.
		closeTo(t, "confidence", conf, a.Confidence)
	}
}

func TestCharToAudioBetweenAnchors(t *testing.T) {
	tr := New(audioFixture(), nil)

	// Halfway across the first segment: 1000→3000 chars spans 60→160s.
	got, conf, ok := tr.CharToAudio(2000)
	if !ok {
		t.Fatal("not ok")
	}
	closeTo(t, "seconds", got, 110)
	closeTo(t, "confidence", conf, 0.8) // the weaker of 0.9 and 0.8

	// A quarter into the second segment, which has a different slope:
	// 3000→9000 chars spans 160→460s.
	got, conf, ok = tr.CharToAudio(4500)
	if !ok {
		t.Fatal("not ok")
	}
	closeTo(t, "seconds", got, 235)
	closeTo(t, "confidence", conf, 0.8)
}

func TestCharToAudioBeforeFirstAnchor(t *testing.T) {
	tr := New(audioFixture(), nil)

	got, conf, ok := tr.CharToAudio(0)
	if !ok {
		t.Fatal("not ok")
	}
	// Clamped to the first anchor, not extrapolated backwards into
	// negative seconds.
	closeTo(t, "seconds", got, 60)
	closeTo(t, "confidence", conf, 0.9*outsidePenalty)
}

func TestCharToAudioAfterLastAnchor(t *testing.T) {
	tr := New(audioFixture(), nil)

	got, conf, ok := tr.CharToAudio(500_000)
	if !ok {
		t.Fatal("not ok")
	}
	closeTo(t, "seconds", got, 460)
	closeTo(t, "confidence", conf, 0.95*outsidePenalty)
}

func TestAudioToCharRoundTrips(t *testing.T) {
	tr := New(audioFixture(), nil)

	for _, char := range []int{1000, 1500, 2000, 3000, 5000, 8999, 9000} {
		seconds, _, ok := tr.CharToAudio(char)
		if !ok {
			t.Fatalf("char %d: CharToAudio not ok", char)
		}
		back, _, ok := tr.AudioToChar(seconds)
		if !ok {
			t.Fatalf("char %d: AudioToChar not ok", char)
		}
		if back != char {
			t.Errorf("round trip char %d -> %vs -> char %d", char, seconds, back)
		}
	}
}

func TestAudioToCharOutsideTheSpan(t *testing.T) {
	tr := New(audioFixture(), nil)

	// Before the recording's first anchor — the publisher's blurb.
	got, conf, ok := tr.AudioToChar(5)
	if !ok {
		t.Fatal("not ok")
	}
	if got != 1000 {
		t.Errorf("char = %d, want the first anchor's 1000", got)
	}
	closeTo(t, "confidence", conf, 0.9*outsidePenalty)

	// Past the end, and past a negative second that must not blow up.
	got, _, ok = tr.AudioToChar(99_999)
	if !ok || got != 9000 {
		t.Errorf("after last anchor = %d (ok %v), want 9000", got, ok)
	}
	got, _, ok = tr.AudioToChar(-30)
	if !ok || got != 1000 {
		t.Errorf("negative seconds = %d (ok %v), want the first anchor", got, ok)
	}
}

func TestSingleAnchorClampsEverywhere(t *testing.T) {
	tr := New([]Anchor{{CharOffset: 4000, Value: 200, Confidence: 1}}, nil)

	if v, c, ok := tr.CharToAudio(4000); !ok || v != 200 || c != 1 {
		t.Errorf("on the anchor = (%v, %v, %v), want (200, 1, true)", v, c, ok)
	}
	if v, c, ok := tr.CharToAudio(10); !ok || v != 200 || c != outsidePenalty {
		t.Errorf("before the anchor = (%v, %v, %v), want (200, %v, true)", v, c, ok, outsidePenalty)
	}
	if v, _, ok := tr.CharToAudio(400_000); !ok || v != 200 {
		t.Errorf("after the anchor = (%v, %v), want 200", v, ok)
	}
}

func TestPageMapIsIndependentOfAudio(t *testing.T) {
	pages := []Anchor{
		{CharOffset: 0, Value: 1, Confidence: 0.7},
		{CharOffset: 2000, Value: 11, Confidence: 0.7},
	}
	tr := New(nil, pages)

	if tr.HasAudio() {
		t.Error("audio map is populated from page anchors")
	}
	if !tr.HasPages() {
		t.Fatal("page map is empty")
	}
	if _, _, ok := tr.CharToAudio(500); ok {
		t.Error("CharToAudio answered from page anchors")
	}
	// 200 chars per page: char 1000 sits on page 6.
	if page, conf, ok := tr.CharToPage(1000); !ok || page != 6 || conf != 0.7 {
		t.Errorf("CharToPage(1000) = (%d, %v, %v), want (6, 0.7, true)", page, conf, ok)
	}
	if char, _, ok := tr.PageToChar(6); !ok || char != 1000 {
		t.Errorf("PageToChar(6) = (%d, %v), want 1000", char, ok)
	}
}

func TestContradictoryAnchorsAreDropped(t *testing.T) {
	// Unsorted input with a duplicate offset, a backwards pair and a
	// garbage value: only the strictly increasing spine survives.
	tr := New([]Anchor{
		{CharOffset: 3000, Value: 160, Confidence: 0.8},
		{CharOffset: 1000, Value: 60, Confidence: 0.9},
		{CharOffset: 3000, Value: 900, Confidence: 0.1}, // duplicate offset
		{CharOffset: 5000, Value: 100, Confidence: 0.9}, // goes backwards in time
		{CharOffset: 7000, Value: math.NaN(), Confidence: 1},
		{CharOffset: 9000, Value: 460, Confidence: 0.95},
	}, nil)

	if n := len(tr.audio.anchors); n != 3 {
		t.Fatalf("kept %d anchors, want 3: %+v", n, tr.audio.anchors)
	}
	// The surviving map is the fixture, so it interpolates identically.
	if got, _, ok := tr.CharToAudio(2000); !ok || math.Abs(got-110) > 1e-9 {
		t.Errorf("CharToAudio(2000) = (%v, %v), want 110", got, ok)
	}
}

func TestConfidenceIsClampedToUnit(t *testing.T) {
	tr := New([]Anchor{
		{CharOffset: 0, Value: 0, Confidence: 4},
		{CharOffset: 100, Value: 10, Confidence: -1},
	}, nil)

	if _, conf, _ := tr.CharToAudio(0); conf != 1 {
		t.Errorf("confidence 4 became %v, want 1", conf)
	}
	if _, conf, _ := tr.CharToAudio(100); conf != 0 {
		t.Errorf("confidence -1 became %v, want 0", conf)
	}
}

// A larger anchor set exercises the binary search rather than the two- and
// three-anchor cases a linear scan would also get right.
func TestInterpolationAcrossManyAnchors(t *testing.T) {
	anchors := make([]Anchor, 0, 500)
	for i := range 500 {
		anchors = append(anchors, Anchor{CharOffset: i * 100, Value: float64(i) * 7, Confidence: 1})
	}
	tr := New(anchors, nil)

	for i := range 499 {
		mid := i*100 + 50
		want := float64(i)*7 + 3.5
		got, _, ok := tr.CharToAudio(mid)
		if !ok || math.Abs(got-want) > 1e-9 {
			t.Fatalf("CharToAudio(%d) = (%v, %v), want %v", mid, got, ok, want)
		}
	}
}

type stubProvider struct {
	audio, pages []Anchor
	err          error
}

func (s stubProvider) AudioAnchors(context.Context, string) ([]Anchor, error) {
	return s.audio, s.err
}
func (s stubProvider) PageAnchors(context.Context, string) ([]Anchor, error) {
	return s.pages, s.err
}

func TestLoadFromProvider(t *testing.T) {
	tr, err := Load(t.Context(), stubProvider{audio: audioFixture()}, "entry-1")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if !tr.HasAudio() || tr.HasPages() {
		t.Fatalf("loaded maps = audio %v, pages %v", tr.HasAudio(), tr.HasPages())
	}

	// A provider with nothing for an entry — an unaligned book — leaves
	// every method unable to answer, which is what makes the API report
	// derived: false.
	tr, err = Load(t.Context(), stubProvider{}, "entry-1")
	if err != nil {
		t.Fatalf("load empty provider: %v", err)
	}
	if tr.HasAudio() || tr.HasPages() {
		t.Error("an empty provider produced a map")
	}

	// No provider at all is the same honest silence.
	tr, err = Load(t.Context(), nil, "entry-1")
	if err != nil {
		t.Fatalf("load nil provider: %v", err)
	}
	if tr.HasAudio() || tr.HasPages() {
		t.Error("a nil provider produced a map")
	}

	boom := errors.New("anchor store down")
	if _, err := Load(t.Context(), stubProvider{err: boom}, "entry-1"); !errors.Is(err, boom) {
		t.Errorf("load error = %v, want %v", err, boom)
	}
}
