package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// KosyncDocument is one document id a sync client may present for an
// entry: the binary partial MD5, or a filename hash under one naming format.
type KosyncDocument struct {
	Hash           string
	Kind           string // "binary" or "filename"
	FilenameFormat string // "" for binary
}

// KosyncDocumentsFresh reports whether an entry's document ids were
// computed from this exact file — same media file, same (size, mtime) —
// and so can be trusted without rereading it.
func (s *Store) KosyncDocumentsFresh(ctx context.Context, entryID string, mediaFileID, size, mtime int64) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM kosync_documents
		WHERE entry_id = ? AND media_file_id = ? AND file_size = ? AND file_mtime = ?`,
		entryID, mediaFileID, size, mtime).Scan(&n)
	return n > 0, err
}

// ReplaceKosyncDocuments swaps an entry's document ids for a freshly
// computed set, in one transaction, so a lookup never sees half of each.
func (s *Store) ReplaceKosyncDocuments(ctx context.Context, entryID string, mediaFileID, size, mtime int64, docs []KosyncDocument) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM kosync_documents WHERE entry_id = ?`, entryID); err != nil {
		return err
	}
	for _, d := range docs {
		// Two formats can name the same file (no author: every format is
		// the bare title), so the second of a pair is a no-op.
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO kosync_documents
				(doc_hash, entry_id, kind, filename_format, media_file_id, file_size, file_mtime)
			VALUES (?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT (doc_hash, entry_id) DO NOTHING`,
			d.Hash, entryID, d.Kind, d.FilenameFormat, mediaFileID, size, mtime); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// KosyncEntryForHash resolves a document id to the caller's own library
// entry. The lookup is per user: two people holding the same EPUB share a
// binary hash, and each must land on their own entry and progress. A binary
// match outranks a filename one, the way the bytes outrank a name.
//
// A binding (a metadata match or a hand link, kosync_bindings) is consulted
// after the computed ids: the bytes are the stronger evidence, and a book
// re-downloaded since it was linked by hand is recognised by them anyway.
func (s *Store) KosyncEntryForHash(ctx context.Context, userID, hash string) (string, error) {
	var entryID string
	err := s.db.QueryRowContext(ctx, `
		SELECT entry_id FROM (
			SELECT d.entry_id, CASE d.kind WHEN 'binary' THEN 0 ELSE 1 END AS rank, d.computed_at AS at
			FROM kosync_documents d
			JOIN library_entries e ON e.id = d.entry_id
			WHERE d.doc_hash = ? AND e.user_id = ?
			UNION ALL
			SELECT b.entry_id, 2, b.created_at FROM kosync_bindings b
			JOIN library_entries e ON e.id = b.entry_id
			WHERE b.doc_hash = ? AND b.user_id = ? AND e.user_id = b.user_id
		) ORDER BY rank, at DESC LIMIT 1`, hash, userID, hash, userID).Scan(&entryID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return entryID, err
}

// ReadableEpubs narrows entries to the ones an e-reader can open: book
// entries of this user whose designated text file — the same choice
// EpubMediaFileForBook makes — is an .epub they may read (owner, or a share
// that carries files) and that the last scan still found. The value is the
// file's size, for the catalog's length attribute.
//
// The catalog lists only these. A paper-only shelf entry, an audiobook
// without text, or a MOBI/PDF primary is a book the device could not open,
// and an entry it cannot open is noise on a 6-inch list.
func (s *Store) ReadableEpubs(ctx context.Context, userID string, entryIDs []string) (map[string]int64, error) {
	out := make(map[string]int64, len(entryIDs))
	const chunk = 400 // under SQLite's bound-parameter ceiling, with room for the rule's
	for start := 0; start < len(entryIDs); start += chunk {
		ids := entryIDs[start:min(start+chunk, len(entryIDs))]
		args := make([]any, 0, len(ids)+3)
		args = append(args, userID)
		for _, id := range ids {
			args = append(args, id)
		}
		args = append(args, userID, userID)
		rows, err := s.db.QueryContext(ctx, `
			SELECT e.id, (SELECT mf.size_bytes FROM media_files mf
			              WHERE mf.book_id = e.book_id AND mf.missing_at IS NULL AND `+primaryTextIs+`
			              ORDER BY `+TextFormatRank+`, mf.id LIMIT 1),
			       LOWER((SELECT mf.path FROM media_files mf
			              WHERE mf.book_id = e.book_id AND mf.missing_at IS NULL AND `+primaryTextIs+`
			              ORDER BY `+TextFormatRank+`, mf.id LIMIT 1))
			FROM library_entries e
			WHERE e.user_id = ? AND e.media_type = 'book'
			  AND e.id IN (`+placeholders(len(ids))+`)
			  AND `+fileAccessRule("e.book_id"), args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var id string
			var size sql.NullInt64
			var path sql.NullString
			if err := rows.Scan(&id, &size, &path); err != nil {
				rows.Close()
				return nil, err
			}
			if path.Valid && strings.HasSuffix(path.String, ".epub") {
				out[id] = size.Int64
			}
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// placeholders renders n comma-separated SQL parameters.
func placeholders(n int) string {
	return strings.TrimSuffix(strings.Repeat("?,", n), ",")
}

// KosyncProgress is a device's last push for an entry (kosync_progress).
type KosyncProgress struct {
	EntryID    string
	DocHash    string
	Progress   string
	Percentage float64
	Device     string
	DeviceID   string
	CharOffset int
	PushedAt   time.Time
}

// GetKosyncProgress returns the last push stored for an entry, or
// ErrNotFound when no device has pushed one.
func (s *Store) GetKosyncProgress(ctx context.Context, entryID string) (KosyncProgress, error) {
	var p KosyncProgress
	err := s.db.QueryRowContext(ctx, `
		SELECT entry_id, doc_hash, progress, percentage, device, device_id, char_offset, pushed_at
		FROM kosync_progress WHERE entry_id = ?`, entryID).
		Scan(&p.EntryID, &p.DocHash, &p.Progress, &p.Percentage, &p.Device, &p.DeviceID, &p.CharOffset, &p.PushedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return p, ErrNotFound
	}
	return p, err
}

// SaveKosyncProgress records a device's push, replacing the previous one.
func (s *Store) SaveKosyncProgress(ctx context.Context, p KosyncProgress) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO kosync_progress (entry_id, doc_hash, progress, percentage, device, device_id, char_offset, pushed_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (entry_id) DO UPDATE SET
			doc_hash = excluded.doc_hash, progress = excluded.progress,
			percentage = excluded.percentage, device = excluded.device,
			device_id = excluded.device_id, char_offset = excluded.char_offset,
			pushed_at = excluded.pushed_at`,
		p.EntryID, p.DocHash, p.Progress, p.Percentage, p.Device, p.DeviceID, p.CharOffset, p.PushedAt.UTC())
	return err
}

