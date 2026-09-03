package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/collinpendleton/backhog/api/internal/models"
)

// CreatePhysicalCopy registers a printing the user holds against one of
// their book entries. The edition must exist and belong to the entry's
// work — page numbers of some other book's printing are a client bug,
// not a 404 worth distinguishing. ErrConflict means this printing is
// already registered; ErrNotFound means the entry is not the caller's.
func (s *Store) CreatePhysicalCopy(ctx context.Context, userID, entryID, editionID, notes string) (models.PhysicalCopy, error) {
	if strings.TrimSpace(editionID) == "" {
		return models.PhysicalCopy{}, errors.New("edition_id is required")
	}
	bookID, err := s.BookIDForEntry(ctx, userID, entryID)
	if err != nil {
		return models.PhysicalCopy{}, err
	}
	var edBookID string
	err = s.db.QueryRowContext(ctx,
		`SELECT book_id FROM book_editions WHERE id = ?`, editionID).Scan(&edBookID)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return models.PhysicalCopy{}, fmt.Errorf("no such edition %q", editionID)
	case err != nil:
		return models.PhysicalCopy{}, err
	case edBookID != bookID:
		return models.PhysicalCopy{}, errors.New("that edition is not a printing of this book")
	}

	c := models.PhysicalCopy{
		ID: newID(), UserID: userID, EntryID: entryID,
		EditionID: editionID, Notes: notes,
	}
	err = s.db.QueryRowContext(ctx, `
		INSERT INTO physical_copies (id, user_id, entry_id, edition_id, notes)
		VALUES (?, ?, ?, ?, ?)
		RETURNING created_at`,
		c.ID, c.UserID, c.EntryID, c.EditionID, c.Notes).Scan(&c.CreatedAt)
	if isUniqueViolation(err) {
		return models.PhysicalCopy{}, ErrConflict
	}
	if err != nil {
		return models.PhysicalCopy{}, err
	}

	// Only the copy of the printing the entry is anchored to feeds the page
	// map, and most entries are added by title and anchored to nothing. "I
	// own this on paper" is the strongest statement anyone makes about which
	// printing an entry is, so an unanchored entry adopts it — otherwise the
	// reader scans pages into a map nothing reads and is never told why. An
	// entry already anchored elsewhere keeps its printing: owning two is
	// allowed, and silently re-pointing progress at the other one is not.
	if _, err := s.db.ExecContext(ctx, `
		UPDATE library_entries SET edition_id = ?
		WHERE id = ? AND user_id = ? AND (edition_id IS NULL OR edition_id = '')`,
		c.EditionID, entryID, userID); err != nil {
		return models.PhysicalCopy{}, err
	}
	var anchored sql.NullString
	if err := s.db.QueryRowContext(ctx,
		`SELECT edition_id FROM library_entries WHERE id = ?`, entryID).Scan(&anchored); err != nil {
		return models.PhysicalCopy{}, err
	}
	c.DrivesPages = anchored.Valid && anchored.String == c.EditionID
	return c, nil
}

