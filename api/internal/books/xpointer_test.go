package books

import (
	"archive/zip"
	"bytes"
	"strings"
	"testing"

	"github.com/collinpendleton/backhog/api/internal/books/epub"
)

// fixtureXPointer is shaped to hit the places the two DOMs disagree: a
// non-linear notes page first in the spine (so DocFragment numbers run
// ahead of the canonical text's spine), nested containers, inline spans,
// loose text sitting directly in a <div>, a <br>, a blank paragraph, a
// table with no <tbody> (the HTML parser inserts one; an XML engine does
// not), and an image-only chapter that owns no text.
func fixtureXPointer(t *testing.T) []byte {
	t.Helper()
	return buildZip(t, [][2]string{
		{"META-INF/container.xml", `<?xml version="1.0"?>
<container version="1.0" xmlns="urn:oasis:names:tc:opendocument:xmlns:container">
  <rootfiles><rootfile full-path="OEBPS/content.opf" media-type="application/oebps-package+xml"/></rootfiles>
</container>`},
		{"OEBPS/content.opf", `<?xml version="1.0"?>
<package xmlns="http://www.idpf.org/2007/opf" version="2.0">
  <manifest>
    <item id="notes" href="notes.xhtml" media-type="application/xhtml+xml"/>
    <item id="c1" href="ch1.xhtml" media-type="application/xhtml+xml"/>
    <item id="art" href="art.xhtml" media-type="application/xhtml+xml"/>
    <item id="c2" href="ch2.xhtml" media-type="application/xhtml+xml"/>
    <item id="c3" href="ch3.xhtml" media-type="application/xhtml+xml"/>
  </manifest>
  <spine>
    <itemref idref="notes" linear="no"/><itemref idref="c1"/><itemref idref="art"/><itemref idref="c2"/><itemref idref="c3"/>
  </spine>
</package>`},
		{"OEBPS/notes.xhtml", `<html><body><p>A note nobody reads.</p></body></html>`},
		{"OEBPS/ch1.xhtml", `<?xml version="1.0" encoding="utf-8"?>
<html xmlns="http://www.w3.org/1999/xhtml"><head><title>One</title></head><body>
<section>
<h1>Chapter One</h1>
<div class="body">
<p>First <span>para</span> here, with “curly” quotes.</p>
<p>   </p>
<p>Third paragraph.</p>
Loose text in the div
<p>Second<br/>line</p>
</div>
</section>
<table><tr><td>Cell A</td><td>Cell B</td></tr></table>
</body></html>`},
		{"OEBPS/art.xhtml", `<html><body><div><img src="pic.png"/></div></body></html>`},
		{"OEBPS/pic.png", "png"},
		{"OEBPS/ch2.xhtml", `<html><body><p>Two one.</p><p>Two two.</p></body></html>`},
		// Self-closing page-break markers, as a real KOReader-tested book
		// has them: XHTML means an empty element, the HTML5 parser nests
		// everything after one inside it. Also a spacer paragraph holding
		// only a no-break space, which owns no block.
		{"OEBPS/ch3.xhtml", `<?xml version="1.0" encoding="utf-8"?>
<html xmlns="http://www.w3.org/1999/xhtml"><body><div>
<header><p class="SP"> </p><h1><span id="pg_201" role="doc-pagebreak"/><a href="#ack"><b>ACKNOWLEDGMENTS</b></a></h1></header>
<p>Oprah, I never thought you would know <span id="pg_202" role="doc-pagebreak"/>who I am at all.</p>
</div></body></html>`},
	})
}

type locFixture struct {
	loc       *Locator
	canonical string
}

