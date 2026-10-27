package epub

import (
	"strings"

	"golang.org/x/net/html"
)

// The typographic half of a block: what kind of thing it is and which of
// its words are set in italics, bold or small caps. The canonical text
// throws all of this away on purpose — it is an address space — and the
// display text kept only the characters, which is how a chapter opener set
// in three sizes on a Kindle came out of the web reader as three plain
// paragraphs. BlockStyle is the missing layer, carried beside the blocks
// and never inside them: Blocks is byte-for-byte what it was, so every
// stored offset, alignment and page anchor means what it meant.
//
// Everything here is structure the book asserts in its markup — element
// names, epub:type and ARIA roles, and a few whole-token class conventions
// — never its CSS. A publisher's stylesheet is the one part of an EPUB a
// reader is right to replace with its own.

// Block kinds. The empty kind is a body paragraph.
const (
	KindHeading  = "heading"
	KindSubtitle = "subtitle"
	KindEpigraph = "epigraph"
	KindQuote    = "quote"
	KindItem     = "item"
	KindPre      = "pre"
	KindCaption  = "caption"
)

// Inline marks, one letter each so a run's marks are a short string.
const (
	MarkBold      = 'b'
	MarkItalic    = 'i'
	MarkSmallCaps = 's'
	MarkSuper     = '^'
	MarkSub       = '_'
	MarkCode      = 'c'
	// MarkBreak tags the space a <br> became: still a word boundary in the
	// text, a line break on the page — which is what verse needs.
	MarkBreak = 'n'
)

// BlockStyle is one block's typography, parallel to Doc.Blocks.
type BlockStyle struct {
	Kind string `json:"kind,omitempty"`
	// Level is a heading's level, 1 (the chapter title) to 6.
	Level int `json:"level,omitempty"`
	// Break reports a scene break (<hr>) between this block and the last.
	// The break owns no text, so it rides on the block after it rather
	// than becoming a block of its own.
	Break bool `json:"break,omitempty"`
	// Runs are the block's raw text cut where its marks change; their
	// concatenation is the block's raw text exactly.
	Runs []Run `json:"runs,omitempty"`
}

// Run is a stretch of a block's text under one set of marks.
type Run struct {
	Text  string `json:"t"`
	Marks string `json:"m,omitempty"`
}

// marks counts how deep the walk is inside each inline mark, so nested and
// repeated wrappers (<b><span class="bold">) close cleanly.
type marks map[rune]int

func (m marks) String() string {
	var b strings.Builder
	for _, r := range []rune{MarkBold, MarkItalic, MarkSmallCaps, MarkSuper, MarkSub, MarkCode} {
		if m[r] > 0 {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// inlineMarks is what an inline element adds: its tag's meaning, plus the
// class conventions production pipelines use for spans ("ePub-B-SC" is
// bold small caps, "italic", "smallcaps"). Classes are split on - and _
// and matched as whole parts, so "block_2" and "calibre1" mean nothing.
func inlineMarks(n *html.Node) []rune {
	var out []rune
	switch n.Data {
	case "b", "strong":
		out = append(out, MarkBold)
	case "i", "em", "cite", "dfn", "var":
		out = append(out, MarkItalic)
	case "small":
		out = append(out, MarkSmallCaps)
	case "sup":
		out = append(out, MarkSuper)
	case "sub":
		out = append(out, MarkSub)
	case "code", "kbd", "samp", "tt":
		out = append(out, MarkCode)
	}
	if n.Data != "span" {
		return out
	}
	for _, a := range n.Attr {
		if a.Key != "class" {
			continue
		}
		for _, token := range strings.Fields(strings.ToLower(a.Val)) {
			for _, part := range strings.FieldsFunc(token, func(r rune) bool { return r == '-' || r == '_' }) {
				switch part {
				case "b", "bold", "strong":
					out = append(out, MarkBold)
				case "i", "it", "ital", "italic", "em":
					out = append(out, MarkItalic)
				case "sc", "smallcaps", "smcap", "smcaps":
					out = append(out, MarkSmallCaps)
				}
			}
		}
	}
	return out
}

// containerKind is the kind a block element lends the blocks inside it.
func containerKind(n *html.Node) string {
	switch n.Data {
	case "li", "dd", "dt":
		return KindItem
	case "pre":
		return KindPre
	case "figcaption", "caption":
		return KindCaption
	}
	if hasSemantic(n, "epigraph") {
		return KindEpigraph
	}
	if hasSemantic(n, "subtitle") {
		return KindSubtitle
	}
	if n.Data == "blockquote" {
		return KindQuote
	}
	return ""
}

// hasSemantic reports an epub:type or doc-* ARIA role naming what. The
// class "subtitle" counts for subtitles, the one heading class that is not
// a title.
func hasSemantic(n *html.Node, what string) bool {
	for _, a := range n.Attr {
		switch a.Key {
		case "epub:type", "type":
			for _, f := range strings.Fields(a.Val) {
				if f == what {
					return true
				}
			}
		case "role":
			for _, f := range strings.Fields(a.Val) {
				if f == "doc-"+what {
					return true
				}
			}
		case "class":
			if what == "subtitle" {
				for _, f := range strings.Fields(strings.ToLower(a.Val)) {
					if f == "subtitle" {
						return true
					}
				}
			}
		}
	}
	return false
}

// headingLevel is a heading element's level; a heading asserted by class or
// epub:type is a chapter title, level 1.
func headingLevel(n *html.Node) int {
	if headingTags[n.Data] {
		return int(n.Data[1] - '0')
	}
	return 1
}

// containerTags are block elements that hold other blocks. A title class or
// epub:type on one describes what it contains, so it never makes the blocks
// inside it headings — only a leaf (<h2>, <p class="ct">) is a title.
var containerTags = map[string]bool{
	"article": true, "aside": true, "body": true, "div": true, "figure": true,
	"footer": true, "header": true, "main": true, "nav": true, "section": true,
	"blockquote": true,
}

// kindRank orders kinds when several apply: a heading inside a blockquote
// is still a heading, and a list inside an epigraph is still a list.
var kindRank = map[string]int{
	"": 0, KindQuote: 1, KindEpigraph: 2, KindCaption: 3, KindPre: 4,
	KindItem: 5, KindSubtitle: 6, KindHeading: 7,
}
