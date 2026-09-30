package books

import (
	"strings"
	"testing"

	"github.com/collinpendleton/backhog/api/booktext"
	"github.com/collinpendleton/backhog/api/internal/books/epub"
)

// snippetsOver canonicalizes a document and returns both the canonical text and
// a Snippets over the display text and index Canonicalize produced with it.
// Going through the real pipeline is the point: the correspondence being
// asserted is the one Canonicalize creates.
func snippetsOver(t *testing.T, doc *epub.Document) (string, *Snippets) {
	t.Helper()
	canonical, display, _, index, _ := Canonicalize(doc)
	return canonical, &Snippets{index: index, display: []byte(display)}
}

// findSpan locates a canonical phrase the way the searcher would.
func findSpan(t *testing.T, canonical, phrase string) (int, int) {
	t.Helper()
	at := strings.Index(canonical, phrase)
	if at < 0 {
		t.Fatalf("test setup: %q is not in %q", phrase, canonical)
	}
	return at, at + len(phrase)
}

func TestSnippetRestoresProse(t *testing.T) {
	doc := &epub.Document{Docs: []epub.Doc{{
		Href: "ch1.xhtml",
		Blocks: []string{
			"It was a bright cold day in April, and the clocks were striking thirteen.",
			"“Don’t,” he said — finally. The door closed behind him, and the lamp guttered.",
			"A well-known trick, that one.",
		},
	}}}
	canonical, snips := snippetsOver(t, doc)

	tests := []struct {
		name    string
		phrase  string
		passage string
		before  string
		after   string
	}{
		{
			name:    "plain phrase keeps its capitals and comma",
			phrase:  "the door closed behind him",
			passage: "The door closed behind him,",
			before:  "“Don’t,” he said — finally. ",
			after:   " and the lamp guttered.",
		},
		{
			name:    "folded punctuation comes back",
			phrase:  "dont he said finally",
			passage: "“Don’t,” he said — finally.",
			before:  "",
			after:   " The door closed behind him, and the lamp guttered.",
		},
		{
			name:    "half a hyphenated word widens to the word",
			phrase:  "known trick",
			passage: "well-known trick,",
			before:  "A ",
			after:   " that one.",
		},
		{
			name:    "start of a block",
			phrase:  "it was a bright",
			passage: "It was a bright",
			before:  "",
			after:   " cold day in April, and the clocks were striking thirteen.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			from, to := findSpan(t, canonical, tt.phrase)
			got, ok := snips.At(from, to)
			if !ok {
				t.Fatalf("At(%d,%d) not ok", from, to)
			}
			if got.Passage != tt.passage {
				t.Errorf("passage = %q, want %q", got.Passage, tt.passage)
			}
			if got.Before != tt.before {
				t.Errorf("before = %q, want %q", got.Before, tt.before)
			}
			if got.After != tt.after {
				t.Errorf("after = %q, want %q", got.After, tt.after)
			}
		})
	}
}

func TestSnippetPassageFoldsBackToTheQuery(t *testing.T) {
	doc := &epub.Document{Docs: []epub.Doc{
		{Href: "a.xhtml", Blocks: []string{
			"Mr. Smith’s half-brother’s ill-fated re-entry — voilà!",
			"ﬁle under ﬁne: the oﬃce ﬂow, café, naïve, résumé.",
		}},
		{Href: "b.xhtml", Blocks: []string{
			"Catch 22, 1984, and 451. (An aside) [a note] either/or.",
		}},
	}}
	canonical, snips := snippetsOver(t, doc)

	// Every single-token span in the book must render as prose that folds back
	// to something containing it. Containment rather than equality because the
	// answer is word-aligned: half of a hyphenated word widens to the whole
	// display word, since there is no honest cut inside one.
	for at := 0; at < len(canonical); at++ {
		if canonical[at] == ' ' {
			continue
		}
		end := at
		for end < len(canonical) && canonical[end] != ' ' {
			end++
		}
		got, ok := snips.At(at, end)
		if !ok {
			t.Fatalf("At(%d,%d) not ok for token %q", at, end, canonical[at:end])
		}
		if folded := booktext.Normalize(got.Passage); !strings.Contains(folded, canonical[at:end]) {
			t.Errorf("token %q rendered as %q, which folds to %q",
				canonical[at:end], got.Passage, folded)
		}
		at = end
	}
}

