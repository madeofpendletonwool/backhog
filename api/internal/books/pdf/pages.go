package pdf

import (
	"errors"
	"fmt"
	"io"
	"os"
	"sync"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
)

// Page serving for image-native PDFs — the paged reader's backend.
//
// The spine parser (pdf.go) answers "what does this file's text say"; this
// file answers the other population's question, "what do its pages look
// like". The display strategy is companion page images: the embedded page
// image XObjects are extracted — lazily, one page at a time — and served as
// ordinary rasters, so the API stays pure Go and the reader stays ours
// (a vendored PDF.js was the alternative; see BOOKS.md for the decision).
//
// pdfcpu does the extraction. It already reads the file's outline (toc.go)
// and it is the one pure-Go implementation that handles the real-world
// filter zoo — Flate, LZW, CCITT, DCT with SMask compositing, colorspace
// conversion. A page's image comes back either re-encoded as PNG (bitmap
// sources) or as the original JPEG bytes (DCT passthrough), both of which a
// browser renders natively. Codecs a browser cannot render — JPX, JBIG2,
// TIFF — are named refusals, never a broken image.
//
// Extraction is not free: pdfcpu wants a validated, optimized context over
// the whole file before it will walk one page's resources. A PageSource
// holds that context open over the file on disk, so the expensive pass
// happens once per file per process and every page after it is a single
// page's decode. The context and the reader underneath it are single-thread
// state, so one PageSource serializes its own extractions.

// ErrNoPageImage reports a page with nothing to serve: no image XObject at
// all (vector art or a blank page). Under the companion-image strategy
// such a page is a named refusal, not a blank reader.
var ErrNoPageImage = errors.New("pdf: page carries no image")

// ErrUnsupportedPageImage reports a page whose image exists but whose codec
// a browser cannot render (JPX, JBIG2, TIFF). The page is honestly named
// unservable rather than served as garbage bytes.
var ErrUnsupportedPageImage = errors.New("pdf: page image uses an unservable codec")

// ErrPageOutOfRange reports a page number outside the file.
var ErrPageOutOfRange = errors.New("pdf: page number out of range")

// pageImageLimit caps one extracted page image's bytes. pdfcpu's own
// resource limits bound the decode; this bounds what one request may hold
// in memory afterwards, the same job assetSizeLimit does for EPUB images.
const pageImageLimit = 64 << 20

// PageInfo is one page of the manifest: whether it has a servable image
// and, when it does, the pixel dimensions a reader wants for layout before
// the bytes arrive.
type PageInfo struct {
	// Index is the 0-based page number — the paged position axis.
	Index int
	// Width and Height are the page's dominant image's pixels, 0 when the
	// page has no image.
	Width, Height int
	// HasImage reports whether the page carries an image XObject at all.
	// A page without one is vector art or blank; the reader shows it as a
	// labeled refusal rather than a hole.
	HasImage bool
}

// PageImage is one extracted page raster with the content type to serve it
// as. Data is the complete encoded image — PNG or JPEG bytes.
type PageImage struct {
	ContentType string
	Data        []byte
}

// PageSource is an open PDF held ready for per-page image extraction. It
// is safe for concurrent use; extractions serialize on the file. Close
// releases the file handle, after which every method errors.
type PageSource struct {
	mu     sync.Mutex
	file   *os.File
	ctx    *model.Context
	count  int
	closed bool
}

// OpenPageSource validates and optimizes the PDF at path and holds it open
// for page extraction. The one-time cost is the whole-file pass; pages
// afterwards are individually cheap.
func OpenPageSource(path string) (*PageSource, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("pdf: open page source: %w", err)
	}
	conf := model.NewDefaultConfiguration()
	conf.ValidationMode = model.ValidationRelaxed
	conf.Cmd = model.EXTRACTIMAGES
	ctx, err := api.ReadValidateAndOptimize(f, conf)
	if err != nil {
		f.Close()
		return nil, fmt.Errorf("pdf: prepare page source: %w", err)
	}
	if ctx.PageCount == 0 {
		f.Close()
		return nil, fmt.Errorf("pdf: page source: %w", ErrCorrupt)
	}
	return &PageSource{file: f, ctx: ctx, count: ctx.PageCount}, nil
}

