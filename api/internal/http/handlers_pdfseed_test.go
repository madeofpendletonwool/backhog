package http

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/collinpendleton/backhog/api/internal/fixtures"
)

// The PDF-seeded page map, end to end over a real parsed text-native PDF:
// a PDF is a printing, so its per-page ranges grow a paper copy's map the
// day the copy is registered — at seed confidence, yielding to every real
// scan, never crossing texts.

// seedTestApp boots the epub harness with the four-page prose PDF attached
// as the book's primary text and an edition + copy registered against it.
type seedTestApp struct {
	*epubTestApp
	copyID string
}

func newPDFSeedTestApp(t *testing.T) *seedTestApp {
	t.Helper()
	inner := newEpubTestApp(t)
	// Seeds the book and its entry (attaching no epub — the PDF below is
	// this book's one and only text).
	inner.attachEpub(t, nil)

	data := fixtures.BuildProsePDF()
	p := filepath.Join(inner.root, "book.pdf")
	if err := os.MkdirAll(inner.root, 0o755); err != nil {
		t.Fatalf("mkdir root: %v", err)
	}
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatalf("write pdf: %v", err)
	}
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := inner.store.DB().Exec(q, args...); err != nil {
			t.Fatalf("seed %q: %v", q, err)
		}
	}
	exec(`INSERT INTO media_files (root, path, kind, size_bytes, mtime, book_id,
	                              is_primary_text, scanned_at)
		VALUES (?, 'book.pdf', 'epub', ?, ?, 'OL1W', 1, ?)`,
		inner.root, len(data), time.Now().UnixNano(), time.Now().UTC())
	exec(`INSERT INTO book_editions (id, book_id, page_count) VALUES ('OL1M', 'OL1W', 320)`)
	exec(`UPDATE library_entries SET edition_id = 'OL1M' WHERE id = ?`, epubFixtureEntry)

	// Force the parse (the attach flow would have done it): the canonical
	// text, the classification and the page ranges all land together.
	inner.canonicalText(t)

	status, body := inner.send(t, http.MethodPost, "/api/books/"+epubFixtureEntry+"/copies",
		map[string]string{"edition_id": "OL1M"})
	if status != http.StatusCreated {
		t.Fatalf("create copy: status %d: %v", status, body)
	}
	return &seedTestApp{
		epubTestApp: inner,
		copyID:      body["copy"].(map[string]any)["id"].(string),
	}
}

