package pdf_test

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/collinpendleton/backhog/api/internal/books"
	"github.com/collinpendleton/backhog/api/internal/books/epub"
	"github.com/collinpendleton/backhog/api/internal/books/pdf"
	"github.com/collinpendleton/backhog/api/internal/fixtures"
)

// The tests run every fixture through the same spine contract the EPUB and
// MOBI parsers feed: parse to *epub.Document, canonicalize with the real
// books.Canonicalize, and assert the chapter partition invariant 7 backs.
// The fixture bytes come from internal/fixtures, shared with the scanner
// and ingester suites so every layer tests the same files.

func readerAt(t *testing.T, data []byte) *bytes.Reader {
	t.Helper()
	return bytes.NewReader(data)
}

// parseResult runs the parser under test over fixture bytes.
func parseResult(t *testing.T, data []byte) (*pdf.ParseResult, error) {
	t.Helper()
	return pdf.ParseWithQuality(bytes.NewReader(data), int64(len(data)))
}

func TestParseProse(t *testing.T) {
	res, err := parseResult(t, fixtures.BuildProsePDF())
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if res.Class != pdf.TextNative {
		t.Fatalf("class = %q (%s), want text-native", res.Class, res.Reason)
	}
	doc := res.Doc
	if len(doc.Docs) != 4 {
		t.Fatalf("got %d docs, want one per page (4): %+v", len(doc.Docs), doc.Docs)
	}
	for i, d := range doc.Docs {
		if d.SpineIndex != i || d.Href != fmt.Sprintf("pdf:page:%d", i+1) {
			t.Errorf("doc %d spine index/href = %d/%q", i, d.SpineIndex, d.Href)
		}
	}

	// The running head and folio recur on every page and must be gone.
	for i, d := range doc.Docs {
		for _, b := range d.Blocks {
			if strings.Contains(strings.ToLower(b), "synthetic book") {
				t.Errorf("page %d kept its running head: %q", i+1, b)
			}
		}
		if joined := strings.Join(d.Blocks, " "); joined == "1" || joined == "2" || joined == "3" || joined == "4" {
			t.Errorf("page %d kept its folio: %q", i+1, joined)
		}
	}

	// Display type marks the chapter headings.
	if got := doc.Docs[0].Headings; len(got) != 1 || got[0] != 0 {
		t.Errorf("page 1 headings = %v, want [0] (the 18pt chapter line)", got)
	}
	if got := doc.Docs[1].Headings; len(got) != 1 || got[0] != 0 {
		t.Errorf("page 2 headings = %v, want [0]", got)
	}

	// Page 1's blocks: the heading, the dehyphenated paragraph, the
	// gap-separated second paragraph.
	got := doc.Docs[0].Blocks
	want := []string{
		"Chapter One",
		"The first paragraph opens the book with plain words in sentences that a reader can follow, and nothing here is extraordinary until the hyphen joins.",
		"The second paragraph starts after a vertical gap, so the block rule can see the boundary.",
	}
	if strings.Join(got, "\x00") != strings.Join(want, "\x00") {
		t.Errorf("page 1 blocks:\n got %q\nwant %q", got, want)
	}

	// The outline titled the pages.
	wantTitles := []string{"Chapter One", "Chapter Two", "Chapter Three", ""}
	for i, want := range wantTitles {
		if doc.Docs[i].Title != want {
			t.Errorf("doc %d title = %q, want %q", i, doc.Docs[i].Title, want)
		}
	}
	if doc.TOC.Source != "outline" || doc.TOC.Entries != 3 || doc.TOC.Err != "" {
		t.Errorf("toc report = %+v, want outline/3/clean", doc.TOC)
	}
}

// The class sentinels are the routing contract: the ingester and the text
// endpoints name each refusal with errors.Is, never by matching message
// text.
func TestNotTextErrorSentinels(t *testing.T) {
	_, imageErr := parseResult(t, fixtures.BuildImageOnlyPDF())
	if !errors.Is(imageErr, pdf.ErrImageNative) {
		t.Errorf("image-native error = %v, want errors.Is pdf.ErrImageNative", imageErr)
	}
	if errors.Is(imageErr, pdf.ErrCorrupt) {
		t.Errorf("image-native error also claims corrupt: %v", imageErr)
	}
	corruptErr := func() error {
		res, err := parseResult(t, fixtures.BuildCorruptPDF())
		if res != nil {
			t.Fatal("corrupt parse returned a result")
		}
		return err
	}()
	if !errors.Is(corruptErr, pdf.ErrCorrupt) {
		t.Errorf("corrupt error = %v, want errors.Is pdf.ErrCorrupt", corruptErr)
	}
	if !errors.Is(fmt.Errorf("wrapped: %w", imageErr), pdf.ErrImageNative) {
		t.Error("sentinel does not survive wrapping")
	}
}

