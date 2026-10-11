package books

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/net/html"

	"github.com/collinpendleton/backhog/api/internal/books/epub"
)

// XPointers (MAD-832): the position language of KOReader and CrossPoint.
//
//	/body/DocFragment[8]/body/div[2]/p[4]/text()[1].37
//
// DocFragment[N] is the N-th itemref of the OPF spine (1-based, non-linear
// items counted), the path below it walks the document's XHTML from <body>
// counting same-named element siblings, and the tail names a text node and
// a code-point offset into it.
//
// Backhog's position is one canonical char offset, addressed through the
// block index. The Locator bridges the two at block (paragraph) precision:
// it re-walks each spine document with the very extractor that built the
// canonical text, now noting the DOM node each block starts in, and pairs
// those blocks one-for-one with the index's block offsets. A device's
// XPointer lands on the block containing it; a Backhog offset leaves as the
// XPointer of its block's first character.
//
// The rendering engines build their DOM with an XML parser and Backhog
// with an HTML5 one, so paths can disagree in the corners (a <tbody> the
// HTML parser inserted, whitespace text nodes the engine dropped). The
// walk tolerates both and, when a step cannot be followed, settles on the
// deepest node it reached — a nearby paragraph rather than an error. A
// caller that gets ok=false falls back to the percentage.

// ErrLocatorDrift reports that re-walking the EPUB did not reproduce the
// block index — the text was ingested by a different extractor version or
// from a different file — so block-level positions cannot be trusted.
var ErrLocatorDrift = errors.New("books: epub blocks do not match the stored block index")

// Locator translates between canonical offsets and XPointers for one EPUB.
type Locator struct {
	docs      []locDoc
	starts    []int // every block's canonical offset, in book order
	owners    []int // owners[g] is the locDoc block g belongs to
	charCount int
}

type locDoc struct {
	fragment int // 0-based raw spine position
	body     *html.Node
	anchors  []epub.Anchor
	first    int // global index of this doc's first block (len(starts) when empty)
	order    map[*html.Node]int
}

// NewLocator builds a Locator from an open EPUB, its parsed document and
// the block index its canonical text was written with.
func NewLocator(zr *zip.Reader, doc *epub.Document, index *BlockIndex) (*Locator, error) {
	l := &Locator{charCount: index.CharCount}
	for _, d := range index.Documents {
		l.starts = append(l.starts, d.Blocks...)
	}
	g := 0
	for _, d := range doc.Docs {
		root, anchors, err := epub.AnchorBlocks(zr, d.Href)
		if err != nil {
			return nil, err
		}
		if len(anchors) != len(d.Blocks) {
			return nil, fmt.Errorf("%w: %s has %d anchors for %d blocks", ErrLocatorDrift, d.Href, len(anchors), len(d.Blocks))
		}
		kept := anchors[:0:0]
		for _, a := range anchors {
			if Normalize(a.Raw) != "" {
				kept = append(kept, a)
			}
		}
		ld := locDoc{fragment: d.FragmentIndex, body: findBody(root), anchors: kept, first: g,
			order: textOrder(root)}
		for range kept {
			l.owners = append(l.owners, len(l.docs))
		}
		g += len(kept)
		l.docs = append(l.docs, ld)
	}
	if g != len(l.starts) {
		return nil, fmt.Errorf("%w: %d blocks walked, %d indexed", ErrLocatorDrift, g, len(l.starts))
	}
	return l, nil
}

// ToXPointer returns the XPointer of the block holding a canonical offset,
// pointing at that block's first character.
func (l *Locator) ToXPointer(offset int) (string, bool) {
	if len(l.starts) == 0 {
		return "", false
	}
	g := sort.SearchInts(l.starts, offset+1) - 1
	if g < 0 {
		g = 0
	}
	d := l.docs[l.owners[g]]
	a := d.anchors[g-d.first]
	path, ok := nodePath(d.body, a.Node)
	if !ok {
		return "", false
	}
	cp := utf8.RuneCountInString(a.Node.Data[:a.Offset])
	return fmt.Sprintf("/body/DocFragment[%d]/body%s.%d", d.fragment+1, path, cp), true
}

var fragmentRE = regexp.MustCompile(`^/body/DocFragment\[(\d+)\](.*)$`)

