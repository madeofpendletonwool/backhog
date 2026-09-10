package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/collinpendleton/backhog/api/internal/models"
)

// The PDF half of the page bridge: per-page ranges seed a paper copy's
// map, seeds yield to scans, and a PDF whose text is not the book's
// primary text is refused rather than rescaled.

// seedParsedPDF inventories one text-native PDF for the harness book and
// parses it through the real write path: an epub_texts row, the
// classification row, and per-page ranges partitioning [0, chars).
func seedParsedPDF(t *testing.T, s *Store, path, sha string, primary bool, chars int) int64 {
	t.Helper()
	ctx := context.Background()
	now := time.Now()
	primaryBit := 0
	if primary {
		primaryBit = 1
	}
	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO media_files (root, path, kind, size_bytes, mtime, book_id,
		                         is_primary_text, scanned_at)
		VALUES ('/nas', ?, 'epub', 100, ?, 'OL1W', ?, ?)`,
		path, now.UnixNano(), primaryBit, now.UTC()); err != nil {
		t.Fatalf("insert media file %s: %v", path, err)
	}
	var fileID int64
	if err := s.db.QueryRowContext(ctx,
		`SELECT id FROM media_files WHERE path = ?`, path).Scan(&fileID); err != nil {
		t.Fatalf("file id: %v", err)
	}
	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO pdf_files (id, media_file_id, classification, page_count,
		                       has_text_layer, reason, parser_version)
		VALUES ('pf'||?, ?, 'text-native', 4, 1, '', '2')`, path, fileID); err != nil {
		t.Fatalf("insert classification: %v", err)
	}
	pages := make([]models.PDFPage, 4)
	for i := range pages {
		pages[i] = models.PDFPage{
			PageNumber: i + 1,
			CharStart:  chars * i / 4,
			CharEnd:    chars * (i + 1) / 4,
		}
	}
	et := models.EpubText{
		MediaFileID: fileID, CharCount: chars, WordCount: chars / 6,
		NormalizedSHA256: sha, ParserVersion: "2",
	}
	if err := s.ReplaceEpubText(ctx, et, nil, pages); err != nil {
		t.Fatalf("seed text: %v", err)
	}
	return fileID
}

