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

	// One attach batch of audio is one recording. Which edition it lands in
	// is decided before the loop so that every file of the batch carries the
	// same one, including a re-attach that only reorders an existing tape.
	var editionID any
	if kind == models.MediaFileAudio {
		id, err := audioEditionForBatchTx(ctx, tx, bookID, fileIDs)
		if err != nil {
			return nil, err
		}
		editionID = id
	}

	attached := make([]models.MediaFile, 0, len(fileIDs))
	for i, id := range fileIDs {
		f, err := scanMediaFileTx(ctx, tx,
			`SELECT `+mediaFileColumns+` FROM media_files WHERE id = ?`, id)
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
		// The attacher owns the attachment: it is what "my copy" means
		// once more than one account can reach the same book, and what
		// every share is a grant against.
		if _, err := tx.ExecContext(ctx,
			`UPDATE media_files
			 SET book_id = ?, track_number = ?, audio_edition_id = ?, attached_by = ?
			 WHERE id = ?`,
			bookID, track, editionID, userID, id); err != nil {
			return nil, err
		}
		f.BookID = &bookID
		f.AttachedBy = &userID
		attached = append(attached, f)
	}

	if kind == models.MediaFileAudio {
		// Files that moved out of an older edition can have emptied it. The
		// designation only moves if the emptied edition was holding it.
		if _, err := pruneEmptyAudioEditionsTx(ctx, tx, bookID); err != nil {
			return nil, err
		}
		if err := ensurePrimaryAudioEditionTx(ctx, tx, bookID); err != nil {
			return nil, err
		}
	}

	if kind == models.MediaFileEpub {
		if err := ensurePrimaryTextTx(ctx, tx, bookID); err != nil {
			return nil, err
		}
		for i := range attached {
			var primary bool
			if err := tx.QueryRowContext(ctx,
				`SELECT is_primary_text FROM media_files WHERE id = ?`,
				attached[i].ID).Scan(&primary); err != nil {
				return nil, err
			}
			attached[i].PrimaryText = primary
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return attached, nil
}

// audioEditionForBatchTx decides which recording an audio attach batch is.
//
// Attaching audio to a book that already has some is the whole point of this
// feature — a second narrator, an unabridged rip beside an abridgement — so a
// batch is a NEW edition by default. The one case that is not a new recording
// is a re-attach of the same files to fix their order: the attach flow is how
// track order is corrected, and re-sending a tape's files must renumber that
// tape rather than clone it.
//
// "The same files" is exact: every file already in this book's same edition,
// and the whole of it. A subset is deliberately treated as a new edition —
// re-sending three files of five and reusing the edition would number them
// 1..3 against siblings still holding 4 and 5, which is the interleaving this
// grouping exists to prevent.
func audioEditionForBatchTx(ctx context.Context, tx *sql.Tx, bookID string, fileIDs []int64) (int64, error) {
	if reused, err := existingAudioEditionTx(ctx, tx, bookID, fileIDs); err != nil || reused != 0 {
		return reused, err
	}
	res, err := tx.ExecContext(ctx,
		`INSERT INTO audio_editions (book_id) VALUES (?)`, bookID)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// existingAudioEditionTx returns the edition these exact files already form,
// or 0 when they do not form one.
func existingAudioEditionTx(ctx context.Context, tx *sql.Tx, bookID string, fileIDs []int64) (int64, error) {
	var edition int64
	for _, id := range fileIDs {
		var attachedBook sql.NullString
		var editionID sql.NullInt64
		err := tx.QueryRowContext(ctx,
			`SELECT book_id, audio_edition_id FROM media_files WHERE id = ?`, id).
			Scan(&attachedBook, &editionID)
		if errors.Is(err, sql.ErrNoRows) {
			return 0, nil // the attach loop reports the unknown id
		}
		if err != nil {
			return 0, err
		}
		if !attachedBook.Valid || attachedBook.String != bookID || !editionID.Valid {
			return 0, nil
		}
		if edition == 0 {
			edition = editionID.Int64
		} else if edition != editionID.Int64 {
			return 0, nil
		}
	}
	if edition == 0 {
		return 0, nil
	}
	var held int
	if err := tx.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM media_files WHERE audio_edition_id = ?`, edition).Scan(&held); err != nil {
		return 0, err
	}
	if held != len(fileIDs) {
		return 0, nil
	}
	return edition, nil
}

// ensurePrimaryTextTx guarantees a book with any text-side file attached has
// exactly one designated primary. A book that already has one keeps it — this
// is the reason attaching a second format is a safe, boring operation: the
// text every stored offset refers to does not move because a .mobi showed up
// beside the .epub. Only a book with no primary at all gets one, chosen by
// container fidelity (TextFormatRank) and then by id.
//
// Present files are preferred over missing ones so that a first attachment
// made while the NAS is half-mounted still designates something readable.
func ensurePrimaryTextTx(ctx context.Context, tx *sql.Tx, bookID string) error {
	var existing int
	err := tx.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM media_files
		WHERE book_id = ? AND kind = 'epub' AND is_primary_text = 1`, bookID).Scan(&existing)
	if err != nil {
		return err
	}
	if existing > 0 {
		return nil
	}
	_, err = tx.ExecContext(ctx, `
		UPDATE media_files SET is_primary_text = 1
		WHERE id = (SELECT mf.id FROM media_files mf
		            WHERE mf.book_id = ? AND mf.kind = 'epub'
		            ORDER BY (mf.missing_at IS NOT NULL), `+TextFormatRank+`, mf.id
		            LIMIT 1)`, bookID)
	return err
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

	// Read the file's coordinate system while the row still says it is the
	// book's canonical text: after the UPDATE below there is no way back to
	// "what were the stored offsets measured against".
	var before textIdentity
	var wasPrimary bool
	var kind sql.NullString
	if err := tx.QueryRowContext(ctx,
		`SELECT kind, is_primary_text FROM media_files WHERE id = ? AND book_id = ?`,
		fileID, bookID).Scan(&kind, &wasPrimary); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if wasPrimary {
		if before, err = textIdentityTx(ctx, tx, fileID); err != nil {
			return err
		}
	}

	// The tape as it stands, for the same reason: once the row is cleared
	// there is no way back to what a stored listening position was measured
	// against. Only the last file of the designated edition can move it.
	var beforeTape []audioTrack
	if kind.String == models.MediaFileAudio {
		if beforeTape, err = primaryAudioTracksTx(ctx, tx, bookID); err != nil {
			return err
		}
	}

	res, err := tx.ExecContext(ctx,
		`UPDATE media_files
		 SET book_id = NULL, track_number = NULL, is_primary_text = 0,
		     audio_edition_id = NULL, attached_by = NULL
		 WHERE id = ? AND book_id = ?`,
		fileID, bookID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	if kind.String == models.MediaFileAudio {
		if err := promoteAudioAfterDetachTx(ctx, tx, bookID, beforeTape); err != nil {
			return err
		}
	}
	// Detaching the primary leaves the book's remaining formats with no
	// canonical text at all, which would read as "no ebook" even though one
	// is still attached. Promote the best survivor, migrating the offsets
	// off the text that just left exactly as an explicit switch would.
	if err := promoteAfterDetachTx(ctx, tx, bookID, before); err != nil {
		return err
	}
	return tx.Commit()
}

// promoteAudioAfterDetachTx keeps the audio designation pointing at something
// playable after a detach.
//
// Detaching one track of a multi-file tape changes nothing here: the edition
// still exists, still holds the rest, and the position inside it is still
// meaningful. Only detaching the last file of the designated edition — which
// is the whole edition when it is a single .m4b — leaves the book with audio
// attached and nothing designated, and then another recording takes over.
// That is a different tape, so the position is carried across by proportion
// and the alignment goes, exactly as an explicit switch would do it.
func promoteAudioAfterDetachTx(ctx context.Context, tx *sql.Tx, bookID string, beforeTape []audioTrack) error {
	lostPrimary, err := pruneEmptyAudioEditionsTx(ctx, tx, bookID)
	if err != nil {
		return err
	}
	if err := ensurePrimaryAudioEditionTx(ctx, tx, bookID); err != nil {
		return err
	}
	if !lostPrimary {
		return nil
	}
	afterTape, err := primaryAudioTracksTx(ctx, tx, bookID)
	if err != nil {
		return err
	}
	if len(afterTape) == 0 {
		// No recording left to move onto. The stored position stays put for
		// whenever one is attached again, which is what detaching the last
		// text file does too.
		return nil
	}
	if err := remapAudioProgressTx(ctx, tx, bookID, beforeTape, afterTape); err != nil {
		return err
	}
	return dropAlignmentsTx(ctx, tx, bookID)
}

// promoteAfterDetachTx designates a new primary when the detached file was
// the old one. A book left with no text files is a no-op: nothing to
// promote, and the offsets stay put for whenever a file is attached again.
func promoteAfterDetachTx(ctx context.Context, tx *sql.Tx, bookID string, before textIdentity) error {
	if before.sha == "" {
		// The detached file was not the canonical text (or was never
		// parsed), so nothing that is stored refers to it.
		var existing int
		if err := tx.QueryRowContext(ctx, `
			SELECT COUNT(*) FROM media_files
			WHERE book_id = ? AND kind = 'epub' AND is_primary_text = 1`, bookID).Scan(&existing); err != nil {
			return err
		}
		if existing > 0 {
			return nil
		}
	}
	var newID int64
	err := tx.QueryRowContext(ctx, `
		SELECT mf.id FROM media_files mf
		WHERE mf.book_id = ? AND mf.kind = 'epub'
		ORDER BY (mf.missing_at IS NOT NULL), `+TextFormatRank+`, mf.id
		LIMIT 1`, bookID).Scan(&newID)
	if errors.Is(err, sql.ErrNoRows) {
		// No survivor. Text offsets stay put for whenever a file is
		// attached again, but a page index cannot: it points into a paged
		// file the book no longer has, and a future attach may bring a
		// text instead. The page axis is dropped the same honest way a
		// switch drops it.
		if before.paged {
			return resetProgressToTextTx(ctx, tx, bookID)
		}
		return nil
	}
	if err != nil {
		return err
	}
	return designatePrimaryTx(ctx, tx, bookID, newID, before)
}

// MediaFilesForEntry lists the files attached to a user's book entry — text
// files first, the book's designated canonical text at their head, then audio
// in track order.
//
// Gated on file access, not just on owning the entry: this list is where the
// reader learns a book has an audiobook and a text at all, and a book whose
// files were never shared with them should look exactly like a book with
// nothing attached.
func (s *Store) MediaFilesForEntry(ctx context.Context, userID, entryID string) ([]models.MediaFile, error) {
	bookID, err := s.BookFilesForEntry(ctx, userID, entryID)
	if err != nil {
		return nil, err
	}
	return s.MediaFilesForBook(ctx, bookID)
}

// MediaFilesForBook lists a book's attached files: text files first with the
// designated canonical text at their head, then audio in track order.
// Book-level, not user-level: the inventory is shared.
func (s *Store) MediaFilesForBook(ctx context.Context, bookID string) ([]models.MediaFile, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT `+mediaFileColumns+`
		FROM media_files
		WHERE book_id = ?
		ORDER BY kind DESC, is_primary_text DESC, track_number, path`, bookID)
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

// mediaFileColumns is the one column list every media_files row read shares,
// in the order scanMediaFile expects. track_number is deliberately absent:
// it is the audio timeline's ordering key, applied by the query, not a fact
// the model carries.
const mediaFileColumns = `id, root, path, kind, size_bytes, mtime, sha256,
	       duration_seconds, container_metadata, book_id, is_primary_text,
	       attached_by, scanned_at, missing_at`

// qualify prefixes every column in a list with a table alias, for the queries
// that join media_files against something else and would otherwise have to
// keep a second, hand-aliased copy of mediaFileColumns in sync with this one.
func qualify(columns, alias string) string {
	parts := strings.Split(columns, ",")
	for i, c := range parts {
		parts[i] = strings.Replace(c, strings.TrimSpace(c), alias+"."+strings.TrimSpace(c), 1)
	}
	return strings.Join(parts, ",")
}

// scanMediaFile reads a media_files row (without track_number) from a Rows.
func scanMediaFile(rows interface{ Scan(dest ...any) error }) (models.MediaFile, error) {
	var f models.MediaFile
	var sha256, metadata, attachedBook, attachedBy sql.NullString
	if err := rows.Scan(&f.ID, &f.Root, &f.Path, &f.Kind, &f.SizeBytes, &f.Mtime,
		&sha256, &f.DurationSeconds, &metadata, &attachedBook, &f.PrimaryText,
		&attachedBy, &f.ScannedAt, &f.MissingAt); err != nil {
		return f, err
	}
	if attachedBy.Valid {
		f.AttachedBy = &attachedBy.String
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
