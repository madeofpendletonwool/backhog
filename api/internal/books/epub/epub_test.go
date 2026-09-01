package epub

import (
	"archive/zip"
	"bytes"
	"errors"
	"io"
	"testing"
)

// writeZip builds an in-memory zip from name → content pairs, mimicking how
// an EPUB stores its parts. The mimetype entry is written first and stored
// uncompressed like the spec demands (our parser does not care, but the
// fixtures should look like the real thing).
func writeZip(t *testing.T, mimetype string, entries [][2]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	if mimetype != "" {
		w, err := zw.CreateHeader(&zip.FileHeader{Name: "mimetype", Method: zip.Store})
		if err != nil {
			t.Fatalf("mimetype header: %v", err)
		}
		if _, err := w.Write([]byte(mimetype)); err != nil {
			t.Fatalf("mimetype write: %v", err)
		}
	}
	for _, e := range entries {
		w, err := zw.Create(e[0])
		if err != nil {
			t.Fatalf("create %s: %v", e[0], err)
		}
		if _, err := io.WriteString(w, e[1]); err != nil {
			t.Fatalf("write %s: %v", e[0], err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
	return buf.Bytes()
}

func parseBytes(t *testing.T, data []byte) *Document {
	t.Helper()
	doc, err := Parse(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	return doc
}

// buildEPUB2 is a real-ish EPUB 2: cover page, three text chapters, NCX TOC
// with nested points, prose full of curly quotes and dashes, plus script and
// style noise that must never reach the blocks.
func buildEPUB2(t *testing.T) []byte {
	t.Helper()
	container := `<?xml version="1.0"?>
<container version="1.0" xmlns="urn:oasis:names:tc:opendocument:xmlns:container">
  <rootfiles>
    <rootfile full-path="OEBPS/content.opf" media-type="application/oebps-package+xml"/>
  </rootfiles>
</container>`
	opf := `<?xml version="1.0" encoding="UTF-8"?>
<package xmlns="http://www.idpf.org/2007/opf" version="2.0" unique-identifier="uid">
  <metadata xmlns:dc="http://purl.org/dc/elements/1.1/">
    <dc:title>A Study in Curly Quotes</dc:title>
    <dc:creator>Testa Author</dc:creator>
  </metadata>
  <manifest>
    <item id="ncx" href="toc.ncx" media-type="application/x-dtbncx+xml"/>
    <item id="c0" href="cover.xhtml" media-type="application/xhtml+xml"/>
    <item id="c1" href="text/ch1.xhtml" media-type="application/xhtml+xml"/>
    <item id="c2" href="text/ch2.xhtml" media-type="application/xhtml+xml"/>
    <item id="c3" href="text/ch3.xhtml" media-type="application/xhtml+xml"/>
  </manifest>
  <spine toc="ncx">
    <itemref idref="c0"/>
    <itemref idref="c1"/>
    <itemref idref="c2"/>
    <itemref idref="c3"/>
  </spine>
</package>`
	ncx := `<?xml version="1.0" encoding="UTF-8"?>
<ncx xmlns="http://www.daisy.org/z3986/2005/ncx/" version="2005-1">
  <navMap>
    <navPoint id="np1" playOrder="1">
      <navLabel><text>Part One</text></navLabel>
      <content src="text/ch1.xhtml"/>
      <navPoint id="np1a" playOrder="2">
        <navLabel><text>The Second Chapter</text></navLabel>
        <content src="text/ch2.xhtml#sec2"/>
      </navPoint>
    </navPoint>
    <navPoint id="np3" playOrder="3">
      <navLabel><text>chapter three</text></navLabel>
      <content src="text/ch3.xhtml"/>
    </navPoint>
  </navMap>
</ncx>`
	cover := `<html><head><title>Cover</title></head>
<body><div><img src="cover.png"/></div></body></html>`
	ch1 := `<html><head><title>ch1</title><style>p { color: red; }</style>
<script>var x = "should not appear";</script></head>
<body>
<div class="chapter">
  <h1>The ﬁrst chapter</h1>
  <p>“It’s ﬁne,” he said—mostly.</p>
  <p>Second paragraph with<br/>a hard break.</p>
  Direct text in the div, then
  <p>a real paragraph after it.</p>
</div>
</body></html>`
	ch2 := `<html><head><title>ch2</title></head>
<body>
  <h2 id="sec2">Chapter the Second</h2>
  <p>Text in chapter two — twenty-three lines of it…</p>
  <blockquote><p>Quoted speech stays a block.</p></blockquote>
</body></html>`
	ch3 := `<html><head><title>ch3</title></head>
<body><p>Final words.</p><ul><li>one</li><li>two</li></ul></body></html>`
	// A cover image, so the cover page has a real binary part.
	png := "\x89PNG\r\n\x1a\nfakecover"

	return writeZip(t, "application/epub+zip", [][2]string{
		{"META-INF/container.xml", container},
		{"OEBPS/content.opf", opf},
		{"OEBPS/toc.ncx", ncx},
		{"OEBPS/cover.xhtml", cover},
		{"OEBPS/text/ch1.xhtml", ch1},
		{"OEBPS/text/ch2.xhtml", ch2},
		{"OEBPS/text/ch3.xhtml", ch3},
		{"OEBPS/cover.png", png},
	})
}

// buildEPUB3 is a real-ish EPUB 3: nav document TOC (nested parts), a
// non-linear spine item (a note that must be skipped), and hrefs that need
// path resolution from a subdirectory.
func buildEPUB3(t *testing.T) []byte {
	t.Helper()
	container := `<?xml version="1.0"?>
<container version="1.0" xmlns="urn:oasis:names:tc:opendocument:xmlns:container">
  <rootfiles>
    <rootfile full-path="content.opf" media-type="application/oebps-package+xml"/>
  </rootfiles>
</container>`
	opf := `<?xml version="1.0" encoding="UTF-8"?>
<package xmlns="http://www.idpf.org/2007/opf" version="3.0" unique-identifier="uid">
  <metadata xmlns:dc="http://purl.org/dc/elements/1.1/">
    <dc:title>Nav Doc Nights</dc:title>
  </metadata>
  <manifest>
    <item id="nav" href="nav.xhtml" media-type="application/xhtml+xml" properties="nav"/>
    <item id="s1" href="docs/one.xhtml" media-type="application/xhtml+xml"/>
    <item id="s2" href="docs/two.xhtml" media-type="application/xhtml+xml"/>
    <item id="note" href="docs/note.xhtml" media-type="application/xhtml+xml"/>
  </manifest>
  <spine>
    <itemref idref="s1"/>
    <itemref idref="s2" linear="no"/>
    <itemref idref="note"/>
  </spine>
</package>`
	nav := `<html xmlns="http://www.w3.org/1999/xhtml" xmlns:epub="http://www.idpf.org/2007/ops">
<body>
  <nav epub:type="toc">
    <ol>
      <li><a href="docs/one.xhtml">One</a></li>
      <li><span>Extras</span><ol>
        <li><a href="docs/two.xhtml">Two</a></li>
        <li><a href="docs/note.xhtml#n1">A Note</a></li>
      </ol></li>
    </ol>
  </nav>
</body></html>`
	one := `<html><body><h1>Uno</h1><p>First of the nav docs.</p></body></html>`
	two := `<html><body><p>Two.</p></body></html>`
	note := `<html><body><p id="n1">Note body.</p></body></html>`

	return writeZip(t, "application/epub+zip", [][2]string{
		{"META-INF/container.xml", container},
		{"content.opf", opf},
		{"nav.xhtml", nav},
		{"docs/one.xhtml", one},
		{"docs/two.xhtml", two},
		{"docs/note.xhtml", note},
	})
}

func TestParseEPUB2(t *testing.T) {
	doc := parseBytes(t, buildEPUB2(t))

	if len(doc.Docs) != 4 {
		t.Fatalf("got %d spine docs, want 4", len(doc.Docs))
	}

	cover := doc.Docs[0]
	if cover.Href != "OEBPS/cover.xhtml" {
		t.Errorf("cover href = %q", cover.Href)
	}
	if len(cover.Blocks) != 0 {
		t.Errorf("image-only cover produced blocks: %q", cover.Blocks)
	}
	if cover.Title != "" {
		t.Errorf("cover not in TOC, got title %q", cover.Title)
	}

	ch1 := doc.Docs[1]
	if ch1.Href != "OEBPS/text/ch1.xhtml" {
		t.Errorf("ch1 href = %q", ch1.Href)
	}
	if ch1.Title != "Part One" {
		t.Errorf("ch1 title = %q, want %q", ch1.Title, "Part One")
	}
	if ch1.Depth != 0 {
		t.Errorf("ch1 depth = %d, want 0", ch1.Depth)
	}
	wantBlocks := []string{
		"The ﬁrst chapter",
		"“It’s ﬁne,” he said—mostly.",
		"Second paragraph with a hard break.",
		"Direct text in the div, then",
		"a real paragraph after it.",
	}
	if len(ch1.Blocks) != len(wantBlocks) {
		t.Fatalf("ch1 blocks = %#v", ch1.Blocks)
	}
	for i, want := range wantBlocks {
		if got := ch1.Blocks[i]; got != want {
			t.Errorf("ch1 block %d = %q, want %q", i, got, want)
		}
	}

	ch2 := doc.Docs[2]
	if ch2.Title != "The Second Chapter" {
		t.Errorf("ch2 title = %q (fragment TOC entry must still match by href)", ch2.Title)
	}
	if ch2.Depth != 1 {
		t.Errorf("ch2 depth = %d, want 1 (nested NCX point)", ch2.Depth)
	}
	if len(ch2.Blocks) != 3 {
		t.Errorf("ch2 blocks = %#v, want heading + para + blockquote para", ch2.Blocks)
	}

	ch3 := doc.Docs[3]
	if ch3.Title != "chapter three" {
		t.Errorf("ch3 title = %q", ch3.Title)
	}
	if len(ch3.Blocks) != 3 {
		t.Errorf("ch3 blocks = %#v, want para + 2 list items", ch3.Blocks)
	}
}

func TestParseEPUB3(t *testing.T) {
	doc := parseBytes(t, buildEPUB3(t))

	// The non-linear doc is skipped: spine indices stay dense.
	if len(doc.Docs) != 2 {
		t.Fatalf("got %d spine docs, want 2 (linear=no skipped)", len(doc.Docs))
	}
	if doc.Docs[0].Href != "docs/one.xhtml" || doc.Docs[0].Title != "One" {
		t.Errorf("doc0 = %+v", doc.Docs[0])
	}
	if doc.Docs[0].SpineIndex != 0 || doc.Docs[1].SpineIndex != 1 {
		t.Errorf("spine indices not dense: %d %d", doc.Docs[0].SpineIndex, doc.Docs[1].SpineIndex)
	}
	if doc.Docs[1].Title != "A Note" {
		t.Errorf("doc1 title = %q, want the nav entry targeting it", doc.Docs[1].Title)
	}
	if doc.Docs[1].Depth != 1 {
		t.Errorf("doc1 depth = %d, want 1 (nested under Extras)", doc.Docs[1].Depth)
	}
	if len(doc.Docs[1].Blocks) != 1 || doc.Docs[1].Blocks[0] != "Note body." {
		t.Errorf("doc1 blocks = %#v", doc.Docs[1].Blocks)
	}
}

func TestParseDRM(t *testing.T) {
	data := writeZip(t, "application/epub+zip", [][2]string{
		{"META-INF/container.xml", "<container/>"},
		{"META-INF/encryption.xml", `<encryption xmlns="urn:ietf:params:xml:ns:encryption"/>`},
	})
	_, err := Parse(bytes.NewReader(data), int64(len(data)))
	if !errors.Is(err, ErrDRM) {
		t.Fatalf("err = %v, want ErrDRM", err)
	}
}

func TestParseMissingContainer(t *testing.T) {
	data := writeZip(t, "application/epub+zip", [][2]string{
		{"OEBPS/content.opf", "<package/>"},
	})
	if _, err := Parse(bytes.NewReader(data), int64(len(data))); err == nil {
		t.Fatal("missing container.xml must fail")
	}
}

func TestParseNotAZip(t *testing.T) {
	data := []byte("this is not an epub at all")
	if _, err := Parse(bytes.NewReader(data), int64(len(data))); err == nil {
		t.Fatal("garbage input must fail")
	}
}
