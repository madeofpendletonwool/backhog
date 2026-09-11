package books

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/collinpendleton/backhog/api/internal/books/pdf"
	"github.com/collinpendleton/backhog/api/internal/models"
)

// Page serving for image-native PDFs — the paged reader's storage half.
//
// The display strategy is **companion page images** (decided against a
// vendored PDF.js; BOOKS.md holds the tradeoff): the embedded page image is
// extracted lazily, one page at a time, written beside the canonical texts
// as `{pdf-file-id}.page-N.{png|jpg}`, and served through the
// asset-endpoint pattern — authenticated per request, path-contained,
// ETagged, never a capability in the URL.
//
// Lazy is what bounds the disk: a companion exists only for a page someone
// actually looked at, so a 500 MB comic read to page 30 costs thirty page
// images, and read to the end costs at most roughly the size of the
// source's own image payload — never a multiple of it. The per-process
// extraction cost is bounded the same way: one validated pdfcpu context is
// held open per file (a small LRU), so the whole-file pass happens once and
// every page after it is a single decode.

// ErrNotPaged reports an entry the paged reader cannot serve: its
// designated text file is not an image-native PDF. It is the paged twin of
// the text endpoints' image-native refusal — the two populations each name
// the other honestly.
var ErrNotPaged = errors.New("books: not a paged book")

// NotPagedError is ErrNotPaged with the reason a reader can be told: an
// epub or mobi primary, or a PDF whose text layer won and reads as prose.
type NotPagedError struct {
	Reason string
}

func (e *NotPagedError) Error() string { return "books: not a paged book: " + e.Reason }
func (e *NotPagedError) Unwrap() error { return ErrNotPaged }

// ErrPageOutOfRange reports a page index outside the classified count.
var ErrPageOutOfRange = errors.New("books: page index outside this book")

// pageSourceCap is how many PDFs hold an open extraction context at once.
// One per actively-read comic is the real workload; a few spares cover
// browsing between books without paying the whole-file pass again.
const pageSourceCap = 4

// BookPage is one manifest entry: the page's image shape, known before any
// image bytes are paid for, so a reader can lay out every page and label
// the ones it must refuse.
type BookPage struct {
	Index    int  `json:"index"`
	Width    int  `json:"width"`
	Height   int  `json:"height"`
	HasImage bool `json:"has_image"`
}

// PageManifest is the paged reader's whole world: the page axis's
// denominator and every page's servability.
type PageManifest struct {
	PageCount int        `json:"page_count"`
	Pages     []BookPage `json:"pages"`
}

// PageAsset is one servable page raster — the paged twin of the EPUB
// Asset in asset.go.
type PageAsset struct {
	ContentType string
	Data        []byte
	// ModTime is the companion file's own timestamp — stable across
	// re-reads, and what the endpoint validates against.
	ModTime time.Time
}

// pagedBook resolves an entry to its image-native primary: the file door,
// the designated text file, and the gate's classification — the dispatch
// the reader UI makes on the entry's classification, not its extension.
func (ing *Ingester) pagedBook(ctx context.Context, userID, entryID string) (models.MediaFile, models.PDFFile, error) {
	bookID, err := ing.store.BookFilesForEntry(ctx, userID, entryID)
	if err != nil {
		return models.MediaFile{}, models.PDFFile{}, err
	}
	f, err := ing.store.EpubMediaFileForBook(ctx, bookID)
	if err != nil {
		return models.MediaFile{}, models.PDFFile{}, ErrNoEpub
	}
	if !strings.EqualFold(filepath.Ext(f.Path), ".pdf") {
		return models.MediaFile{}, models.PDFFile{}, &NotPagedError{
			Reason: "this book's text is not a PDF — the paged reader serves comics, scans and picture books",
		}
	}
	pf, err := ing.EnsurePDFFile(ctx, f)
	if err != nil {
		return models.MediaFile{}, models.PDFFile{}, err
	}
	if pf.Classification != models.PDFImageNative {
		// A hybrid PDF (text plus full-page art) follows its text
		// classification: it reads in the scrolled reader.
		return models.MediaFile{}, models.PDFFile{}, &NotPagedError{
			Reason: "this PDF has a readable text layer — it reads as prose in the scrolled reader",
		}
	}
	return f, pf, nil
}

