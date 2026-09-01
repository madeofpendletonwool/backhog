package books

import "sort"

// BlockIndex is the JSON sidecar mapping canonical char offsets to
// locations and back, good enough for the reader to scroll to a position:
//
//	charOffset → (href, spineIndex, blockIndex)   via Resolve
//	(href, blockIndex) → charOffset               via Locate
//
// Both directions are O(log n) (binary searches; the href scan is linear in
// the number of spine documents, which is tens, not thousands). Stored at
// {EPUB_TEXT_DIR}/{id}.blocks.json next to the canonical text.
type BlockIndex struct {
	Version       int          `json:"version"`
	ParserVersion string       `json:"parser_version"`
	CharCount     int          `json:"char_count"`
	Documents     []IndexedDoc `json:"documents"`
}

// IndexedDoc is one spine document's slice of the index.
type IndexedDoc struct {
	Href       string `json:"href"`
	SpineIndex int    `json:"spine_index"`
	// CharStart/CharEnd mirror the epub_chapters row.
	CharStart int `json:"char_start"`
	CharEnd   int `json:"char_end"`
	// Blocks holds the absolute canonical offset of every block-level
	// element's first byte, ascending. A block runs from its offset to the
	// next block's offset (or the document's CharEnd).
	Blocks []int `json:"blocks"`
}

// Loc is a resolved position in the book.
type Loc struct {
	Href       string `json:"href"`
	SpineIndex int    `json:"spine_index"`
	BlockIndex int    `json:"block_index"`
	// CharStart is the offset of the containing block.
	CharStart int `json:"char_start"`
}

// docByOffset returns the document containing offset, preferring non-empty
// documents when the offset lands on a shared boundary.
func (b *BlockIndex) docByOffset(offset int) (int, bool) {
	docs := b.Documents
	// Last document whose CharStart <= offset.
	i := sort.Search(len(docs), func(i int) bool { return docs[i].CharStart > offset }) - 1
	for i >= 0 {
		if docs[i].CharEnd > offset && len(docs[i].Blocks) > 0 {
			return i, true
		}
		// Empty document or boundary: keep walking left for the document
		// that owns the bytes up to this point.
		i--
	}
	return 0, false
}

// Resolve maps a canonical char offset to the block-level element
// containing it.
func (b *BlockIndex) Resolve(offset int) (Loc, bool) {
	if offset < 0 || offset >= b.CharCount {
		return Loc{}, false
	}
	di, ok := b.docByOffset(offset)
	if !ok {
		return Loc{}, false
	}
	doc := b.Documents[di]
	// Last block whose offset <= the doc's CharEnd and <= offset.
	blocks := doc.Blocks
	bi := sort.Search(len(blocks), func(i int) bool { return blocks[i] > offset }) - 1
	if bi < 0 {
		bi = 0
	}
	return Loc{
		Href:       doc.Href,
		SpineIndex: doc.SpineIndex,
		BlockIndex: bi,
		CharStart:  blocks[bi],
	}, true
}

// Locate maps a document href and block index back to its canonical offset.
func (b *BlockIndex) Locate(href string, blockIndex int) (int, bool) {
	for i := range b.Documents {
		if b.Documents[i].Href != href {
			continue
		}
		blocks := b.Documents[i].Blocks
		if blockIndex < 0 || blockIndex >= len(blocks) {
			return 0, false
		}
		return blocks[blockIndex], true
	}
	return 0, false
}
