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
	got := BuildStyles(doc)
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
