package fixtures

import (
	"bytes"
	"compress/zlib"
	"fmt"
	"strings"

	"github.com/go-pdf/fpdf"
)

// Synthetic PDF fixtures, built here so every test suite — the parser's own,
// the scanner's, the ingester's and the HTTP integration tests — parses the
// exact same bytes under one name (the package's rule: one copy beats
// duplicates that can drift). The hand-built fixtures carry explicit glyph
// widths so extraction yields real positions; go-pdf/fpdf is used only for
// the encrypted pair, where its standard-security output is exactly the DRM
// shape the tests refuse.

// PDFPage is one page of a hand-built fixture.
type PDFPage struct {
	// Content is the page's raw content-stream operations.
	Content string
	// Outline, when set, titles this page in the bookmark tree.
	Outline string
	// Image, when set, draws an image XObject on the page.
	Image bool
}

// PDFFixture describes a complete hand-built PDF.
type PDFFixture struct {
	Pages []PDFPage
	// Info is the trailer /Info dictionary: PDF string values keyed by
	// name ("Title", "Author", ...). Nil for no Info dict.
	Info map[string]string
	// XMP, when set, is the catalog /Metadata stream body — XMP XML the
	// scanner's metadata reader must find.
	XMP string
}

// pdfPt converts top-down page y to PDF bottom-up y on an 842pt page.
func pdfPt(y float64) float64 { return 842 - y }

// PDFLineOp is one text operation at an exact position and size.
func PDFLineOp(size int, x, yTop float64, text string) string {
	return fmt.Sprintf("BT /F1 %d Tf %.1f %.1f Td (%s) Tj ET\n", size, x, pdfPt(yTop), escapePDFString(text))
}

// escapePDFString escapes a literal string operand.
func escapePDFString(s string) string {
	var sb strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '(' || c == ')' || c == '\\':
			sb.WriteByte('\\')
			sb.WriteByte(c)
		case c < 33 || c > 126:
			fmt.Fprintf(&sb, "\\%03o", c)
		default:
			sb.WriteByte(c)
		}
	}
	return sb.String()
}

