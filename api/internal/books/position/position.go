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
// linearly; outside them it clamps and says so by damping the confidence it
// returns. Everything here is pure: it takes anchors and a number and returns
// a number, so the awkward part of the arena has tests that do not need a
// database, an EPUB or an audiobook.
package position

import (
	"context"
	"math"
	"sort"
)

// outsidePenalty scales the confidence of an answer that fell outside the
// anchor span. Front matter before the first anchor and back matter after the
// last are exactly where linear extrapolation misbehaves — extrapolating an
// audiobook backwards from its first anchor lands on a negative timestamp —
// so the answer is clamped to the nearest anchor and marked half as
// trustworthy rather than invented.
const outsidePenalty = 0.5

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
	return &Translator{audio: newAnchorMap(audioAnchors), page: newAnchorMap(pageAnchors)}
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

// CharToAudio derives the audiobook second holding a character offset.
func (t *Translator) CharToAudio(charOffset int) (seconds, confidence float64, ok bool) {
	v, c, ok := t.audio.interpolate(float64(charOffset), anchorChar, anchorValue)
	if !ok {
		return 0, 0, false
	}
	return math.Max(0, v), c, true
}

// AudioToChar derives the character offset being read at an audiobook second.
func (t *Translator) AudioToChar(seconds float64) (charOffset int, confidence float64, ok bool) {
	v, c, ok := t.audio.interpolate(seconds, anchorValue, anchorChar)
	if !ok {
		return 0, 0, false
	}
	return roundNonNegative(v), c, true
}

// CharToPage derives the printed page holding a character offset.
func (t *Translator) CharToPage(charOffset int) (page int, confidence float64, ok bool) {
	v, c, ok := t.page.interpolate(float64(charOffset), anchorChar, anchorValue)
	if !ok {
		return 0, 0, false
	}
	return roundNonNegative(v), c, true
}

// PageToChar derives the character offset a printed page starts at.
func (t *Translator) PageToChar(page int) (charOffset int, confidence float64, ok bool) {
	v, c, ok := t.page.interpolate(float64(page), anchorValue, anchorChar)
	if !ok {
		return 0, 0, false
	}
	return roundNonNegative(v), c, true
}

// anchorMap is a strictly increasing sequence of anchors, readable in either
// direction. Strict monotonicity in *both* axes is established at
// construction, which is what lets one interpolation routine serve the
// forward and inverse directions and guarantees no zero-length span.
type anchorMap struct {
	anchors []Anchor
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
		a.Confidence = math.Min(1, math.Max(0, a.Confidence))
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
// which axis is which, so the same code runs both directions.
//
// A hit exactly on an anchor returns that anchor's own value and confidence —
// no interpolation error, no penalty. Between two anchors the confidence is
// the *lower* of the pair: a segment is only as trustworthy as its weaker
// end. Outside the span the answer is clamped to the nearest anchor and its
// confidence damped by outsidePenalty.
func (m anchorMap) interpolate(x float64, key, val func(Anchor) float64) (float64, float64, bool) {
	n := len(m.anchors)
	if n == 0 || math.IsNaN(x) {
		return 0, 0, false
	}

	if first := m.anchors[0]; x <= key(first) {
		if x == key(first) {
			return val(first), first.Confidence, true
		}
		return val(first), first.Confidence * outsidePenalty, true
	}
	if last := m.anchors[n-1]; x >= key(last) {
		if x == key(last) {
			return val(last), last.Confidence, true
		}
		return val(last), last.Confidence * outsidePenalty, true
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
		return val(a), a.Confidence, true
	}
	t := (x - key(a)) / (key(b) - key(a))
	return val(a) + t*(val(b)-val(a)), math.Min(a.Confidence, b.Confidence), true
}

func anchorChar(a Anchor) float64  { return float64(a.CharOffset) }
func anchorValue(a Anchor) float64 { return a.Value }

// roundNonNegative rounds an interpolated coordinate to the integer the
// discrete axes (character offset, page number) are counted in. Clamped at
// zero: an interpolation may land marginally below the first anchor, and
// there is no such thing as character -1.
func roundNonNegative(v float64) int {
	if v <= 0 {
		return 0
	}
	return int(math.Round(v))
}