// PhysicalCopies lists the caller's printings for one entry, oldest
// first, each with a count of its recorded page anchors.
func (s *Store) PhysicalCopies(ctx context.Context, userID, entryID string) ([]models.PhysicalCopy, error) {
	if _, err := s.BookIDForEntry(ctx, userID, entryID); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT pc.id, pc.user_id, pc.entry_id, pc.edition_id, pc.notes, pc.created_at,
		       (SELECT COUNT(*) FROM page_anchors pa WHERE pa.physical_copy_id = pc.id),
		       (e.edition_id = pc.edition_id)
		FROM physical_copies pc
		JOIN library_entries e ON e.id = pc.entry_id
		WHERE pc.user_id = ? AND pc.entry_id = ?
		ORDER BY pc.created_at, pc.id`, userID, entryID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []models.PhysicalCopy{}
	for rows.Next() {
		var c models.PhysicalCopy
		var drives sql.NullBool
		if err := rows.Scan(&c.ID, &c.UserID, &c.EntryID, &c.EditionID,
			&c.Notes, &c.CreatedAt, &c.AnchorCount, &drives); err != nil {
			return nil, err
		}
		c.DrivesPages = drives.Bool
		out = append(out, c)
	}
	return out, rows.Err()
}

// UpdatePhysicalCopyNotes rewrites a copy's notes. The edition is
// immutable: anchors hang off the printing, so "moving" a copy to another
// printing would silently re-mean every page number it holds.
func (s *Store) UpdatePhysicalCopyNotes(ctx context.Context, userID, entryID, copyID, notes string) (models.PhysicalCopy, error) {
	if _, err := s.physicalCopyForUser(ctx, userID, entryID, copyID); err != nil {
		return models.PhysicalCopy{}, err
	}
	c := models.PhysicalCopy{ID: copyID, UserID: userID, EntryID: entryID}
	err := s.db.QueryRowContext(ctx, `
		UPDATE physical_copies SET notes = ? WHERE id = ?
		RETURNING edition_id, notes, created_at`, notes, copyID).
		Scan(&c.EditionID, &c.Notes, &c.CreatedAt)
	if err != nil {
		return models.PhysicalCopy{}, err
	}
	return c, nil
}

// DeletePhysicalCopy drops a copy and its whole page map; re-scanning
// rebuilds it. ErrNotFound covers both "no such copy" and "not yours",
// indistinguishably.
func (s *Store) DeletePhysicalCopy(ctx context.Context, userID, entryID, copyID string) error {
	if _, err := s.physicalCopyForUser(ctx, userID, entryID, copyID); err != nil {
		return err
	}
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM physical_copies WHERE id = ? AND user_id = ? AND entry_id = ?`,
		copyID, userID, entryID)
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err == nil && n == 0 {
		return ErrNotFound
	}
	return nil
}

// SavePageAnchor records one (printed page, char offset) pair for one of
// the caller's copies, last-write-wins: re-scanning a page corrects the
// anchor instead of stacking a conflicting one.
func (s *Store) SavePageAnchor(ctx context.Context, userID, entryID, copyID string, a models.PageAnchor) (models.PageAnchor, error) {
	if a.PrintedPage <= 0 {
		return models.PageAnchor{}, errors.New("printed_page must be a positive page number")
	}
	if a.CharOffset < 0 {
		return models.PageAnchor{}, errors.New("char_offset must not be negative")
	}
	if a.Source == "" {
		a.Source = models.PageAnchorSourceManual
	}
	if !models.ValidPageAnchorSource(a.Source) {
		return models.PageAnchor{}, fmt.Errorf("source must be %q or %q",
			models.PageAnchorSourceOCR, models.PageAnchorSourceManual)
	}
	if math.IsNaN(a.Confidence) || a.Confidence < 0 || a.Confidence > 1 {
		return models.PageAnchor{}, errors.New("confidence must be between 0 and 1")
	}
	if _, err := s.physicalCopyForUser(ctx, userID, entryID, copyID); err != nil {
		return models.PageAnchor{}, err
	}

	a.PhysicalCopyID = copyID
	err := s.db.QueryRowContext(ctx, `
		INSERT INTO page_anchors (physical_copy_id, printed_page, char_offset,
		                          source, confidence, created_at)
		VALUES (?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(physical_copy_id, printed_page) DO UPDATE SET
			char_offset = excluded.char_offset,
			source      = excluded.source,
			confidence  = excluded.confidence,
			created_at  = CURRENT_TIMESTAMP
		RETURNING printed_page, char_offset, source, confidence, created_at`,
		copyID, a.PrintedPage, a.CharOffset, a.Source, a.Confidence).
		Scan(&a.PrintedPage, &a.CharOffset, &a.Source, &a.Confidence, &a.CreatedAt)
	if err != nil {
		return models.PageAnchor{}, err
	}
	return a, nil
}