// TestSeedPageAnchorsFromPDF walks the seeding: a fresh copy grows the
// whole map at the seed confidence class, a scan overwrites its page's
// seed through the composite PK, and re-seeding skips scanned pages and
// replaces stale seeds instead of stacking them.
func TestSeedPageAnchorsFromPDF(t *testing.T) {
	s := newPageAnchorStore(t)
	ctx := context.Background()
	seedParsedPDF(t, s, "book.pdf", "shaA", true, 400)

	info, err := s.PDFSeedInfo(ctx, "u1", "e1")
	if err != nil {
		t.Fatalf("seed info: %v", err)
	}
	if !info.Available || info.PageCount != 4 {
		t.Fatalf("seed info = %+v, want available with 4 pages", info)
	}

	c, err := s.CreatePhysicalCopy(ctx, "u1", "e1", "OL1M", "", models.CopyAcquisitionOwned, nil)
	if err != nil {
		t.Fatalf("create copy: %v", err)
	}

	res, err := s.SeedPageAnchorsFromPDF(ctx, "u1", "e1", c.ID)
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	if res.Seeded != 4 || res.Skipped != 0 || len(res.Anchors) != 4 {
		t.Fatalf("seed result = %d/%d anchors %d, want 4/0 and 4 rows",
			res.Seeded, res.Skipped, len(res.Anchors))
	}
	for i, a := range res.Anchors {
		if a.Source != models.PageAnchorSourcePDF || a.Confidence != models.PageSeedConfidence {
			t.Errorf("anchor %d = source %q conf %.2f, want the pdf seed class", i, a.Source, a.Confidence)
		}
		if a.PrintedPage != i+1 || a.CharOffset != 100*i {
			t.Errorf("anchor %d = page %d @ %d, want page %d @ %d", i, a.PrintedPage, a.CharOffset, i+1, 100*i)
		}
	}

	// The provenance count: four mapped, all four seeded.
	copies, err := s.PhysicalCopies(ctx, "u1", "e1")
	if err != nil {
		t.Fatalf("copies: %v", err)
	}
	if copies[0].AnchorCount != 4 || copies[0].SeededCount != 4 {
		t.Errorf("copy counts = %d mapped / %d seeded, want 4 / 4",
			copies[0].AnchorCount, copies[0].SeededCount)
	}

	// A real scan on page 2 overwrites its seed — the composite PK is
	// the correction path — and re-seeding afterwards drops that page's
	// seed on arrival instead of evicting the measurement.
	if _, err := s.SavePageAnchor(ctx, "u1", "e1", c.ID, models.PageAnchor{
		PrintedPage: 2, CharOffset: 150, Source: models.PageAnchorSourceOCR, Confidence: 0.9,
	}); err != nil {
		t.Fatalf("scan page 2: %v", err)
	}
	res, err = s.SeedPageAnchorsFromPDF(ctx, "u1", "e1", c.ID)
	if err != nil {
		t.Fatalf("re-seed: %v", err)
	}
	if res.Seeded != 3 || res.Skipped != 1 {
		t.Errorf("re-seed = %d seeded / %d skipped, want 3 / 1", res.Seeded, res.Skipped)
	}
	anchors, err := s.PageAnchors(ctx, "u1", "e1", c.ID)
	if err != nil {
		t.Fatalf("anchors: %v", err)
	}
	if len(anchors) != 4 {
		t.Fatalf("anchors after re-seed = %d, want 4 (no stacking)", len(anchors))
	}
	if anchors[1].Source != models.PageAnchorSourceOCR || anchors[1].CharOffset != 150 {
		t.Errorf("page 2 after re-seed = %+v, want the scan kept", anchors[1])
	}

	// A re-parse with new ranges followed by a re-seed moves the seeds:
	// yesterday's offsets never survive today's parse.
	var fileID int64
	if err := s.db.QueryRowContext(ctx,
		`SELECT id FROM media_files WHERE path = 'book.pdf'`).Scan(&fileID); err != nil {
		t.Fatalf("file id: %v", err)
	}
	shifted := []models.PDFPage{
		{PageNumber: 1, CharStart: 0, CharEnd: 90},
		{PageNumber: 2, CharStart: 90, CharEnd: 180},
		{PageNumber: 3, CharStart: 180, CharEnd: 290},
		{PageNumber: 4, CharStart: 290, CharEnd: 400},
	}
	if err := s.ReplaceEpubText(ctx, models.EpubText{
		MediaFileID: fileID, CharCount: 400, WordCount: 66,
		NormalizedSHA256: "shaA", ParserVersion: "2",
	}, nil, shifted); err != nil {
		t.Fatalf("re-parse: %v", err)
	}
	if _, err := s.SeedPageAnchorsFromPDF(ctx, "u1", "e1", c.ID); err != nil {
		t.Fatalf("seed after re-parse: %v", err)
	}
	anchors, err = s.PageAnchors(ctx, "u1", "e1", c.ID)
	if err != nil {
		t.Fatalf("anchors: %v", err)
	}
	if anchors[0].CharOffset != 0 || anchors[2].CharOffset != 180 || anchors[3].CharOffset != 290 {
		t.Errorf("anchors after re-parse = %+v, want the shifted offsets", anchors)
	}
	if anchors[1].CharOffset != 150 || anchors[1].Source != models.PageAnchorSourceOCR {
		t.Errorf("page 2 after re-parse = %+v, want the scan still kept", anchors[1])
	}

	// Foreign eyes and unknown copies change nothing.
	if _, err := s.SeedPageAnchorsFromPDF(ctx, "u2", "e1", c.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("cross-user seed = %v, want ErrNotFound", err)
	}
	if _, err := s.SeedPageAnchorsFromPDF(ctx, "u1", "e1", "nosuch"); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown copy seed = %v, want ErrNotFound", err)
	}
}