// BuildPDF assembles a classic xref-table PDF from a fixture: a Helvetica
// Type1 font with monospace widths (so extraction yields real advances),
// A4 pages at 595×842, an outline over the pages that carry titles, then
// the optional /Info dictionary and /Metadata XMP stream. Objects are
// numbered deterministically: 1 catalog, 2 pages, 3 font, 4 widths, per
// page content/(image/)page objects, outline items, outline root, info,
// metadata.
func BuildPDF(f PDFFixture) []byte {
	const (
		objCatalog = 1
		objPages   = 2
		objFont    = 3
		objWidths  = 4
	)
	next := 5
	type pageObjs struct{ content, image, page int }
	pos := make([]pageObjs, len(f.Pages))
	for i := range f.Pages {
		pos[i].content = next
		next++
		if f.Pages[i].Image {
			pos[i].image = next
			next++
		}
		pos[i].page = next
		next++
	}
	type outlineObjs struct {
		item    int
		pageIdx int
	}
	var outlines []outlineObjs
	for i, pg := range f.Pages {
		if pg.Outline != "" {
			outlines = append(outlines, outlineObjs{item: next, pageIdx: i})
			next++
		}
	}
	outlinesRoot := 0
	if len(outlines) > 0 {
		outlinesRoot = next
		next++
	}
	objInfo := 0
	if len(f.Info) > 0 {
		objInfo = next
		next++
	}
	objMeta := 0
	if f.XMP != "" {
		objMeta = next
		next++
	}

	bodies := make(map[int]string, next)
	// Glyph width 450/1000: a plausible text face's advance, wide enough
	// that positions are real and narrow enough that two columns with a
	// gutter stay two columns.
	bodies[objWidths] = "[" + strings.TrimSpace(strings.Repeat("450 ", 95)) + "]"
	bodies[objFont] = fmt.Sprintf("<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /Encoding /WinAnsiEncoding /FirstChar 32 /LastChar 126 /Widths %d 0 R >>", objWidths)

	kids := make([]string, len(f.Pages))
	for i, pg := range f.Pages {
		c := pg.Content
		res := fmt.Sprintf("<< /Font << /F1 %d 0 R >>", objFont)
		if pg.Image {
			c += "q 400 0 0 400 100 342 cm /Im1 Do Q\n"
			res += fmt.Sprintf(" /XObject << /Im1 %d 0 R >>", pos[i].image)
			bodies[pos[i].image] = pdfImageXObject()
		}
		res += " >>"
		bodies[pos[i].content] = fmt.Sprintf("<< /Length %d >>\nstream\n%sendstream", len(c), c)
		bodies[pos[i].page] = fmt.Sprintf("<< /Type /Page /Parent %d 0 R /MediaBox [0 0 595 842] /Resources %s /Contents %d 0 R >>",
			objPages, res, pos[i].content)
		kids[i] = fmt.Sprintf("%d 0 R", pos[i].page)
	}
	bodies[objPages] = fmt.Sprintf("<< /Type /Pages /Count %d /Kids [%s] >>", len(kids), strings.Join(kids, " "))

	catalog := fmt.Sprintf("<< /Type /Catalog /Pages %d 0 R", objPages)
	if outlinesRoot > 0 {
		for k, o := range outlines {
			b := fmt.Sprintf("<< /Title (%s) /Parent %d 0 R /Dest [%d 0 R /XYZ null null null]",
				escapePDFString(f.Pages[o.pageIdx].Outline), outlinesRoot, pos[o.pageIdx].page)
			if k > 0 {
				b += fmt.Sprintf(" /Prev %d 0 R", outlines[k-1].item)
			}
			if k < len(outlines)-1 {
				b += fmt.Sprintf(" /Next %d 0 R", outlines[k+1].item)
			}
			bodies[o.item] = b + " >>"
		}
		bodies[outlinesRoot] = fmt.Sprintf("<< /Type /Outlines /First %d 0 R /Last %d 0 R /Count %d >>",
			outlines[0].item, outlines[len(outlines)-1].item, len(outlines))
		catalog += fmt.Sprintf(" /Outlines %d 0 R", outlinesRoot)
	}
	if objInfo > 0 {
		var dict strings.Builder
		dict.WriteString("<<")
		for _, k := range []string{"Title", "Author", "Subject", "Keywords", "Creator", "Producer", "CreationDate"} {
			if v, ok := f.Info[k]; ok {
				dict.WriteString(fmt.Sprintf(" /%s (%s)", k, escapePDFString(v)))
			}
		}
		dict.WriteString(" >>")
		bodies[objInfo] = dict.String()
	}
	if objMeta > 0 {
		bodies[objMeta] = fmt.Sprintf("<< /Type /Metadata /Subtype /XML /Length %d >>\nstream\n%sendstream", len(f.XMP), f.XMP)
		catalog += fmt.Sprintf(" /Metadata %d 0 R", objMeta)
	}
	bodies[objCatalog] = catalog + " >>"

	var body bytes.Buffer
	body.WriteString("%PDF-1.4\n")
	offsets := make([]int, next)
	for nr := 1; nr < next; nr++ {
		offsets[nr] = body.Len()
		fmt.Fprintf(&body, "%d 0 obj\n%s\nendobj\n", nr, bodies[nr])
	}
	xref := body.Len()
	fmt.Fprintf(&body, "xref\n0 %d\n0000000000 65535 f \n", next)
	for nr := 1; nr < next; nr++ {
		fmt.Fprintf(&body, "%010d 00000 n \n", offsets[nr])
	}
	fmt.Fprintf(&body, "trailer\n<< /Size %d /Root 1 0 R", next)
	if objInfo > 0 {
		fmt.Fprintf(&body, " /Info %d 0 R", objInfo)
	}
	fmt.Fprintf(&body, " >>\nstartxref\n%d\n%%%%EOF\n", xref)
	return body.Bytes()
}

// pdfImageXObject returns an 8×8 DeviceRGB image XObject body:
// zlib-compressed raw pixel rows, the way a real scanner page embeds a
// plate.
func pdfImageXObject() string {
	rows := make([]byte, 0, 8*8*3)
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			rows = append(rows, byte(x*32), byte(y*32), 128)
		}
	}
	var z bytes.Buffer
	w := zlib.NewWriter(&z)
	w.Write(rows)
	w.Close()
	return fmt.Sprintf("<< /Type /XObject /Subtype /Image /Width 8 /Height 8 /ColorSpace /DeviceRGB /BitsPerComponent 8 /Filter /FlateDecode /Length %d >>\nstream\n%s\nendstream",
		z.Len(), z.String())
}

