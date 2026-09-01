package store

import (
	"context"
	"database/sql"
	"errors"

	"github.com/collinpendleton/backhog/api/internal/models"
)

const audioFileColumns = `id, root, path, kind, size_bytes, mtime, sha256,
	       duration_seconds, container_metadata, book_id, scanned_at, missing_at`

// AudioMediaFilesForBook lists a book's attached audio files in timeline
// order: the explicit track_number the attach flow recorded, with path as the
// tiebreak for rows that predate one. Missing files are included — they still
// hold their slot in the running order, and dropping them would silently
// renumber every later track while a NAS is unmounted.
func (s *Store) AudioMediaFilesForBook(ctx context.Context, bookID string) ([]models.MediaFile, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT `+audioFileColumns+`
		FROM media_files
		WHERE book_id = ? AND kind = 'audio'
		ORDER BY track_number, path`, bookID)
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

// AudioMediaFileForBook loads one attached audio file, scoped to the book it
// is attached to. A file id that exists but belongs to another book is
// ErrNotFound, not a permission error: the caller turns that into the same
// 404 an unknown id gets, so the endpoint never confirms that someone else's
// file exists.
func (s *Store) AudioMediaFileForBook(ctx context.Context, bookID string, fileID int64) (models.MediaFile, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT `+audioFileColumns+`
		FROM media_files
		WHERE id = ? AND book_id = ? AND kind = 'audio'`, fileID, bookID)
	f, err := scanMediaFile(row)
	if errors.Is(err, sql.ErrNoRows) {
		return f, ErrNotFound
	}
	return f, err
}

// SetMediaFileDuration records a duration derived after the scan, so the
// container headers are parsed once per file rather than once per request.
func (s *Store) SetMediaFileDuration(ctx context.Context, fileID int64, seconds float64) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE media_files SET duration_seconds = ? WHERE id = ?`, seconds, fileID)
	return err
}