// PagedBookForEntry resolves one of the caller's entries to its
// image-native primary — the exported half of pagedBook, for the surfaces
// (the OCR queue) that need the file and its classification rather than a
// page. Every refusal pagedBook names, it names identically.
func (ing *Ingester) PagedBookForEntry(ctx context.Context, userID, entryID string) (models.MediaFile, models.PDFFile, error) {
	return ing.pagedBook(ctx, userID, entryID)
}

// Pages returns an entry's page manifest: the classified page count and
// every page's image shape, cheaply, from stub lookups — no page is decoded
// until it is asked for by name.
//
// A file whose pages carry no images at all is vector art, and under the
// companion-image strategy it is a named refusal with the same honesty as
// `.kfx` — never a reader of empty pages.
func (ing *Ingester) Pages(ctx context.Context, userID, entryID string) (PageManifest, error) {
	f, pf, err := ing.pagedBook(ctx, userID, entryID)
	if err != nil {
		return PageManifest{}, err
	}
	src, err := ing.pageSourceFor(f)
	if err != nil {
		return PageManifest{}, err
	}
	infos, err := src.Pages()
	if err != nil {
		ing.dropPageSource(f.ID)
		return PageManifest{}, err
	}
	manifest := PageManifest{PageCount: pf.PageCount, Pages: make([]BookPage, len(infos))}
	servable := 0
	for i, p := range infos {
		manifest.Pages[i] = BookPage{Index: p.Index, Width: p.Width, Height: p.Height, HasImage: p.HasImage}
		if p.HasImage {
			servable++
		}
	}
	if servable == 0 {
		return PageManifest{}, &NotPagedError{
			Reason: "this PDF's pages are vector art with no embedded page images, which the reader cannot serve (a PDF.js-style renderer is the unsupported alternative)",
		}
	}
	return manifest, nil
}

// PageImage returns one page's raster for one of the caller's entries,
// extracting it lazily on first request and serving the companion file ever
// after. Ownership and path containment are re-checked on every request —
// the URL is not a capability, the same rule the EPUB asset endpoint holds.
func (ing *Ingester) PageImage(ctx context.Context, userID, entryID string, page int) (PageAsset, error) {
	f, pf, err := ing.pagedBook(ctx, userID, entryID)
	if err != nil {
		return PageAsset{}, err
	}
	return ing.PageImageByFile(ctx, f, pf, page)
}

// PageImageByFile is PageImage against a resolved media file and its
// classification — the internal OCR worker's door. The worker holds the
// shared token and no session, so it cannot walk the entry-scoped path;
// what it may read was ownership-checked when the job was enqueued. The
// bytes are the exact same companion-or-extract stream the reader gets:
// there is no second decode path, and the worker's page fetches warm the
// same companion cache the paged reader serves from.
func (ing *Ingester) PageImageByFile(ctx context.Context, f models.MediaFile, pf models.PDFFile, page int) (PageAsset, error) {
	if page < 0 || page >= pf.PageCount {
		return PageAsset{}, fmt.Errorf("%w: page %d of %d", ErrPageOutOfRange, page, pf.PageCount)
	}

	// The companion cache: a page looked at once is a file read forever
	// after. Both extensions are probed because the servable type is only
	// known after extraction.
	for _, c := range []struct{ ext, ctype string }{
		{"png", "image/png"},
		{"jpg", "image/jpeg"},
	} {
		if img, ok := ing.readPageCompanion(pf.ID, page, c.ext, c.ctype); ok {
			return img, nil
		}
	}

	src, err := ing.pageSourceFor(f)
	if err != nil {
		return PageAsset{}, err
	}
	img, err := src.Image(page)
	if err != nil {
		// A page that defeats extraction may still be a source-level
		// problem; a named page-level refusal is not. Only the former
		// drops the cached context.
		if !errors.Is(err, pdf.ErrNoPageImage) && !errors.Is(err, pdf.ErrUnsupportedPageImage) &&
			!errors.Is(err, pdf.ErrPageOutOfRange) {
			ing.dropPageSource(f.ID)
		}
		return PageAsset{}, err
	}
	ext := "png"
	if img.ContentType == "image/jpeg" {
		ext = "jpg"
	}
	path := ing.PageImagePath(pf.ID, page, ext)
	out := PageAsset{ContentType: img.ContentType, Data: img.Data, ModTime: time.Now()}
	// Serve the bytes even when the companion write fails (a full disk
	// degrades to re-extraction, not to a broken reader).
	if err := writeFileAtomic(path, img.Data); err == nil {
		if info, err := os.Stat(path); err == nil {
			out.ModTime = info.ModTime()
		}
	}
	return out, nil
}

