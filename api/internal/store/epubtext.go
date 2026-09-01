package store

import (
	"context"
	"database/sql"
	"encoding/json"
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
		       parsed_at, parser_version
		FROM epub_texts WHERE media_file_id = ?`, mediaFileID).
		Scan(&et.ID, &et.MediaFileID, &et.CharCount, &et.WordCount,
			&et.NormalizedSHA256, &et.ParsedAt, &et.ParserVersion)
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
		                       normalized_sha256, parsed_at, parser_version)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(media_file_id) DO UPDATE SET
			char_count        = excluded.char_count,
			word_count        = excluded.word_count,
			normalized_sha256 = excluded.normalized_sha256,
			parsed_at         = excluded.parsed_at,
			parser_version    = excluded.parser_version`,
		et.ID, et.MediaFileID, et.CharCount, et.WordCount,
		et.NormalizedSHA256, et.ParsedAt, et.ParserVersion); err != nil {
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
				                          title, char_start, char_end, depth)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
				ch.ID, ch.EpubTextID, ch.SpineIndex, ch.Href, ch.Title,
				ch.CharStart, ch.CharEnd, ch.Depth); err != nil {
				return fmt.Errorf("insert chapter %d: %w", ch.SpineIndex, err)
			}
		}
	}
	return tx.Commit()
}

// ListEpubChapters returns a canonical text's spine documents in reading
// order.
func (s *Store) ListEpubChapters(ctx context.Context, epubTextID string) ([]models.EpubChapter, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, epub_text_id, spine_index, href, title, char_start, char_end, depth
		FROM epub_chapters WHERE epub_text_id = ? ORDER BY spine_index`, epubTextID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	chapters := []models.EpubChapter{}
	for rows.Next() {
		var ch models.EpubChapter
		if err := rows.Scan(&ch.ID, &ch.EpubTextID, &ch.SpineIndex, &ch.Href,
			&ch.Title, &ch.CharStart, &ch.CharEnd, &ch.Depth); err != nil {
			return nil, err
		}
		chapters = append(chapters, ch)
	}
	return chapters, rows.Err()
}

// EpubMediaFileForBook returns the EPUB attached to a book whose file is
// currently present on its root, or ErrNotFound. A book with several EPUBs
// (different editions on the NAS) parses the first scanned; attaching
// workflows replace the association before that becomes a problem.
func (s *Store) EpubMediaFileForBook(ctx context.Context, bookID string) (models.MediaFile, error) {
	return s.mediaFileForBook(ctx, bookID, models.MediaFileEpub)
}

func (s *Store) mediaFileForBook(ctx context.Context, bookID, kind string) (models.MediaFile, error) {
	var f models.MediaFile
	var sha256, metadata, attachedBook sql.NullString
	err := s.db.QueryRowContext(ctx, `
		SELECT id, root, path, kind, size_bytes, mtime, sha256,
		       duration_seconds, container_metadata, book_id, scanned_at, missing_at
		FROM media_files
		WHERE book_id = ? AND kind = ? AND missing_at IS NULL
		ORDER BY id LIMIT 1`, bookID, kind).
		Scan(&f.ID, &f.Root, &f.Path, &f.Kind, &f.SizeBytes, &f.Mtime,
			&sha256, &f.DurationSeconds, &metadata, &attachedBook, &f.ScannedAt, &f.MissingAt)
	if errors.Is(err, sql.ErrNoRows) {
		return f, ErrNotFound
	}
	if err != nil {
		return f, err
	}
	if sha256.Valid {
		f.SHA256 = &sha256.String
	}
	if metadata.Valid {
		f.ContainerMetadata = json.RawMessage(metadata.String)
	}
	if attachedBook.Valid {
		f.BookID = &attachedBook.String
	}
	return f, nil
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
