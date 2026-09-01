package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/collinpendleton/backhog/api/internal/models"
)

// ReplaceMediaSkipped swaps one root's skipped-file inventory in a single
// transaction: delete-then-insert, because nothing is attached to a skipped
// row and the scan just walked the whole root.
func (s *Store) ReplaceMediaSkipped(ctx context.Context, root string, files []models.MediaSkipped) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `DELETE FROM media_skipped WHERE root = ?`, root); err != nil {
		return err
	}
	const batchSize = 128
	for start := 0; start < len(files); start += batchSize {
		batch := files[start:min(start+batchSize, len(files))]
		var sb strings.Builder
		args := make([]any, 0, len(batch)*7)
		sb.WriteString(`INSERT INTO media_skipped (root, path, ext, reason, size_bytes, mtime, seen_at) VALUES `)
		for i, f := range batch {
			if i > 0 {
				sb.WriteByte(',')
			}
			sb.WriteString(`(?,?,?,?,?,?,?)`)
			args = append(args, f.Root, f.Path, f.Ext, f.Reason, f.SizeBytes, f.Mtime, f.SeenAt)
		}
		if _, err := tx.ExecContext(ctx, sb.String(), args...); err != nil {
			return fmt.Errorf("insert skipped files: %w", err)
		}
	}
	return tx.Commit()
}

// ListMediaSkipped returns every skipped file, ordered by root and path for
// stable display grouping.
func (s *Store) ListMediaSkipped(ctx context.Context) ([]models.MediaSkipped, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, root, path, ext, reason, size_bytes, seen_at
		FROM media_skipped ORDER BY root, path`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []models.MediaSkipped{}
	for rows.Next() {
		var f models.MediaSkipped
		if err := rows.Scan(&f.ID, &f.Root, &f.Path, &f.Ext, &f.Reason, &f.SizeBytes, &f.SeenAt); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// ErrAttach marks a rejected attachment whose cause is the request itself
// (mixed kinds, a missing file, an empty batch): handlers answer 400.
var ErrAttach = errors.New("invalid attachment")

// AttachMediaFiles points the given media files at the book behind a user's
// library entry. For audio, the slice order is the track order (1-based);
// epubs carry no track number. The whole batch is one transaction: every id
// must exist, match the declared kind, be present on its root, and either be
// unattached or already attached to this same book.
func (s *Store) AttachMediaFiles(ctx context.Context, userID, entryID string, fileIDs []int64, kind string) ([]models.MediaFile, error) {
	if len(fileIDs) == 0 {
		return nil, fmt.Errorf("%w: no file ids supplied", ErrAttach)
	}
	seen := make(map[int64]bool, len(fileIDs))
	for _, id := range fileIDs {
		if seen[id] {
			return nil, fmt.Errorf("%w: duplicate file id %d", ErrAttach, id)
		}
		seen[id] = true
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	// Ownership first, exactly like BookIDForEntry: an entry that does not
	// exist or is not the caller's book entry is a 404, never a leak.
	var bookID string
	err = tx.QueryRowContext(ctx, `
		SELECT book_id FROM library_entries
		WHERE id = ? AND user_id = ? AND media_type = 'book'`, entryID, userID).Scan(&bookID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}

	attached := make([]models.MediaFile, 0, len(fileIDs))
	for i, id := range fileIDs {
		f, err := scanMediaFileTx(ctx, tx, `SELECT id, root, path, kind, size_bytes, mtime, sha256,
			       duration_seconds, container_metadata, book_id, scanned_at, missing_at
			  FROM media_files WHERE id = ?`, id)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		if err != nil {
			return nil, err
		}
		if f.Kind != kind {
			return nil, fmt.Errorf("%w: file %d is %s, not %s", ErrAttach, id, f.Kind, kind)
		}
		if f.MissingAt != nil {
			return nil, fmt.Errorf("%w: file %d is currently missing from its root", ErrAttach, id)
		}
		if f.BookID != nil && *f.BookID != bookID {
			return nil, ErrConflict
		}

		var track any
		if kind == models.MediaFileAudio {
			track = i + 1
		}
		if _, err := tx.ExecContext(ctx,
			`UPDATE media_files SET book_id = ?, track_number = ? WHERE id = ?`,
			bookID, track, id); err != nil {
			return nil, err
		}
		f.BookID = &bookID
		attached = append(attached, f)
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return attached, nil
}

// DetachMediaFile clears one file's attachment. It is scoped through the
// entry's book, so a file attached elsewhere is a 404 rather than a
// cross-library edit. The file on disk is never touched, and the row stays.
func (s *Store) DetachMediaFile(ctx context.Context, userID, entryID string, fileID int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var bookID string
	err = tx.QueryRowContext(ctx, `
		SELECT book_id FROM library_entries
		WHERE id = ? AND user_id = ? AND media_type = 'book'`, entryID, userID).Scan(&bookID)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}

	res, err := tx.ExecContext(ctx,
		`UPDATE media_files SET book_id = NULL, track_number = NULL WHERE id = ? AND book_id = ?`,
		fileID, bookID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return tx.Commit()
}

// MediaFilesForEntry lists the files attached to a user's book entry — the
// epub first (there is at most one that parses), then audio in track order.
func (s *Store) MediaFilesForEntry(ctx context.Context, userID, entryID string) ([]models.MediaFile, error) {
	var bookID string
	err := s.db.QueryRowContext(ctx, `
		SELECT book_id FROM library_entries
		WHERE id = ? AND user_id = ? AND media_type = 'book'`, entryID, userID).Scan(&bookID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return s.MediaFilesForBook(ctx, bookID)
}

// MediaFilesForBook lists a book's attached files, epubs first then audio in
// track order. Book-level, not user-level: the inventory is shared.
func (s *Store) MediaFilesForBook(ctx context.Context, bookID string) ([]models.MediaFile, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, root, path, kind, size_bytes, mtime, sha256,
		       duration_seconds, container_metadata, book_id, scanned_at, missing_at
		FROM media_files
		WHERE book_id = ?
		ORDER BY kind DESC, track_number, path`, bookID)
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

