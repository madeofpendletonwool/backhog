package store

import (
	"context"
	"database/sql"
	"errors"

	"github.com/collinpendleton/backhog/api/internal/models"
)

// Book series storage (MAD-469): membership columns on the shared work row,
// written from Calibre sidecar evidence at attach time and healed by a boot
// walker, read here for the series endpoints the "series memory" stands on.
// Identity is the normalized series name; the read queries scope through
// library_entries exactly the way the shelf does, so a series is only ever
// the caller's own books.

// SetBookSeriesIfEmpty records a book's series membership when no earlier
// evidence got there first. First evidence wins by design: two libraries
// organized differently (or disagreeing catalogue rows) is a metadata
// conflict the app reports rather than silently resolves. The bool reports
// whether this call wrote.
func (s *Store) SetBookSeriesIfEmpty(ctx context.Context, bookID, name string, number *float64) (bool, error) {
	if name == "" {
		return false, nil
	}
	res, err := s.db.ExecContext(ctx, `
		UPDATE books SET series_name = ?, series_number = ?
		WHERE id = ? AND series_name = ''`, name, number, bookID)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// SeriesCandidateBook is one book the series walker may be able to name:
// attached files, but no series recorded yet. ID is the work key.
type SeriesCandidateBook struct {
	ID string
}

// BooksWithoutSeries lists books that have files attached and no series
// name yet, ordered by id past the cursor — the walker's rotating queue,
// so a book whose evidence never arrives (an unorganized shelf) cannot
// wedge the head and starve everything behind it.
func (s *Store) BooksWithoutSeries(ctx context.Context, cursor string, max int) ([]SeriesCandidateBook, error) {
	if max <= 0 {
		max = 50
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT b.id
		FROM books b
		WHERE b.series_name = '' AND b.id > ?
		  AND EXISTS (SELECT 1 FROM media_files mf WHERE mf.book_id = b.id)
		ORDER BY b.id LIMIT ?`, cursor, max)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SeriesCandidateBook
	for rows.Next() {
		var b SeriesCandidateBook
		if err := rows.Scan(&b.ID); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// AttachedFilesInDirs lists attached files under any of the (root, dir)
// pairs — the occupancy a directory-level sidecar verdict needs. Directory
// prefixes match the pair exactly, after folding ripper platter names the
// way the matcher does; callers pass already-folded dirs.
func (s *Store) AttachedFilesInDirs(ctx context.Context, dirs [][2]string) ([]models.MediaFile, error) {
	if len(dirs) == 0 {
		return nil, nil
	}
	query := `SELECT id, root, path, kind, size_bytes, mtime, book_id
		FROM media_files WHERE book_id IS NOT NULL AND (`
	var args []any
	for i, d := range dirs {
		if i > 0 {
			query += " OR "
		}
		query += "(root = ? AND path LIKE ?)"
		// The platter-folded directory is a prefix; its files sit inside
		// it, one level or (still folded) deeper.
		args = append(args, d[0], d[1]+"/%")
	}
	query += ")"
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.MediaFile
	for rows.Next() {
		var f models.MediaFile
		if err := rows.Scan(&f.ID, &f.Root, &f.Path, &f.Kind, &f.SizeBytes, &f.Mtime, &f.BookID); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// BookIDForWorkKey resolves an Open Library work key the sidecars carry to
// a cached book row, when the work is in the catalogue at all.
func (s *Store) BookIDForWorkKey(ctx context.Context, workKey string) (string, error) {
	var id string
	err := s.db.QueryRowContext(ctx, `SELECT id FROM books WHERE id = ?`, workKey).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return id, err
}

// BookIDForISBN resolves an ISBN to the work carrying it, through the
// editions cache — the exact identity a sidecar's isbn field offers.
func (s *Store) BookIDForISBN(ctx context.Context, isbn string) (string, error) {
	var id string
	err := s.db.QueryRowContext(ctx, `
		SELECT book_id FROM book_editions
		WHERE isbn10 = ? OR isbn13 = ?
		ORDER BY book_id LIMIT 1`, isbn, isbn).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return id, err
}

// SeriesIndexRow is one series of the caller's library: its books, how
// many are finished, and how many are mid-read.
type SeriesIndexRow struct {
	Name     string
	Books    int
	Finished int
	Reading  int
}

// SeriesIndex lists every series the caller's shelf holds a member of.
func (s *Store) BookSeriesIndex(ctx context.Context, userID string) ([]SeriesIndexRow, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT b.series_name, COUNT(*),
		       SUM(CASE WHEN e.status = 'played' THEN 1 ELSE 0 END),
		       SUM(CASE WHEN e.status = 'playing' THEN 1 ELSE 0 END)
		FROM library_entries e
		JOIN books b ON b.id = e.book_id
		WHERE e.user_id = ? AND e.media_type = 'book' AND b.series_name <> ''
		GROUP BY b.series_name COLLATE NOCASE
		ORDER BY b.series_name COLLATE NOCASE`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SeriesIndexRow
	for rows.Next() {
		var r SeriesIndexRow
		if err := rows.Scan(&r.Name, &r.Books, &r.Finished, &r.Reading); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// SeriesBook is one member of a series, with everything the returning
// reader's "where was I" needs: the shelf status, the stored position, and
// the rank inside the series.
type SeriesBook struct {
	EntryID          string
	BookID           string
	Title            string
	AuthorsJSON      string
	Status           string
	SeriesNumber     *float64
	FirstPublishYear *int
	PositionMode     string
	CharOffset       int
	Percent          float64
}

// SeriesDetail lists the caller's books of one series in reading order:
// rank inside the series first (Calibre's series_index), with publish year
// and title breaking ties and ordering the ranks nobody recorded.
func (s *Store) BookSeriesDetail(ctx context.Context, userID, name string) ([]SeriesBook, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT e.id, b.id, b.title, b.authors_json, e.status,
		       b.series_number, b.first_publish_year,
		       COALESCE(bp.position_mode, 'text'),
		       COALESCE(bp.char_offset, 0),
		       COALESCE(bp.percent_complete, 0)
		FROM library_entries e
		JOIN books b ON b.id = e.book_id
		LEFT JOIN book_progress bp ON bp.entry_id = e.id
		WHERE e.user_id = ? AND e.media_type = 'book'
		  AND b.series_name = ? COLLATE NOCASE
		ORDER BY (b.series_number IS NULL), b.series_number,
		         b.first_publish_year, b.title`, userID, name)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SeriesBook
	for rows.Next() {
		var b SeriesBook
		if err := rows.Scan(&b.EntryID, &b.BookID, &b.Title, &b.AuthorsJSON, &b.Status,
			&b.SeriesNumber, &b.FirstPublishYear, &b.PositionMode, &b.CharOffset, &b.Percent); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}