// BindKosyncDocument links a document id to one of the user's entries and
// clears it from the unmatched list. The entry must be the user's own.
func (s *Store) BindKosyncDocument(ctx context.Context, userID, hash, entryID, source string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var owned int
	if err := tx.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM library_entries WHERE id = ? AND user_id = ? AND media_type = 'book'`,
		entryID, userID).Scan(&owned); err != nil {
		return err
	}
	if owned == 0 {
		return ErrNotFound
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO kosync_bindings (user_id, doc_hash, entry_id, source) VALUES (?, ?, ?, ?)
		ON CONFLICT (user_id, doc_hash) DO UPDATE SET entry_id = excluded.entry_id,
			source = excluded.source, created_at = CURRENT_TIMESTAMP`,
		userID, hash, entryID, source); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		DELETE FROM kosync_unmatched WHERE user_id = ? AND doc_hash = ?`, userID, hash); err != nil {
		return err
	}
	return tx.Commit()
}

// KosyncUnmatched is a push nothing could place.
type KosyncUnmatched struct {
	DocHash    string    `json:"document"`
	Title      string    `json:"title"`
	Authors    string    `json:"authors"`
	Filename   string    `json:"filename"`
	Progress   string    `json:"progress"`
	Percentage float64   `json:"percentage"`
	Device     string    `json:"device"`
	SeenAt     time.Time `json:"seen_at"`
}

// SaveKosyncUnmatched remembers (or refreshes) a push for a document no
// entry claims.
func (s *Store) SaveKosyncUnmatched(ctx context.Context, userID string, u KosyncUnmatched) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO kosync_unmatched (user_id, doc_hash, title, authors, filename, progress, percentage, device, seen_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (user_id, doc_hash) DO UPDATE SET
			title = excluded.title, authors = excluded.authors, filename = excluded.filename,
			progress = excluded.progress, percentage = excluded.percentage,
			device = excluded.device, seen_at = excluded.seen_at`,
		userID, u.DocHash, u.Title, u.Authors, u.Filename, u.Progress, u.Percentage, u.Device, u.SeenAt.UTC())
	return err
}

