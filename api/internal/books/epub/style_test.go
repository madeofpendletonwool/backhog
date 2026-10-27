package epub

import (
	"reflect"
	"testing"
)

// A chapter opener the way production pipelines mark one up: the section
// asserts "chapter", the number and title are <h1>s, the subtitle is an
// ARIA doc-subtitle, the epigraph a blockquote with epub:type, the body
// carries a small-caps lead-in, italics and a span-class small cap.
const opener = `<html><body><section epub:type="chapter" role="doc-chapter">
<header><p class="SP"> </p><h1 class="CN"><a href="#"><b>1.</b></a></h1></header>
<h1 class="CTT"><b>THE OPENING</b></h1>
<header><p class="CST" role="doc-subtitle"><b>A Subtitle</b></p></header>
<blockquote epub:type="epigraph"><p>First line,<br/>second line.</p><p>—A POET</p></blockquote>
<p class="CO"><small>IT BEGAN</small> on a <i>Tuesday</i>, said <span class="ePub-B-SC">COLBERT</span>.</p>
<hr class="transition"/>
<section epub:type="division"><h2 class="H1"><b><i>A Section</i></b></h2>
<ul><li>One point</li></ul><p>Plain.</p></section>
</section></body></html>`

func TestStyledExtractionKeepsTheBlocksAndAddsTheTypography(t *testing.T) {
	blocks, headings, styles := ExtractStyledHTML(opener)
	plain, plainHeadings := ExtractBlocksHTML(opener)
	if !reflect.DeepEqual(blocks, plain) || !reflect.DeepEqual(headings, plainHeadings) {
		t.Fatalf("styled extraction changed the blocks:\n%q\n%q", blocks, plain)
	}
	if len(styles) != len(blocks) {
		t.Fatalf("%d styles for %d blocks", len(styles), len(blocks))
	}

	want := []struct {
		kind  string
		level int
		brk   bool
	}{
		{KindHeading, 1, false},  // 1.
		{KindHeading, 1, false},  // THE OPENING
		{KindSubtitle, 0, false}, // A Subtitle
		{KindEpigraph, 0, false}, // First line, second line.
		{KindEpigraph, 0, false}, // —A POET
		{"", 0, false},           // IT BEGAN …
		{KindHeading, 2, true},   // A Section, after the <hr>
		{KindItem, 0, false},     // One point
		{"", 0, false},           // Plain.
	}
	if len(styles) != len(want) {
		t.Fatalf("blocks = %q", blocks)
	}
	for i, w := range want {
		got := styles[i]
		if got.Kind != w.kind || got.Level != w.level || got.Break != w.brk {
			t.Errorf("block %d %q = %s/%d/%v, want %s/%d/%v",
				i, blocks[i], got.Kind, got.Level, got.Break, w.kind, w.level, w.brk)
		}
	}

	// The verse keeps its line break as a marked space.
	if !hasRun(styles[3].Runs, " ", "n") {
		t.Errorf("epigraph runs = %+v, want a line-break space", styles[3].Runs)
	}
	// Small caps from <small> and from a span class; italics from <i>.
	body := styles[5].Runs
	for _, w := range []Run{{"IT BEGAN", "s"}, {"Tuesday", "i"}, {"COLBERT", "bs"}} {
		if !hasRun(body, w.Text, w.Marks) {
			t.Errorf("body runs = %+v, want %+v", body, w)
		}
	}
}

func TestContainerTitlesDoNotMakeHeadingsOfTheirContents(t *testing.T) {
	_, _, styles := ExtractStyledHTML(`<div class="chapter"><p>Just prose.</p></div>`)
	if len(styles) != 1 || styles[0].Kind != "" {
		t.Errorf("styles = %+v, want one plain paragraph", styles)
	}
	_, _, styles = ExtractStyledHTML(`<p class="ct">Chapter Title</p>`)
	if len(styles) != 1 || styles[0].Kind != KindHeading || styles[0].Level != 1 {
		t.Errorf("styles = %+v, want a level-1 heading", styles)
	}
}

func TestClassMarksMatchWholeParts(t *testing.T) {
	_, _, styles := ExtractStyledHTML(`<p><span class="block_2 calibre1">x</span> <span class="italic">y</span></p>`)
	if !hasRun(styles[0].Runs, "x ", "") || !hasRun(styles[0].Runs, "y", "i") {
		t.Errorf("runs = %+v", styles[0].Runs)
	}
}

func hasRun(runs []Run, text, marks string) bool {
	for _, r := range runs {
		if r.Text == text && r.Marks == marks {
			return true
		}
	}
	return false
}