func TestParseTwoColumnReadingOrder(t *testing.T) {
	res, err := parseResult(t, fixtures.BuildTwoColumnPDF())
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	blocks := res.Doc.Docs[0].Blocks
	if len(blocks) != 2 {
		t.Fatalf("got %d blocks, want 2 (one per column): %q", len(blocks), blocks)
	}
	if !strings.HasPrefix(blocks[0], "Left column opens") || !strings.HasSuffix(blocks[0], "reading order.") {
		t.Errorf("left column block = %q", blocks[0])
	}
	if !strings.HasPrefix(blocks[1], "The right column follows") || !strings.HasSuffix(blocks[1], "left column.") {
		t.Errorf("right column block = %q", blocks[1])
	}
}

// TestDehyphenationFrozen checks the acceptance property: no "hyphen ated"
// split survives into the canonical output on the fixture set. The in-page
// joins ("extraordi-/nary", "twenty-/three") and the cross-page join
// ("ordi-"/"nary") must all come out as single words.
func TestDehyphenationFrozen(t *testing.T) {
	res, err := parseResult(t, fixtures.BuildProsePDF())
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	canonical, _, _, _ := books.Canonicalize(res.Doc)
	for _, want := range []string{"extraordinary", "twentythree", "mid word ordinary"} {
		if !strings.Contains(canonical, want) {
			t.Errorf("canonical text missing %q:\n%q", want, canonical)
		}
	}
	for _, frozen := range []string{"extraordi nary", "twenty three inside", "ordi nary"} {
		if strings.Contains(canonical, frozen) {
			t.Errorf("canonical text froze a hyphenated split as %q:\n%q", frozen, canonical)
		}
	}
}

func TestParseImageOnly(t *testing.T) {
	data := fixtures.BuildImageOnlyPDF()
	res, err := parseResult(t, data)
	if err == nil {
		t.Fatal("image-only parse returned no gate error")
	}
	var nte *pdf.NotTextError
	if !errors.As(err, &nte) || nte.Class != pdf.ImageNative {
		t.Fatalf("error = %v, want image-native NotTextError", err)
	}
	if res == nil || res.Class != pdf.ImageNative {
		t.Fatalf("result class = %+v, want image-native", res)
	}
	if len(res.Doc.Docs) != 2 {
		t.Fatalf("got %d docs, want 2", len(res.Doc.Docs))
	}
	for i, d := range res.Doc.Docs {
		if len(d.Blocks) != 0 {
			t.Errorf("page %d invented %d blocks from images: %q", i+1, len(d.Blocks), d.Blocks)
		}
		if len(d.Images) != 1 || d.Images[0].BeforeBlock != 0 {
			t.Errorf("page %d images = %+v, want one at block 0", i+1, d.Images)
		}
	}
	// The plain Parse entry point refuses rather than returning the doc.
	if _, err := pdf.Parse(readerAt(t, data), int64(len(data))); !errors.As(err, &nte) || nte.Class != pdf.ImageNative {
		t.Fatalf("Parse error = %v, want image-native refusal", err)
	}
}

func TestParseGarbageToUnicode(t *testing.T) {
	res, err := parseResult(t, fixtures.BuildGarbagePDF())
	if err == nil {
		t.Fatal("garbage parse returned no gate error")
	}
	var nte *pdf.NotTextError
	if !errors.As(err, &nte) || nte.Class != pdf.ImageNative {
		t.Fatalf("error = %v, want image-native NotTextError", err)
	}
	if !strings.Contains(nte.Reason, "unmapped") {
		t.Errorf("reason = %q, want the broken-mapping diagnosis", nte.Reason)
	}
	// Never a stored text: the garbage fixture's document holds nothing a
	// reader could mistake for prose.
	if res.Doc != nil {
		for i, d := range res.Doc.Docs {
			for _, b := range d.Blocks {
				if !strings.ContainsRune(b, '\ufffd') && !strings.ContainsRune(b, 0) {
					t.Errorf("page %d produced prose-looking block %q", i+1, b)
				}
			}
		}
	}
}