// Close releases the underlying file.
func (ps *PageSource) Close() error {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	if ps.closed {
		return nil
	}
	ps.closed = true
	return ps.file.Close()
}

// PageCount is the file's own page count, the denominator of "page N of M".
func (ps *PageSource) PageCount() int {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	return ps.count
}

// Pages walks every page in stub mode — dimensions and presence only, no
// decode — and returns the manifest. Stubs are the cheap inventory: a
// reader learns every page's shape for the price of resource-dict lookups,
// before any image bytes are paid for.
func (ps *PageSource) Pages() ([]PageInfo, error) {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	if ps.closed {
		return nil, os.ErrClosed
	}
	out := make([]PageInfo, ps.count)
	for page := 1; page <= ps.count; page++ {
		imgs, err := pdfcpu.ExtractPageImages(ps.ctx, page, true)
		if err != nil {
			// A page whose resources defeat even the stub walk keeps its
			// refusal; the manifest as a whole still answers.
			out[page-1] = PageInfo{Index: page - 1}
			continue
		}
		out[page-1] = dominantStub(imgs)
		out[page-1].Index = page - 1
	}
	return out, nil
}

// Image extracts one page's dominant image (the largest by pixel area —
// for a scan or a comic plate that is the page itself) as servable bytes.
// A page with no image fails with ErrNoPageImage; a codec a browser cannot
// render fails with ErrUnsupportedPageImage; a page past the end with
// ErrPageOutOfRange.
func (ps *PageSource) Image(page int) (PageImage, error) {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	if ps.closed {
		return PageImage{}, os.ErrClosed
	}
	if page < 0 || page >= ps.count {
		return PageImage{}, fmt.Errorf("%w: %d of %d", ErrPageOutOfRange, page, ps.count)
	}
	imgs, err := pdfcpu.ExtractPageImages(ps.ctx, page+1, false)
	if err != nil {
		return PageImage{}, fmt.Errorf("pdf: extract page %d: %w", page+1, err)
	}
	img, ok := dominantImage(imgs)
	if !ok {
		return PageImage{}, fmt.Errorf("%w: page %d", ErrNoPageImage, page+1)
	}
	contentType, ok := servableType(img.FileType)
	if !ok {
		return PageImage{}, fmt.Errorf("%w: page %d is %s", ErrUnsupportedPageImage, page+1, img.FileType)
	}
	data, err := io.ReadAll(io.LimitReader(img, pageImageLimit+1))
	if err != nil {
		return PageImage{}, fmt.Errorf("pdf: read page %d image: %w", page+1, err)
	}
	if len(data) == 0 || len(data) > pageImageLimit {
		return PageImage{}, fmt.Errorf("pdf: page %d image is empty or over the size limit", page+1)
	}
	return PageImage{ContentType: contentType, Data: data}, nil
}

// dominantStub picks a page's representative image for the manifest: the
// largest by pixel area, ignoring thumbnails and image masks (a stencil a
// page paints through is not the page).
func dominantStub(imgs map[int]model.Image) PageInfo {
	var best model.Image
	found := false
	for _, img := range imgs {
		if img.Thumb || img.IsImgMask {
			continue
		}
		if !found || img.Width*img.Height > best.Width*best.Height {
			best, found = img, true
		}
	}
	if !found {
		return PageInfo{}
	}
	return PageInfo{Width: best.Width, Height: best.Height, HasImage: true}
}

// dominantImage is dominantStub for the full extraction, returning the
// chosen image.
func dominantImage(imgs map[int]model.Image) (model.Image, bool) {
	var best model.Image
	found := false
	for _, img := range imgs {
		if img.Thumb || img.IsImgMask {
			continue
		}
		if !found || img.Width*img.Height > best.Width*best.Height {
			best, found = img, true
		}
	}
	return best, found
}

// servableType maps pdfcpu's rendered file types onto the ones a browser
// renders in an <img> everywhere. PNG (bitmap sources, re-encoded) and
// JPEG (DCT passthrough) serve; JPX, JBIG2 and TIFF are refusals.
func servableType(fileType string) (string, bool) {
	switch fileType {
	case "png":
		return "image/png", true
	case "jpg":
		return "image/jpeg", true
	}
	return "", false
}
