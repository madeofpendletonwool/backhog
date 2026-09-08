package books

import (
	"fmt"
	"strings"
	"testing"

	"github.com/collinpendleton/backhog/api/internal/books/epub"
)

// doc builds one spine document. headings lists the block indexes that came
// from heading markup.
func doc(href, title string, headings []int, blocks ...string) epub.Doc {
	return epub.Doc{Href: href, Title: title, Headings: headings, Blocks: blocks}
}

// filler is a block of prose long enough that a chapter holding it is past
// the "this is only a heading file" threshold.
func filler(n int) string { return strings.Repeat("word ", n) }

// grouped runs the real canonicalizer and reports each chapter as
// "title|source".
func grouped(t *testing.T, d *epub.Document) []string {
	t.Helper()
	_, _, chapters, index := Canonicalize(d)
	if len(chapters) != len(index.Documents) {
		t.Fatalf("chapters %d != index documents %d", len(chapters), len(index.Documents))
	}
	out := make([]string, len(chapters))
	for i, c := range chapters {
		out[i] = fmt.Sprintf("%s|%s", c.Title, c.TitleSource)
	}
	return out
}

func eq(t *testing.T, got, want []string) {
	t.Helper()
	if strings.Join(got, " / ") != strings.Join(want, " / ") {
		t.Errorf("chapters:\n got %q\nwant %q", got, want)
	}
}

// TestGroupCalibreSplit is the Chamber of Secrets shape: the TOC points at a
// document holding only the chapter heading, and the chapter itself is the
// next document, which the TOC never mentions. The two are one chapter.
func TestGroupCalibreSplit(t *testing.T) {
	d := &epub.Document{TOC: epub.TOCReport{Source: "ncx", Entries: 2}, Docs: []epub.Doc{
		doc("ch1head.xhtml", "CHAPTER ONE", nil, "Harry Potter and the Chamber of Secrets"),
		doc("ch1body.xhtml", "", nil, "CHAPTER ONE", filler(400)),
		doc("ch2head.xhtml", "CHAPTER TWO", nil, "Harry Potter and the Chamber of Secrets"),
		doc("ch2body.xhtml", "", nil, "CHAPTER TWO", filler(400)),
	}}
	eq(t, grouped(t, d), []string{"CHAPTER ONE|toc", "CHAPTER TWO|toc"})
}

// TestGroupSplitWithoutRepeatedHeading is the same shape when the body does
// not reprint the heading: the stub is still not a chapter on its own.
func TestGroupSplitWithoutRepeatedHeading(t *testing.T) {
	d := &epub.Document{TOC: epub.TOCReport{Source: "ncx", Entries: 1}, Docs: []epub.Doc{
		doc("head.xhtml", "Chapter One", nil, "running header"),
		doc("body.xhtml", "", nil, filler(400)),
	}}
	eq(t, grouped(t, d), []string{"Chapter One|toc"})
}

// TestGroupTitlesFromMarkup is The Stand: the NCX names nothing, but every
// document opens with its own heading.
func TestGroupTitlesFromMarkup(t *testing.T) {
	d := &epub.Document{TOC: epub.TOCReport{Source: "ncx", Entries: 1}, Docs: []epub.Doc{
		doc("c01.xhtml", "", []int{0}, "THE CIRCLE OPENS", filler(400)),
		// No heading markup at all, so the opening line is judged on shape.
		doc("c02.xhtml", "", nil, "CHAPTER 1", filler(400)),
		doc("c03.xhtml", "", nil, "CHAPTER 2", filler(400)),
	}}
	eq(t, grouped(t, d), []string{
		"THE CIRCLE OPENS|heading", "CHAPTER 1|text", "CHAPTER 2|text",
	})
}

// TestGroupPagebreakFragments is A Man Without a Country: a MOBI cut at page
// breaks, where one chapter spans several unnamed runs.
func TestGroupPagebreakFragments(t *testing.T) {
	d := &epub.Document{TOC: epub.TOCReport{Source: "mobi-pagebreak"}, Docs: []epub.Doc{
		doc("mobi:chapter:0", "", []int{0}, "1", filler(200)),
		doc("mobi:chapter:1", "", nil, filler(200)),
		doc("mobi:chapter:2", "", nil, filler(200)),
		doc("mobi:chapter:3", "", []int{0}, "2", filler(200)),
		doc("mobi:chapter:4", "", nil, filler(200)),
	}}
	eq(t, grouped(t, d), []string{"1|heading", "2|heading"})
}

