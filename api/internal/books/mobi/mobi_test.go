package mobi

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
	"testing"

	mobigo "github.com/madeofpendletonwool/mobi-go"

	"github.com/collinpendleton/backhog/api/internal/books/epub"
	"github.com/collinpendleton/backhog/api/internal/fixtures"
)

// buildMOBI6 assembles a minimal valid uncompressed MOBI6 container around
// text: the PalmDB wrapper, a record-0 carrying the PalmDOC and MOBI headers
// mobi-go validates, and the text split into raw records. The INDX index
// stays absent, so the TOC comes from the legacy <toc> block in the text (or
// from pagebreaks when the text has neither). It is the byte-level inverse
// of the parts of the format this parser branches on, authored here so the
// tests control every offset exactly — the richer containers (PalmDOC,
// HUFF/CDIC, INDX, KF8, DRM) are the committed mobi-go fixtures below.
func buildMOBI6(text string, encoding uint32) []byte {
	const recordSize = 4096
	var records [][]byte
	for start := 0; ; start += recordSize {
		end := min(start+recordSize, len(text))
		records = append(records, []byte(text[start:end]))
		if end >= len(text) {
			break
		}
	}

	title := "Test Book"
	rec0 := make([]byte, 248+len(title))
	binary.BigEndian.PutUint16(rec0[0:], 1) // compression: none
	binary.BigEndian.PutUint32(rec0[4:], uint32(len(text)))
	binary.BigEndian.PutUint16(rec0[8:], uint16(len(records)))
	binary.BigEndian.PutUint16(rec0[10:], recordSize)
	copy(rec0[16:], "MOBI")
	binary.BigEndian.PutUint32(rec0[20:], 232) // MOBI header length
	binary.BigEndian.PutUint32(rec0[24:], 2)   // type: book
	binary.BigEndian.PutUint32(rec0[28:], encoding)
	binary.BigEndian.PutUint32(rec0[36:], 6) // version: MOBI6
	binary.BigEndian.PutUint32(rec0[84:], uint32(248))
	binary.BigEndian.PutUint32(rec0[88:], uint32(len(title)))
	binary.BigEndian.PutUint32(rec0[108:], 0xFFFFFFFF) // first image: absent
	binary.BigEndian.PutUint32(rec0[112:], 0xFFFFFFFF) // HUFF/CDIC: absent
	binary.BigEndian.PutUint32(rec0[168:], 0xFFFFFFFF) // DRM offset: absent
	binary.BigEndian.PutUint32(rec0[244:], 0xFFFFFFFF) // INDX: absent → legacy TOC
	copy(rec0[248:], title)

	all := append([][]byte{rec0}, records...)

	var out bytes.Buffer
	name := "Test Book"
	out.WriteString(name)
	out.Write(make([]byte, 60-len(name))) // name NUL-padded, then the unused header fields
	out.WriteString("BOOKMOBI")           // type at 60, creator at 64
	out.Write(make([]byte, 76-out.Len()))
	binary.Write(&out, binary.BigEndian, uint16(len(all)))
	offset := 78 + 8*len(all)
	for i, rec := range all {
		binary.Write(&out, binary.BigEndian, uint32(offset))
		var id [4]byte
		binary.BigEndian.PutUint32(id[:], uint32(i))
		out.Write(id[:])
		offset += len(rec)
	}
	for _, rec := range all {
		out.Write(rec)
	}
	return out.Bytes()
}

// legacyTOCText assembles a MOBI6 HTML document whose <toc> block carries
// the real byte offsets of the chapter headings — two passes with
// fixed-width filepos values so the second pass cannot shift anything.
func legacyTOCText(chapters []string) string {
	build := func(offs []string) string {
		var b strings.Builder
		b.WriteString("<html><head></head><body><toc>")
		for i, ch := range chapters {
			title := ch[strings.Index(ch, "<h1>")+4 : strings.Index(ch, "</h1>")]
			fmt.Fprintf(&b, `<tocpoint filepos="%s" tocdepth="1">%s</tocpoint>`, offs[i], title)
		}
		b.WriteString("</toc>")
		for _, ch := range chapters {
			b.WriteString("<mbp:pagebreak/>")
			b.WriteString(ch)
		}
		b.WriteString("</body></html>")
		return b.String()
	}

	dummy := make([]string, len(chapters))
	for i := range dummy {
		dummy[i] = "00000000"
	}
	text := build(dummy)
	real := make([]string, len(chapters))
	for i := range chapters {
		// Chapter i's content starts right after the (i+1)th pagebreak;
		// walking the pagebreaks in order yields each heading's offset.
		pos := 0
		for range i + 1 {
			pos += strings.Index(text[pos:], "<mbp:pagebreak/>") + len("<mbp:pagebreak/>")
		}
		real[i] = fmt.Sprintf("%08d", pos)
	}
	return build(real)
}

func parseBytes(t *testing.T, data []byte) (*epub.Document, error) {
	t.Helper()
	return Parse(bytes.NewReader(data), int64(len(data)))
}

