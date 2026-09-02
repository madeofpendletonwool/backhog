package epub

import (
	"archive/zip"
	"path"
	"strings"

	"golang.org/x/net/html"
)

// blockTags are the elements that start a new block: their inline content
// becomes one entry of Doc.Blocks. Containers (div, section, ...) recurse
// so their block children become blocks of their own; text sitting directly
// in a container is flushed as a block when the next block starts.
var blockTags = map[string]bool{
	"address": true, "article": true, "aside": true,
	"blockquote": true, "caption": true, "dd": true,
	"div": true, "dt": true, "figcaption": true, "figure": true,
	"footer": true, "h1": true, "h2": true, "h3": true,
	"h4": true, "h5": true, "h6": true, "header": true,
	"hr": true, "li": true, "main": true, "nav": true,
	"p": true, "pre": true, "section": true, "td": true,
	"th": true,
}

// skipTags contribute no text at all: script/style bodies are code, not
// prose, and must never leak into the canonical text.
var skipTags = map[string]bool{
	"script": true, "style": true, "head": true,
	"title": true, "noscript": true, "template": true,
}

// extractor walks one document, buffering inline text and flushing a block
// at every block-element boundary. Illustrations are collected alongside,
// anchored to the block they precede.
type extractor struct {
	blocks []string
	images []Image
	buf    strings.Builder
	// baseDir is the directory of the document being walked, which image
	// references resolve against; exists reports whether a resolved zip
	// path is really in this EPUB.
	baseDir string
	exists  func(string) bool
}

// extractBlocks returns the raw text of every block-level element in the
// spine document at href, in document order, together with the internal
// images it references.
func extractBlocks(zr *zip.Reader, href string) ([]string, []Image, error) {
	root, err := parseHTML(zr, href)
	if err != nil {
		return nil, nil, err
	}
	ex := &extractor{
		baseDir: path.Dir(href),
		exists:  func(name string) bool { return zipLookup(zr, name) != nil },
	}
	ex.walk(root)
	ex.flush()
	return ex.blocks, ex.images, nil
}

// walk descends the node tree. Inline content accumulates in the buffer; a
// block element first flushes what precedes it, then contributes its own
// content, which the matching flush on the way out emits as one block.
func (ex *extractor) walk(n *html.Node) {
	switch n.Type {
	case html.TextNode:
		ex.buf.WriteString(n.Data)
	case html.ElementNode:
		if skipTags[n.Data] {
			return
		}
		if n.Data == "br" {
			// A line break is a word boundary, not a paragraph boundary:
			// the surrounding text stays one block, with a space.
			ex.buf.WriteByte(' ')
			return
		}
		if n.Data == "img" || n.Data == "image" {
			// <img> in XHTML, <image> inside an SVG cover wrapper. Both
			// contribute no text, only a place in the running order.
			ex.image(n)
			return
		}
		if blockTags[n.Data] {
			ex.flush()
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				ex.walk(c)
			}
			ex.flush()
			return
		}
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		ex.walk(c)
	}
}

// image records an illustration at the point it appears, anchored to the
// block index that is about to be filled — an image sitting between two
// paragraphs precedes the second one, and an image inside a paragraph
// precedes that paragraph, since the paragraph has not flushed yet.
// References that resolveAsset refuses are dropped silently: a book that
// hotlinks an image is not a broken book, it just does not get that image.
func (ex *extractor) image(n *html.Node) {
	var src, alt string
	for _, a := range n.Attr {
		switch a.Key {
		case "src", "href":
			// xlink:href and href arrive with the same Key; the first
			// non-empty one is the reference.
			if src == "" {
				src = strings.TrimSpace(a.Val)
			}
		case "alt":
			alt = strings.TrimSpace(a.Val)
		}
	}
	href, ok := ex.resolveAsset(src)
	if !ok {
		return
	}
	ex.images = append(ex.images, Image{Href: href, Alt: alt, BeforeBlock: len(ex.blocks)})
}

// resolveAsset turns a document-relative reference into the zip path it
// names, and refuses everything that is not a plain relative reference to a
// file that is really in this EPUB.
//
// This is the choke point for design invariant 5: an EPUB is untrusted
// input, and a reference carrying a scheme (http:, https:, data:, file:) or
// a protocol-relative "//host/..." is a third-party load. Dropping those
// here — rather than in the reader — means no remote address is ever
// written to the sidecar, served in a payload, or given to a browser.
// A reference that climbs out of the zip with ".." is refused for the same
// reason the audio endpoint re-checks containment: the row is not authority.
func (ex *extractor) resolveAsset(src string) (string, bool) {
	if src == "" || strings.HasPrefix(src, "#") || strings.HasPrefix(src, "//") {
		return "", false
	}
	if i := strings.Index(src, ":"); i >= 0 && i < strings.IndexAny(src+"/", "/") {
		return "", false // a scheme, so not a file in this book
	}
	href := joinZipPath(ex.baseDir, src)
	if href == "" || href == "." || href == ".." || strings.HasPrefix(href, "../") {
		return "", false
	}
	if ex.exists == nil || !ex.exists(href) {
		return "", false
	}
	return href, true
}

// flush emits the buffered inline text as a block if it holds anything
// besides whitespace, and resets the buffer. Blocks keep their internal
// whitespace (the canonicalizer collapses it); only the edges are trimmed.
func (ex *extractor) flush() {
	if s := strings.TrimSpace(ex.buf.String()); s != "" {
		ex.blocks = append(ex.blocks, s)
	}
	ex.buf.Reset()
}
