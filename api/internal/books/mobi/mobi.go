// Package mobi parses MOBI, AZW and AZW3 (KF8) files into the same spine
// structure the EPUB parser produces: ordered chapter documents, each with
// its block-level text runs and its TOC title. It is pure parsing — no
// normalization, no storage, no filesystem — mirroring internal/books/epub,
// and it deliberately returns *epub.Document: that type is the arena's
// shared extraction model (blocks + chapter shape), so the canonicalizer,
// the chapter rows and the block index are one code path for every ebook
// format and cannot drift apart.
//
// Decompression and container handling live in mobi-go
// (github.com/madeofpendletonwool/mobi-go), the pure-Go MOBI reader this
// package was written against. DRM-protected files are refused by the
// library with mobi.ErrDRM before any content is parsed and pass through
// unchanged — this project does not handle DRM by decision.
package mobi

import (
	"fmt"
	"io"
	"sort"
	"strings"

	mobigo "github.com/madeofpendletonwool/mobi-go"

	"github.com/collinpendleton/backhog/api/internal/books/epub"
)

// Parse reads a MOBI/AZW/AZW3 file held in r and returns its chapter
// structure. A DRM-protected file fails with an error wrapping
// mobi.ErrDRM; a structurally invalid one fails with the library's
// mobi.ErrCorrupt / mobi.ErrNotPalmDB sentinels.
//
// The two format families map onto the chapter model differently:
//
//   - KF8 (AZW3, and the KF8 half of combo files): one chapter per
//     reassembled XHTML section in spine order, skipping non-linear
//     sections exactly as the EPUB spine skips linear="no" documents.
//     Titles come from the NCX TOC's pos pairs, resolved by the library to
//     section indexes.
//   - MOBI6: chapters split at the TOC's filepos byte offsets into the raw
//     book text (INDX NCX when the book carries one, else the legacy HTML
//     <toc> block — the library owns that fallback). With no TOC at all,
//     the <mbp:pagebreak>-delimited sections are the chapters.
//
// Every offset mobi-go reports is a byte offset into the raw, possibly
// windows-1252 text; all slicing happens on those raw bytes and decoding
// runs per chapter slice afterwards.
func Parse(r io.ReaderAt, size int64) (*epub.Document, error) {
	b, err := mobigo.Open(r, size)
	if err != nil {
		return nil, err
	}
	if b.IsKF8() {
		return parseKF8(b)
	}
	return parseMOBI6(b)
}

// parseKF8 builds one chapter per linear KF8 section.
func parseKF8(b *mobigo.Book) (*epub.Document, error) {
	// A broken TOC is not a broken book: sections still define the
	// canonical text, only titles are lost (the epub parser's rule).
	toc, _ := b.TOC()

	type title struct {
		label string
		depth int
	}
	titles := map[int]title{}
	var walk func(items []mobigo.TOCItem, depth int)
	walk = func(items []mobigo.TOCItem, depth int) {
		for _, it := range items {
			if it.Section >= 0 && strings.TrimSpace(it.Label) != "" {
				if _, ok := titles[it.Section]; !ok {
					titles[it.Section] = title{strings.TrimSpace(it.Label), depth}
				}
			}
			walk(it.Children, depth+1)
		}
	}
	walk(toc, 0)

	secs := b.KF8Sections()
	doc := &epub.Document{Docs: make([]epub.Doc, 0, len(secs))}
	for i, sec := range secs {
		// A skeleton with zero fragments holds only structural markup —
		// linear="no" in the EPUB spine's terms — and owns no chapter.
		if !sec.Linear {
			continue
		}
		d := epub.Doc{
			SpineIndex: len(doc.Docs),
			Href:       fmt.Sprintf("mobi:section:%d", i),
			Blocks:     epub.ExtractBlocksHTML(sec.XHTML()),
		}
		if t, ok := titles[i]; ok {
			d.Title, d.Depth = t.label, t.depth
		}
		doc.Docs = append(doc.Docs, d)
	}
	return doc, nil
}

// anchor is one usable MOBI6 TOC entry: a byte offset into the raw text
// with the label and tree depth of the entry that named it.
type anchor struct {
	start int
	label string
	depth int
}

