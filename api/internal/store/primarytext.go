package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/collinpendleton/backhog/api/internal/models"
)

// ErrTextNotParsed marks a promotion whose target has no coordinate system
// yet: no canonical text was parsed and no paged classification exists.
// Switching onto a text nobody has read means switching onto a length nobody
// knows, and every stored offset would have to be migrated blind. Callers
// parse (or classify) first and retry; the handler does exactly that.
var ErrTextNotParsed = errors.New("text file has not been parsed yet")

// SetPrimaryTextFile designates which of a book's attached text-side files is
// its canonical text — the one the reader reads, the one offsets are measured
// against, and the one alignment aligns to.
//
// This is the deliberate half of owning two formats. Attaching a .mobi beside
// an .epub never moves the primary, precisely because moving it is not free:
// the two containers canonicalize to different byte counts, so every offset
// stored against the old text means a different place in the new one. What
// survives the switch is the *percentage*, which is the number the reader
// actually recognises as their position; the offset is recomputed from it.
//
// The exception is the common one, and it is exact. Both formats of a book
// converted from a single source (a Calibre library's .epub/.mobi pairs) can
// canonicalize byte-for-byte identically. When the two normalized hashes
// agree, the coordinates are literally the same and nothing is migrated at
// all — not the offsets, not the page anchors, not the alignment.
func (s *Store) SetPrimaryTextFile(ctx context.Context, userID, entryID string, fileID int64) (models.MediaFile, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return models.MediaFile{}, err
	}
	defer tx.Rollback()

	var bookID string
	err = tx.QueryRowContext(ctx, `
		SELECT book_id FROM library_entries
		WHERE id = ? AND user_id = ? AND media_type = 'book'`, entryID, userID).Scan(&bookID)
	if errors.Is(err, sql.ErrNoRows) {
		return models.MediaFile{}, ErrNotFound
	}
	if err != nil {
		return models.MediaFile{}, err
	}

	// Scoped through the book, like every other attach-side edit: a file id
	// that exists but hangs off someone else's book is a 404, not a hint.
	f, err := scanMediaFileTx(ctx, tx,
		`SELECT `+mediaFileColumns+`
		 FROM media_files WHERE id = ? AND book_id = ? AND kind = 'epub'`, fileID, bookID)
	if errors.Is(err, sql.ErrNoRows) {
		return models.MediaFile{}, ErrNotFound
	}
	if err != nil {
		return models.MediaFile{}, err
	}
	if f.MissingAt != nil {
		return models.MediaFile{}, fmt.Errorf("%w: file %d is currently missing from its root", ErrAttach, fileID)
	}
	if f.PrimaryText {
		return f, nil
	}

	var parsed, paged int
	if err := tx.QueryRowContext(ctx, `
		SELECT (SELECT COUNT(*) FROM epub_texts WHERE media_file_id = ?),
		       (SELECT COUNT(*) FROM pdf_files
		        WHERE media_file_id = ? AND classification = 'image-native')`,
		fileID, fileID).Scan(&parsed, &paged); err != nil {
		return models.MediaFile{}, err
	}
	if parsed == 0 && paged == 0 {
		return models.MediaFile{}, ErrTextNotParsed
	}

	if err := switchPrimaryTextTx(ctx, tx, bookID, fileID); err != nil {
		return models.MediaFile{}, err
	}
	if err := tx.Commit(); err != nil {
		return models.MediaFile{}, err
	}
	f.PrimaryText = true
	return f, nil
}

// textIdentity is what a canonical text is worth comparing by: the hash that
// says whether two parses produced the same bytes, and the length every
// offset in them is relative to. Both are zero when the file has never been
// parsed. `paged` says the coordinate system being described is a page axis
// (an image-native PDF primary): it has no hash by construction, and the
// flag is what tells the switch to drop the old axis rather than look for a
// hash to compare.
type textIdentity struct {
	sha   string
	chars int64
	paged bool
}