// FromXPointer returns the canonical offset of the block an XPointer lands
// in. ok is false when the pointer names no fragment of this book's text.
func (l *Locator) FromXPointer(xp string) (int, bool) {
	m := fragmentRE.FindStringSubmatch(strings.TrimSpace(xp))
	if m == nil || len(l.starts) == 0 {
		return 0, false
	}
	n, err := strconv.Atoi(m[1])
	if err != nil || n < 1 {
		return 0, false
	}
	fragment := n - 1

	// A fragment the canonical text skipped (non-linear front matter, say)
	// lands at the start of the next one it kept.
	di := sort.Search(len(l.docs), func(i int) bool { return l.docs[i].fragment >= fragment })
	if di == len(l.docs) {
		return l.starts[len(l.starts)-1], true
	}
	d := l.docs[di]
	if d.fragment != fragment || len(d.anchors) == 0 {
		return l.startFrom(d.first), true
	}

	node, offset := walkXPath(d.body, strings.TrimPrefix(m[2], "/body"))
	order, byteOff := d.position(node, offset)
	// The block holding the target is the last one starting at or before it.
	k := sort.Search(len(d.anchors), func(i int) bool {
		a := d.anchors[i]
		ao := d.order[a.Node]
		return ao > order || (ao == order && a.Offset > byteOff)
	}) - 1
	if k < 0 {
		k = 0
	}
	return l.starts[d.first+k], true
}

// FromPercent places a 0–1 fraction of the book on the block it falls in,
// the last resort when a pointer cannot be followed.
func (l *Locator) FromPercent(p float64) int {
	return PercentOffset(l.starts, l.charCount, p)
}

// PercentOffset places a 0–1 fraction of a text of charCount bytes on the
// start of the block it falls in (starts ascending), or on the raw offset
// when there are no blocks.
func PercentOffset(starts []int, charCount int, p float64) int {
	if p < 0 {
		p = 0
	}
	if p > 1 {
		p = 1
	}
	target := int(p * float64(charCount))
	if target >= charCount && charCount > 0 {
		target = charCount - 1
	}
	if len(starts) == 0 {
		return target
	}
	g := sort.SearchInts(starts, target+1) - 1
	if g < 0 {
		g = 0
	}
	return starts[g]
}

// CharCount is the canonical text length the Locator was built against.
func (l *Locator) CharCount() int { return l.charCount }

func (l *Locator) startFrom(g int) int {
	if g < len(l.starts) {
		return l.starts[g]
	}
	return l.starts[len(l.starts)-1]
}

// position orders a DOM target against the anchors: the document-order
// rank of a text node and a byte offset in it. An element stands for the
// first text inside or after it.
func (d locDoc) position(n *html.Node, cpOffset int) (int, int) {
	if n != nil && n.Type == html.TextNode {
		return d.order[n], byteOffset(n.Data, cpOffset)
	}
	for c := n; c != nil; c = nextInOrder(c, nil) {
		if c.Type == html.TextNode {
			if o, ok := d.order[c]; ok {
				return o, 0
			}
		}
	}
	return len(d.order), 0
}

// stepRE is one path step: a name, an optional [index], and on the last
// step an optional .offset.
var stepRE = regexp.MustCompile(`^([A-Za-z0-9_:\-]+|text\(\))(?:\[(\d+)\])?(?:\.(\d+))?$`)

// walkXPath follows a path below <body> as far as it can, returning the
// node it reached and the code-point offset the pointer's tail named.
func walkXPath(body *html.Node, path string) (*html.Node, int) {
	cur := body
	offset := 0
	for _, step := range strings.Split(strings.Trim(path, "/"), "/") {
		if step == "" {
			continue
		}
		m := stepRE.FindStringSubmatch(step)
		if m == nil {
			break
		}
		idx := 1
		if m[2] != "" {
			idx, _ = strconv.Atoi(m[2])
		}
		if m[3] != "" {
			offset, _ = strconv.Atoi(m[3])
		}
		var next *html.Node
		if m[1] == "text()" {
			next = nthText(cur, idx)
		} else {
			name := strings.ToLower(m[1])
			next = nthElement(cur, name, idx)
			// The HTML parser wraps table rows in a <tbody> the book never
			// wrote; an XML-built path steps straight from table to tr.
			if next == nil && name == "tr" {
				if tb := nthElement(cur, "tbody", 1); tb != nil {
					next = nthElement(tb, name, idx)
				}
			}
		}
		if next == nil {
			break
		}
		cur = next
	}
	return cur, offset
}

