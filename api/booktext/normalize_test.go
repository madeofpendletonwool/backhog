package booktext

import (
	"strings"
	"testing"
	"unicode"
)

func TestNormalize(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", ""},
		{"plain text unchanged", "the quick brown fox", "the quick brown fox"},
		{"digits kept", "catch 22 and 1984", "catch 22 and 1984"},

		// Quotes: curly, guillemets and ASCII are dropped.
		{"curly double quotes", "“hello world” he said", "hello world he said"},
		{"curly single quotes", "‘nicely’ done", "nicely done"},
		{"ascii double quotes", `he said "hi" and left`, "he said hi and left"},
		{"guillemets", "«voilà» dit elle", "voilà dit elle"},
		{"low quotes", "„guten tag“", "guten tag"},

		// Apostrophes drop and the word joins: transcripts write "dont".
		{"apostrophe curly", "don’t stop", "dont stop"},
		{"apostrophe ascii", "don't stop", "dont stop"},
		{"apostrophe joins word", "it’s a fine day", "its a fine day"},

		// Dashes: every dash family member becomes a word boundary.
		{"em dash", "well—known trick", "well known trick"},
		{"en dash", "pages 12–14", "pages 12 14"},
		{"horizontal bar", "a―b", "a b"},
		{"ascii hyphen", "twenty-three skidoo", "twenty three skidoo"},
		{"minus sign", "3−4", "3 4"},
		{"figure dash", "20‒30", "20 30"},
		{"spaced em dash", "word — word", "word word"},

		// Ligatures fold via NFKC.
		{"fi ligature", "ﬁle under ﬁne", "file under fine"},
		{"fl ligature", "ﬂow after ﬂat", "flow after flat"},
		{"ffi ligature", "oﬃce", "office"},

		// Non-breaking and exotic spaces collapse.
		{"nbsp", "a\u00a0b", "a b"},
		{"narrow nbsp", "a\u202fb", "a b"},
		{"thin space", "a\u2009b", "a b"},

		// Whitespace: runs collapse, newlines become single spaces, edges trim.
		{"multi newline", "one\n\ntwo\n\n\nthree", "one two three"},
		{"tabs and crlf", "a\tb\r\nc", "a b c"},
		{"leading trailing", "  \n pad me \t ", "pad me"},
		{"mixed run", "a \n\t \n b", "a b"},

		// Punctuation is dropped entirely.
		{"sentence punctuation", "Hello, world! Again?", "hello world again"},
		{"ellipsis", "and then… silence.", "and then silence"},
		{"parens brackets", "(aside) [note] {tag}", "aside note tag"},
		{"slashes", "either/or", "eitheror"},
		{"periods in abbreviations", "e.g. i.e. etc.", "eg ie etc"},

		// Case folds; non-ASCII letters survive as letters.
		{"uppercase", "The QUICK Brown", "the quick brown"},
		{"accented letters", "Élan VITAL café", "élan vital café"},
		{"cyrillic", "ЧЕБУРАШКА и Гена", "чебурашка и гена"},
		{"cjk letters", "漢字と ひらがな", "漢字と ひらがな"},
		{"greek question mark drops", "οἴκοι;", "οἴκοι"},

		// Fullwidth forms fold via NFKC.
		{"fullwidth latin", "Ｂｏｏｋ ２", "book 2"},
		{"circled digits", "⑵ continued", "2 continued"},

		{"word-internal digits", "level 7 wizard", "level 7 wizard"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Normalize(tt.in); got != tt.want {
				t.Errorf("Normalize(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// TestNormalizeIdempotent asserts Normalize(Normalize(x)) == Normalize(x) for
// every table case plus long composites — every stored offset in the arena
// depends on this stability.
func TestNormalizeIdempotent(t *testing.T) {
	inputs := []string{
		"",
		"“Don’t—well, ﬁne.” she said…",
		"ONE\u00a0TWO\tthree\n\n—ﬁve—",
		strings.Repeat("It’s a ﬁne—“day”…\n\n", 200),
		"a b c d e f g 1 2 3",
		strings.Repeat("ъentos—éﬂux«»…\r\n\t ", 100),
	}
	for _, in := range inputs {
		once := Normalize(in)
		twice := Normalize(once)
		if once != twice {
			t.Errorf("not idempotent:\n once  = %q\n twice = %q", once, twice)
		}
	}
}

// TestNormalizeOutputAlphabet asserts the invariant the whole arena relies
// on: the canonical text contains only letters, digits and single spaces.
func TestNormalizeOutputAlphabet(t *testing.T) {
	inputs := []string{
		"“Don’t—well, ﬁne.” she said… 42%",
		"Mixed\n\tWHITESPACE—“everywhere”‘’",
		"„Plain‘‘  \u00a0\u2028 ﬂow —",
	}
	for _, in := range inputs {
		out := Normalize(in)
		if out == "" {
			continue
		}
		for i, r := range out {
			switch {
			case unicode.IsLetter(r) || unicode.IsDigit(r):
			case r == ' ':
				if i == 0 || i == len(out)-1 || strings.HasPrefix(out[i+1:], " ") {
					t.Fatalf("bad space placement in %q at %d", out, i)
				}
			default:
				t.Fatalf("unexpected rune %q in %q", r, out)
			}
		}
	}
}