// PageImagePath is where a page's companion image lives. Page numbers are
// 1-based in the filename (a human grepping the directory) and 0-based
// everywhere in the API (the position axis).
func (ing *Ingester) PageImagePath(id string, page int, ext string) string {
	return filepath.Join(ing.dir, id+".page-"+strconv.Itoa(page+1)+"."+ext)
}

// readPageCompanion serves a cached page image off disk.
func (ing *Ingester) readPageCompanion(id string, page int, ext, contentType string) (PageAsset, bool) {
	path := ing.PageImagePath(id, page, ext)
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return PageAsset{}, false
	}
	if info.Size() > pageImageReadLimit {
		return PageAsset{}, false
	}
	data, err := os.ReadFile(path)
	if err != nil || len(data) == 0 {
		return PageAsset{}, false
	}
	return PageAsset{ContentType: contentType, Data: data, ModTime: info.ModTime()}, true
}

// pageImageReadLimit bounds one companion read. Companions are written by
// this process from an extraction already capped at pageImageLimit; a file
// bigger than that is not one of ours.
const pageImageReadLimit = 64 << 20

// clearPageCompanions drops every cached page image for a PDF file row —
// the re-classification path, where the old parse's pages are stale.
func (ing *Ingester) clearPageCompanions(id string) {
	matches, err := filepath.Glob(filepath.Join(ing.dir, id+".page-*"))
	if err != nil {
		return
	}
	for _, m := range matches {
		os.Remove(m)
	}
}

// pageSourceFor returns the open extraction context for a media file,
// opening it (the one-time whole-file pass) on first use. A small LRU
// bounds open handles; eviction closes the oldest.
func (ing *Ingester) pageSourceFor(f models.MediaFile) (*pdf.PageSource, error) {
	ing.pageMu.Lock()
	defer ing.pageMu.Unlock()
	if ing.pageSources == nil {
		ing.pageSources = make(map[int64]*pdf.PageSource)
	}
	if src, ok := ing.pageSources[f.ID]; ok {
		ing.touchPageSource(f.ID)
		return src, nil
	}
	path, err := resolveWithinRoot(f.Root, f.Path)
	if err != nil {
		return nil, err
	}
	src, err := pdf.OpenPageSource(path)
	if err != nil {
		return nil, err
	}
	for len(ing.pageOrder) >= pageSourceCap {
		oldest := ing.pageOrder[0]
		ing.pageOrder = ing.pageOrder[1:]
		if evicted, ok := ing.pageSources[oldest]; ok {
			delete(ing.pageSources, oldest)
			evicted.Close()
		}
	}
	ing.pageSources[f.ID] = src
	ing.pageOrder = append(ing.pageOrder, f.ID)
	return src, nil
}

// dropPageSource closes and forgets a file's context — the error path for a
// source that has gone bad underneath its cache entry.
func (ing *Ingester) dropPageSource(mediaFileID int64) {
	ing.pageMu.Lock()
	defer ing.pageMu.Unlock()
	if src, ok := ing.pageSources[mediaFileID]; ok {
		delete(ing.pageSources, mediaFileID)
		src.Close()
	}
	for i, id := range ing.pageOrder {
		if id == mediaFileID {
			ing.pageOrder = append(ing.pageOrder[:i], ing.pageOrder[i+1:]...)
			break
		}
	}
}

// touchPageSource moves a file to the newest end of the LRU.
func (ing *Ingester) touchPageSource(mediaFileID int64) {
	for i, id := range ing.pageOrder {
		if id == mediaFileID {
			ing.pageOrder = append(append(ing.pageOrder[:i], ing.pageOrder[i+1:]...), mediaFileID)
			return
		}
	}
}
