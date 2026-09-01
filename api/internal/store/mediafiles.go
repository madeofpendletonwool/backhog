package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/collinpendleton/backhog/api/internal/models"
)

// MediaFileStamp is the cheap change-detection snapshot the scanner keeps in
// memory for every row: (size, mtime) unchanged means the file is skipped
// without being opened.
type MediaFileStamp struct {
	ID      int64
	Size    int64
	Mtime   int64
	Missing bool
}

// MediaFileFilter describes a media file query. Zero values mean "no filter".
type MediaFileFilter struct {
	Kind string
	// Unattached limits results to files no book is attached to yet.
	Unattached bool
	// IncludeMissing also returns rows whose path is currently absent from
	// its root. By default only present files come back — those are the ones
	// the attach UI can actually open.
	IncludeMissing bool
}

// MediaFileIndex loads every media file keyed by root, then by path, so one
// SELECT drives the whole change-detection pass instead of a query per file.
func (s *Store) MediaFileIndex(ctx context.Context) (map[string]map[string]MediaFileStamp, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT root, path, id, size_bytes, mtime, missing_at FROM media_files`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	index := map[string]map[string]MediaFileStamp{}
	for rows.Next() {
		var root, path string
		var stamp MediaFileStamp
		var missingAt sql.NullTime
		if err := rows.Scan(&root, &path, &stamp.ID, &stamp.Size, &stamp.Mtime, &missingAt); err != nil {
			return nil, err
		}
		stamp.Missing = missingAt.Valid
		if index[root] == nil {
			index[root] = map[string]MediaFileStamp{}
		}
		index[root][path] = stamp
	}
	return index, rows.Err()
}

// InsertMediaFiles adds newly discovered files in one chunked statement per
// batch. The scanner only inserts paths it did not see in the index, so the
// (root, path) unique constraint holds by construction.
func (s *Store) InsertMediaFiles(ctx context.Context, files []models.MediaFile) error {
	const cols = `(root, path, kind, size_bytes, mtime, duration_seconds, container_metadata, scanned_at)`
	const batchSize = 128
	for start := 0; start < len(files); start += batchSize {
		end := min(start+batchSize, len(files))
		batch := files[start:end]

		var sb strings.Builder
		args := make([]any, 0, len(batch)*8)
		sb.WriteString(`INSERT INTO media_files ` + cols + ` VALUES `)
		for i, f := range batch {
			if i > 0 {
				sb.WriteByte(',')
			}
			sb.WriteString(`(?,?,?,?,?,?,?,?)`)
			args = append(args, f.Root, f.Path, f.Kind, f.SizeBytes, f.Mtime,
				f.DurationSeconds, rawJSONArg(f.ContainerMetadata), f.ScannedAt)
		}
		if _, err := s.db.ExecContext(ctx, sb.String(), args...); err != nil {
			return fmt.Errorf("insert media files: %w", err)
		}
	}
	return nil
}

// UpdateMediaFileContent refreshes a row whose (size, mtime) moved: new
// content facts and a fresh scanned_at, and missing_at cleared in case the
// path was absent at the previous scan.
func (s *Store) UpdateMediaFileContent(ctx context.Context, f models.MediaFile) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE media_files
		SET kind = ?, size_bytes = ?, mtime = ?, duration_seconds = ?, container_metadata = ?,
		    scanned_at = ?, missing_at = NULL
		WHERE id = ?`,
		f.Kind, f.SizeBytes, f.Mtime, f.DurationSeconds, rawJSONArg(f.ContainerMetadata),
		f.ScannedAt, f.ID)
	return err
}

// RestoreMediaFiles clears missing_at for files that came back on their root,
// keeping the row (and any book_id association) intact.
func (s *Store) RestoreMediaFiles(ctx context.Context, ids []int64, at time.Time) error {
	const batchSize = 128
	for start := 0; start < len(ids); start += batchSize {
		batch := ids[start:min(start+batchSize, len(ids))]
		args := make([]any, 0, len(batch)+1)
		args = append(args, at)
		placeholders := make([]string, len(batch))
		for i, id := range batch {
			placeholders[i] = `?`
			args = append(args, id)
		}
		query := `UPDATE media_files SET missing_at = NULL, scanned_at = ? WHERE id IN (` +
			strings.Join(placeholders, `,`) + `)`
		if _, err := s.db.ExecContext(ctx, query, args...); err != nil {
			return err
		}
	}
	return nil
}

// MarkMediaFilesMissing flags previously-seen paths that this scan did not
// find. Rows are never deleted, so associations survive an unmounted NAS.
func (s *Store) MarkMediaFilesMissing(ctx context.Context, ids []int64, at time.Time) error {
	const batchSize = 128
	for start := 0; start < len(ids); start += batchSize {
		batch := ids[start:min(start+batchSize, len(ids))]
		args := make([]any, 0, len(batch)+1)
		args = append(args, at)
		placeholders := make([]string, len(batch))
		for i, id := range batch {
			placeholders[i] = `?`
			args = append(args, id)
		}
		query := `UPDATE media_files SET missing_at = ? WHERE missing_at IS NULL AND id IN (` +
			strings.Join(placeholders, `,`) + `)`
		if _, err := s.db.ExecContext(ctx, query, args...); err != nil {
			return err
		}
	}
	return nil
}

// ListMediaFiles returns scanned files for the attach UI, ordered by root and
// path for stable pagination-free listing.
func (s *Store) ListMediaFiles(ctx context.Context, f MediaFileFilter) ([]models.MediaFile, error) {
	var where []string
	var args []any

	if models.ValidMediaFileKind(f.Kind) {
		where = append(where, `kind = ?`)
		args = append(args, f.Kind)
	}
	if f.Unattached {
		where = append(where, `book_id IS NULL`)
	}
	if !f.IncludeMissing {
		where = append(where, `missing_at IS NULL`)
	}

	query := `SELECT id, root, path, kind, size_bytes, mtime, sha256, duration_seconds,
	                 container_metadata, book_id, scanned_at, missing_at
	          FROM media_files`
	if len(where) > 0 {
		query += ` WHERE ` + strings.Join(where, ` AND `)
	}
	query += ` ORDER BY root, path`

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	files := []models.MediaFile{}
	for rows.Next() {
		var f models.MediaFile
		var sha256 sql.NullString
		var metadata sql.NullString
		var bookID sql.NullString
		if err := rows.Scan(&f.ID, &f.Root, &f.Path, &f.Kind, &f.SizeBytes, &f.Mtime,
			&sha256, &f.DurationSeconds, &metadata, &bookID, &f.ScannedAt, &f.MissingAt); err != nil {
			return nil, err
		}
		if sha256.Valid {
			f.SHA256 = &sha256.String
		}
		if metadata.Valid {
			f.ContainerMetadata = json.RawMessage(metadata.String)
		}
		if bookID.Valid {
			f.BookID = &bookID.String
		}
		files = append(files, f)
	}
	return files, rows.Err()
}

// rawJSONArg turns a JSON value into a nullable SQL argument.
func rawJSONArg(raw json.RawMessage) any {
	if len(raw) == 0 {
		return nil
	}
	return string(raw)
}
