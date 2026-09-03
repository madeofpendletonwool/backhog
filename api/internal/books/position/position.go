// Package position translates one reading position between the three
// coordinate spaces a reader actually moves through: the canonical character
// offset into the normalized EPUB text, the audiobook's global seconds, and
// the printed page of a specific printing.
//
// The character offset is the only stored truth. Audio timestamp and page are
// derived on read, so a position written from the reader and a position
// written from the player can never drift apart — there is one number, not
// three that have to be kept in sync.
//
// Derivation runs off *anchors*: sampled (char offset, value) pairs supplied
// by whatever produced them — forced alignment for audio (Stage 7), OCR page
// scans for print (Stage 9). Between anchors this package interpolates
// linearly. Outside them the two maps part company: the audio map clamps,
// because extrapolating a tape past its ends invents seconds that do not
// exist, while the page map follows the nearest pair's slope, because a
// reader past their last scanned page still wants a page number. Either way
// the confidence is damped and the answer carries an error bar.
//
// Every derivation reports that bar, because linear interpolation is a lie
// that is only locally true: front matter, illustrations and chapter-break
// whitespace all stretch the page axis against the text. Two anchors around
// a point make the bar tight; one anchor two hundred pages away does not,
// and the number says so rather than the UI guessing. Everything here is
// pure: it takes anchors and a number and returns a number, so the awkward
// part of the arena has tests that do not need a database, an EPUB or an
// audiobook.
package position

import (
	"context"
	"math"
	"sort"
)

// outsidePenalty scales the confidence of an answer that fell outside the
// anchor span. Front matter before the first anchor and back matter after
// the last are exactly where linear extrapolation misbehaves, so whether the
// map extrapolated or clamped there, the answer is marked half as
// trustworthy as the anchor it was measured from.
const outsidePenalty = 0.5

// errorBar turns "how far from the nearest anchor" into "how wrong could
// this be", in the units of the axis being answered in. The two rates are
// the drift this package is willing to admit to per unit of distance from
// something measured: interior applies inside the anchor span, where both
// ends of the segment constrain the answer, and exterior outside it, where
// only one does and the error compounds with every page past the last scan.
//
// The rates are deliberately blunt. A real page map drifts because of front
// matter, plates and chapter-break whitespace, none of which are modelled
// anywhere and none of which are worth modelling — the point of the bar is
// that a reader can see at a glance whether the number is worth trusting,
// and a bar derived from a pretend model of typesetting would be no more
// honest than one derived from distance.
type errorBar struct {
	// floor is the smallest bar this axis will ever report for an
	// interpolated answer, in output-axis units.
	floor float64
	// interior and exterior are fractions of the output-axis distance to
	// the nearest anchor, inside and outside the span respectively.
	interior, exterior float64
}

// Page answers are asked for by a person holding the book, so they are the
// bars that get read, and the rate is set by the worst thing that routinely
// happens to a page map: a plate section or a run of chapter-break blank
// pages dropped somewhere inside a gap between scans. A four-page insert
// halfway along a gap throws the linear fit out by three pages, and the bar
// has to cover it, so fifteen percent of the way to the nearest scan inside
// the span and thirty percent per page beyond it.
//
// The mechanic that follows is the point: a dozen scans through a novel
// keep every prediction inside a page or two, while a reader who scanned
// once two hundred pages ago is told in pages exactly how little that one
// scan is now worth.
var (
	pageBar = errorBar{interior: 0.15, exterior: 0.30}
	charBar = errorBar{interior: 0.15, exterior: 0.30}
	// The audio map's bar exists for symmetry and for the same honesty;
	// nothing renders it yet, and alignment anchors are dense enough that
	// it stays small.
	audioBar = errorBar{interior: 0.05, exterior: 0.20}
)

// widen scales an already-rated bar by how much the anchors behind it were
// believed: a segment bounded by two half-trusted anchors is twice as loose
// as one bounded by two certain ones.
func (b errorBar) widen(raw, confidence float64) float64 {
	if raw <= 0 {
		return b.floor
	}
	return math.Max(b.floor, raw*(2-clampUnit(confidence)))
}

