package booktext

import (
	"strings"
	"testing"
)

// awkwardBlocks are the display strings that make the canonical fold
// interesting: every rule of Normalize that changes a field's length, drops it
// entirely, or splits it in two.
var awkwardBlocks = []string{
	"the quick brown fox",
	"“Don’t,” he said — finally.",
	"a well-known trick, twenty-three skidoo",
	"pages 12–14 and 20‒30, see also 3−4",
	"ﬁle under ﬁne: the oﬃce ﬂow",
	"voilà, dit-elle; «bonjour» — café, naïve, résumé",
	"and then… silence.",
	"(aside) [note] {tag} either/or",
	"a b c d",
	"— — —",
	"He said \"hi\" and left.",
	"one\ttwo   three",
	"Catch 22, 1984, and 451.",
	"…",
	"Mr. Smith's half-brother's ill-fated re-entry.",
}

// TestNormalizedFieldsMatchesNormalize asserts the property SpanInDisplay is
// built on: Normalize distributes over whitespace-separated fields. If this
// ever fails, every span this package hands back is off by however much the
// two disagreed, so it is the load-bearing test of the file.
func TestNormalizedFieldsMatchesNormalize(t *testing.T) {
	for _, block := range awkwardBlocks {
		if got, want := NormalizedFields(block), Normalize(block); got != want {
			t.Errorf("NormalizedFields(%q) = %q, Normalize = %q", block, got, want)
		}
	}
}

func TestSpanInDisplay(t *testing.T) {
	tests := []struct {
		name    string
		display string
		// canonical substring to locate, resolved to offsets below
		canonical string
		want      string
	}{
		{"plain word", "the quick brown fox", "quick", "quick"},
		{"plain phrase", "the quick brown fox", "quick brown", "quick brown"},
		{"first word", "the quick brown fox", "the", "the"},
		{"last word", "the quick brown fox", "fox", "fox"},
		{"whole block", "the quick brown fox", "the quick brown fox", "the quick brown fox"},

		// Punctuation the fold dropped comes back with the display bytes.
		{"quotes restored", "“Don’t,” he said — finally.", "dont", "“Don’t,”"},
		{"across a dropped dash", "“Don’t,” he said — finally.", "he said finally", "he said — finally."},
		{"apostrophe word", "It’s a fine day.", "its", "It’s"},

		// A hyphenated word is two canonical tokens; either half widens to the
		// whole display word, because there is no honest cut inside it.
		{"hyphen first half", "a well-known trick", "well", "well-known"},
		{"hyphen second half", "a well-known trick", "known", "well-known"},
		{"hyphen both halves", "a well-known trick", "well known", "well-known"},
		{"hyphen and neighbour", "a well-known trick", "known trick", "well-known trick"},

		// Ligatures shorten a field; the offsets after it must still line up.
		{"after a ligature", "ﬁle under ﬁne prose", "prose", "prose"},
		{"the ligature itself", "ﬁle under ﬁne prose", "file", "ﬁle"},

		// Exotic spaces fold to separators inside one display field.
		{"exotic space run", "x a b y", "a b", "a b"},

		// Digits and mixed punctuation.
		{"digits", "Catch 22, 1984, and 451.", "1984", "1984,"},
		{"slash joins", "either/or is the choice", "eitheror", "either/or"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			canon := Normalize(tt.display)
			at := strings.Index(canon, tt.canonical)
			if at < 0 {
				t.Fatalf("test setup: %q is not in canonical %q", tt.canonical, canon)
			}
			start, end, ok := SpanInDisplay(tt.display, at, at+len(tt.canonical))
			if !ok {
				t.Fatalf("SpanInDisplay(%q, %d, %d) not ok", tt.display, at, at+len(tt.canonical))
			}
			if got := tt.display[start:end]; got != tt.want {
				t.Errorf("span = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestSpanInDisplayCoversEveryOffset walks every canonical offset of every
// awkward block and requires the span to be well-formed and to contain the
// canonical bytes asked for. This is the fuzz-ish half: it catches
// off-by-one drift that a fixed table would not.
func TestSpanInDisplayCoversEveryOffset(t *testing.T) {
	for _, display := range awkwardBlocks {
		canon := Normalize(display)
		for at, r := range canon {
			if r == ' ' {
				continue // a separator belongs to no field
			}
			start, end, ok := SpanInDisplay(display, at, at+len(string(r)))
			if !ok {
				t.Errorf("%q: offset %d of %q not ok", display, at, canon)
				continue
			}
			if start < 0 || end > len(display) || end <= start {
				t.Errorf("%q: offset %d gave span [%d,%d)", display, at, start, end)
				continue
			}
			// The display span must fold to something holding that character.
			folded := Normalize(display[start:end])
			if !strings.ContainsRune(folded, r) {
				t.Errorf("%q: offset %d (%q) → %q, which folds to %q",
					display, at, string(r), display[start:end], folded)
			}
		}
	}
}

func TestSpanInDisplayRejects(t *testing.T) {
	const display = "the quick brown fox"
	canon := Normalize(display)

	tests := []struct {
		name       string
		start, end int
	}{
		{"empty range", 3, 3},
		{"inverted", 8, 4},
		{"negative", -1, 4},
		{"past the end", 0, len(canon) + 1},
		{"entirely past the end", len(canon) + 5, len(canon) + 9},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, _, ok := SpanInDisplay(display, tt.start, tt.end); ok {
				t.Errorf("SpanInDisplay(%q, %d, %d) was ok, want rejected", display, tt.start, tt.end)
			}
		})
	}

	if _, _, ok := SpanInDisplay("", 0, 1); ok {
		t.Error("SpanInDisplay on an empty block was ok, want rejected")
	}
	if _, _, ok := SpanInDisplay("— — —", 0, 1); ok {
		t.Error("SpanInDisplay on a block that folds away was ok, want rejected")
	}
}
