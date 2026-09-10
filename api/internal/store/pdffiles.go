package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/collinpendleton/backhog/api/internal/models"
)

// GetPDFFile returns a media file's persisted quality-gate classification,
// or ErrNotFound when the PDF has not been classified yet.
func (s *Store) GetPDFFile(ctx context.Context, mediaFileID int64) (models.PDFFile, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, media_file_id, classification, page_count, has_text_layer,
		       reason, classified_at, parser_version
		FROM pdf_files WHERE media_file_id = ?`, mediaFileID)
	pf, err := scanPDFFile(row)
	if errors.Is(err, sql.ErrNoRows) {
		return pf, ErrNotFound
	}
	return pf, err
}

// ReplacePDFFile writes one PDF's classification, replacing any previous
// verdict for the same media file (same row id, the ReplaceEpubText rule).
// The caller supplies the classification; the store pins when it was made.
func (s *Store) ReplacePDFFile(ctx context.Context, pf models.PDFFile) (models.PDFFile, error) {
	var existingID string
	err := s.db.QueryRowContext(ctx,
		`SELECT id FROM pdf_files WHERE media_file_id = ?`, pf.MediaFileID).Scan(&existingID)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		pf.ID = newID()
	case err != nil:
		return pf, err
	default:
		pf.ID = existingID
	}
	pf.ClassifiedAt = time.Now().UTC()

	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO pdf_files (id, media_file_id, classification, page_count,
		                       has_text_layer, reason, classified_at, parser_version)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(media_file_id) DO UPDATE SET
			classification = excluded.classification,
			page_count     = excluded.page_count,
			has_text_layer = excluded.has_text_layer,
			reason         = excluded.reason,
			classified_at  = excluded.classified_at,
			parser_version = excluded.parser_version`,
		pf.ID, pf.MediaFileID, pf.Classification, pf.PageCount,
		pf.HasTextLayer, pf.Reason, pf.ClassifiedAt, pf.ParserVersion); err != nil {
		return pf, err
	}
	return pf, nil
}

func scanPDFFile(row interface{ Scan(...any) error }) (models.PDFFile, error) {
	var pf models.PDFFile
	var hasTextLayer int
	err := row.Scan(&pf.ID, &pf.MediaFileID, &pf.Classification, &pf.PageCount,
		&hasTextLayer, &pf.Reason, &pf.ClassifiedAt, &pf.ParserVersion)
	if err != nil {
		return pf, err
	}
	pf.HasTextLayer = hasTextLayer == 1
	return pf, nil
}

// primaryTextPageCount is the page count of a book's designated text file
// when that file is an image-native PDF, as a correlated subquery expecting
// `e.book_id` in scope. It sits beside primaryTextCharCount for the same
// reason that column sits beside the edition's page count: the sizing paths
// need the primary's own length, and for a paged primary that length is a
// number of pages. Text-native rows are excluded — a PDF with a canonical
// text is measured by it, the same as any other container.
const primaryTextPageCount = `(SELECT pf.page_count FROM pdf_files pf
	          JOIN media_files mf ON mf.id = pf.media_file_id
	          WHERE mf.book_id = e.book_id AND ` + primaryTextIs + `
	            AND pf.classification = 'image-native'
	          ORDER BY ` + TextFormatRank + `, mf.id
	          LIMIT 1)`

// PagedPrimaryForBook reports whether a book's designated text file is an
// image-native PDF, and returns its classification row when it is. This is
// the arena's primary predicate answering for a paged primary: the file is
// still the one the book is "read" from, only its axis is the page index.
func (s *Store) PagedPrimaryForBook(ctx context.Context, bookID string) (models.PDFFile, bool, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT `+pdfFileColumns("pf")+`
		FROM pdf_files pf
		JOIN media_files mf ON mf.id = pf.media_file_id
		WHERE mf.book_id = ? AND mf.missing_at IS NULL AND `+primaryTextIs+`
		  AND pf.classification = 'image-native'
		ORDER BY `+TextFormatRank+`, mf.id
		LIMIT 1`, bookID)
	pf, err := scanPDFFile(row)
	if errors.Is(err, sql.ErrNoRows) {
		return models.PDFFile{}, false, nil
	}
	if err != nil {
		return models.PDFFile{}, false, err
	}
	return pf, true, nil
}

func pdfFileColumns(alias string) string {
	return alias + `.id, ` + alias + `.media_file_id, ` + alias + `.classification,
		` + alias + `.page_count, ` + alias + `.has_text_layer,
		` + alias + `.reason, ` + alias + `.classified_at, ` + alias + `.parser_version`
}