// PageAnchors lists one copy's page map by page number.
func (s *Store) PageAnchors(ctx context.Context, userID, entryID, copyID string) ([]models.PageAnchor, error) {
	if _, err := s.physicalCopyForUser(ctx, userID, entryID, copyID); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT physical_copy_id, printed_page, char_offset, source, confidence, created_at
		FROM page_anchors
		WHERE physical_copy_id = ?
		ORDER BY printed_page`, copyID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []models.PageAnchor{}
	for rows.Next() {
		var a models.PageAnchor
		if err := rows.Scan(&a.PhysicalCopyID, &a.PrintedPage, &a.CharOffset,
			&a.Source, &a.Confidence, &a.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// PageAnchorsForEntry is the position translator's page map: the anchors
// of the copy registered for the printing the entry itself is anchored
// to (library_entries.edition_id). An entry with no edition, or no copy
// of it, simply has no page map — the normal case, not an error.
func (s *Store) PageAnchorsForEntry(ctx context.Context, entryID string) ([]models.PageAnchor, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT pa.physical_copy_id, pa.printed_page, pa.char_offset, pa.source,
		       pa.confidence, pa.created_at
		FROM page_anchors pa
		JOIN physical_copies pc ON pc.id = pa.physical_copy_id
		JOIN library_entries e ON e.id = pc.entry_id AND e.edition_id = pc.edition_id
		WHERE e.id = ?
		ORDER BY pa.char_offset`, entryID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []models.PageAnchor{}
	for rows.Next() {
		var a models.PageAnchor
		if err := rows.Scan(&a.PhysicalCopyID, &a.PrintedPage, &a.CharOffset,
			&a.Source, &a.Confidence, &a.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// PageMapSeedForEntry returns the coarse page map an entry starts with:
// the page count of the printing the user registered a copy of, and the
// length of the canonical text those pages hold. Both halves are
// optional — a printing Open Library never recorded a page count for, or
// a book whose EPUB has not been parsed, seeds nothing — and an entry
// with no registered copy seeds nothing either, because a page readout
// for a printing nobody owns is a page readout for nothing.
func (s *Store) PageMapSeedForEntry(ctx context.Context, entryID string) (models.PageMapSeed, error) {
	var seed models.PageMapSeed
	var pageCount sql.NullInt64
	err := s.db.QueryRowContext(ctx, `
		SELECT ed.page_count
		FROM library_entries e
		JOIN physical_copies pc ON pc.entry_id = e.id AND pc.edition_id = e.edition_id
		JOIN book_editions ed ON ed.id = pc.edition_id
		WHERE e.id = ?
		ORDER BY pc.created_at, pc.id
		LIMIT 1`, entryID).Scan(&pageCount)
	if errors.Is(err, sql.ErrNoRows) {
		return seed, nil
	}
	if err != nil {
		return seed, err
	}
	if !pageCount.Valid {
		return seed, nil
	}
	seed.PageCount = int(pageCount.Int64)

	// The other half is the canonical text's length, which lives behind
	// the entry's book and its parsed EPUB. Neither absence is an error:
	// a book with no EPUB attached has no text for pages to divide.
	var charCount sql.NullInt64
	err = s.db.QueryRowContext(ctx, `
		SELECT et.char_count
		FROM library_entries e
		JOIN media_files mf ON mf.book_id = e.book_id AND mf.kind = ? AND mf.missing_at IS NULL
		JOIN epub_texts et ON et.media_file_id = mf.id
		WHERE e.id = ?
		ORDER BY mf.id
		LIMIT 1`, models.MediaFileEpub, entryID).Scan(&charCount)
	if errors.Is(err, sql.ErrNoRows) {
		return seed, nil
	}
	if err != nil {
		return seed, err
	}
	if charCount.Valid {
		seed.CharCount = int(charCount.Int64)
	}
	return seed, nil
}

// physicalCopyForUser resolves a copy id within one user's entry,
// answering ErrNotFound for unknown, foreign and mismatched alike.
func (s *Store) physicalCopyForUser(ctx context.Context, userID, entryID, copyID string) (models.PhysicalCopy, error) {
	if _, err := s.BookIDForEntry(ctx, userID, entryID); err != nil {
		return models.PhysicalCopy{}, err
	}
	var c models.PhysicalCopy
	err := s.db.QueryRowContext(ctx, `
		SELECT id, user_id, entry_id, edition_id, notes, created_at
		FROM physical_copies
		WHERE id = ? AND user_id = ? AND entry_id = ?`, copyID, userID, entryID).
		Scan(&c.ID, &c.UserID, &c.EntryID, &c.EditionID, &c.Notes, &c.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return models.PhysicalCopy{}, ErrNotFound
	}
	if err != nil {
		return models.PhysicalCopy{}, err
	}
	return c, nil
}