func TestParseLegacyTOC(t *testing.T) {
	chapters := []string{
		"<h1>Alpha</h1><p>First things first.</p>",
		"<h1>Beta</h1><p>Second chapter, with a cross reference.</p>",
		"<h1>Gamma</h1><p>Third chapter wraps up.</p>",
	}
	data := buildMOBI6(legacyTOCText(chapters), 65001)

	doc, err := parseBytes(t, data)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	// Four chapters: the front matter before the first heading, then one
	// per TOC entry. The <toc> labels land in the front matter's only
	// block; the TOC entries themselves title the real chapters.
	if len(doc.Docs) != 4 {
		t.Fatalf("got %d docs, want 4: %+v", len(doc.Docs), doc.Docs)
	}
	wantTitles := []string{"", "Alpha", "Beta", "Gamma"}
	for i, want := range wantTitles {
		if doc.Docs[i].Title != want {
			t.Errorf("doc %d title = %q, want %q", i, doc.Docs[i].Title, want)
		}
	}
	if got := strings.Join(doc.Docs[1].Blocks, " / "); got != "Alpha / First things first." {
		t.Errorf("chapter 1 blocks = %q", got)
	}
	if got := strings.Join(doc.Docs[3].Blocks, " / "); got != "Gamma / Third chapter wraps up." {
		t.Errorf("chapter 3 blocks = %q", got)
	}
}

func TestParsePagebreakFallback(t *testing.T) {
	text := "<html><body>" +
		"<h1>One</h1><p>Uno.</p><mbp:pagebreak/>" +
		"<h1>Two</h1><p>Dos.</p><mbp:pagebreak/>" +
		"<h1>Three</h1><p>Tres.</p>" +
		"</body></html>"
	data := buildMOBI6(text, 65001)

	doc, err := parseBytes(t, data)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(doc.Docs) != 3 {
		t.Fatalf("got %d docs, want 3 (one per pagebreak section): %+v", len(doc.Docs), doc.Docs)
	}
	for i, d := range doc.Docs {
		if d.Title != "" {
			t.Errorf("doc %d titled %q with no TOC in the book", i, d.Title)
		}
	}
	if got := strings.Join(doc.Docs[0].Blocks, " / "); got != "One / Uno." {
		t.Errorf("section 0 blocks = %q", got)
	}
	if got := strings.Join(doc.Docs[2].Blocks, " / "); got != "Three / Tres." {
		t.Errorf("section 2 blocks = %q", got)
	}
}

func TestParseCP1252(t *testing.T) {
	text := "<html><body><h1>Caf\xe9</h1><p>Un caf\xe9 s'il vous pla\xeet.</p></body></html>"
	data := buildMOBI6(text, 1252)

	doc, err := parseBytes(t, data)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(doc.Docs) != 1 {
		t.Fatalf("got %d docs, want 1", len(doc.Docs))
	}
	want := []string{"Café", "Un café s'il vous plaît."}
	if len(doc.Docs[0].Blocks) != len(want) {
		t.Fatalf("blocks = %q", doc.Docs[0].Blocks)
	}
	for i, w := range want {
		if doc.Docs[0].Blocks[i] != w {
			t.Errorf("block %d = %q, want %q", i, doc.Docs[0].Blocks[i], w)
		}
	}
}

// The committed fixtures are byte-identical to mobi-go's oracle-verified
// corpus; they exercise the real containers: PalmDOC records with trailing
// bookkeeping, EXTH metadata, INDX NCX, KF8 reassembly, DRM refusal.
func TestParsePalmDOCFixture(t *testing.T) {
	doc, err := parseBytes(t, fixtures.MOBI6Palmdoc)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(doc.Docs) != 4 {
		t.Fatalf("got %d docs, want 4 (one per NCX entry): %+v", len(doc.Docs), doc.Docs)
	}
	wantTitles := []string{"Begin Reading", "Synthetic PalmDOC", "Second Chapter", "Third Chapter"}
	for i, want := range wantTitles {
		if doc.Docs[i].Title != want {
			t.Errorf("doc %d title = %q, want %q", i, doc.Docs[i].Title, want)
		}
	}
	joined := strings.Join(doc.Docs[1].Blocks, " / ")
	if !strings.Contains(joined, "Synthetic PalmDOC / This book exists to be parsed.") {
		t.Errorf("chapter 1 blocks = %q", joined)
	}
	joined = strings.Join(doc.Docs[3].Blocks, " / ")
	if !strings.Contains(joined, "Third Chapter / See the beginning.") {
		t.Errorf("chapter 3 blocks = %q", joined)
	}
}

func TestParseKF8Fixture(t *testing.T) {
	doc, err := parseBytes(t, fixtures.AZW3KF8)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	// Three linear sections; the non-linear colophon is skipped exactly as
	// the EPUB spine skips linear="no" documents.
	if len(doc.Docs) != 3 {
		t.Fatalf("got %d docs, want 3 (non-linear section skipped): %+v", len(doc.Docs), doc.Docs)
	}
	wantTitles := []string{"Cover", "Chapter 1", "Chapter 2"}
	for i, want := range wantTitles {
		if doc.Docs[i].Title != want {
			t.Errorf("doc %d title = %q, want %q", i, doc.Docs[i].Title, want)
		}
	}
	if len(doc.Docs[0].Blocks) != 0 {
		t.Errorf("cover should be image-only, got blocks %q", doc.Docs[0].Blocks)
	}
	joined := strings.Join(doc.Docs[1].Blocks, " / ")
	if !strings.Contains(joined, "Chapter One / The first section reassembles from a skeleton and its fragments. / Fragments splice at insert offsets, even mid-tag.") {
		t.Errorf("chapter 1 blocks = %q", joined)
	}
	joined = strings.Join(doc.Docs[2].Blocks, " / ")
	if joined != "Chapter Two / Second section, also fragmented." {
		t.Errorf("chapter 2 blocks = %q", joined)
	}
}

func TestParseDRMFixtureRefused(t *testing.T) {
	_, err := parseBytes(t, fixtures.MOBI6DRM)
	if !errors.Is(err, mobigo.ErrDRM) {
		t.Fatalf("error = %v, want one wrapping mobi.ErrDRM", err)
	}
}
