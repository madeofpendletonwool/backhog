// Package books holds the canonical-text machinery shared by every position
// in the Books arena: the pinned normalizer, the EPUB ingester that builds
// the canonical text and its offset indexes, and the parser version that
// invalidates stored offsets when the rules below change.
//
// # The canonical text
//
// Every position — reader location, alignment anchor, OCR page anchor — is a
// byte offset into one normalized canonical text per EPUB. Offsets are byte
// offsets into the UTF-8 encoding of that text (Go string indexing), not rune
// counts.
//
// # Normalize rules (pinned)
//
// Normalize is the single function applied to EPUB text, Whisper transcripts
// and OCR output before any matching or offset math. It must stay
// deterministic and idempotent forever: every stored offset in the arena
// assumes it. Changing behaviour here requires bumping ParserVersion so
// stored texts are re-parsed and stale offsets rebuilt.
//
// The rules, in order:
//
//  1. Unicode NFKC normalization. This folds compatibility forms:
//     ligatures (ﬁ → fi), non-breaking spaces (U+00A0 → space),
//     fullwidth Latin (Ａ → A), and friends.
//  2. Lowercase (simple Unicode case folding via strings.ToLower).
//  3. Quote folding: the curly/guillemet quote families (‘ ’ ‚ ‛ “ ” „ « »)
//     are dropped. Quotes mark speech, not content; transcripts and OCR
//     render them inconsistently or not at all, and they cluster at word
//     boundaries where dropping is safe.
//  4. Dash folding: the dash/hyphen family (‐ ‑ ‒ – — ― − and ASCII -)
//     becomes a space. Speech renders an em-dash as a word boundary
//     ("well—known" is spoken "well known"), so a space — never a join —
//     is the faithful fold.
//  5. Apostrophes (ASCII ' and ’) are dropped, not spaced: transcripts
//     write "dont" for "don't", so the words must join.
//  6. Everything that is not a Unicode letter or digit is dropped.
//     Punctuation carries no alignment signal and is the noisiest
//     dimension in both transcripts and OCR.
//  7. Whitespace runs collapse to a single space; leading/trailing
//     whitespace is trimmed. The canonical text contains only letters,
//     digits and single spaces.
//
// Non-ASCII letters and digits (é, ü, я, 漢) are kept as-is after NFKC;
// the canonical form is not ASCII-only.
package books

import (
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// ParserVersion identifies the normalizer/extractor behaviour a canonical
// text was produced with. Bump it whenever Normalize or the EPUB block
// extraction changes: rows parsed under an older version are re-parsed on
// next access, rebuilding every derived offset.
const ParserVersion = "1"

// quoteRunes are dropped by rule 3 (quotes mark speech, not content).
var quoteRunes = map[rune]bool{
	'‘': true, '’': true, '‚': true, '‛': true,
	'“': true, '”': true, '„': true, '‟': true,
	'«': true, '»': true, '‹': true, '›': true,
	'"': true,
}

// dashRunes become a space by rule 4 (speech renders dashes as word
// boundaries).
var dashRunes = map[rune]bool{
	'-': true, '‐': true, '‑': true, '‒': true,
	'–': true, '—': true, '―': true, '−': true,
}

// Normalize applies the pinned rules from the package comment. It is pure,
// deterministic and idempotent: Normalize(Normalize(s)) == Normalize(s).
func Normalize(s string) string {
	s = norm.NFKC.String(s)
	s = strings.ToLower(s)

	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case dashRunes[r]:
			b.WriteByte(' ')
		case quoteRunes[r] || r == '\'':
			// Dropped: quotes and apostrophes carry no alignment signal.
		case r == ' ' || r == '\t' || r == '\n' || r == '\r' || r == '\v' || r == '\f' || unicode.IsSpace(r):
			b.WriteByte(' ')
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
		default:
			// Every other character is dropped.
		}
	}
	return collapseSpaces(b.String())
}

// collapseSpaces shrinks runs of spaces to one and trims the ends. The input
// only ever contains single-byte spaces by construction.
func collapseSpaces(s string) string {
	fields := strings.Split(s, " ")
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		if f != "" {
			out = append(out, f)
		}
	}
	return strings.Join(out, " ")
}