// TestSeedPageMapFromPDF walks the offer, the seed, the scan's victory and
// the cross-text refusal — the honesty rules the feature stands on.
func TestSeedPageMapFromPDF(t *testing.T) {
	app := newPDFSeedTestApp(t)

	// The copies list offers the seed with the PDF's own page count.
	status, body := app.send(t, http.MethodGet, "/api/books/"+epubFixtureEntry+"/copies", nil)
	if status != http.StatusOK {
		t.Fatalf("list copies: status %d: %v", status, body)
	}
	seed := body["pdf_seed"].(map[string]any)
	if seed["available"] != true || seed["page_count"].(float64) != 4 {
		t.Fatalf("pdf_seed = %v, want available with the fixture's 4 pages", seed)
	}

	// Seed: four anchors, page 1 at the text's start, all at the seed
	// confidence class with the pdf provenance.
	status, body = app.send(t, http.MethodPost,
		"/api/books/"+epubFixtureEntry+"/copies/"+app.copyID+"/seed-from-pdf", nil)
	if status != http.StatusOK {
		t.Fatalf("seed: status %d: %v", status, body)
	}
	if body["seeded"].(float64) != 4 || body["skipped"].(float64) != 0 {
		t.Fatalf("seed result = %v / %v, want 4 / 0", body["seeded"], body["skipped"])
	}
	anchors := body["anchors"].([]any)
	if len(anchors) != 4 {
		t.Fatalf("anchors = %d, want 4", len(anchors))
	}
	first := anchors[0].(map[string]any)
	if first["source"] != "pdf" || first["char_offset"].(float64) != 0 ||
		first["confidence"].(float64) != 0.3 {
		t.Errorf("first seed = %v, want page 1 at 0, source pdf, confidence 0.3", first)
	}

	// The provenance counts travel with the copy list.
	status, body = app.send(t, http.MethodGet, "/api/books/"+epubFixtureEntry+"/copies", nil)
	if status != http.StatusOK {
		t.Fatalf("relist copies: status %d", status)
	}
	copied := body["copies"].([]any)[0].(map[string]any)
	if copied["anchor_count"].(float64) != 4 || copied["seeded_count"].(float64) != 4 {
		t.Errorf("copy counts = %v mapped / %v seeded, want 4 / 4",
			copied["anchor_count"], copied["seeded_count"])
	}

	// The position endpoint interpolates inside the seeded map: an exact
	// anchor hit answers with its own confidence and no invented margin
	// beyond the seed's own honesty.
	et := app.canonicalText(t)
	status, body = app.send(t, http.MethodGet,
		"/api/books/"+epubFixtureEntry+"/position?char=0", nil)
	if status != http.StatusOK {
		t.Fatalf("position: status %d: %v", status, body)
	}
	page, ok := body["page"].(map[string]any)
	if !ok || page["page"].(float64) != 1 || page["derived"] != true {
		t.Fatalf("page view at offset 0 = %v, want page 1 derived", body["page"])
	}
	// Mid-page-3: between the page 3 and page 4 anchors, interpolated on
	// the seeds' slope at the segment's (seed) confidence.
	mid := et[len(et)*3/4] // inside page 4's span by construction below
	status, body = app.send(t, http.MethodGet,
		"/api/books/"+epubFixtureEntry+"/position?char="+fmt.Sprint(mid), nil)
	if status != http.StatusOK {
		t.Fatalf("position mid: status %d: %v", status, body)
	}
	if page, ok = body["page"].(map[string]any); !ok || page["confidence"].(float64) > 0.35 {
		t.Errorf("page view mid-map = %v, want seed-class confidence", body["page"])
	}

	// A scan on a seeded page overwrites its seed; re-seeding skips that
	// page and keeps the scan.
	status, body = app.send(t, http.MethodPost,
		"/api/books/"+epubFixtureEntry+"/copies/"+app.copyID+"/pages",
		map[string]any{"printed_page": 2, "char_offset": et[len(et)/4], "source": "ocr", "confidence": 0.9})
	if status != http.StatusOK {
		t.Fatalf("scan page 2: status %d: %v", status, body)
	}
	status, body = app.send(t, http.MethodPost,
		"/api/books/"+epubFixtureEntry+"/copies/"+app.copyID+"/seed-from-pdf", nil)
	if status != http.StatusOK {
		t.Fatalf("re-seed: status %d: %v", status, body)
	}
	if body["seeded"].(float64) != 3 || body["skipped"].(float64) != 1 {
		t.Fatalf("re-seed result = %v / %v, want 3 / 1 (the scan keeps its page)", body["seeded"], body["skipped"])
	}
	status, body = app.send(t, http.MethodGet,
		"/api/books/"+epubFixtureEntry+"/copies/"+app.copyID+"/pages", nil)
	if status != http.StatusOK {
		t.Fatalf("list pages: status %d", status)
	}
	anchors = body["anchors"].([]any)
	if len(anchors) != 4 {
		t.Fatalf("anchors after re-seed = %d, want 4 — dropped, not stacked", len(anchors))
	}
	scanned := anchors[1].(map[string]any)
	if scanned["source"] != "ocr" || scanned["confidence"].(float64) != 0.9 {
		t.Errorf("page 2 after re-seed = %v, want the scan kept", scanned)
	}

	// A client cannot smuggle a pin in as a seed — provenance is the
	// seed endpoint's to write.
	status, body = app.send(t, http.MethodPost,
		"/api/books/"+epubFixtureEntry+"/copies/"+app.copyID+"/pages",
		map[string]any{"printed_page": 9, "char_offset": 5, "source": "pdf"})
	if status != http.StatusBadRequest {
		t.Errorf("pin with source pdf: status %d, want 400", status)
	}

	// Unknown copy is a 404 like every copy-scoped write.
	status, _ = app.send(t, http.MethodPost,
		"/api/books/"+epubFixtureEntry+"/copies/nosuch/seed-from-pdf", nil)
	if status != http.StatusNotFound {
		t.Errorf("seed unknown copy: status %d, want 404", status)
	}
}

// TestSeedRefusedAcrossTexts: a book whose primary text canonicalizes
// differently from the PDF refuses the seed whole — the offsets would be
// plausible-looking nonsense in the primary's coordinate system, and the
// offer disappears with it.
func TestSeedRefusedAcrossTexts(t *testing.T) {
	app := newPDFSeedTestApp(t)

	// Replace the primary with an EPUB sibling of a different text: the
	// PDF stays attached and parsed, but its coordinates no longer mean
	// anything against the book's primary.
	data := passageEpubFixture(t)
	p := filepath.Join(app.root, "sibling.epub")
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatalf("write epub: %v", err)
	}
	if _, err := app.store.DB().Exec(
		`UPDATE media_files SET is_primary_text = 0 WHERE path = 'book.pdf'`); err != nil {
		t.Fatalf("undesignate pdf: %v", err)
	}
	if _, err := app.store.DB().Exec(`
		INSERT INTO media_files (root, path, kind, size_bytes, mtime, book_id,
		                         is_primary_text, scanned_at)
		VALUES (?, 'sibling.epub', 'epub', ?, ?, 'OL1W', 1, ?)`,
		app.root, len(data), time.Now().UnixNano(), time.Now().UTC()); err != nil {
		t.Fatalf("insert epub: %v", err)
	}
	// Parse the new primary so the refusal is the text mismatch itself,
	// not a missing parse: the sibling canonicalizes to different bytes
	// and every offset in the PDF means somewhere else in it.
	app.canonicalText(t)

	status, body := app.send(t, http.MethodPost,
		"/api/books/"+epubFixtureEntry+"/copies/"+app.copyID+"/seed-from-pdf", nil)
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("seed across texts: status %d: %v, want 422", status, body)
	}
	status, body = app.send(t, http.MethodGet, "/api/books/"+epubFixtureEntry+"/copies", nil)
	if status != http.StatusOK {
		t.Fatalf("list copies: status %d", status)
	}
	if seed := body["pdf_seed"].(map[string]any); seed["available"] != false {
		t.Errorf("pdf_seed across texts = %v, want the offer withdrawn", seed)
	}
}