func textIdentityTx(ctx context.Context, tx *sql.Tx, fileID int64) (textIdentity, error) {
	var t textIdentity
	err := tx.QueryRowContext(ctx,
		`SELECT normalized_sha256, char_count FROM epub_texts WHERE media_file_id = ?`,
		fileID).Scan(&t.sha, &t.chars)
	if errors.Is(err, sql.ErrNoRows) {
		err = tx.QueryRowContext(ctx,
			`SELECT EXISTS(SELECT 1 FROM pdf_files
			 WHERE media_file_id = ? AND classification = 'image-native')`,
			fileID).Scan(&t.paged)
		if err != nil {
			return textIdentity{}, err
		}
		return t, nil
	}
	return t, err
}

// switchPrimaryTextTx moves the designation to newID, taking the text that
// was current as the coordinate system to migrate away from.
func switchPrimaryTextTx(ctx context.Context, tx *sql.Tx, bookID string, newID int64) error {
	var oldID sql.NullInt64
	if err := tx.QueryRowContext(ctx, `
		SELECT id FROM media_files
		WHERE book_id = ? AND kind = 'epub' AND is_primary_text = 1`, bookID).Scan(&oldID); err != nil &&
		!errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if oldID.Valid && oldID.Int64 == newID {
		return nil
	}
	var before textIdentity
	if oldID.Valid {
		var err error
		if before, err = textIdentityTx(ctx, tx, oldID.Int64); err != nil {
			return err
		}
	}
	return designatePrimaryTx(ctx, tx, bookID, newID, before)
}

// designatePrimaryTx makes newID the book's canonical text and migrates
// everything measured against `before`, the coordinate system being left
// behind. The caller supplies `before` rather than having it looked up here
// because the detach path has already cleared the old row by the time it
// gets to promote a replacement — the text is gone from the book, but the
// offsets it produced are still in book_progress.
//
// The flag is cleared before it is set: the partial unique index allows
// exactly one primary per book and SQLite checks it statement by statement,
// so the two updates cannot be reordered.
//
// A paged target (or a paged `before`) crosses an axis boundary, and axes
// are never translated across each other — a page index and a char offset
// are not two encodings of one position, they are positions in different
// books-shaped spaces. Both directions drop the old axis and start the new
// one unpositioned, mirroring the alignment-deletion stance: a plausible
// mistranslation is worse than an honest reset.
func designatePrimaryTx(ctx context.Context, tx *sql.Tx, bookID string, newID int64, before textIdentity) error {
	after, err := textIdentityTx(ctx, tx, newID)
	if err != nil {
		return err
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE media_files SET is_primary_text = 0
		WHERE book_id = ? AND kind = 'epub' AND is_primary_text = 1`, bookID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE media_files SET is_primary_text = 1 WHERE id = ?`, newID); err != nil {
		return err
	}

	if after.paged {
		// Switching onto a paged PDF: the char axis is dropped (not
		// rescaled — there is nothing to scale onto), the page axis starts
		// at page one, and the maps that lived in char space are deleted
		// rather than reinterpreted. A raw listening position survives:
		// it is track-relative and was never measured against the text.
		if err := resetProgressToPageTx(ctx, tx, bookID); err != nil {
			return err
		}
		if err := dropPageAnchorsTx(ctx, tx, bookID); err != nil {
			return err
		}
		return dropAlignmentsTx(ctx, tx, bookID)
	}
	if before.paged {
		// Leaving the page axis: no text coordinates were stored against
		// the paged primary (its sha is empty by construction), and the
		// page axis is dropped rather than guessed into an offset. The
		// text axis starts unpositioned — page one's equivalent, offset 0.
		return resetProgressToTextTx(ctx, tx, bookID)
	}

	// Nothing was measured against the old text (a first designation, or a
	// text nobody ever parsed), or both texts are byte-identical: the
	// coordinates carry over untouched.
	if before.sha == "" || before.sha == after.sha {
		return nil
	}
	return migrateOffsetsTx(ctx, tx, bookID, before, after)
}

