package epub

import (
	"archive/zip"
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
// at every block-element boundary.
type extractor struct {
	blocks []string
	buf    strings.Builder
}

// extractBlocks returns the raw text of every block-level element in the
// spine document at href, in document order.
func extractBlocks(zr *zip.Reader, href string) ([]string, error) {
	root, err := parseHTML(zr, href)
	if err != nil {
		return nil, err
	}
	ex := &extractor{}
	ex.walk(root)
	ex.flush()
	return ex.blocks, nil
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

// flush emits the buffered inline text as a block if it holds anything
// besides whitespace, and resets the buffer. Blocks keep their internal
// whitespace (the canonicalizer collapses it); only the edges are trimmed.
func (ex *extractor) flush() {
	if s := strings.TrimSpace(ex.buf.String()); s != "" {
		ex.blocks = append(ex.blocks, s)
	}
	ex.buf.Reset()
}
