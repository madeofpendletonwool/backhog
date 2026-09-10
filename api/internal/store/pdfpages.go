package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/collinpendleton/backhog/api/internal/models"
)

// The PDF half of the page-anchor map: a text-native PDF's own per-page
// ranges (pdf_pages, written by every parse) seeding a paper copy's map
// the way the catalogue's page count only stretches it. See BOOKS.md,
// "the page-anchor map".

var (
	// ErrNoPDFPageRanges marks a seeding asked of a book whose attached
	// PDFs cannot supply one: no text-native PDF, or one that never
	// yielded page ranges (parsed before they were recorded, or not
	// parsed at all).
	ErrNoPDFPageRanges = errors.New("no attached text-native PDF carries page ranges to seed from")
	// ErrPDFSeedTextMismatch marks the refusal that keeps seeds honest:
	// the candidate PDF's canonical text is not the text the book's page
	// anchors are measured against, and a seed is never rescaled onto a
	// different one — the plausible-wrong-answer failure mode. Covers a
	// book with no parsed primary text at all (nothing to map onto).
	ErrPDFSeedTextMismatch = errors.New("the PDF's text does not match this book's primary text")
)

// pdfSeedSource is the resolved half of a seeding: the PDF to seed from
// and its per-page ranges.
type pdfSeedSource struct {
	fileID int64
	pages  []models.PDFPage
}

// resolvePDFSeed finds the PDF a copy's page map can be seeded from: an
// attached text-native PDF that carries page ranges AND canonicalizes to
// exactly the book's primary text. The primary itself (a text-native PDF
// primary) is the common case; a sibling whose normalized hash matches
// (the converted .epub/.pdf pair) is the other. A PDF whose text differs
// from the primary is refused, never rescaled — a different
// canonicalization means every offset in it means somewhere else.
func (s *Store) resolvePDFSeed(ctx context.Context, userID, entryID string) (pdfSeedSource, error) {
	bookID, err := s.BookIDForEntry(ctx, userID, entryID)
	if err != nil {
		return pdfSeedSource{}, err
	}

	// The target: the primary text's identity, the coordinate system page
	// anchors live in. A primary with no parsed canonical text (unparsed,
	// or a paged PDF primary) leaves seeds nowhere honest to land.
	var primarySHA string
	err = s.db.QueryRowContext(ctx, `
		SELECT et.normalized_sha256
		FROM epub_texts et
		JOIN media_files mf ON mf.id = et.media_file_id
		WHERE mf.book_id = ? AND `+primaryTextIs+`
		ORDER BY `+TextFormatRank+`, mf.id
		LIMIT 1`, bookID).Scan(&primarySHA)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return pdfSeedSource{}, fmt.Errorf("%w: this book has no parsed primary text to map pages onto",
			ErrPDFSeedTextMismatch)
	case err != nil:
		return pdfSeedSource{}, err
	}

	// The candidates: parsed text-native PDFs, the primary first, the
	// format ranking after that (a designated PDF primary outranks a
	// matching sibling; between siblings the deterministic rank decides).
	// Collected before any second query runs — the store sits on one
	// connection, and a nested read under open rows would block forever.
	rows, err := s.db.QueryContext(ctx, `
		SELECT mf.id, et.normalized_sha256,
		       (SELECT COUNT(*) FROM pdf_pages pp WHERE pp.media_file_id = mf.id)
		FROM media_files mf
		JOIN pdf_files pf ON pf.media_file_id = mf.id AND pf.classification = 'text-native'
		JOIN epub_texts et ON et.media_file_id = mf.id
		WHERE mf.book_id = ?
		ORDER BY mf.is_primary_text DESC, `+TextFormatRank+`, mf.id`, bookID)
	if err != nil {
		return pdfSeedSource{}, err
	}
	type candidate struct {
		fileID    int64
		sha       string
		pageCount int
	}
	var candidates []candidate
	for rows.Next() {
		var c candidate
		if err := rows.Scan(&c.fileID, &c.sha, &c.pageCount); err != nil {
			rows.Close()
			return pdfSeedSource{}, err
		}
		candidates = append(candidates, c)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return pdfSeedSource{}, err
	}
	rows.Close()

	var mismatched string
	for _, c := range candidates {
		if c.pageCount == 0 {
			mismatched = "no page ranges were recorded for it"
			continue
		}
		if c.sha != primarySHA {
			mismatched = "its text differs from the book's primary text"
			continue
		}
		pages, err := s.PDFPagesForFile(ctx, c.fileID)
		if err != nil {
			return pdfSeedSource{}, err
		}
		return pdfSeedSource{fileID: c.fileID, pages: pages}, nil
	}
	if mismatched != "" {
		return pdfSeedSource{}, fmt.Errorf("%w: %s", ErrPDFSeedTextMismatch, mismatched)
	}
	return pdfSeedSource{}, ErrNoPDFPageRanges
}