// ProsePDF is a four-page synthetic novel: a running head and folio on
// every page, three chapters with display-type headings and outline
// bookmarks, paragraphs separated by vertical gaps, an in-page hyphenated
// line-end, and a hyphenated word split across a page break.
func ProsePDF() PDFFixture {
	return PDFFixture{Pages: []PDFPage{
		{
			Outline: "Chapter One",
			Content: PDFLineOp(9, 250, 40, "THE SYNTHETIC BOOK") + PDFLineOp(9, 296, 800, "1") +
				PDFLineOp(18, 72, 120, "Chapter One") +
				PDFLineOp(11, 72, 160, "The first paragraph opens the book with plain") +
				PDFLineOp(11, 72, 174, "words in sentences that a reader can follow,") +
				PDFLineOp(11, 72, 188, "and nothing here is extraordi-") +
				PDFLineOp(11, 72, 202, "nary until the hyphen joins.") +
				PDFLineOp(11, 72, 240, "The second paragraph starts after a vertical gap,") +
				PDFLineOp(11, 72, 254, "so the block rule can see the boundary."),
		},
		{
			Outline: "Chapter Two",
			Content: PDFLineOp(9, 250, 40, "THE SYNTHETIC BOOK") + PDFLineOp(9, 296, 800, "2") +
				PDFLineOp(18, 72, 120, "Chapter Two") +
				PDFLineOp(11, 72, 160, "Chapter two holds more prose of its own, with") +
				PDFLineOp(11, 72, 174, "words like time and water and people so the") +
				PDFLineOp(11, 72, 188, "dictionary coverage of the gate is satisfied."),
		},
		{
			Outline: "Chapter Three",
			Content: PDFLineOp(9, 250, 40, "THE SYNTHETIC BOOK") + PDFLineOp(9, 296, 800, "3") +
				PDFLineOp(18, 72, 120, "Chapter Three") +
				PDFLineOp(11, 72, 160, "The last chapter is short but real, and it") +
				PDFLineOp(11, 72, 174, "carries the word twenty-") +
				PDFLineOp(11, 72, 188, "three inside one page so the in-page rule") +
				PDFLineOp(11, 72, 202, "has a fixture, and it ends mid-word ordi-"),
		},
		{
			Content: PDFLineOp(9, 250, 40, "THE SYNTHETIC BOOK") + PDFLineOp(9, 296, 800, "4") +
				PDFLineOp(11, 72, 120, "nary across the page boundary.") +
				PDFLineOp(11, 72, 160, "The end."),
		},
	}}
}

// BuildProsePDF renders ProsePDF to bytes.
func BuildProsePDF() []byte { return BuildPDF(ProsePDF()) }

// BuildTwoColumnPDF draws one page whose body text sits in two columns with
// a clear gutter between them.
func BuildTwoColumnPDF() []byte {
	left := []string{
		"Left column opens the page and its",
		"lines are read top to bottom first,",
		"all three of them, before the right",
		"column ever enters the reading order.",
	}
	right := []string{
		"The right column follows after,",
		"read top to bottom in its turn,",
		"even though its first line shares",
		"a baseline with the left column.",
	}
	var c strings.Builder
	y := 100.0
	for i := range left {
		c.WriteString(PDFLineOp(11, 72, y, left[i]))
		c.WriteString(PDFLineOp(11, 320, y, right[i]))
		y += 14
	}
	return BuildPDF(PDFFixture{Pages: []PDFPage{{Content: c.String()}}})
}

// BuildImageOnlyPDF draws two pages carrying an image XObject and no text:
// the comics-and-scans population.
func BuildImageOnlyPDF() []byte {
	return BuildPDF(PDFFixture{Pages: []PDFPage{{Image: true}, {Image: true}}})
}