func TestParseEncryptedRefused(t *testing.T) {
	for name, locked := range map[string]bool{"locked": true, "restricted": false} {
		t.Run(name, func(t *testing.T) {
			data, err := fixtures.BuildEncryptedPDF(locked)
			if err != nil {
				t.Fatal(err)
			}
			res, err := parseResult(t, data)
			if !errors.Is(err, pdf.ErrDRM) {
				t.Fatalf("error = %v, want pdf.ErrDRM", err)
			}
			if res != nil {
				t.Errorf("result = %+v, want nil", res)
			}
			if _, err := pdf.Parse(readerAt(t, data), int64(len(data))); !errors.Is(err, pdf.ErrDRM) {
				t.Fatalf("Parse error = %v, want pdf.ErrDRM", err)
			}
		})
	}
}

func TestParseCorrupt(t *testing.T) {
	res, err := parseResult(t, fixtures.BuildCorruptPDF())
	if err == nil {
		t.Fatal("corrupt parse returned no error")
	}
	var nte *pdf.NotTextError
	if !errors.As(err, &nte) || nte.Class != pdf.Corrupt {
		t.Fatalf("error = %v, want corrupt NotTextError", err)
	}
	if res != nil {
		t.Errorf("result = %+v, want nil", res)
	}
}

// TestSpineContractAndPartition runs the interchangeability property: PDF
// fixtures parse into the same *epub.Document shape the EPUB and MOBI
// parsers emit, canonicalize through the same books.Canonicalize, and the
// resulting chapters partition [0, charCount) exactly — contiguous,
// gapless, non-overlapping, reassembling the canonical text. The same
// property assertions the mobi parse runs.
func TestSpineContractAndPartition(t *testing.T) {
	prose, err := parseResult(t, fixtures.BuildProsePDF())
	if err != nil {
		t.Fatalf("prose: %v", err)
	}
	columns, err := parseResult(t, fixtures.BuildTwoColumnPDF())
	if err != nil {
		t.Fatalf("columns: %v", err)
	}
	for name, doc := range map[string]*epub.Document{
		"prose":   prose.Doc,
		"columns": columns.Doc,
	} {
		t.Run(name, func(t *testing.T) { runPartition(t, doc) })
	}

	// The prose fixture's chapter shape: three outline-titled chapters,
	// page four absorbed by chapter three.
	canonical, _, chapters, index := books.Canonicalize(prose.Doc)
	if len(chapters) != 3 {
		t.Fatalf("got %d chapters, want 3: %+v", len(chapters), chapters)
	}
	for i, want := range []string{"Chapter One", "Chapter Two", "Chapter Three"} {
		if chapters[i].Title != want || chapters[i].TitleSource != books.TitleSourceTOC {
			t.Errorf("chapter %d = %q/%q, want %q/toc", i, chapters[i].Title, chapters[i].TitleSource, want)
		}
	}
	if index.CharCount != len(canonical) {
		t.Errorf("index char count %d != %d", index.CharCount, len(canonical))
	}
}

// runPartition is assertContiguous plus the reassembly check: chapters
// cover [0, len) exactly and concatenate back to the canonical text.
func runPartition(t *testing.T, doc *epub.Document) {
	t.Helper()
	canonical, _, chapters, _ := books.Canonicalize(doc)
	if len(chapters) == 0 {
		t.Fatal("no chapters")
	}
	if chapters[0].CharStart != 0 {
		t.Errorf("first chapter starts at %d, want 0", chapters[0].CharStart)
	}
	for i := 1; i < len(chapters); i++ {
		if chapters[i].CharStart != chapters[i-1].CharEnd {
			t.Errorf("gap/overlap between chapters %d (ends %d) and %d (starts %d)",
				i-1, chapters[i-1].CharEnd, i, chapters[i].CharStart)
		}
	}
	if last := chapters[len(chapters)-1]; last.CharEnd != len(canonical) {
		t.Errorf("last chapter ends at %d, want %d", last.CharEnd, len(canonical))
	}
	var rebuilt strings.Builder
	for _, ch := range chapters {
		rebuilt.WriteString(canonical[ch.CharStart:ch.CharEnd])
	}
	if rebuilt.String() != canonical {
		t.Errorf("chapter ranges do not reassemble the canonical text:\n got %q\nwant %q", rebuilt.String(), canonical)
	}
}

// TestUnnamedPDFFallsBackToPages: with no outline, the page documents are
// the book's only structure — the fallback chapter marks the issue names.
func TestUnnamedPDFFallsBackToPages(t *testing.T) {
	res, err := parseResult(t, fixtures.BuildTwoColumnPDF())
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if res.Doc.TOC.Source != "pdf-page" {
		t.Errorf("toc source = %q, want pdf-page", res.Doc.TOC.Source)
	}
	_, _, chapters, _ := books.Canonicalize(res.Doc)
	if len(chapters) != 1 {
		t.Errorf("got %d chapters, want 1 for a single page: %+v", len(chapters), chapters)
	}
}
