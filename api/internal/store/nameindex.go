package store

import (
	"context"

	"github.com/collinpendleton/backhog/api/internal/models"
)

// The name index (MAD-670): every occurrence of every candidate proper
// name, written on ingest and re-parse by the ingester's indexNames and
// healed by the index walk, exactly like the FTS table beside it. Reads
// never build SQL of their own beyond what lives here.

// ReplaceNameIndex writes one media file's name occurrences, replacing
// whatever was there, and stamps the extractor version on the epub_texts
// row in the same transaction — the two facts must land together or a
// crash between them would leave a current stamp over stale offsets.
//
// Idempotent by construction: delete-then-insert keyed by media file, so a
// re-parse (same file, same epub_texts row id) lands in place. A nil slice
// is a legitimate write: a book whose heuristics found no names is fully
// indexed at version.
func (s *Store) ReplaceNameIndex(ctx context.Context, mediaFileID int64, version string, occ []models.NameOccurrence) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx,
		`DELETE FROM name_occurrences WHERE media_file_id = ?`, mediaFileID); err != nil {
		return err
	}
	const batchSize = 128
	for start := 0; start < len(occ); start += batchSize {
		end := min(start+batchSize, len(occ))
		for i := start; i < end; i++ {
			o := occ[i]
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO name_occurrences (media_file_id, name, display, char_start, char_end)
				VALUES (?, ?, ?, ?, ?)`,
				mediaFileID, o.Name, o.Display, o.CharStart, o.CharEnd); err != nil {
				return err
			}
		}
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE epub_texts SET names_version = ? WHERE media_file_id = ?`,
		version, mediaFileID); err != nil {
		return err
	}
	return tx.Commit()
}

// ListNameOccurrences returns a media file's whole index in book order.
// The names endpoint groups and clamps in the caller: the grouping rule
// ("a name first appearing past the bound does not exist yet") is a
// policy about positions, and policy lives with the handler that owns the
// bound, not in SQL that would quietly disagree with it.
func (s *Store) ListNameOccurrences(ctx context.Context, mediaFileID int64) ([]models.NameOccurrence, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT media_file_id, name, display, char_start, char_end
		FROM name_occurrences WHERE media_file_id = ? ORDER BY char_start`, mediaFileID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []models.NameOccurrence{}
	for rows.Next() {
		var o models.NameOccurrence
		if err := rows.Scan(&o.MediaFileID, &o.Name, &o.Display, &o.CharStart, &o.CharEnd); err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// NameMentions returns one name's occurrences up to the spoiler bound
// (exclusive: everything at or past it is withheld), in book order, plus
// how many there were — the clamp is a WHERE clause because the bound is
// known before the query runs, and the count is then the answer's honest
// total rather than a census of the whole book.
func (s *Store) NameMentions(ctx context.Context, mediaFileID int64, name string, bound int) ([]models.NameOccurrence, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT media_file_id, name, display, char_start, char_end
		FROM name_occurrences
		WHERE media_file_id = ? AND name = ? AND char_start < ?
		ORDER BY char_start`, mediaFileID, name, bound)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []models.NameOccurrence{}
	for rows.Next() {
		var o models.NameOccurrence
		if err := rows.Scan(&o.MediaFileID, &o.Name, &o.Display, &o.CharStart, &o.CharEnd); err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// HiddenBookNames returns the folded names one reader has hidden on one
// book. Empty is the common case and not an error.
func (s *Store) HiddenBookNames(ctx context.Context, userID, bookID string) (map[string]bool, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT name FROM hidden_book_names WHERE user_id = ? AND book_id = ?`,
		userID, bookID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make(map[string]bool)
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		out[name] = true
	}
	return out, rows.Err()
}

// HideBookName records a reader's verdict that a name is a false positive.
// Idempotent: hiding twice is still hidden.
func (s *Store) HideBookName(ctx context.Context, userID, bookID, name string) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO hidden_book_names (user_id, book_id, name) VALUES (?, ?, ?)
		ON CONFLICT(user_id, book_id, name) DO NOTHING`, userID, bookID, name)
	return err
}

// UnhideBookName takes a name back into the index. Absent is fine — the
// end state is what matters.
func (s *Store) UnhideBookName(ctx context.Context, userID, bookID, name string) error {
	_, err := s.db.ExecContext(ctx, `
		DELETE FROM hidden_book_names WHERE user_id = ? AND book_id = ? AND name = ?`,
		userID, bookID, name)
	return err
}

// ClearNameIndexForMediaFile drops a file's occurrences — the detach path,
// where the file stops resolving and its rows must stop answering the same
// breath, exactly like the FTS rows beside them.
func (s *Store) ClearNameIndexForMediaFile(ctx context.Context, mediaFileID int64) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM name_occurrences WHERE media_file_id = ?`, mediaFileID)
	return err
}
