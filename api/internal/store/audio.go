package store

import (
	"context"
	"database/sql"
	"errors"

	"github.com/collinpendleton/backhog/api/internal/models"
)

// AudioMediaFilesForBook lists the book's audiobook in timeline order: the
// explicit track_number the attach flow recorded, with path as the tiebreak
// for rows that predate one. Missing files are included — they still hold
// their slot in the running order, and dropping them would silently renumber
// every later track while a NAS is unmounted.
//
// "The" audiobook, not "the audio files": a book can have several recordings
// attached and exactly one of them is designated (see audioeditions.go). Only
// that one is a timeline. The others are recordings the user owns, and a
// query that returned all of them would hand the player chapter one twice in
// two different voices.
func (s *Store) AudioMediaFilesForBook(ctx context.Context, bookID string) ([]models.MediaFile, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT `+qualify(mediaFileColumns, "f")+`
		FROM media_files f
		JOIN audio_editions e ON e.id = f.audio_edition_id AND e.is_primary = 1
		WHERE f.book_id = ? AND f.kind = 'audio'
		ORDER BY f.track_number, f.path`, bookID)
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
		SELECT `+mediaFileColumns+`
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
