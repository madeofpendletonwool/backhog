package pdf_test

import (
	"bytes"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/collinpendleton/backhog/api/internal/books/pdf"
	"github.com/collinpendleton/backhog/api/internal/fixtures"
)

// The paged reader's backend: a PageSource over a PDF on disk answers the
// manifest (every page's shape, cheaply) and per-page rasters (decoded
// once per request). The image-only fixture is the shape a scan or a comic
// plate is — one FlateDecode image XObject per page.

func writePDF(t *testing.T, data []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "book.pdf")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	return path
}

func TestPageSourceManifest(t *testing.T) {
	src, err := pdf.OpenPageSource(writePDF(t, fixtures.BuildImageOnlyPDF()))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer src.Close()

	if src.PageCount() != 2 {
		t.Fatalf("page count = %d, want 2", src.PageCount())
	}
	pages, err := src.Pages()
	if err != nil {
		t.Fatalf("pages: %v", err)
	}
	if len(pages) != 2 {
		t.Fatalf("manifest pages = %d, want 2", len(pages))
	}
	for i, p := range pages {
		if p.Index != i || !p.HasImage {
			t.Errorf("page %d = %+v, want HasImage with Index %d", i, p, i)
		}
		// The fixture's plate is 8×8.
		if p.Width != 8 || p.Height != 8 {
			t.Errorf("page %d dims = %dx%d, want 8x8", i, p.Width, p.Height)
		}
	}
}

func TestPageSourceImage(t *testing.T) {
	src, err := pdf.OpenPageSource(writePDF(t, fixtures.BuildImageOnlyPDF()))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer src.Close()

	img, err := src.Image(1)
	if err != nil {
		t.Fatalf("image: %v", err)
	}
	if img.ContentType != "image/png" {
		t.Errorf("content type = %q, want image/png (a Flate bitmap re-encodes)", img.ContentType)
	}
	cfg, err := png.DecodeConfig(bytes.NewReader(img.Data))
	if err != nil {
		t.Fatalf("decode png: %v", err)
	}
	if cfg.Width != 8 || cfg.Height != 8 {
		t.Errorf("decoded = %dx%d, want 8x8", cfg.Width, cfg.Height)
	}
	decoded, err := png.Decode(bytes.NewReader(img.Data))
	if err != nil {
		t.Fatalf("decode png fully: %v", err)
	}
	if _, ok := decoded.(*image.RGBA); !ok {
		t.Errorf("decoded type = %T, want RGBA (a DeviceRGB plate)", decoded)
	}

	if _, err := src.Image(-1); err == nil {
		t.Error("page -1 served")
	}
	if _, err := src.Image(2); err == nil {
		t.Error("page 2 of a 2-page file served")
	}
}

// A vector-only page — drawings, no text, no image XObject — classifies
// image-native (there is no text layer) but has nothing the companion-image
// strategy can serve. The refusal is named, not a blank raster.
func TestPageSourceVectorRefusal(t *testing.T) {
	vector := fixtures.BuildPDF(fixtures.PDFFixture{Pages: []fixtures.PDFPage{{
		Content: "0 0 1 rg 100 100 300 500 re f\n",
	}}})
	src, err := pdf.OpenPageSource(writePDF(t, vector))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer src.Close()

	pages, err := src.Pages()
	if err != nil {
		t.Fatalf("pages: %v", err)
	}
	if pages[0].HasImage {
		t.Errorf("vector page reports an image: %+v", pages[0])
	}
	if _, err := src.Image(0); err == nil {
		t.Fatal("vector page served an image")
	}
}

// A text-native PDF opens as a page source too — the pages endpoint never
// reaches it (the classification routes the book to the scrolled reader),
// but a source that misbehaves on one would be a trap for later callers.
func TestPageSourceProsePDF(t *testing.T) {
	src, err := pdf.OpenPageSource(writePDF(t, fixtures.BuildProsePDF()))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer src.Close()
	if src.PageCount() != 4 {
		t.Errorf("page count = %d, want 4", src.PageCount())
	}
	pages, err := src.Pages()
	if err != nil || len(pages) != 4 {
		t.Fatalf("pages = %v err = %v", pages, err)
	}
	for i, p := range pages {
		if p.HasImage {
			t.Errorf("prose page %d unexpectedly has an image", i)
		}
	}
}

func TestPageSourceClose(t *testing.T) {
	src, err := pdf.OpenPageSource(writePDF(t, fixtures.BuildImageOnlyPDF()))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := src.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if err := src.Close(); err != nil {
		t.Errorf("second close: %v", err)
	}
	if _, err := src.Image(0); err == nil {
		t.Error("closed source served an image")
	}
	if _, err := src.Pages(); err == nil {
		t.Error("closed source served a manifest")
	}
}

// A corrupt file never becomes a page source.
func TestPageSourceCorrupt(t *testing.T) {
	if _, err := pdf.OpenPageSource(writePDF(t, fixtures.BuildCorruptPDF())); err == nil {
		t.Fatal("corrupt file opened as a page source")
	}
}