// BuildTitledPDF is a one-page book whose identity lives in its metadata —
// an /Info dictionary and an XMP packet carrying title, author, language
// and an ISBN identifier — the shape the scanner's metadata reader looks
// for. The page body is too thin to pass the parser's quality gate, which
// is the point: metadata is read at scan time, text judged at parse time.
func BuildTitledPDF() []byte {
	xmp := `<?xpacket begin="" id="W5M0MpCehiHzreSzNTczkc9d"?>
<x:xmpmeta xmlns:x="adobe:ns:meta/" xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#" xmlns:dc="http://purl.org/dc/elements/1.1/">
 <rdf:RDF>
  <rdf:Description rdf:about="">
   <dc:title><rdf:Alt><rdf:li xml:lang="x-default">The Titled Synthetic Book</rdf:li></rdf:Alt></dc:title>
   <dc:creator><rdf:Seq><rdf:li>Fixture Author</rdf:li></rdf:Seq></dc:creator>
   <dc:language><rdf:Bag><rdf:li>en</rdf:li></rdf:Bag></dc:language>
   <dc:identifier>urn:isbn:9780000000002</dc:identifier>
  </rdf:Description>
 </rdf:RDF>
</x:xmpmeta>
<?xpacket end="w"?>`
	return BuildPDF(PDFFixture{
		Pages: []PDFPage{{
			Content: PDFLineOp(11, 72, 120, "A single page of prose with a normal amount of words to say."),
		}},
		Info: map[string]string{
			"Title":        "The Titled Synthetic Book",
			"Author":       "Fixture Author",
			"CreationDate": "D:20060102030405Z",
		},
		XMP: xmp,
	})
}

// BuildGarbagePDF is the broken-ToUnicode fixture: a Type0/Identity-H font
// with no ToUnicode CMap, whose content stream draws two-byte GID codes.
// The codes mix real letters (which byte-decode to plausible ASCII) with
// low control codes — the plausible-looking garbage the quality gate
// exists to catch, in the shape a subset producer that omits ToUnicode
// leaves behind.
func BuildGarbagePDF() []byte {
	var content bytes.Buffer
	content.WriteString("BT /F1 12 Tf 72 700 Td\n")
	for range 6 {
		// "The " (codes 0x54 0x68 0x65 0x20), three junk codes, " word "
		// (0x20 0x77 0x6f 0x72 0x64 0x20), three more junk codes.
		content.WriteString("<005400680065002000010002000300200077006f007200640020000400050006> Tj 0 -20 Td\n")
	}
	content.WriteString("ET\n")
	c := content.String()
	objs := []struct {
		nr   int
		body string
	}{
		{1, "<< /Type /Catalog /Pages 2 0 R >>"},
		{2, "<< /Type /Pages /Kids [3 0 R] /Count 1 >>"},
		{3, "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F1 4 0 R >> >> /Contents 5 0 R >>"},
		{4, "<< /Type /Font /Subtype /Type0 /BaseFont /Broken /Encoding /Identity-H /DescendantFonts [6 0 R] >>"},
		{5, fmt.Sprintf("<< /Length %d >>\nstream\n%sendstream", len(c), c)},
		{6, "<< /Type /Font /Subtype /CIDFontType2 /BaseFont /Broken /CIDSystemInfo << /Registry (Adobe) /Ordering (Identity) /Supplement 0 >> /DW 500 >>"},
	}
	var body bytes.Buffer
	body.WriteString("%PDF-1.4\n")
	offsets := make([]int, 7)
	for _, o := range objs {
		offsets[o.nr] = body.Len()
		fmt.Fprintf(&body, "%d 0 obj\n%s\nendobj\n", o.nr, o.body)
	}
	xref := body.Len()
	fmt.Fprintf(&body, "xref\n0 7\n0000000000 65535 f \n")
	for i := 1; i <= 6; i++ {
		fmt.Fprintf(&body, "%010d 00000 n \n", offsets[i])
	}
	fmt.Fprintf(&body, "trailer\n<< /Size 7 /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", xref)
	return body.Bytes()
}

// BuildCorruptPDF carries a header and an EOF marker but a startxref that
// points past the file: structurally broken, the classification corrupt.
func BuildCorruptPDF() []byte {
	return []byte("%PDF-1.4\n1 0 obj\n<< /Type /Catalog >>\nendobj\ntrailer\n<< /Size 2 /Root 1 0 R >>\nstartxref\n999999\n%%EOF\n")
}

// BuildEncryptedPDF produces the two DRM shapes via fpdf's standard
// security handler: locked (user password required) and restricted (empty
// user password; owner-password "restrictions" that open freely).
func BuildEncryptedPDF(locked bool) ([]byte, error) {
	f := fpdf.New("P", "pt", "A4", "")
	f.SetAutoPageBreak(false, 0)
	f.SetFont("Helvetica", "", 11)
	f.AddPage()
	f.Text(72, 720, "This text never gets parsed.")
	user := ""
	if locked {
		user = "user-secret"
	}
	f.SetProtection(fpdf.CnProtectPrint, user, "owner-secret")
	var buf bytes.Buffer
	if err := f.Output(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
