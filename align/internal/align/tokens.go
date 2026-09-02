package align

import (
	"github.com/collinpendleton/backhog/api/booktext"
)

// canonToken is one word of the canonical text together with its byte
// offset in it. That offset is the whole point of this package: every
// anchor the worker produces is one of these offsets paired with a moment
// on the audiobook's global timeline.
type canonToken struct {
	Start int
	Word  string
}

// tokenizeCanonical splits the canonical text into words without
// re-normalizing it. That restraint is deliberate: booktext.Normalize is
// idempotent, so re-running it should be a no-op — but "should" is not good
// enough when the result is every stored offset in the arena. The bytes on
// disk are the bytes the reader positions were computed against, so they are
// the bytes the offsets are measured in.
func tokenizeCanonical(text string) []canonToken {
	out := make([]canonToken, 0, len(text)/6+1)
	start := -1
	for i := 0; i < len(text); i++ {
		switch text[i] {
		case ' ', '\n', '\r', '\t', '\v', '\f':
			if start >= 0 {
				out = append(out, canonToken{Start: start, Word: text[start:i]})
				start = -1
			}
		default:
			if start < 0 {
				start = i
			}
		}
	}
	if start >= 0 {
		out = append(out, canonToken{Start: start, Word: text[start:]})
	}
	return out
}

// matchTok is one word in the matching vocabulary — after numeral and
// abbreviation folding — carrying the index of whatever it came from. On
// the canonical side src is a canonToken index; on the transcript side it
// is a segment index. Folding is not one-to-one ("23" becomes two words),
// which is exactly why the back-reference has to be carried rather than
// recomputed from a position.
type matchTok struct {
	Word string
	Src  int32
}

// abbreviations folds the handful of forms a book abbreviates and a
// narrator does not (or the reverse) onto one token each. The list is
// deliberately short and conservative: every entry here is a word whose
// spelled-out form is unambiguous in ordinary prose. "no." for "number",
// "gen." for "general" and "in." for "inches" are all absent for that
// reason — they collide with common words, and a bad fold costs more than
// a missed one.
//
// "st" is the interesting case: it stands for both Saint and Street, so
// both spellings fold onto it. That is lossy in principle and harmless in
// practice — a single token either way, decided by the fifty around it.
var abbreviations = map[string]string{
	"mister": "mr", "mr": "mr",
	"missus": "mrs", "missis": "mrs", "mrs": "mrs",
	"doctor": "dr", "dr": "dr",
	"professor": "prof", "prof": "prof",
	"reverend": "rev", "rev": "rev",
	"captain": "capt", "capt": "capt",
	"colonel": "col", "col": "col",
	"lieutenant": "lt", "lt": "lt",
	"sergeant": "sgt", "sgt": "sgt",
	"junior": "jr", "jr": "jr",
	"senior": "sr", "sr": "sr",
	"saint": "st", "street": "st", "st": "st",
	"mount": "mt", "mt": "mt",
	"avenue": "ave", "ave": "ave",
	"road": "rd", "rd": "rd",
	"boulevard": "blvd", "blvd": "blvd",
	"versus": "vs", "vs": "vs",
	"etcetera": "etc", "etc": "etc",
}

// appendFolded writes the match-vocabulary form of one normalized word.
func appendFolded(dst []string, word string) []string {
	if folded, ok := abbreviations[word]; ok {
		return append(dst, folded)
	}
	if out, ok := appendNumeral(dst, word); ok {
		return out
	}
	return append(dst, word)
}

// foldCanonical builds the canonical side's match tokens, each pointing
// back at the canonToken (and therefore the byte offset) it came from.
func foldCanonical(tokens []canonToken) []matchTok {
	out := make([]matchTok, 0, len(tokens)+len(tokens)/8)
	var scratch []string
	for i, t := range tokens {
		scratch = appendFolded(scratch[:0], t.Word)
		for _, w := range scratch {
			out = append(out, matchTok{Word: w, Src: int32(i)})
		}
	}
	return out
}

// transcript is the Whisper side reduced to the same vocabulary: one flat
// run of match tokens plus the index that says which segment each stretch
// belongs to. Segments keep their order, which is what makes the whole
// alignment a monotone problem.
type transcript struct {
	toks []matchTok
	// bounds[i] is the half-open token range of segment i: [bounds[i],
	// bounds[i+1]). It has len(segments)+1 entries.
	bounds []int
}

// newTranscript normalizes and folds every segment. Segments that
// normalize to nothing (music cues, a lone "uh") keep their place in the
// index with an empty range rather than being dropped, so segment indexes
// still line up with the caller's slice.
func newTranscript(segments []Segment) *transcript {
	t := &transcript{
		toks:   make([]matchTok, 0, len(segments)*16),
		bounds: make([]int, len(segments)+1),
	}
	var scratch []string
	for i, s := range segments {
		t.bounds[i] = len(t.toks)
		for _, word := range fields(booktext.Normalize(s.Text)) {
			scratch = appendFolded(scratch[:0], word)
			for _, w := range scratch {
				t.toks = append(t.toks, matchTok{Word: w, Src: int32(i)})
			}
		}
	}
	t.bounds[len(segments)] = len(t.toks)
	return t
}

// fields splits a normalized string on its single spaces. strings.Fields
// would do, but the input's shape is known exactly and this avoids the
// unicode.IsSpace call per byte.
func fields(s string) []string {
	if s == "" {
		return nil
	}
	out := make([]string, 0, len(s)/6+1)
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == ' ' {
			if i > start {
				out = append(out, s[start:i])
			}
			start = i + 1
		}
	}
	if len(s) > start {
		out = append(out, s[start:])
	}
	return out
}
