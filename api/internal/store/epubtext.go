package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/collinpendleton/backhog/api/internal/models"
)

// GetEpubText returns the canonical-text row for a media file, or
// ErrNotFound when the EPUB has not been parsed yet.
func (s *Store) GetEpubText(ctx context.Context, mediaFileID int64) (models.EpubText, error) {
	var et models.EpubText
	err := s.db.QueryRowContext(ctx, `
		SELECT id, media_file_id, char_count, word_count, normalized_sha256,
		       parsed_at, parser_version, toc_source, toc_entries, toc_error
		FROM epub_texts WHERE media_file_id = ?`, mediaFileID).
		Scan(&et.ID, &et.MediaFileID, &et.CharCount, &et.WordCount,
			&et.NormalizedSHA256, &et.ParsedAt, &et.ParserVersion,
			&et.TOCSource, &et.TOCEntries, &et.TOCError)
	if errors.Is(err, sql.ErrNoRows) {
		return et, ErrNotFound
	}
	return et, err
}

// ReplaceEpubText writes one canonical text and its chapters atomically,
// replacing any previous parse of the same media file (same row id, so the
// companion files keep their names across re-parses).
func (s *Store) ReplaceEpubText(ctx context.Context, et models.EpubText, chapters []models.EpubChapter) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var existingID string
	err = tx.QueryRowContext(ctx,
		`SELECT id FROM epub_texts WHERE media_file_id = ?`, et.MediaFileID).Scan(&existingID)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		if et.ID == "" {
			et.ID = newID()
		}
	case err != nil:
		return err
	default:
		et.ID = existingID
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO epub_texts (id, media_file_id, char_count, word_count,
		                       normalized_sha256, parsed_at, parser_version,
		                       toc_source, toc_entries, toc_error)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(media_file_id) DO UPDATE SET
			char_count        = excluded.char_count,
			word_count        = excluded.word_count,
			normalized_sha256 = excluded.normalized_sha256,
			parsed_at         = excluded.parsed_at,
			parser_version    = excluded.parser_version,
			toc_source        = excluded.toc_source,
			toc_entries       = excluded.toc_entries,
			toc_error         = excluded.toc_error`,
		et.ID, et.MediaFileID, et.CharCount, et.WordCount,
		et.NormalizedSHA256, et.ParsedAt, et.ParserVersion,
		et.TOCSource, et.TOCEntries, et.TOCError); err != nil {
		return err
	}

	if _, err := tx.ExecContext(ctx,
		`DELETE FROM epub_chapters WHERE epub_text_id = ?`, et.ID); err != nil {
		return err
	}
	const batchSize = 128
	for start := 0; start < len(chapters); start += batchSize {
		end := min(start+batchSize, len(chapters))
		for i := start; i < end; i++ {
			chapters[i].ID = newID()
			chapters[i].EpubTextID = et.ID
		}
		batch := chapters[start:end]
		for _, ch := range batch {
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO epub_chapters (id, epub_text_id, spine_index, href,
				                          title, title_source, char_start, char_end, depth)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				ch.ID, ch.EpubTextID, ch.SpineIndex, ch.Href, ch.Title,
				ch.TitleSource, ch.CharStart, ch.CharEnd, ch.Depth); err != nil {
				return fmt.Errorf("insert chapter %d: %w", ch.SpineIndex, err)
			}
		}
	}
	return tx.Commit()
}

// ListEpubChapters returns a canonical text's chapters in reading order.
func (s *Store) ListEpubChapters(ctx context.Context, epubTextID string) ([]models.EpubChapter, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, epub_text_id, spine_index, href, title, title_source,
		       char_start, char_end, depth
		FROM epub_chapters WHERE epub_text_id = ? ORDER BY spine_index`, epubTextID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	chapters := []models.EpubChapter{}
	for rows.Next() {
		var ch models.EpubChapter
		if err := rows.Scan(&ch.ID, &ch.EpubTextID, &ch.SpineIndex, &ch.Href,
			&ch.Title, &ch.TitleSource, &ch.CharStart, &ch.CharEnd, &ch.Depth); err != nil {
			return nil, err
		}
		chapters = append(chapters, ch)
	}
	return chapters, rows.Err()
}

// TextFormatRank orders the text-side containers by how much of the book
// survives the trip to canonical text. An EPUB is structured XHTML the
// parser reads directly; AZW3 is the same content in a Kindle wrapper; AZW
// and MOBI are the older PalmDOC lineage, where the spine is reconstructed
// from fragments and the TOC is whatever the EXTH block preserved; a PDF
// is a text extraction — running heads stripped, pages its only structure —
// so it ranks last among the containers that parse at all, ahead only of
// files this query cannot classify. When a book arrives as more than one
// of them, the highest-fidelity container is the one worth measuring
// positions against.
//
// It is a tiebreak, never an override: a primary the user designated wins
// outright, and this only decides books that have never had one.
// It is written against the alias `mf`, which every query below uses for
// media_files so that one wording of "the book's text file" can be shared.
const TextFormatRank = `CASE
		WHEN mf.path LIKE '%.epub' THEN 0
		WHEN mf.path LIKE '%.azw3' THEN 1
		WHEN mf.path LIKE '%.azw'  THEN 2
		WHEN mf.path LIKE '%.mobi' THEN 3
		WHEN mf.path LIKE '%.pdf'  THEN 4
		ELSE 5
	END`

