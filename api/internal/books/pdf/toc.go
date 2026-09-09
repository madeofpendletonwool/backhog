package pdf

import (
	"fmt"
	"io"
	"strings"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"

	"github.com/collinpendleton/backhog/api/internal/books/epub"
)

// The table of contents. A PDF's outline is a bookmark tree whose
// destinations must be resolved to page numbers, including named ones —
// that resolution is real work pdfcpu already does correctly, so it reads
// the outline while the text reader does everything else. A broken outline
// is not a broken book (the epub parser's rule): the pages still define
// the text, only titles are lost, and the report says why.

// tocEntry is one flattened outline item in document order.
type tocEntry struct {
	title string
	depth int
	page  int // 1-based
}

// attachTOC titles the page documents from the file's outline and reports
// what became of it. With no outline — and only then — the page documents
// stand as their own chapter marks, the same fallback position a MOBI
// without TOC positions falls back to.
func attachTOC(r io.ReaderAt, size int64, hasOutline bool, docs []epub.Doc) epub.TOCReport {
	if !hasOutline {
		return epub.TOCReport{Source: "pdf-page"}
	}
	report := epub.TOCReport{Source: "outline"}
	entries, err := outlineEntries(r, size)
	if err != nil {
		report.Err = err.Error()
		return report
	}
	report.Entries = len(entries)
	for _, e := range entries {
		if e.page < 1 || e.page > len(docs) {
			continue
		}
		d := &docs[e.page-1]
		if d.Title == "" {
			d.Title, d.Depth = e.title, e.depth
		}
	}
	return report
}

// outlineEntries reads the bookmark tree with pdfcpu (relaxed validation —
// the file already proved readable once) and flattens it in document
// order, numbering depth from zero like the epub and mobi TOC walkers.
func outlineEntries(r io.ReaderAt, size int64) ([]tocEntry, error) {
	conf := model.NewDefaultConfiguration()
	conf.ValidationMode = model.ValidationRelaxed
	bms, err := api.Bookmarks(io.NewSectionReader(r, 0, size), conf)
	if err != nil {
		return nil, fmt.Errorf("outline: %w", err)
	}
	var out []tocEntry
	var walk func(kids []pdfcpu.Bookmark, depth int)
	walk = func(kids []pdfcpu.Bookmark, depth int) {
		for _, b := range kids {
			if t := strings.TrimSpace(b.Title); t != "" && b.PageFrom >= 1 {
				out = append(out, tocEntry{title: t, depth: depth, page: b.PageFrom})
			}
			walk(b.Kids, depth+1)
		}
	}
	walk(bms, 0)
	return out, nil
}