// scanMediaFile reads a media_files row (without track_number) from a Rows.
func scanMediaFile(rows interface{ Scan(dest ...any) error }) (models.MediaFile, error) {
	var f models.MediaFile
	var sha256, metadata, attachedBook sql.NullString
	if err := rows.Scan(&f.ID, &f.Root, &f.Path, &f.Kind, &f.SizeBytes, &f.Mtime,
		&sha256, &f.DurationSeconds, &metadata, &attachedBook, &f.ScannedAt, &f.MissingAt); err != nil {
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

// scanMediaFileTx is scanMediaFile for a single-row query inside a
// transaction, translating "no row" for the caller.
func scanMediaFileTx(ctx context.Context, tx *sql.Tx, query string, args ...any) (models.MediaFile, error) {
	row := tx.QueryRowContext(ctx, query, args...)
	f, err := scanMediaFile(row)
	return f, err
}

// IgnoreMediaFiles records "stop suggesting these files" for one user.
// Unknown ids are fine to ignore: the point is a quiet list, not an audit.
func (s *Store) IgnoreMediaFiles(ctx context.Context, userID string, fileIDs []int64) (int, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	ignored := 0
	for _, id := range fileIDs {
		res, err := tx.ExecContext(ctx,
			`INSERT INTO media_ignores (user_id, media_file_id) VALUES (?, ?)
			 ON CONFLICT(user_id, media_file_id) DO NOTHING`, userID, id)
		if err != nil {
			return 0, err
		}
		if n, _ := res.RowsAffected(); n > 0 {
			ignored++
		}
	}
	return ignored, tx.Commit()
}

// UnignoreMediaFile reverses one ignore.
func (s *Store) UnignoreMediaFile(ctx context.Context, userID string, fileID int64) error {
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM media_ignores WHERE user_id = ? AND media_file_id = ?`, userID, fileID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// IgnoredMediaFileIDs loads the user's ignored file set so the matcher can
// drop those candidates without per-file queries.
func (s *Store) IgnoredMediaFileIDs(ctx context.Context, userID string) (map[int64]bool, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT media_file_id FROM media_ignores WHERE user_id = ?`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	ignored := map[int64]bool{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ignored[id] = true
	}
	return ignored, rows.Err()
}