// primaryTextIs is the single predicate for "this media_files row (aliased
// mf) is its book's canonical text". Every consumer — the reader, the two
// sizing projections, the page-anchor seed and the alignment lookup — goes
// through it, because the failure mode of having four subtly different
// versions is not an error anyone sees: it is a percentage computed against
// one text and an offset measured in another.
//
// The NOT EXISTS branch is the fallback for a book whose files carry no
// designation at all, which the 00024 backfill leaves none of. It keeps the
// rule total: with nothing designated the format ranking decides, rather
// than the answer depending on which query ran.
const primaryTextIs = `mf.kind = 'epub' AND (mf.is_primary_text = 1 OR NOT EXISTS (
	          SELECT 1 FROM media_files p
	          WHERE p.book_id = mf.book_id AND p.kind = 'epub' AND p.is_primary_text = 1))`

// primaryTextCharCount is the canonical text length of a book's designated
// text file, as a correlated subquery expecting `e.book_id` in scope. It is
// the sizing half of the same choice EpubMediaFileForBook makes for reading,
// and the two must never disagree: percent_complete divides an offset
// measured against one text by a length, and a length taken from a different
// text is simply a wrong percentage.
//
// Unlike the reading path it does NOT require the file to be present. A
// char count is a fact about the book, and an unmounted NAS should not make
// a shelf of books briefly report zero length; the offsets it divides are
// still perfectly valid. Reading needs bytes on disk, sizing needs only the
// number — same row, different tolerance for the mount being away.
const primaryTextCharCount = `(SELECT et.char_count FROM epub_texts et
	          JOIN media_files mf ON mf.id = et.media_file_id
	          WHERE mf.book_id = e.book_id AND ` + primaryTextIs + `
	          ORDER BY ` + TextFormatRank + `, mf.id
	          LIMIT 1)`

// EpubMediaFileForBook returns the text-side file a book's canonical text is
// made from — its designated primary — provided the file is present on its
// root. Otherwise ErrNotFound.
//
// A book with several text files (the same title as .epub and .mobi, or two
// editions) has exactly one primary; the others are formats the user owns
// and nothing reads. Note what this deliberately does NOT do: when the
// primary is missing from its root it returns ErrNotFound rather than
// falling back to a sibling. Every stored offset was measured against the
// primary's canonicalization, so quietly serving a different text during a
// NAS outage would not be a graceful degradation — it would silently move
// the reader's position, their percentage and their alignment anchors to
// coordinates that mean something else. "The text is unavailable right now"
// is the honest answer, and it heals itself on the next scan.
//
// The NOT EXISTS branch covers a book whose files predate any designation,
// which the 00024 backfill should have left none of: with no primary to
// respect, the format ranking picks deterministically instead of leaving
// the answer to row-insert order.
func (s *Store) EpubMediaFileForBook(ctx context.Context, bookID string) (models.MediaFile, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT `+qualify(mediaFileColumns, "mf")+`
		FROM media_files mf
		WHERE mf.book_id = ? AND mf.missing_at IS NULL AND `+primaryTextIs+`
		ORDER BY `+TextFormatRank+`, mf.id
		LIMIT 1`, bookID)
	f, err := scanMediaFile(row)
	if errors.Is(err, sql.ErrNoRows) {
		return f, ErrNotFound
	}
	return f, err
}

// TextMediaFilesForBook lists every text-side file attached to a book,
// primary first. The attach UI needs the alternates to show them as owned
// formats, and the promote path needs them to know what it can switch to.
func (s *Store) TextMediaFilesForBook(ctx context.Context, bookID string) ([]models.MediaFile, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT `+qualify(mediaFileColumns, "mf")+`
		FROM media_files mf
		WHERE mf.book_id = ? AND mf.kind = 'epub'
		ORDER BY mf.is_primary_text DESC, `+TextFormatRank+`, mf.id`, bookID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []models.MediaFile{}
	for rows.Next() {
		f, err := scanMediaFile(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// BookIDForEntry resolves a library entry to its book id, scoped to the
// owner. It deliberately avoids the entry projections: book entries are not
// joinable there yet (the read-path task owns that rework), and ownership
// is the only fact the text endpoints need.
func (s *Store) BookIDForEntry(ctx context.Context, userID, entryID string) (string, error) {
	var bookID string
	err := s.db.QueryRowContext(ctx, `
		SELECT book_id FROM library_entries
		WHERE id = ? AND user_id = ? AND media_type = 'book'`, entryID, userID).
		Scan(&bookID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return bookID, err
}