// TestGroupDoesNotSwallowTheBook is Our Mutual Friend and Lullaby: one
// document happens to look titled and the rest of the novel does not. The
// named chapter must not absorb the whole book.
func TestGroupDoesNotSwallowTheBook(t *testing.T) {
	docs := []epub.Doc{doc("d0.xhtml", "", nil, "Chapter 1", filler(20_000))}
	for i := 1; i < 12; i++ {
		docs = append(docs, doc(fmt.Sprintf("d%d.xhtml", i), "", nil, filler(20_000)))
	}
	got := grouped(t, &epub.Document{TOC: epub.TOCReport{Source: "ncx"}, Docs: docs})
	if len(got) < 5 {
		t.Errorf("12 documents collapsed to %d chapters (%q); the size bound should keep them apart", len(got), got)
	}
	if got[0] != "Chapter 1|text" {
		t.Errorf("first chapter = %q, want %q", got[0], "Chapter 1|text")
	}
	for _, c := range got[1:] {
		if c != "|none" {
			t.Errorf("unnamed chapter reported as %q, want %q", c, "|none")
		}
	}
}

// TestGroupUnnamedBookKeepsItsDocuments is Neverwhere's MOBI: nothing in the
// book is named, so its documents are the only structure it has and each one
// stays a chapter rather than fusing into a single unreadable slab.
func TestGroupUnnamedBookKeepsItsDocuments(t *testing.T) {
	var docs []epub.Doc
	for i := range 8 {
		docs = append(docs, doc(fmt.Sprintf("d%d", i), "", nil, filler(500)))
	}
	got := grouped(t, &epub.Document{TOC: epub.TOCReport{Source: "mobi-pagebreak"}, Docs: docs})
	if len(got) != 8 {
		t.Errorf("got %d chapters, want 8 (one per document): %q", len(got), got)
	}
}

// TestGroupShoutedDialogueIsNotAHeading is the Reaper Man guard. Pratchett
// writes DEATH in capitals; a chapter title rule that trusted letter case
// anywhere in a document would find a heading on every line he speaks.
func TestGroupShoutedDialogueIsNotAHeading(t *testing.T) {
	d := &epub.Document{TOC: epub.TOCReport{Source: "ncx", Entries: 1}, Docs: []epub.Doc{
		doc("c1.xhtml", "Chapter One", nil,
			filler(200), "I AM NOT KNOWN FOR MY SENSE OF FUN.", filler(200),
			"YES. IT WILL BE A GREAT ADVENTURE.", filler(200)),
	}}
	eq(t, grouped(t, d), []string{"Chapter One|toc"})
}

// TestLooksLikeHeading pins the shape rule that only ever runs on a
// document's opening line.
func TestLooksLikeHeading(t *testing.T) {
	for _, s := range []string{"CHAPTER 1", "Chapter Nine", "17", "XIV", "PART TWO", "THE CIRCLE OPENS"} {
		if !looksLikeHeading(s) {
			t.Errorf("looksLikeHeading(%q) = false, want true", s)
		}
	}
	for _, s := range []string{
		"It was the best of times, it was the worst of times.",
		"He said nothing at all.",
		"",
		"NEVERTHELESS, I AM GOING TO DIE. THERE IS NO APPEAL. I SHALL NOT.",
	} {
		if looksLikeHeading(s) {
			t.Errorf("looksLikeHeading(%q) = true, want false", s)
		}
	}
}

// TestSameTitle covers the comparison that recognises a heading reprinted
// inside the chapter it names.
func TestSameTitle(t *testing.T) {
	if !sameTitle("CHAPTER ONE", "Chapter One.") {
		t.Error("case and trailing punctuation should not separate two spellings of one title")
	}
	if sameTitle("", "") {
		t.Error("two absent titles are not the same title")
	}
	if sameTitle("Chapter One", "Chapter Two") {
		t.Error("different chapters compared equal")
	}
}
