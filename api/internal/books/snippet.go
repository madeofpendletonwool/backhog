package books

import (
	"context"
	"os"
	"strings"
	"unicode/utf8"

	"github.com/collinpendleton/backhog/api/booktext"
	"github.com/collinpendleton/backhog/api/internal/models"
)

// contextBytes is how much display text surrounds a snippet's match on each
// side before it is elided. About a line and a half of prose either way: enough
// to recognise the passage, short enough that twenty of them are a list rather
// than a chapter.
const contextBytes = 140

// Snippet is one canonical span rendered as the book reads it: the matched
// words with their own punctuation and capitals, and the prose either side.
//
// The split mirrors the passage endpoint's context object, so the same client
// rendering serves both.
type Snippet struct {
	Before  string `json:"before"`
	Passage string `json:"passage"`
	After   string `json:"after"`
}

// Snippets renders canonical char spans as readable prose for one book.
//
// It exists as a loaded object rather than a function because both files it
// needs — the block-offset sidecar and the display text — are read whole, and a
// search answering twenty hits would otherwise read a multi-megabyte file twenty
// times. Build one per request, ask it for every hit.
type Snippets struct {
	index   *BlockIndex
	display []byte
}

// Snippets loads a book's display text and block index together.
func (ing *Ingester) Snippets(ctx context.Context, et models.EpubText) (*Snippets, error) {
	index, err := ing.LoadIndex(ctx, et)
	if err != nil {
		return nil, err
	}
	display, err := os.ReadFile(ing.DisplayPath(et.ID))
	if err != nil {
		return nil, err
	}
	return &Snippets{index: index, display: display}, nil
}

// At renders the canonical span [charStart, charEnd) as prose.
//
// The unit is the block containing charStart — a paragraph, which is the
// smallest thing that reads like the book rather than like a grep. A span
// running past the end of that block (a phrase the canonical text joined across
// a paragraph break) highlights the part that lives in it; the rest is context
// the reader can see for themselves once they jump there.
//
// ok is false when the span is outside the text, or when the sidecar and the
// display file disagree about how many blocks a document has — which only
// happens if one was written by an older parser, and is the same mismatch the
// reader refuses to render through.
func (s *Snippets) At(charStart, charEnd int) (Snippet, bool) {
	loc, ok := s.index.Resolve(charStart)
	if !ok {
		return Snippet{}, false
	}
	doc, ok := s.docBySpine(loc.SpineIndex)
	if !ok {
		return Snippet{}, false
	}
	if doc.DisplayStart < 0 || doc.DisplayEnd < doc.DisplayStart || doc.DisplayEnd > len(s.display) {
		return Snippet{}, false
	}
	blocks := strings.Split(string(s.display[doc.DisplayStart:doc.DisplayEnd]), "\n")
	if loc.BlockIndex < 0 || loc.BlockIndex >= len(blocks) || loc.BlockIndex >= len(doc.Blocks) {
		return Snippet{}, false
	}
	block := blocks[loc.BlockIndex]

	// The match is addressed relative to the block it landed in, and clamped
	// to it: doc.CharEnd carries a separator space this block's prose does not.
	blockStart := doc.Blocks[loc.BlockIndex]
	canonLen := len(booktext.Normalize(block))
	from := charStart - blockStart
	to := min(charEnd-blockStart, canonLen)

	start, end, ok := booktext.SpanInDisplay(block, from, to)
	if !ok {
		return Snippet{}, false
	}
	return Snippet{
		Before:  elideHead(block[:start]),
		Passage: block[start:end],
		After:   elideTail(block[end:]),
	}, true
}

// docBySpine finds a spine document in the index. Documents are tens, not
// thousands, so the scan is cheaper than an index over them.
func (s *Snippets) docBySpine(spine int) (IndexedDoc, bool) {
	for i := range s.index.Documents {
		if s.index.Documents[i].SpineIndex == spine {
			return s.index.Documents[i], true
		}
	}
	return IndexedDoc{}, false
}

// elideHead keeps the tail of the text before a match, cut at a word boundary
// and marked with an ellipsis so nobody reads a fragment as a sentence.
func elideHead(s string) string {
	if len(s) <= contextBytes {
		return s
	}
	cut := len(s) - contextBytes
	for cut < len(s) && !isCutPoint(s[cut]) {
		cut++
	}
	for cut < len(s) && isCutPoint(s[cut]) {
		cut++
	}
	if cut >= len(s) {
		return "…"
	}
	return "…" + s[cut:]
}

// elideTail is elideHead for the text after a match.
func elideTail(s string) string {
	if len(s) <= contextBytes {
		return s
	}
	cut := contextBytes
	for cut > 0 && !isCutPoint(s[cut]) {
		cut--
	}
	if cut == 0 {
		// One very long word: cut it on a rune boundary rather than mid-glyph.
		cut = contextBytes
		for cut > 0 && !utf8.RuneStart(s[cut]) {
			cut--
		}
	}
	return strings.TrimRight(s[:cut], " ") + "…"
}

// isCutPoint marks the bytes an elision may land on: ASCII whitespace, which
// is also always a rune boundary.
func isCutPoint(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == '\r'
}