// nodePath renders a text node's path below body in crengine's style: an
// element step carries [k] when its parent has more than one child of that
// name, and the text step counts only text nodes holding something besides
// whitespace, the ones an engine keeps.
func nodePath(body, n *html.Node) (string, bool) {
	var steps []string
	for c := n; c != body; c = c.Parent {
		if c == nil || c.Parent == nil {
			return "", false
		}
		switch c.Type {
		case html.TextNode:
			k, total := 0, 0
			for s := c.Parent.FirstChild; s != nil; s = s.NextSibling {
				if s.Type == html.TextNode && !blank(s.Data) {
					total++
					if s == c {
						k = total
					}
				}
			}
			steps = append(steps, indexed("text()", k, total))
		case html.ElementNode:
			if c.Data == "tbody" && !hasAttrs(c) {
				continue // the parser's, not the book's (see walkXPath)
			}
			k, total := 0, 0
			for s := c.Parent.FirstChild; s != nil; s = s.NextSibling {
				if s.Type == html.ElementNode && s.Data == c.Data {
					total++
					if s == c {
						k = total
					}
				}
			}
			steps = append(steps, indexed(c.Data, k, total))
		}
	}
	for i, j := 0, len(steps)-1; i < j; i, j = i+1, j-1 {
		steps[i], steps[j] = steps[j], steps[i]
	}
	return "/" + strings.Join(steps, "/"), true
}

func indexed(name string, k, total int) string {
	if total > 1 {
		return name + "[" + strconv.Itoa(k) + "]"
	}
	return name
}

func nthElement(parent *html.Node, name string, idx int) *html.Node {
	for c := parent.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.ElementNode && c.Data == name {
			if idx--; idx == 0 {
				return c
			}
		}
	}
	return nil
}

func nthText(parent *html.Node, idx int) *html.Node {
	for c := parent.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.TextNode && !blank(c.Data) {
			if idx--; idx == 0 {
				return c
			}
		}
	}
	return nil
}

// findBody returns the document's <body>, or the root when there is none.
func findBody(root *html.Node) *html.Node {
	for c := root; c != nil; c = nextInOrder(c, root) {
		if c.Type == html.ElementNode && c.Data == "body" {
			return c
		}
	}
	return root
}

// textOrder numbers every text node in document order.
func textOrder(root *html.Node) map[*html.Node]int {
	order := map[*html.Node]int{}
	for c := root; c != nil; c = nextInOrder(c, nil) {
		if c.Type == html.TextNode {
			order[c] = len(order)
		}
	}
	return order
}

// nextInOrder is a pre-order step. With stop set, the walk descends into
// stop but never climbs out of it; with stop nil it runs to the end of the
// document, which is what "first text at or after an element" needs.
func nextInOrder(n, stop *html.Node) *html.Node {
	if n.FirstChild != nil {
		return n.FirstChild
	}
	for c := n; c != nil; c = c.Parent {
		if c == stop && stop != nil {
			return nil
		}
		if c.NextSibling != nil {
			return c.NextSibling
		}
	}
	return nil
}

func byteOffset(s string, cp int) int {
	i := 0
	for cp > 0 && i < len(s) {
		_, size := utf8.DecodeRuneInString(s[i:])
		i += size
		cp--
	}
	return i
}

func blank(s string) bool {
	return strings.IndexFunc(s, func(r rune) bool { return !unicode.IsSpace(r) }) < 0
}

func hasAttrs(n *html.Node) bool { return len(n.Attr) > 0 }

// LocatorForEntry opens the EPUB behind a user's entry and builds its
// Locator against the stored canonical text. It fails with ErrNoEpub when
// there is no EPUB to walk (a MOBI- or PDF-sourced text has no XPointers),
// and with ErrLocatorDrift when the stored text no longer matches the file;
// both mean the caller falls back to percentages.
func (ing *Ingester) LocatorForEntry(ctx context.Context, userID, entryID string) (*Locator, error) {
	ef, err := ing.OpenEpubFile(ctx, userID, entryID)
	if err != nil {
		return nil, err
	}
	defer ef.File.Close()
	et, err := ing.EnsureForEntry(ctx, userID, entryID)
	if err != nil {
		return nil, err
	}
	index, err := ing.LoadIndex(ctx, et)
	if err != nil {
		return nil, err
	}
	doc, err := epub.Parse(ef.File, ef.Info.Size())
	if err != nil {
		return nil, err
	}
	zr, err := zip.NewReader(ef.File, ef.Info.Size())
	if err != nil {
		return nil, err
	}
	return NewLocator(zr, doc, index)
}