// Anchor ties a canonical character offset to a value in one other coordinate
// space: seconds for the audio map, page number for the page map. Confidence
// is the producer's own belief in this pair, in [0,1].
type Anchor struct {
	CharOffset int     `json:"char_offset"`
	Value      float64 `json:"value"`
	Confidence float64 `json:"confidence"`
}

// Provider supplies a library entry's anchors. It is the seam the
// alignment and page-map stages plug into: they add data, not methods.
// A provider that has nothing for an entry returns no anchors and no
// error — an unaligned book is the normal case, not a failure.
type Provider interface {
	AudioAnchors(ctx context.Context, entryID string) ([]Anchor, error)
	PageAnchors(ctx context.Context, entryID string) ([]Anchor, error)
}

// Translator converts one book's positions between coordinate spaces. Build
// one per entry: it holds that entry's anchor maps and nothing else.
type Translator struct {
	audio anchorMap
	page  anchorMap
}

// New builds a translator over the given anchors. Either set may be empty;
// the methods that read an empty map report ok == false.
func New(audioAnchors, pageAnchors []Anchor) *Translator {
	audio, page := newAnchorMap(audioAnchors), newAnchorMap(pageAnchors)
	page.extrapolate = true
	return &Translator{audio: audio, page: page}
}

// Load builds a translator from a provider's anchors for one entry.
func Load(ctx context.Context, p Provider, entryID string) (*Translator, error) {
	if p == nil {
		return New(nil, nil), nil
	}
	audio, err := p.AudioAnchors(ctx, entryID)
	if err != nil {
		return nil, err
	}
	pages, err := p.PageAnchors(ctx, entryID)
	if err != nil {
		return nil, err
	}
	return New(audio, pages), nil
}

// HasAudio reports whether an audio alignment exists for this entry.
func (t *Translator) HasAudio() bool { return len(t.audio.anchors) > 0 }

// HasPages reports whether a page map exists for this entry.
func (t *Translator) HasPages() bool { return len(t.page.anchors) > 0 }

// Translation is one derivation, carried with everything a client needs to
// be honest about it: the value, the confidence of the anchor segment it
// came from, how far the query sat from the nearest anchor on the axis the
// query was made in, and the error bar around the answer itself.
//
// AnchorDistance is zero on an exact anchor, large mid-segment — the number
// that lets a UI say "estimated" instead of implying precision. Margin is
// that distance turned into the units of the answer: "page 214 ± 3". It is
// only meaningful when MarginKnown is set, which needs two anchors — a map
// holding a single anchor knows where one page is and nothing whatsoever
// about how fast pages go by, so it reports no bar rather than a made-up
// one, and the UI says "scan another page" instead of implying a precision
// that does not exist.
type Translation struct {
	Value          float64
	Confidence     float64
	AnchorDistance float64
	Margin         float64
	MarginKnown    bool
}

// CharToAudio derives the audiobook second holding a character offset.
func (t *Translator) CharToAudio(charOffset int) (seconds, confidence float64, ok bool) {
	tr, ok := t.CharToAudioT(charOffset)
	return tr.Value, tr.Confidence, ok
}

// AudioToChar derives the character offset being read at an audiobook second.
func (t *Translator) AudioToChar(seconds float64) (charOffset int, confidence float64, ok bool) {
	tr, ok := t.AudioToCharT(seconds)
	return int(tr.Value), tr.Confidence, ok
}

// CharToPage derives the printed page holding a character offset.
func (t *Translator) CharToPage(charOffset int) (page int, confidence float64, ok bool) {
	tr, ok := t.CharToPageT(charOffset)
	return int(tr.Value), tr.Confidence, ok
}

// PageToChar derives the character offset a printed page starts at.
func (t *Translator) PageToChar(page int) (charOffset int, confidence float64, ok bool) {
	tr, ok := t.PageToCharT(page)
	return int(tr.Value), tr.Confidence, ok
}

// CharToAudioT is CharToAudio with the anchor distance and error bar too.
func (t *Translator) CharToAudioT(charOffset int) (Translation, bool) {
	return t.audio.interpolate(float64(charOffset), anchorChar, anchorValue, clampNonNegative, audioBar)
}

// AudioToCharT is AudioToChar with the anchor distance and error bar too.
func (t *Translator) AudioToCharT(seconds float64) (Translation, bool) {
	return t.audio.interpolate(seconds, anchorValue, anchorChar, roundNonNegative, charBar)
}

