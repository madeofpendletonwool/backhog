package books

import (
	"reflect"
	"testing"

	"github.com/collinpendleton/backhog/api/internal/books/epub"
)

func TestDisplayRunsCollapseLikeTheDisplayText(t *testing.T) {
	raw := []epub.Run{
		{Text: "  THE ", Marks: "b"}, {Text: "\n NAME  ", Marks: "b"},
		{Text: " of the "}, {Text: "game", Marks: "i"}, {Text: " ", Marks: "n"}, {Text: " next line  "},
	}
	want := "THE NAME of the game next line"
	got := displayRuns(raw, want)
	expect := []epub.Run{
		{Text: "THE NAME ", Marks: "b"}, {Text: "of the "},
		{Text: "game", Marks: "i"}, {Text: " ", Marks: "n"}, {Text: "next line"},
	}
	if !reflect.DeepEqual(got, expect) {
		t.Errorf("runs = %+v\nwant %+v", got, expect)
	}
	if displayRuns([]epub.Run{{Text: "plain  text"}}, "plain text") != nil {
		t.Error("an unmarked block should carry no runs")
	}
	if displayRuns([]epub.Run{{Text: "x", Marks: "b"}}, "y") != nil {
		t.Error("runs that do not reproduce the display text must be dropped")
	}
}

func TestBuildStylesFollowsCanonicalBlocks(t *testing.T) {
	doc := &epub.Document{Docs: []epub.Doc{
		{Blocks: []string{"Plain one.", "Plain two."}},
		{
			Blocks: []string{"Title", "* * *", "After the break."},
			Styles: []epub.BlockStyle{{Kind: epub.KindHeading, Level: 1}, {}, {}},
		},
	}}
	index := &BlockIndex{Documents: []IndexedDoc{
		{SpineIndex: 0, Blocks: []int{0, 11}},
		{SpineIndex: 1, Blocks: []int{22, 28}},
	}}
	got := BuildStyles(doc, index)
	if _, ok := got.Docs["0"]; ok {
		t.Error("a document of plain paragraphs should be omitted")
	}
	styles := got.Docs["1"]
	// The typed scene break normalizes away, so its pause moves onto the
	// next surviving block and the list stays parallel to the display text.
	if len(styles) != 2 || styles[0].Kind != epub.KindHeading || styles[1] == nil || !styles[1].Break {
		t.Fatalf("styles = %+v", styles)
	}
}

// The regression behind StylesVersion 2: the block index folds short spine
// documents into the next one (a cover page into chapter one), and the
// styles must be keyed and concatenated the same way, or the display
// endpoint finds a list for the wrong document — or none at all.
func TestBuildStylesFoldsDocumentsLikeTheIndex(t *testing.T) {
	doc := &epub.Document{Docs: []epub.Doc{
		{Blocks: []string{"Cover"}},
		{
			Blocks: []string{"Chapter One", "Prose."},
			Styles: []epub.BlockStyle{{Kind: epub.KindHeading, Level: 1}, {}},
		},
		{Blocks: []string{"Two", "More."}, Styles: []epub.BlockStyle{{Kind: epub.KindHeading, Level: 1}, {}}},
	}}
	index := &BlockIndex{Documents: []IndexedDoc{
		{SpineIndex: 0, Blocks: []int{0, 6, 18}}, // cover + chapter one, folded
		{SpineIndex: 2, Blocks: []int{25, 29}},
	}}
	got := BuildStyles(doc, index)
	first := got.Docs["0"]
	if len(first) != 3 || first[0] != nil || first[1] == nil || first[1].Kind != epub.KindHeading || first[2] != nil {
		t.Fatalf("folded doc styles = %+v", first)
	}
	if second := got.Docs["2"]; len(second) != 2 || second[0].Kind != epub.KindHeading {
		t.Fatalf("second doc styles = %+v", second)
	}
	if _, ok := got.Docs["1"]; ok {
		t.Error("a folded-away spine index must not get a list of its own")
	}

	// A document whose count disagrees loses its styles; the rest keep theirs.
	index.Documents[0].Blocks = []int{0, 6}
	got = BuildStyles(doc, index)
	if _, ok := got.Docs["0"]; ok {
		t.Error("a mismatched document should carry no styles")
	}
	if _, ok := got.Docs["2"]; !ok {
		t.Error("the matching document should keep its styles")
	}
}
