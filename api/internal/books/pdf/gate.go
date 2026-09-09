package pdf

import (
	"fmt"
	"strings"
	"unicode"
)

// The quality gate: garbage extraction is a classification, not a text.
// A PDF whose fonts carry broken or missing ToUnicode maps extracts as
// plausible-looking garbage, and a canonical text built from it would
// silently poison search, passage matching, alignment and the knowledge
// layer — every offset the arena stores would be plausible and wrong. So
// extraction is measured before the document is trusted, and a file whose
// text layer cannot be defended is classified image-native: its content is
// its pages, and it belongs on the paged path, not in the canonical text
// tables.
type gateStats struct {
	runes         int // non-whitespace runes extracted
	unmapped      int // U+FFFD: a glyph code no encoding resolved
	control       int // C0/C1 control runes, the no-ToUnicode signature
	words         int // tokens holding at least one letter or digit
	dictHits      int // tokens found in the common-word list
	pages         int
	pagesWithText int
	imagePages    int
}

// isJunkRune reports the unmapped-glyph evidence: the replacement char a
// ToUnicode miss decodes to, or a control rune a widthless two-byte subset
// code byte-decodes to.
func isJunkRune(r rune) bool {
	return r == unicode.ReplacementChar ||
		(r < 0x20 && r != '\t' && r != '\n' && r != '\r' && r != '\f' && r != '\v') ||
		r == 0x7F || (r >= 0x80 && r <= 0x9F)
}

// observe folds one line's words into the gate's measurements.
func (g *gateStats) observe(words []word) {
	for _, w := range words {
		if w.hasRunes {
			g.words++
			if w.dictHit {
				g.dictHits++
			}
		}
		for _, r := range w.text {
			if unicode.IsSpace(r) {
				continue
			}
			g.runes++
			switch {
			case r == unicode.ReplacementChar:
				g.unmapped++
			case isJunkRune(r):
				g.control++
			}
		}
	}
}

// verdict classifies the extraction.
//
//   - No text layer worth the name → image-native. Comics, scans, and any
//     book whose pages are pictures. The threshold is deliberately tiny:
//     a real book is thousands of runes, and anything under forty is
//     labels and leftovers.
//   - A broken text mapping shows itself in the runes: glyph codes that
//     resolve to nothing become U+FFFD, and two-byte subset codes with no
//     ToUnicode decode byte-wise into control characters. Past a small
//     fraction of the text, the layer is garbage.
//   - Dictionary coverage is the corroborating witness, not the judge: it
//     only counts when unmapped or control runes are already present, so a
//     clean-extracting book in a language the word list does not cover can
//     never be refused by it. Without that guard the gate would trade
//     garbage for a worse bug — refusing every foreign-language PDF.
//
// What deliberately escapes: a one-byte simple font whose codes decode to
// printable garbage with no control or replacement runes. Nothing
// distinguishes that from an exotic-but-honest encoding at this layer —
// it extracts identically in every text extractor — and refusing on a
// hunch would take real books down with it.
func (g *gateStats) verdict() (Classification, string) {
	if g.words == 0 || g.runes < 40 {
		return ImageNative, fmt.Sprintf("no usable text layer: %d rune(s) across %d pages", g.runes, g.pages)
	}
	junk := g.unmapped + g.control
	if ratio := float64(junk) / float64(g.runes); ratio > 0.02 {
		return ImageNative, fmt.Sprintf("broken text mapping: %.1f%% unmapped or control runes", 100*ratio)
	}
	if g.words >= 120 && junk > 0 && float64(g.dictHits)/float64(g.words) < 0.10 {
		return ImageNative, fmt.Sprintf("garbage extraction: %.0f%% dictionary coverage with unmapped runes",
			100*float64(g.dictHits)/float64(g.words))
	}
	return TextNative, ""
}

// commonWords is a compact list of high-frequency English words. Real
// prose runs 45-65% common words; GID-coded garbage extracts near zero.
// The list is intentionally small — coverage measures prose-ness, not
// vocabulary, and function words carry that signal alone.
const commonWordsList = `a about after all also an and any are as at be because been
before but by can could did do does down even first for from get go had has
have he her here hers him his how i if in into is it its just know like
little made make man many may me more most much must my never no not now of
off on one only or other our out over own people said same see she should
since so some still such take than that the their them then there these
they thing think this those three time to too two under until up us use
very was way we well went were what when where which while who why will
with would years you your after again against all always among another
away back because become began begins behind being between both brought
called came come course day days dead dear does done door during each
early end enough even ever every eyes face fact feel felt few find found
full gave girl give go going gone got great hand hands hard head heard
heart held help herself himself home hour hours however important inside
into itself knew know last late leave left less let life light long looked
looking lost love made make man many matter mean men might mind moment
months mother mr mrs mrs much myself near need never next night nothing
now number off often oh old once upon open order own part pass past perhaps
place point poor present quite rather really right round said sat saw say
school second see seem seen several shall she short should show side since
sister small something sometimes soon sound spirit stand started still
story street sure table talk tell term that their them themselves then
there therefore these they thing things think third though thought three
times told took toward turn turned two under until upon use used very voice
want war water way week well went were what whatever when where whether
which while whole whom why window without woman women word work world
would write year years young`

var commonWords = func() map[string]bool {
	m := make(map[string]bool, 350)
	for _, w := range strings.Fields(commonWordsList) {
		m[w] = true
	}
	return m
}()

// foldWord lowers a token and keeps only its letters, the shape a
// dictionary hit is judged on ("Don't" → "dont").
func foldWord(s string) string {
	var sb strings.Builder
	for _, r := range s {
		if unicode.IsLetter(r) {
			sb.WriteRune(unicode.ToLower(r))
		}
	}
	return sb.String()
}