// parseMOBI6 splits the raw book text into chapters at its TOC's filepos
// offsets, falling back to pagebreak sections when no TOC carries usable
// positions.
func parseMOBI6(b *mobigo.Book) (*epub.Document, error) {
	raw := b.RawText()
	if len(raw) == 0 {
		return &epub.Document{}, nil
	}

	// The book's encoding decides how chapter slices decode. Text() is the
	// whole raw text decoded per the header's codepage; when it is the same
	// length as the raw bytes the decode was byte-for-byte (UTF-8, or
	// cp1252 holding only ASCII), so string(slice) is exact. Any length
	// difference means cp1252 with high bytes, whose decode is the table
	// below — the same consortium mapping the library decodes with.
	utf8 := len(b.Text()) == len(raw)

	anchors := tocAnchors(b)
	starts := chapterStarts(anchors, len(raw))
	if len(starts) <= 1 {
		// No TOC (or none of its entries carried a usable position): the
		// pagebreak-delimited sections are the chapters.
		starts = starts[:0]
		for _, s := range b.Sections() {
			st, _ := s.ByteRange()
			if st > 0 && st < len(raw) {
				starts = append(starts, st)
			}
		}
		sort.Ints(starts)
		starts = append([]int{0}, dedupe(starts)...)
		anchors = nil
	}

	docs := make([]epub.Doc, 0, len(starts))
	for i, start := range starts {
		end := len(raw)
		if i+1 < len(starts) {
			end = starts[i+1]
		}
		body := raw[start:end]
		var chapterHTML string
		if utf8 {
			chapterHTML = string(body)
		} else {
			chapterHTML = decodeCP1252(body)
		}
		// A filepos may land mid-tag; html.Parse recovers by dropping the
		// broken fragment, exactly as it does for EPUB documents that open
		// mid-markup. The chapters partition the raw text regardless.
		docs = append(docs, epub.Doc{
			SpineIndex: i,
			Href:       fmt.Sprintf("mobi:chapter:%d", i),
			Blocks:     epub.ExtractBlocksHTML(chapterHTML),
		})
	}

	// Titles: the first labelled TOC entry whose position falls inside the
	// chapter's range, in byte order (the epub parser's "first TOC entry
	// targeting this document" rule).
	ai := 0
	for i := range docs {
		start, end := starts[i], len(raw)
		if i+1 < len(starts) {
			end = starts[i+1]
		}
		for ai < len(anchors) && anchors[ai].start < start {
			ai++
		}
		for j := ai; j < len(anchors) && anchors[j].start < end; j++ {
			if anchors[j].label != "" {
				docs[i].Title = anchors[j].label
				docs[i].Depth = anchors[j].depth
				break
			}
		}
	}
	return &epub.Document{Docs: docs}, nil
}

// tocAnchors flattens the TOC tree into document-order anchors, keeping
// every entry whose StartByte addresses the raw text. The library owns the
// INDX-NCX-versus-legacy-<toc> fallback; a TOC that fails to parse yields
// no anchors and the pagebreak fallback takes over.
func tocAnchors(b *mobigo.Book) []anchor {
	items, err := b.TOC()
	if err != nil {
		return nil
	}
	var out []anchor
	var walk func(items []mobigo.TOCItem, depth int)
	walk = func(items []mobigo.TOCItem, depth int) {
		for _, it := range items {
			if it.StartByte >= 0 {
				out = append(out, anchor{start: it.StartByte, label: strings.TrimSpace(it.Label), depth: depth})
			}
			walk(it.Children, depth+1)
		}
	}
	walk(items, 0)
	sort.SliceStable(out, func(i, j int) bool { return out[i].start < out[j].start })
	return out
}

// chapterStarts turns TOC anchors into chapter boundaries: sorted, deduped
// byte offsets, prefixed with 0 so the chapters partition [0, rawLen)
// exactly. Offsets at or past the end of the text would name an empty
// trailing chapter and are dropped; the leading 0 makes an offset of 0
// redundant.
func chapterStarts(anchors []anchor, rawLen int) []int {
	seen := map[int]bool{}
	starts := []int{0}
	for _, a := range anchors {
		if a.start > 0 && a.start < rawLen && !seen[a.start] {
			seen[a.start] = true
			starts = append(starts, a.start)
		}
	}
	sort.Ints(starts)
	return starts
}

// dedupe returns s without adjacent duplicates; s must be sorted.
func dedupe(s []int) []int {
	out := s[:0]
	for i, v := range s {
		if i == 0 || s[i-1] != v {
			out = append(out, v)
		}
	}
	return out
}