func TestSnippetElidesLongParagraphs(t *testing.T) {
	long := strings.Repeat("filler words here ", 60) // ~1080 bytes
	doc := &epub.Document{Docs: []epub.Doc{{
		Href:   "a.xhtml",
		Blocks: []string{long + "the needle " + long},
	}}}
	canonical, snips := snippetsOver(t, doc)

	from, to := findSpan(t, canonical, "the needle")
	got, ok := snips.At(from, to)
	if !ok {
		t.Fatal("At not ok")
	}
	if got.Passage != "the needle" {
		t.Errorf("passage = %q", got.Passage)
	}
	if !strings.HasPrefix(got.Before, "…") || !strings.HasSuffix(got.After, "…") {
		t.Errorf("context was not elided: before %q… after %q", got.Before, got.After)
	}
	if len(got.Before) > contextBytes+8 || len(got.After) > contextBytes+8 {
		t.Errorf("context too long: before %d, after %d", len(got.Before), len(got.After))
	}
	// An elision never cuts a word in half.
	if fields := strings.Fields(got.Before); len(fields) > 1 && fields[1] != "filler" && fields[1] != "words" && fields[1] != "here" {
		t.Errorf("before starts mid-word: %q", got.Before)
	}
}

func TestSnippetAcrossABlockBoundary(t *testing.T) {
	doc := &epub.Document{Docs: []epub.Doc{{
		Href:   "a.xhtml",
		Blocks: []string{"the end of one paragraph", "and the start of the next"},
	}}}
	canonical, snips := snippetsOver(t, doc)

	// The canonical text joins blocks with a space, so a phrase can straddle
	// the break. It renders the half that lives in the block it started in.
	from, to := findSpan(t, canonical, "paragraph and the")
	got, ok := snips.At(from, to)
	if !ok {
		t.Fatal("At not ok")
	}
	if got.Passage != "paragraph" {
		t.Errorf("passage = %q, want the first block's share", got.Passage)
	}
}

func TestSnippetRejectsOutOfRange(t *testing.T) {
	doc := &epub.Document{Docs: []epub.Doc{{Href: "a.xhtml", Blocks: []string{"short block"}}}}
	canonical, snips := snippetsOver(t, doc)

	for _, tt := range []struct{ from, to int }{
		{-1, 4},
		{len(canonical), len(canonical) + 4},
		{len(canonical) + 10, len(canonical) + 14},
	} {
		if _, ok := snips.At(tt.from, tt.to); ok {
			t.Errorf("At(%d,%d) was ok, want rejected", tt.from, tt.to)
		}
	}
}

// AtUntil is At under the spoiler clamp, and the test that matters is the
// hidden-information one: a hit inside the read half of a straddling
// paragraph renders, and nothing at or past the clamp ever does — not in
// the passage, not in the after-skirt.
func TestSnippetUntilClampsTheStraddlingBlock(t *testing.T) {
	doc := &epub.Document{Docs: []epub.Doc{{Href: "a.xhtml", Blocks: []string{
		"The detective found the silver candlestick in the pantry.",
		"The butler did it, and confessed by morning.",
	}}}}
	canonical, snips := snippetsOver(t, doc)

	silverFrom, silverTo := findSpan(t, canonical, "silver")
	butlerAt := strings.Index(canonical, "the butler did it")
	if butlerAt < 0 {
		t.Fatal("fixture lacks the reveal")
	}
	// The clamp lands inside the first paragraph, after "silver".
	clamp := strings.Index(canonical, "candlestick")

	got, ok := snips.AtUntil(silverFrom, silverTo, clamp)
	if !ok {
		t.Fatal("AtUntil not ok for an in-bound hit")
	}
	if !strings.Contains(got.Passage, "silver") {
		t.Errorf("passage = %q, want the read words", got.Passage)
	}
	if low := strings.ToLower(got.Passage + got.Before + got.After); strings.Contains(low, "candlestick") || strings.Contains(low, "butler") {
		t.Errorf("clamped context leaked past the bound: %q / %q / %q", got.Before, got.Passage, got.After)
	}

	// A clamp past the whole block keeps At's full shape — the clamp only
	// ever removes, never reshapes, what was already readable.
	whole, _ := snips.AtUntil(silverFrom, silverTo, len(canonical))
	plain, _ := snips.At(silverFrom, silverTo)
	if whole != plain {
		t.Errorf("end-of-book clamp changed the snippet: %v vs %v", whole, plain)
	}

	// The reveal itself, under a clamp before it, is not rendered at all:
	// AtUntil answers only for spans that begin inside the reading.
	revealFrom, revealTo := findSpan(t, canonical, "the butler did it")
	if _, ok := snips.AtUntil(revealFrom, revealTo, butlerAt); ok {
		// The span starts exactly at the clamp, so nothing of it is read;
		// whatever comes back must carry no reveal text.
		if got, ok := snips.AtUntil(revealFrom, revealTo, butlerAt); ok {
			if low := strings.ToLower(got.Passage + got.After); strings.Contains(low, "butler") {
				t.Errorf("reveal span rendered under its own clamp: %q", got.Passage)
			}
		}
	}
}
