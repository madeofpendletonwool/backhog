package store

import (
	"context"
	"database/sql"
	"errors"
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
func (s *Store) KosyncEntryForHash(ctx context.Context, userID, hash string) (string, error) {
	var entryID string
	err := s.db.QueryRowContext(ctx, `
		SELECT d.entry_id FROM kosync_documents d
		JOIN library_entries e ON e.id = d.entry_id
		WHERE d.doc_hash = ? AND e.user_id = ?
		ORDER BY (d.kind = 'binary') DESC, d.computed_at DESC
		LIMIT 1`, hash, userID).Scan(&entryID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return entryID, err
}