func newLocFixture(t *testing.T, data []byte) locFixture {
	t.Helper()
	doc, err := epub.Parse(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	canonical, _, _, index, _ := Canonicalize(doc)
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	loc, err := NewLocator(zr, doc, index)
	if err != nil {
		t.Fatalf("locator: %v", err)
	}
	return locFixture{loc: loc, canonical: canonical}
}

// at is the canonical offset of the block whose text is raw.
func (f locFixture) at(t *testing.T, raw string) int {
	t.Helper()
	i := strings.Index(f.canonical, Normalize(raw))
	if i < 0 {
		t.Fatalf("block %q is not in the canonical text", raw)
	}
	return i
}

func TestToXPointerGoldens(t *testing.T) {
	f := newLocFixture(t, fixtureXPointer(t))
	cases := map[string]string{
		// DocFragment counts the non-linear notes page: ch1 is the 2nd itemref.
		"Chapter One": "/body/DocFragment[2]/body/section/h1/text().0",
		// Two non-blank text nodes in the <p> (the span splits them).
		"First para here, with “curly” quotes.": "/body/DocFragment[2]/body/section/div/p[1]/text()[1].0",
		"Third paragraph.":                      "/body/DocFragment[2]/body/section/div/p[3]/text().0",
		// Loose text: the div's only non-blank text node, after its newline.
		"Loose text in the div": "/body/DocFragment[2]/body/section/div/text().1",
		"Second line":           "/body/DocFragment[2]/body/section/div/p[4]/text()[1].0",
		// No tbody: that is the HTML parser's, not the book's.
		"Cell B":   "/body/DocFragment[2]/body/table/tr/td[2]/text().0",
		"Two two.": "/body/DocFragment[4]/body/p[2]/text().0",
		// No phantom <span> from the self-closed page breaks: these are the
		// paths crengine builds.
		"ACKNOWLEDGMENTS": "/body/DocFragment[5]/body/div/header/h1/a/b/text().0",
		"Oprah, I never thought you would know who I am at all.": "/body/DocFragment[5]/body/div/p/text()[1].0",
	}
	for block, want := range cases {
		got, ok := f.loc.ToXPointer(f.at(t, block))
		if !ok || got != want {
			t.Errorf("ToXPointer(%q) = %q, %v; want %q", block, got, ok, want)
		}
	}
}

func TestXPointerRoundTripsEveryBlock(t *testing.T) {
	for name, data := range map[string][]byte{
		"xpointer": fixtureXPointer(t),
		"ncx":      fixtureNCX(t),
		"nav":      fixtureNav(t),
	} {
		f := newLocFixture(t, data)
		for _, start := range f.loc.starts {
			// From the block start and from inside the block alike.
			for _, offset := range []int{start, start + 1} {
				xp, ok := f.loc.ToXPointer(offset)
				if !ok {
					t.Errorf("%s: no XPointer for offset %d", name, offset)
					continue
				}
				back, ok := f.loc.FromXPointer(xp)
				if !ok || back != start {
					t.Errorf("%s: %d → %q → %d, %v; want %d", name, offset, xp, back, ok, start)
				}
			}
		}
	}
}

func TestFromXPointerAsDevicesSendThem(t *testing.T) {
	f := newLocFixture(t, fixtureXPointer(t))
	cases := []struct {
		name, xp, block string
	}{
		{"mid-paragraph offset", "/body/DocFragment[2]/body/section/div/p[1]/text()[1].4", "First para here, with “curly” quotes."},
		{"inside an inline span", "/body/DocFragment[2]/body/section/div/p[1]/span/text().2", "First para here, with “curly” quotes."},
		{"second text node after a span", "/body/DocFragment[2]/body/section/div/p[1]/text()[2].5", "First para here, with “curly” quotes."},
		{"CrossPoint explicit [1]s", "/body/DocFragment[2]/body/section[1]/div[1]/p[3]/text()[1].0", "Third paragraph."},
		{"second text node after a br", "/body/DocFragment[2]/body/section/div/p[4]/text()[2].2", "Second line"},
		{"element only, no text step", "/body/DocFragment[2]/body/section/div/p[4]", "Second line"},
		{"blank paragraph lands on the block before it", "/body/DocFragment[2]/body/section/div/p[2]", "First para here, with “curly” quotes."},
		{"table row without tbody", "/body/DocFragment[2]/body/table/tr/td[2]/text().1", "Cell B"},
		{"non-linear fragment → next kept text", "/body/DocFragment[1]/body/p/text().3", "Chapter One"},
		{"image-only fragment → next text", "/body/DocFragment[3]/body/div/img", "Two one."},
		{"fragment past the end → last block", "/body/DocFragment[42]/body/p/text().0", "Oprah, I never thought you would know who I am at all."},
		// Pointers a Kindle's KOReader really sent, reshaped onto the fixture.
		{"KOReader: text after a self-closed span", "/body/DocFragment[5]/body/div/p/text()[2].10", "Oprah, I never thought you would know who I am at all."},
		{"KOReader: no-break-space spacer paragraph", "/body/DocFragment[5]/body/div/header/p/text().0", "ACKNOWLEDGMENTS"},
	}
	for _, c := range cases {
		got, ok := f.loc.FromXPointer(c.xp)
		if want := f.at(t, c.block); !ok || got != want {
			t.Errorf("%s: FromXPointer(%q) = %d, %v; want %d (%q)", c.name, c.xp, got, ok, want, c.block)
		}
	}

	// A step that does not exist stops the walk inside the right fragment
	// rather than failing the sync.
	got, ok := f.loc.FromXPointer("/body/DocFragment[4]/body/p[9]/text().0")
	if !ok || got < f.at(t, "Two one.") {
		t.Errorf("missing step: got %d, %v; want somewhere in chapter two", got, ok)
	}

	for _, bad := range []string{"", "garbage", "/body/DocFragment[0]/body", "#_doc_fragment_3"} {
		if _, ok := f.loc.FromXPointer(bad); ok {
			t.Errorf("FromXPointer(%q) claimed a position", bad)
		}
	}
}

func TestFromPercent(t *testing.T) {
	f := newLocFixture(t, fixtureXPointer(t))
	if got := f.loc.FromPercent(0); got != 0 {
		t.Errorf("0%% = %d", got)
	}
	if got, want := f.loc.FromPercent(1), f.at(t, "Oprah, I never thought you would know who I am at all."); got != want {
		t.Errorf("100%% = %d, want the last block %d", got, want)
	}
	if got := f.loc.FromPercent(0.5); got <= 0 || got >= f.loc.CharCount() {
		t.Errorf("50%% = %d", got)
	}
}