// CharToPageT is CharToPage with the anchor distance and error bar too.
func (t *Translator) CharToPageT(charOffset int) (Translation, bool) {
	return t.page.interpolate(float64(charOffset), anchorChar, anchorValue, roundToPage, pageBar)
}

// PageToCharT is PageToChar with the anchor distance and error bar too.
func (t *Translator) PageToCharT(page int) (Translation, bool) {
	return t.page.interpolate(float64(page), anchorValue, anchorChar, roundNonNegative, charBar)
}

// anchorMap is a strictly increasing sequence of anchors, readable in either
// direction. Strict monotonicity in *both* axes is established at
// construction, which is what lets one interpolation routine serve the
// forward and inverse directions and guarantees no zero-length span.
//
// extrapolate is the map's policy past its own ends. The page map sets it:
// a reader thirty pages past their last scan is still somewhere, and the
// nearest pair's slope is the only estimate of where that exists. The audio
// map does not: a tape has a hard end, past which extrapolation produces
// seconds of silence that were never recorded, and clamping to the last
// anchor is the honest answer there.
type anchorMap struct {
	anchors     []Anchor
	extrapolate bool
}

// newAnchorMap sorts the anchors by character offset and drops every one that
// does not strictly advance both axes. A producer that emits a duplicate or a
// backwards pair has said something contradictory about a monotonic mapping
// — reading is monotonic, an audiobook only moves forwards — and the earlier
// anchor is kept because it is the one the anchors before it were validated
// against.
func newAnchorMap(in []Anchor) anchorMap {
	if len(in) == 0 {
		return anchorMap{}
	}
	sorted := make([]Anchor, len(in))
	copy(sorted, in)
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].CharOffset != sorted[j].CharOffset {
			return sorted[i].CharOffset < sorted[j].CharOffset
		}
		return sorted[i].Value < sorted[j].Value
	})

	kept := make([]Anchor, 0, len(sorted))
	for _, a := range sorted {
		if math.IsNaN(a.Value) || math.IsInf(a.Value, 0) || a.CharOffset < 0 {
			continue
		}
		a.Confidence = clampUnit(a.Confidence)
		if n := len(kept); n > 0 {
			prev := kept[n-1]
			if a.CharOffset <= prev.CharOffset || a.Value <= prev.Value {
				continue
			}
		}
		kept = append(kept, a)
	}
	return anchorMap{anchors: kept}
}

// interpolate maps x from the key axis onto the value axis: binary search for
// the bracketing pair, then linear interpolation across it. key and val pick
// which axis is which, so the same code runs both directions. adjust is the
// output axis's own tidying (rounding a discrete coordinate, clamping it to
// the first legal value); the confidence, anchor distance and error bar are
// all decided before it runs, so adjusting the value never polishes the
// honesty.
//
// A hit exactly on an anchor returns that anchor's own value and confidence —
// no interpolation error, no penalty, no bar. Between two anchors the
// confidence is the *lower* of the pair: a segment is only as trustworthy as
// its weaker end. Outside the span the answer either follows the nearest
// pair's slope or clamps to the nearest anchor, depending on the map's
// policy, and its confidence is damped by outsidePenalty either way.
//
// AnchorDistance is always the gap between x and the nearest anchor *on the
// key axis*, in key-axis units — seconds for an audio query, characters for a
// text one — because that is the distance the query actually travelled from
// something measured. Margin is the same gap projected onto the output axis
// and scaled by bar, which is what a person can read: pages, not characters.
func (m anchorMap) interpolate(x float64, key, val func(Anchor) float64, adjust func(float64) float64, bar errorBar) (Translation, bool) {
	n := len(m.anchors)
	if n == 0 || math.IsNaN(x) {
		return Translation{}, false
	}

	if first := m.anchors[0]; x <= key(first) {
		if x == key(first) {
			return Translation{Value: adjust(val(first)), Confidence: first.Confidence, MarginKnown: true}, true
		}
		return m.outside(x, first, m.slopeAt(0, 1, key, val), key(first)-x, key, val, adjust, bar), true
	}
	if last := m.anchors[n-1]; x >= key(last) {
		if x == key(last) {
			return Translation{Value: adjust(val(last)), Confidence: last.Confidence, MarginKnown: true}, true
		}
		return m.outside(x, last, m.slopeAt(n-2, n-1, key, val), x-key(last), key, val, adjust, bar), true
	}

	// Strictly inside the span, so n >= 2 and the invariant
	// key(anchors[lo]) < x < key(anchors[hi]) holds throughout.
	lo, hi := 0, n-1
	for hi-lo > 1 {
		mid := int(uint(lo+hi) >> 1)
		if key(m.anchors[mid]) <= x {
			lo = mid
		} else {
			hi = mid
		}
	}

	a, b := m.anchors[lo], m.anchors[hi]
	if x == key(a) {
		return Translation{Value: adjust(val(a)), Confidence: a.Confidence, MarginKnown: true}, true
	}
	t := (x - key(a)) / (key(b) - key(a))
	value := val(a) + t*(val(b)-val(a))
	confidence := math.Min(a.Confidence, b.Confidence)
	// The bar is measured against the *nearer* end of the segment, so it
	// pinches to nothing at both anchors and peaks in the middle — which is
	// where an interpolation across a stretch of unmeasured typesetting is
	// in fact least sure of itself.
	nearestValue := math.Min(value-val(a), val(b)-value)
	return Translation{
		Value:          adjust(value),
		Confidence:     confidence,
		AnchorDistance: math.Min(x-key(a), key(b)-x),
		Margin:         bar.widen(bar.interior*nearestValue, confidence),
		MarginKnown:    true,
	}, true
}