// resetProgressToPageTx moves every entry of the book onto the page axis,
// unpositioned: page one, no percentage, no char offset. Only the position
// axes are touched — status, history and any raw listening position are
// facts about the reader, not about the coordinate system.
func resetProgressToPageTx(ctx context.Context, tx *sql.Tx, bookID string) error {
	_, err := tx.ExecContext(ctx, `
		UPDATE book_progress
		SET position_mode = 'page', page_index = 0,
		    char_offset = 0, char_offset_source = 'manual',
		    percent_complete = 0
		WHERE entry_id IN (
		      SELECT id FROM library_entries WHERE book_id = ? AND media_type = 'book')`,
		bookID)
	return err
}

// resetProgressToTextTx is the mirror of resetProgressToPageTx: the page
// axis is dropped and the char axis starts at offset zero.
func resetProgressToTextTx(ctx context.Context, tx *sql.Tx, bookID string) error {
	_, err := tx.ExecContext(ctx, `
		UPDATE book_progress
		SET position_mode = 'text', page_index = NULL,
		    char_offset = 0, percent_complete = 0
		WHERE entry_id IN (
		      SELECT id FROM library_entries WHERE book_id = ? AND media_type = 'book')`,
		bookID)
	return err
}

// dropPageAnchorsTx deletes the paper↔text maps of every physical copy of
// the book's entries. Page anchors are char offsets into a canonical text;
// switched onto a paged primary there is no such text, and an anchor kept
// would answer every translation query wrongly — the same reason alignments
// are deleted rather than stretched.
func dropPageAnchorsTx(ctx context.Context, tx *sql.Tx, bookID string) error {
	_, err := tx.ExecContext(ctx, `
		DELETE FROM page_anchors WHERE physical_copy_id IN (
		      SELECT pc.id FROM physical_copies pc
		      JOIN library_entries e ON e.id = pc.entry_id
		      WHERE e.book_id = ? AND e.media_type = 'book')`, bookID)
	return err
}

// migrateOffsetsTx rewrites the coordinates that were relative to `before` so
// they mean the same place in `after`, for every entry of the book.
//
// The reader's own position is recomputed from percent_complete, so the
// number on screen is preserved exactly and the offset lands wherever that
// percentage falls in the new text. Page anchors are scaled by the length
// ratio instead — they are a whole map, not one point, and scaling keeps it
// monotonic, which is the property the position translator interpolates over.
//
// Alignments are deleted rather than scaled, for the reason dropAlignmentsTx
// gives: a map built against one canonical text answers every query wrongly
// once stretched onto another.
//
// A target that has never been parsed (after.chars == 0) cannot be scaled
// onto. SetPrimaryTextFile refuses that case outright; the detach path can
// still reach it, and leaving the old coordinates in place is the least-wrong
// option available — they are a canonicalization of the same book, off by the
// difference between two parses rather than by an arbitrary amount.
func migrateOffsetsTx(ctx context.Context, tx *sql.Tx, bookID string, before, after textIdentity) error {
	const entriesOfBook = `SELECT id FROM library_entries WHERE book_id = ? AND media_type = 'book'`

	if after.chars > 0 {
		if _, err := tx.ExecContext(ctx, `
			UPDATE book_progress
			SET char_offset = MIN(?, CAST(ROUND(percent_complete / 100.0 * ?) AS INTEGER))
			WHERE char_offset > 0 AND entry_id IN (`+entriesOfBook+`)`,
			after.chars, after.chars, bookID); err != nil {
			return err
		}
	}
	if before.chars > 0 && after.chars > 0 {
		if _, err := tx.ExecContext(ctx, `
			UPDATE page_anchors
			SET char_offset = MIN(?, CAST(ROUND(char_offset * 1.0 * ? / ?) AS INTEGER))
			WHERE physical_copy_id IN (
			      SELECT id FROM physical_copies WHERE entry_id IN (`+entriesOfBook+`))`,
			after.chars, after.chars, before.chars, bookID); err != nil {
			return err
		}
	}

	return dropAlignmentsTx(ctx, tx, bookID)
}