// TestSeedRequiresMatchingText holds the honesty line: a seed writes
// against the PDF's own canonical text, so it flows only when that text
// IS the book's primary — directly or by an identical normalized hash —
// and is refused, never rescaled, everywhere else.
func TestSeedRequiresMatchingText(t *testing.T) {
	s := newPageAnchorStore(t)
	ctx := context.Background()

	// A book with no PDF at all has no seed to offer, and says so.
	info, err := s.PDFSeedInfo(ctx, "u1", "e1")
	if err != nil {
		t.Fatalf("seed info: %v", err)
	}
	if info.Available || info.Reason == "" {
		t.Fatalf("seed info without a pdf = %+v, want unavailable with a reason", info)
	}

	// The PDF parsed, but the primary is an EPUB sibling canonicalizing
	// differently: refused, because every offset in the PDF means
	// somewhere else in the sibling.
	pdfID := seedParsedPDF(t, s, "book.pdf", "shaA", false, 400)
	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO media_files (root, path, kind, size_bytes, mtime, book_id,
		                         is_primary_text, scanned_at)
		VALUES ('/nas', 'book.epub', 'epub', 100, 0, 'OL1W', 1, ?)`, time.Now().UTC()); err != nil {
		t.Fatalf("insert epub: %v", err)
	}
	var epubID int64
	if err := s.db.QueryRowContext(ctx,
		`SELECT id FROM media_files WHERE path = 'book.epub'`).Scan(&epubID); err != nil {
		t.Fatalf("epub id: %v", err)
	}
	if err := s.ReplaceEpubText(ctx, models.EpubText{
		MediaFileID: epubID, CharCount: 500, WordCount: 80,
		NormalizedSHA256: "shaB", ParserVersion: "2",
	}, nil, nil); err != nil {
		t.Fatalf("seed epub text: %v", err)
	}

	c, err := s.CreatePhysicalCopy(ctx, "u1", "e1", "OL1M", "", models.CopyAcquisitionOwned, nil)
	if err != nil {
		t.Fatalf("create copy: %v", err)
	}
	_, err = s.SeedPageAnchorsFromPDF(ctx, "u1", "e1", c.ID)
	if !errors.Is(err, ErrPDFSeedTextMismatch) {
		t.Fatalf("seed across texts = %v, want ErrPDFSeedTextMismatch", err)
	}
	info, err = s.PDFSeedInfo(ctx, "u1", "e1")
	if err != nil {
		t.Fatalf("seed info: %v", err)
	}
	if info.Available {
		t.Errorf("seed info across texts = %+v, want unavailable", info)
	}

	// The converted pair: the sibling EPUB canonicalizes to the same
	// bytes (shaA), the coordinates are literally the same, and the seed
	// flows — direct, nothing rescaled.
	if _, err := s.db.ExecContext(ctx,
		`UPDATE epub_texts SET normalized_sha256 = 'shaA' WHERE media_file_id = ?`, epubID); err != nil {
		t.Fatalf("match the hashes: %v", err)
	}
	info, err = s.PDFSeedInfo(ctx, "u1", "e1")
	if err != nil {
		t.Fatalf("seed info: %v", err)
	}
	if !info.Available || info.PageCount != 4 {
		t.Fatalf("seed info for the converted pair = %+v, want available with 4 pages", info)
	}
	res, err := s.SeedPageAnchorsFromPDF(ctx, "u1", "e1", c.ID)
	if err != nil || res.Seeded != 4 {
		t.Fatalf("seed the converted pair = %v / %d, want 4 seeds", err, res.Seeded)
	}

	// A book whose primary never parsed has no coordinate system to seed
	// onto: the refusal names it.
	if _, err := s.db.ExecContext(ctx,
		`UPDATE media_files SET is_primary_text = 0 WHERE id = ?`, epubID); err != nil {
		t.Fatalf("undesignate: %v", err)
	}
	if _, err := s.db.ExecContext(ctx,
		`DELETE FROM epub_texts WHERE media_file_id = ?`, epubID); err != nil {
		t.Fatalf("unparse the primary: %v", err)
	}
	if _, err := s.db.ExecContext(ctx,
		`UPDATE media_files SET is_primary_text = 1 WHERE id = ?`, pdfID); err != nil {
		t.Fatalf("point at the pdf: %v", err)
	}
	if _, err := s.db.ExecContext(ctx,
		`DELETE FROM epub_texts WHERE media_file_id = ?`, pdfID); err != nil {
		t.Fatalf("unparse the pdf: %v", err)
	}
	_, err = s.SeedPageAnchorsFromPDF(ctx, "u1", "e1", c.ID)
	if !errors.Is(err, ErrPDFSeedTextMismatch) {
		t.Errorf("seed with no parsed primary = %v, want ErrPDFSeedTextMismatch", err)
	}
}