// outside answers a query that fell past one end of the span: extrapolate
// along slope when the map allows it, clamp to end when it does not. Either
// way the honest bar is the distance the answer *could* have travelled from
// the last measured point — slope times the key-axis gap — because clamping
// hides the drift, it does not remove it.
//
// A map with a single anchor has no slope, so it has no idea how fast the
// output axis moves and cannot bound its own error. That answer goes out
// with MarginKnown false rather than a fabricated bar.
func (m anchorMap) outside(x float64, end Anchor, slope float64, gap float64,
	key, val func(Anchor) float64, adjust func(float64) float64, bar errorBar) Translation {

	out := Translation{
		Value:          adjust(val(end)),
		Confidence:     end.Confidence * outsidePenalty,
		AnchorDistance: gap,
	}
	if slope <= 0 {
		return out
	}

	drift := slope * gap
	if m.extrapolate {
		out.Value = adjust(val(end) + slope*(x-key(end)))
	}
	out.Margin = bar.widen(bar.exterior*drift, end.Confidence)
	out.MarginKnown = true
	return out
}

// slopeAt is the output-axis units per key-axis unit across one pair of
// adjacent anchors, or 0 when the map is too short to have a pair. Both
// axes strictly increase, so a real slope is always positive.
func (m anchorMap) slopeAt(i, j int, key, val func(Anchor) float64) float64 {
	if i < 0 || j >= len(m.anchors) || i >= j {
		return 0
	}
	a, b := m.anchors[i], m.anchors[j]
	return (val(b) - val(a)) / (key(b) - key(a))
}

func anchorChar(a Anchor) float64  { return float64(a.CharOffset) }
func anchorValue(a Anchor) float64 { return a.Value }

// clampUnit folds a producer's confidence back into [0,1].
func clampUnit(v float64) float64 { return math.Min(1, math.Max(0, v)) }

// clampNonNegative floors an audio coordinate at zero: an extrapolation
// before the first anchor may land on a negative second, and there is no
// such thing as the tape at -1s.
func clampNonNegative(v float64) float64 { return math.Max(0, v) }

// roundNonNegative rounds an interpolated coordinate to the integer the
// character axis is counted in. Clamped at zero: an extrapolation back past
// the first anchor may land below the start of the text, and there is no
// such thing as character -1.
func roundNonNegative(v float64) float64 {
	if v <= 0 {
		return 0
	}
	return math.Round(v)
}

// roundToPage rounds onto the page axis, whose first page is 1. Front matter
// is exactly where an extrapolation back past the first anchor lands, and it
// can easily run off the front of the printing; page 0 is not a page anyone
// can turn to, so the answer stops at the first one that is.
func roundToPage(v float64) float64 {
	if v <= 1 {
		return 1
	}
	return math.Round(v)
}