// ListKosyncUnmatched returns the user's unplaced pushes, newest first.
func (s *Store) ListKosyncUnmatched(ctx context.Context, userID string) ([]KosyncUnmatched, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT doc_hash, title, authors, filename, progress, percentage, device, seen_at
		FROM kosync_unmatched WHERE user_id = ? ORDER BY seen_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []KosyncUnmatched{}
	for rows.Next() {
		var u KosyncUnmatched
		if err := rows.Scan(&u.DocHash, &u.Title, &u.Authors, &u.Filename, &u.Progress,
			&u.Percentage, &u.Device, &u.SeenAt); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// EntriesWithoutDocumentIDs lists the user's book entries that have never
// had document ids computed, up to limit — the candidates a sync for an
// unknown hash hashes on demand.
func (s *Store) EntriesWithoutDocumentIDs(ctx context.Context, userID string, limit int) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT e.id FROM library_entries e
		WHERE e.user_id = ? AND e.media_type = 'book'
		  AND NOT EXISTS (SELECT 1 FROM kosync_documents d WHERE d.entry_id = e.id)
		  AND EXISTS (SELECT 1 FROM media_files mf WHERE mf.book_id = e.book_id
		              AND mf.kind = 'epub' AND mf.missing_at IS NULL)
		ORDER BY e.updated_at DESC LIMIT ?`, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// EntriesByTitle returns the user's book entries whose title matches,
// case-insensitively, with their authors — the candidates a push's own
// metadata can be matched against when its hash is unknown.
func (s *Store) EntriesByTitle(ctx context.Context, userID, title string) (map[string][]string, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT e.id, b.authors_json FROM library_entries e JOIN books b ON b.id = e.book_id
		WHERE e.user_id = ? AND e.media_type = 'book' AND b.title = ? COLLATE NOCASE`, userID, title)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string][]string{}
	for rows.Next() {
		var id, authorsJSON string
		if err := rows.Scan(&id, &authorsJSON); err != nil {
			return nil, err
		}
		var authors []string
		_ = json.Unmarshal([]byte(authorsJSON), &authors)
		out[id] = authors
	}
	return out, rows.Err()
}

// UsernameExists reports whether an account holds this username
// (case-insensitive) — all the kosync registration refusal needs to know.
func (s *Store) UsernameExists(ctx context.Context, username string) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM users WHERE username = ? COLLATE NOCASE`, strings.TrimSpace(username)).Scan(&n)
	return n > 0, err
}

// CatalogBookMatch is a book entry found by the e-reader catalog's search.
type CatalogBookMatch struct {
	EntryID, BookID, Title, AuthorsJSON string
}

// SearchBooksBySeriesOrISBN finds the user's book entries whose series name
// contains q, or one of whose printings has q as its ISBN (digits only, so
// "978-0-06-102069-8" matches) — the two things a reader types into a
// device's search box that title/author search misses. Series hits come in
// reading order.
func (s *Store) SearchBooksBySeriesOrISBN(ctx context.Context, userID, q string) ([]CatalogBookMatch, error) {
	q = strings.TrimSpace(q)
	digits := strings.Map(func(r rune) rune {
		if (r >= '0' && r <= '9') || r == 'X' || r == 'x' {
			return r
		}
		return -1
	}, q)
	if len(digits) != 10 && len(digits) != 13 {
		digits = "-" // never an ISBN
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT e.id, b.id, b.title, b.authors_json FROM library_entries e
		JOIN books b ON b.id = e.book_id
		WHERE e.user_id = ? AND e.media_type = 'book'
		  AND ((b.series_name <> '' AND b.series_name LIKE ? COLLATE NOCASE)
		       OR EXISTS (SELECT 1 FROM book_editions ed WHERE ed.book_id = b.id
		                  AND (ed.isbn13 = ? OR UPPER(ed.isbn10) = UPPER(?))))
		ORDER BY b.series_name COLLATE NOCASE, b.series_number NULLS LAST, b.title COLLATE NOCASE`,
		userID, "%"+escapeLike(q)+"%", digits, digits)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CatalogBookMatch
	for rows.Next() {
		var m CatalogBookMatch
		if err := rows.Scan(&m.EntryID, &m.BookID, &m.Title, &m.AuthorsJSON); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}
