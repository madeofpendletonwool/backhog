package booktext

import "strings"

// SpanInDisplay maps a byte span of a block's canonical text back onto the
// display text of that same block.
//
// The two texts are built together, block for block, by the EPUB ingester:
// the display text is the book's own characters and the canonical text is
// Normalize applied to them, which is what makes every stored offset in the
// arena addressable and unreadable at the same time. Anything that wants to
// *show* a reader where an offset landed has to come back across that fold,
// and this is the crossing.
//
// It works because Normalize is a per-character map followed by a whitespace
// collapse, so it distributes over whitespace-separated fields:
//
//	Normalize(block) == strings.Join(nonEmpty(Normalize(field) for field), " ")
//
// which is asserted directly by the tests. Walking the display fields and
// accumulating the canonical bytes each one contributes therefore reconstructs
// the exact canonical range of every display field, whatever the folding did to
// it: a lone em-dash contributes nothing, "well-known" contributes two tokens
// and the space between them, "don't" contributes one shorter token.
//
// The answer is field-aligned — it widens to whole words rather than cutting
// one in half — because the fold is not byte-for-byte inside a word and a
// highlight that ends mid-word would be claiming a precision that does not
// survive the crossing.
//
// canonStart and canonEnd are byte offsets into Normalize(display). ok is false
// for a range that is empty, inverted, or outside that text.
func SpanInDisplay(display string, canonStart, canonEnd int) (start, end int, ok bool) {
	if canonStart < 0 || canonEnd <= canonStart {
		return 0, 0, false
	}

	start, end = -1, -1
	canon := 0   // canonical bytes emitted so far
	emitted := 0 // fields that contributed canonical bytes

	for _, f := range displayFields(display) {
		n := Normalize(display[f.start:f.end])
		if n == "" {
			// Folded away entirely — a lone dash, a bare ellipsis. It owns no
			// canonical bytes and no separator, so it is invisible here.
			continue
		}
		if emitted > 0 {
			canon++ // the single space Normalize left between the two fields
		}
		fieldStart, fieldEnd := canon, canon+len(n)
		canon = fieldEnd
		emitted++

		if start < 0 && fieldEnd > canonStart {
			start = f.start
		}
		if fieldStart < canonEnd {
			end = f.end
		}
	}

	if start < 0 || end <= start || canonEnd > canon {
		return 0, 0, false
	}
	return start, end, true
}

// field is one whitespace-separated run of a display block, in display bytes.
type field struct{ start, end int }

// displayFields is strings.Fields that keeps the offsets. The canonical text
// contains only single spaces, so the fields of the display text are exactly
// the units Normalize joins back together.
func displayFields(s string) []field {
	var out []field
	start := -1
	for i := 0; i < len(s); i++ {
		if isDisplaySpace(s[i]) {
			if start >= 0 {
				out = append(out, field{start, i})
				start = -1
			}
			continue
		}
		if start < 0 {
			start = i
		}
	}
	if start >= 0 {
		out = append(out, field{start, len(s)})
	}
	return out
}

// isDisplaySpace splits on ASCII whitespace only. Exotic Unicode spaces are
// left inside a field on purpose: Normalize folds them to a separator itself,
// so splitting on them here too would count the same boundary twice.
func isDisplaySpace(b byte) bool {
	switch b {
	case ' ', '\t', '\n', '\r', '\v', '\f':
		return true
	}
	return false
}

// NormalizedFields joins the per-field normalizations the way SpanInDisplay
// assumes they compose. It exists so the assumption can be asserted against
// Normalize itself rather than trusted.
func NormalizedFields(s string) string {
	var parts []string
	for _, f := range displayFields(s) {
		if n := Normalize(s[f.start:f.end]); n != "" {
			parts = append(parts, n)
		}
	}
	return strings.Join(parts, " ")
}