// PDFPagesForFile lists a text-native PDF's per-page ranges in page order.
func (s *Store) PDFPagesForFile(ctx context.Context, mediaFileID int64) ([]models.PDFPage, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT page_number, char_start, char_end
		FROM pdf_pages WHERE media_file_id = ?
		ORDER BY page_number`, mediaFileID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []models.PDFPage{}
	for rows.Next() {
		var pg models.PDFPage
		if err := rows.Scan(&pg.PageNumber, &pg.CharStart, &pg.CharEnd); err != nil {
			return nil, err
		}
		out = append(out, pg)
	}
	return out, rows.Err()
}

// PDFSeedInfo answers whether a copy of this book's page map can be
// seeded from a text-native PDF, and with how many pages — the flag the
// copy panel offers the action on. A no is a fact to display, not an
// error: most books own no PDF, and the reason string says which no it
// was.
func (s *Store) PDFSeedInfo(ctx context.Context, userID, entryID string) (models.PDFSeedInfo, error) {
	src, err := s.resolvePDFSeed(ctx, userID, entryID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return models.PDFSeedInfo{}, err
		}
		return models.PDFSeedInfo{Reason: err.Error()}, nil
	}
	return models.PDFSeedInfo{Available: true, PageCount: len(src.pages)}, nil
}

// SeedPageAnchorsFromPDF writes one copy's page map from a text-native
// PDF's per-page ranges, at the catalogue seed's confidence class: a PDF
// is a printing, so its pages give the map a true slope — per-page text
// density known, not stretched — but nobody has looked at *this* copy's
// paper, so every seed yields to a real scan:
//
//   - pages a scan (ocr or manual) already owns are skipped, not
//     overwritten — a seed that would contradict a measurement is dropped
//     on arrival;
//   - seeds from any earlier run are replaced wholesale, so re-seeding
//     after a re-parse can never leave a stale offset behind;
//   - a scan afterwards overwrites its page's seed through the same
//     composite-PK upsert every anchor write uses.
func (s *Store) SeedPageAnchorsFromPDF(ctx context.Context, userID, entryID, copyID string) (models.PDFSeedResult, error) {
	if _, err := s.physicalCopyForUser(ctx, userID, entryID, copyID); err != nil {
		return models.PDFSeedResult{}, err
	}
	src, err := s.resolvePDFSeed(ctx, userID, entryID)
	if err != nil {
		return models.PDFSeedResult{}, err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return models.PDFSeedResult{}, err
	}
	defer tx.Rollback()

	// Yesterday's seeds go wholesale; today's land fresh. Real anchors
	// are untouched by this delete.
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM page_anchors WHERE physical_copy_id = ? AND source = 'pdf'`, copyID); err != nil {
		return models.PDFSeedResult{}, err
	}

	scanned := map[int]bool{}
	srows, err := tx.QueryContext(ctx, `
		SELECT printed_page FROM page_anchors
		WHERE physical_copy_id = ? AND source IN ('ocr', 'manual')`, copyID)
	if err != nil {
		return models.PDFSeedResult{}, err
	}
	for srows.Next() {
		var page int
		if err := srows.Scan(&page); err != nil {
			srows.Close()
			return models.PDFSeedResult{}, err
		}
		scanned[page] = true
	}
	if err := srows.Err(); err != nil {
		srows.Close()
		return models.PDFSeedResult{}, err
	}
	srows.Close()

	res := models.PDFSeedResult{Anchors: []models.PageAnchor{}}
	for _, pg := range src.pages {
		if scanned[pg.PageNumber] {
			res.Skipped++
			continue
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO page_anchors (physical_copy_id, printed_page, char_offset, source, confidence)
			VALUES (?, ?, ?, 'pdf', ?)`,
			copyID, pg.PageNumber, pg.CharStart, models.PageSeedConfidence); err != nil {
			return models.PDFSeedResult{}, err
		}
		res.Seeded++
	}
	if err := tx.Commit(); err != nil {
		return models.PDFSeedResult{}, err
	}

	res.Anchors, err = s.PageAnchors(ctx, userID, entryID, copyID)
	return res, err
}
