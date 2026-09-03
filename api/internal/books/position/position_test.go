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

// --- page interpolation and error bars ------------------------------------

// pageFixture is a two-anchor page map at a flat 200 characters per page:
// page 1 at the start of the text, page 11 two thousand characters in.
func pageFixture() []Anchor {
	return []Anchor{
		{CharOffset: 0, Value: 1, Confidence: 1},
		{CharOffset: 2000, Value: 11, Confidence: 1},
	}
}

func TestPageErrorBarWithNoAnchors(t *testing.T) {
	tr := New(audioFixture(), nil)
	if tr.HasPages() {
		t.Fatal("an empty page map reports itself populated")
	}
	got, ok := tr.CharToPageT(1000)
	if ok {
		t.Fatalf("CharToPageT answered from an empty map: %+v", got)
	}
	if got.MarginKnown || got.Margin != 0 {
		t.Errorf("empty map reported a bar: %+v", got)
	}
}

func TestPageErrorBarWithOneAnchor(t *testing.T) {
	// One anchor fixes where a single page is and says nothing at all about
	// how fast pages go by, so every other offset gets that page back with
	// no bar rather than a fabricated one.
	tr := New(nil, []Anchor{{CharOffset: 4000, Value: 200, Confidence: 1}})

	on, ok := tr.CharToPageT(4000)
	if !ok {
		t.Fatal("CharToPageT on the anchor: not ok")
	}
	if on.Value != 200 || !on.MarginKnown || on.Margin != 0 {
		t.Errorf("on the anchor = %+v, want page 200 ± 0", on)
	}

	for _, offset := range []int{0, 1000, 400_000} {
		got, ok := tr.CharToPageT(offset)
		if !ok {
			t.Fatalf("char %d: not ok", offset)
		}
		if got.Value != 200 {
			t.Errorf("char %d = page %v, want the only anchor's page 200", offset, got.Value)
		}
		if got.MarginKnown {
			t.Errorf("char %d claimed a bar of ±%v from a single anchor", offset, got.Margin)
		}
		closeTo(t, "confidence", got.Confidence, outsidePenalty)
	}
}

func TestPageErrorBarBetweenTwoAnchors(t *testing.T) {
	tr := New(nil, pageFixture())

	// Dead centre of the segment: five pages from either anchor, so the bar
	// is the interior rate over that distance.
	mid, ok := tr.CharToPageT(1000)
	if !ok {
		t.Fatal("CharToPageT mid-segment: not ok")
	}
	closeTo(t, "page", mid.Value, 6)
	closeTo(t, "margin", mid.Margin, pageBar.interior*5)
	if !mid.MarginKnown {
		t.Error("a bracketed answer reported no bar")
	}

	// The bar pinches to nothing at the anchors themselves: those pages
	// were looked at, not derived.
	for _, offset := range []int{0, 2000} {
		got, ok := tr.CharToPageT(offset)
		if !ok || !got.MarginKnown || got.Margin != 0 {
			t.Errorf("char %d = %+v, want an exact anchor with no bar", offset, got)
		}
	}

	// And it grows monotonically towards the middle of the segment.
	previous := 0.0
	for offset := 0; offset <= 1000; offset += 100 {
		got, _ := tr.CharToPageT(offset)
		if got.Margin < previous {
			t.Fatalf("char %d: bar shrank towards mid-segment (%v after %v)", offset, got.Margin, previous)
		}
		previous = got.Margin
	}
}

func TestPageErrorBarWithDenseAnchors(t *testing.T) {
	// A scanned page every ten printed pages. Dense anchors are the whole
	// point of the mechanic: the bar has to visibly tighten as they arrive.
	dense := make([]Anchor, 0, 20)
	for i := range 20 {
		dense = append(dense, Anchor{CharOffset: i * 2000, Value: float64(1 + i*10), Confidence: 1})
	}
	tight := New(nil, dense)
	loose := New(nil, []Anchor{dense[0], dense[len(dense)-1]})

	for _, offset := range []int{15_000, 19_000, 27_500} {
		near, _ := tight.CharToPageT(offset)
		far, _ := loose.CharToPageT(offset)
		if near.Value != far.Value {
			t.Errorf("char %d: dense map answered page %v, sparse map page %v — a linear map should agree",
				offset, near.Value, far.Value)
		}
		if !(near.Margin < far.Margin) {
			t.Errorf("char %d: dense map's bar ±%v is not tighter than the sparse map's ±%v",
				offset, near.Margin, far.Margin)
		}
		if near.Margin > 1 {
			t.Errorf("char %d: a scan every ten pages should bound the answer inside a page, got ±%v",
				offset, near.Margin)
		}
	}
}

func TestPageExtrapolatesPastTheAnchorSpan(t *testing.T) {
	tr := New(nil, pageFixture())

	// Two hundred characters per page, two thousand characters past the last
	// anchor: ten more pages, with the exterior rate over that drift and half
	// the anchor's confidence.
	after, ok := tr.CharToPageT(4000)
	if !ok {
		t.Fatal("CharToPageT past the span: not ok")
	}
	closeTo(t, "page", after.Value, 21)
	closeTo(t, "margin", after.Margin, pageBar.exterior*10)
	closeTo(t, "confidence", after.Confidence, outsidePenalty)
	closeTo(t, "anchor distance", after.AnchorDistance, 2000)
	if !after.MarginKnown {
		t.Error("an extrapolated answer reported no bar")
	}

	// The bar keeps widening the further out it goes — one scan is worth
	// less the further you walk from it.
	far, _ := tr.CharToPageT(20_000)
	if far.Margin <= after.Margin {
		t.Errorf("bar did not widen with distance: ±%v at char 20000 vs ±%v at char 4000",
			far.Margin, after.Margin)
	}
}

func TestPageExtrapolationStopsAtPageOne(t *testing.T) {
	// Front matter: the first scanned page is page 21, a long way into the
	// text, and extrapolating back past it runs off the front of the
	// printing. There is no page 0 to turn to.
	tr := New(nil, []Anchor{
		{CharOffset: 4000, Value: 21, Confidence: 1},
		{CharOffset: 6000, Value: 31, Confidence: 1},
	})
	got, ok := tr.CharToPageT(0)
	if !ok {
		t.Fatal("CharToPageT before the span: not ok")
	}
	if got.Value != 1 {
		t.Errorf("char 0 = page %v, want the extrapolation clamped to page 1", got.Value)
	}
	if !got.MarginKnown || got.Margin <= 0 {
		t.Errorf("an extrapolation back into front matter reported no bar: %+v", got)
	}
}

func TestPageToCharExtrapolatesToo(t *testing.T) {
	tr := New(nil, pageFixture())

	// Page 21 is ten pages past the last anchor at 200 characters each.
	got, ok := tr.PageToCharT(21)
	if !ok {
		t.Fatal("PageToCharT past the span: not ok")
	}
	closeTo(t, "char offset", got.Value, 4000)
	if !got.MarginKnown || got.Margin <= 0 {
		t.Errorf("an extrapolated offset reported no bar: %+v", got)
	}
}

func TestAudioStillClampsPastItsAnchors(t *testing.T) {
	// The tape has a hard end: extrapolating past the last alignment anchor
	// would invent seconds that were never recorded. Audio clamps where the
	// page map extrapolates, and says so with a bar instead.
	tr := New(audioFixture(), nil)

	last := audioFixture()[2]
	got, ok := tr.CharToAudioT(20_000)
	if !ok {
		t.Fatal("CharToAudioT past the span: not ok")
	}
	closeTo(t, "seconds", got.Value, last.Value)
	if !got.MarginKnown || got.Margin <= 0 {
		t.Errorf("a clamped audio answer reported no bar: %+v", got)
	}
}

// TestThreeScansPredictTheFourth is the arena's real acceptance criterion in
// miniature: scan three pages spread through a book whose page map is *not*
// linear — front matter at the start, a run of plates in the middle — and
// check that the fourth, unscanned page comes back inside the bar the map
// said it would.
func TestThreeScansPredictTheFourth(t *testing.T) {
	// The truth: 250 characters per printed page, 18 pages of front matter
	// before the text starts, and a four-page plate insert at page 150 that
	// holds no text at all.
	const charsPerPage, frontMatter, plateAt, plates = 250.0, 18, 150, 4
	pageOf := func(offset int) float64 {
		page := float64(frontMatter) + float64(offset)/charsPerPage
		if page > plateAt {
			page += plates
		}
		return math.Round(page)
	}
	offsetOf := func(page float64) int {
		if page > plateAt+plates {
			page -= plates
		}
		return int((page - float64(frontMatter)) * charsPerPage)
	}

	scanned := []float64{40, 190, 320}
	anchors := make([]Anchor, 0, len(scanned))
	for _, page := range scanned {
		anchors = append(anchors, Anchor{
			CharOffset: offsetOf(page), Value: page, Confidence: 1,
		})
	}
	tr := New(nil, anchors)

	// Every page between and beyond the scans, checked against the truth.
	for page := 20.0; page <= 400; page += 5 {
		offset := offsetOf(page)
		got, ok := tr.CharToPageT(offset)
		if !ok {
			t.Fatalf("page %v (char %d): not ok", page, offset)
		}
		if !got.MarginKnown {
			t.Fatalf("page %v: three scans should bound the answer", page)
		}
		want := pageOf(offset)
		// The bar is a bar, not a promise of the exact page: a page of
		// rounding on either side is inside "page N ± m".
		if math.Abs(got.Value-want) > got.Margin+1 {
			t.Errorf("char %d: predicted page %v ± %v, truth is page %v — outside the stated bar",
				offset, got.Value, got.Margin, want)
		}
	}
}
