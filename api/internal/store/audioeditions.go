package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"strings"

	"github.com/collinpendleton/backhog/api/internal/models"
)

// An audio edition is one recording of a book — the set of files that behave
// as a single tape. Books routinely have more than one: the same title read
// by two narrators, an abridgement beside the unabridged rip, a re-issue with
// a full cast. Exactly one of them is primary, and that is the one the
// timeline is built from, the player plays, and an alignment is measured
// against.
//
// This file owns the choice: listing the editions a book has, moving the
// designation, and carrying the stored listening position across when it
// moves. The grouping itself is maintained by the attach flow (attach.go),
// which puts each batch of audio into one edition.

// AudioEditionsForBook lists a book's recordings, primary first. Facts about
// an edition — its label, length, track count — are derived from its files on
// read rather than stored, so a re-measured track or a directory renamed on
// the NAS needs no bookkeeping here.
func (s *Store) AudioEditionsForBook(ctx context.Context, bookID string) ([]models.AudioEdition, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, is_primary, created_at
		FROM audio_editions WHERE book_id = ?
		ORDER BY is_primary DESC, id`, bookID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	editions := []models.AudioEdition{}
	byID := map[int64]int{}
	for rows.Next() {
		e := models.AudioEdition{BookID: bookID}
		if err := rows.Scan(&e.ID, &e.Primary, &e.CreatedAt); err != nil {
			return nil, err
		}
		byID[e.ID] = len(editions)
		editions = append(editions, e)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(editions) == 0 {
		return editions, nil
	}

	files, err := s.db.QueryContext(ctx, `
		SELECT audio_edition_id, path, duration_seconds, container_metadata, missing_at
		FROM media_files
		WHERE book_id = ? AND kind = 'audio' AND audio_edition_id IS NOT NULL
		ORDER BY audio_edition_id, track_number, path`, bookID)
	if err != nil {
		return nil, err
	}
	defer files.Close()

	grouped := map[int64][]editionFile{}
	for files.Next() {
		var editionID int64
		var f editionFile
		var duration sql.NullFloat64
		var metadata sql.NullString
		var missingAt sql.NullTime
		if err := files.Scan(&editionID, &f.path, &duration, &metadata, &missingAt); err != nil {
			return nil, err
		}
		f.duration = duration.Float64
		f.measured = duration.Valid && duration.Float64 > 0
		f.metadata = []byte(metadata.String)
		f.missing = missingAt.Valid
		grouped[editionID] = append(grouped[editionID], f)
	}
	if err := files.Err(); err != nil {
		return nil, err
	}

	for id, i := range byID {
		summarizeEdition(&editions[i], grouped[id])
	}
	return editions, nil
}

// AudioEditionsForEntry lists the recordings attached to a user's book entry.
// Gated on file access rather than on owning the entry, exactly like the file
// list it is served beside: a book whose files were never shared with this
// reader should look like a book with nothing attached.
func (s *Store) AudioEditionsForEntry(ctx context.Context, userID, entryID string) ([]models.AudioEdition, error) {
	bookID, err := s.BookFilesForEntry(ctx, userID, entryID)
	if err != nil {
		return nil, err
	}
	return s.AudioEditionsForBook(ctx, bookID)
}

// editionFile is the per-file input to an edition's derived facts: enough to
// name it, measure it and say whether it can be played right now.
type editionFile struct {
	path     string
	duration float64
	measured bool
	metadata []byte
	missing  bool
}

// summarizeEdition fills in everything an edition derives from its files.
func summarizeEdition(e *models.AudioEdition, files []editionFile) {
	e.TrackCount = len(files)
	for _, f := range files {
		if f.measured {
			e.TotalDuration += f.duration
		} else {
			e.Degraded = true
		}
		if f.missing {
			e.MissingCount++
		}
	}
	e.Label = editionLabel(files)
	e.Narrator = editionNarrator(files)
}

// editionLabel names an edition the way its owner already named it on disk.
// A rip lives in a directory whose name is the whole story ("Anathem
// (Unabridged) [William Dufris]"), so a multi-file edition is called after
// the deepest directory all its files share — the parent, not "Disc 2", when
// a rip is split across platters. A lone .m4b has no directory of its own to
// speak for it (two recordings often sit side by side in one folder), so it
// is called after the file.
func editionLabel(files []editionFile) string {
	if len(files) == 0 {
		return "Empty audiobook"
	}
	if len(files) == 1 {
		base := path.Base(files[0].path)
		return strings.TrimSuffix(base, path.Ext(base))
	}
	shared := strings.Split(path.Dir(files[0].path), "/")
	for _, f := range files[1:] {
		segments := strings.Split(path.Dir(f.path), "/")
		if len(segments) < len(shared) {
			shared = shared[:len(segments)]
		}
		for i := range shared {
			if segments[i] != shared[i] {
				shared = shared[:i]
				break
			}
		}
	}
	if len(shared) == 0 || (len(shared) == 1 && shared[0] == ".") {
		base := path.Base(files[0].path)
		return strings.TrimSuffix(base, path.Ext(base))
	}
	return shared[len(shared)-1]
}

// editionNarrator reports the reader's name, and only when the tags are
// unambiguous about it.
//
// Audiobook containers have no narrator field. What they have is artist and
// album artist, and rippers fill them inconsistently: a file tagged with one
// of them is saying "the person responsible for this", which is the author as
// often as the reader. The one arrangement that does mean something is both
// present and different — album artist for the author, artist for whoever
// read it — which is what Audible-shaped rips produce. Anything else returns
// nothing rather than putting a guess under two editions that differ.
func editionNarrator(files []editionFile) string {
	for _, f := range files {
		if len(f.metadata) == 0 {
			continue
		}
		var tags struct {
			Artist      string `json:"artist"`
			AlbumArtist string `json:"album_artist"`
		}
		if err := json.Unmarshal(f.metadata, &tags); err != nil {
			continue
		}
		artist := strings.TrimSpace(tags.Artist)
		author := strings.TrimSpace(tags.AlbumArtist)
		if artist != "" && author != "" && !strings.EqualFold(artist, author) {
			return artist
		}
	}
	return ""
}

// SetPrimaryAudioEdition designates which of a book's recordings it is
// listened to — the tape the timeline is built from and the one every audio
// position means.
//
// The switch is deliberate for the same reason the text-side one is, and it
// costs the same two things. A listening position is (file, seconds into that
// file); the new edition contains neither that file nor, at a different
// pace, that second, so the position is carried across by *proportion* — the
// fraction of the way through the tape, which is the number the listener
// recognises. And any alignment is dropped: its anchors map char offsets onto
// the seconds of one specific recording, and a second narrator does not say
// the same words at the same times.
func (s *Store) SetPrimaryAudioEdition(ctx context.Context, userID, entryID string, editionID int64) (models.AudioEdition, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return models.AudioEdition{}, err
	}
	defer tx.Rollback()

	var bookID string
	err = tx.QueryRowContext(ctx, `
		SELECT book_id FROM library_entries
		WHERE id = ? AND user_id = ? AND media_type = 'book'`, entryID, userID).Scan(&bookID)
	if errors.Is(err, sql.ErrNoRows) {
		return models.AudioEdition{}, ErrNotFound
	}
	if err != nil {
		return models.AudioEdition{}, err
	}

	// Scoped through the book, like every other attach-side edit: an edition
	// id that exists but hangs off someone else's book is a 404, not a hint.
	var isPrimary bool
	err = tx.QueryRowContext(ctx,
		`SELECT is_primary FROM audio_editions WHERE id = ? AND book_id = ?`,
		editionID, bookID).Scan(&isPrimary)
	if errors.Is(err, sql.ErrNoRows) {
		return models.AudioEdition{}, ErrNotFound
	}
	if err != nil {
		return models.AudioEdition{}, err
	}

	if !isPrimary {
		after, err := audioEditionTracksTx(ctx, tx, editionID)
		if err != nil {
			return models.AudioEdition{}, err
		}
		playable := 0
		for _, t := range after {
			if !t.missing {
				playable++
			}
		}
		if playable == 0 {
			return models.AudioEdition{}, fmt.Errorf(
				"%w: every file in that audiobook is currently missing from its root", ErrAttach)
		}

		before, err := primaryAudioTracksTx(ctx, tx, bookID)
		if err != nil {
			return models.AudioEdition{}, err
		}
		if err := designatePrimaryAudioTx(ctx, tx, bookID, editionID); err != nil {
			return models.AudioEdition{}, err
		}
		if err := remapAudioProgressTx(ctx, tx, bookID, before, after); err != nil {
			return models.AudioEdition{}, err
		}
		if err := dropAlignmentsTx(ctx, tx, bookID); err != nil {
			return models.AudioEdition{}, err
		}
	}

	if err := tx.Commit(); err != nil {
		return models.AudioEdition{}, err
	}

	editions, err := s.AudioEditionsForBook(ctx, bookID)
	if err != nil {
		return models.AudioEdition{}, err
	}
	for _, e := range editions {
		if e.ID == editionID {
			return e, nil
		}
	}
	return models.AudioEdition{}, ErrNotFound
}

// audioTrack is one file's slot on a tape: the id a stored position names and
// the length that turns it into a place in the book. An unmeasured file
// contributes no time, exactly as it does on the player's timeline.
type audioTrack struct {
	id       int64
	duration float64
	missing  bool
}

// audioEditionTracksTx loads one edition's files in timeline order.
func audioEditionTracksTx(ctx context.Context, tx *sql.Tx, editionID int64) ([]audioTrack, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT id, duration_seconds, missing_at
		FROM media_files WHERE audio_edition_id = ?
		ORDER BY track_number, path`, editionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanAudioTracks(rows)
}

// primaryAudioTracksTx loads the book's current tape, or nothing when it has
// no designated edition (every edition detached, or a book with no audio).
func primaryAudioTracksTx(ctx context.Context, tx *sql.Tx, bookID string) ([]audioTrack, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT f.id, f.duration_seconds, f.missing_at
		FROM media_files f
		JOIN audio_editions e ON e.id = f.audio_edition_id
		WHERE e.book_id = ? AND e.is_primary = 1
		ORDER BY f.track_number, f.path`, bookID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanAudioTracks(rows)
}

func scanAudioTracks(rows *sql.Rows) ([]audioTrack, error) {
	var out []audioTrack
	for rows.Next() {
		var t audioTrack
		var duration sql.NullFloat64
		var missingAt sql.NullTime
		if err := rows.Scan(&t.id, &duration, &missingAt); err != nil {
			return nil, err
		}
		if duration.Valid && duration.Float64 > 0 {
			t.duration = duration.Float64
		}
		t.missing = missingAt.Valid
		out = append(out, t)
	}
	return out, rows.Err()
}

// designatePrimaryAudioTx moves the designation to editionID.
//
// The flag is cleared before it is set: the partial unique index allows
// exactly one primary per book and SQLite checks it statement by statement,
// so the two updates cannot be reordered.
func designatePrimaryAudioTx(ctx context.Context, tx *sql.Tx, bookID string, editionID int64) error {
	if _, err := tx.ExecContext(ctx,
		`UPDATE audio_editions SET is_primary = 0 WHERE book_id = ? AND is_primary = 1`,
		bookID); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx,
		`UPDATE audio_editions SET is_primary = 1 WHERE id = ?`, editionID)
	return err
}

// remapAudioProgressTx carries stored listening positions from one tape to
// another, for every entry of the book.
//
// A position is (file, seconds into that file), which is deliberately
// track-relative — a global offset moves on its own when a track is
// re-measured — but it makes the position meaningless the moment the file is
// not on the timeline any more. So it is converted to a global second on the
// tape being left, taken as a fraction of that tape's length, and placed at
// the same fraction of the new one. Two readings of the same book are not the
// same length, but they are the same book: two thirds of the way through is
// two thirds of the way through, and it is the number the listener would
// recognise if they looked.
//
// A tape nobody could measure (no durations at all, either side) has no
// fraction to work with. Those positions are cleared rather than guessed:
// percent_complete still stands, so the book keeps its progress and only the
// "resume here" point is gone.
func remapAudioProgressTx(ctx context.Context, tx *sql.Tx, bookID string, before, after []audioTrack) error {
	if len(before) == 0 {
		return nil
	}
	starts := make(map[int64]float64, len(before))
	beforeTotal := 0.0
	for _, t := range before {
		starts[t.id] = beforeTotal
		beforeTotal += t.duration
	}
	afterTotal := 0.0
	for _, t := range after {
		afterTotal += t.duration
	}

	rows, err := tx.QueryContext(ctx, `
		SELECT bp.entry_id, bp.raw_audio_file_id, bp.raw_audio_seconds
		FROM book_progress bp
		JOIN library_entries e ON e.id = bp.entry_id
		WHERE e.book_id = ? AND e.media_type = 'book' AND bp.raw_audio_file_id IS NOT NULL`,
		bookID)
	if err != nil {
		return err
	}
	type placement struct {
		entryID string
		fileID  *int64
		seconds *float64
	}
	var moves []placement
	for rows.Next() {
		var entryID string
		var fileID int64
		var seconds float64
		if err := rows.Scan(&entryID, &fileID, &seconds); err != nil {
			rows.Close()
			return err
		}
		start, onOldTape := starts[fileID]
		if !onOldTape {
			// A position on some other edition, or on a file detached long
			// ago: not this switch's business.
			continue
		}
		move := placement{entryID: entryID}
		if beforeTotal > 0 && afterTotal > 0 {
			fraction := min(1, (start+seconds)/beforeTotal)
			id, offset := locateOnTape(after, fraction*afterTotal)
			move.fileID, move.seconds = &id, &offset
		}
		moves = append(moves, move)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()

	for _, m := range moves {
		if _, err := tx.ExecContext(ctx, `
			UPDATE book_progress
			SET raw_audio_file_id = ?, raw_audio_seconds = ?
			WHERE entry_id = ?`, m.fileID, m.seconds, m.entryID); err != nil {
			return err
		}
	}
	return nil
}

// locateOnTape maps a global second onto the track that owns it, mirroring
// the player's own rule (a track owns [start, start+duration), and the very
// end of the book resolves to the end of the last track holding any time).
// It is only ever called with a tape that has some measured length.
func locateOnTape(tracks []audioTrack, global float64) (int64, float64) {
	offset := 0.0
	for _, t := range tracks {
		if t.duration > 0 && global < offset+t.duration {
			return t.id, global - offset
		}
		offset += t.duration
	}
	// The very end of the tape: the end of the last track owning any time.
	last := tracks[0]
	for _, t := range tracks {
		if t.duration > 0 {
			last = t
		}
	}
	return last.id, last.duration
}

// ensurePrimaryAudioEditionTx guarantees a book with any audio attached has
// exactly one designated edition. A book that already has one keeps it —
// which is the point: attaching a second recording records that you own it
// and changes nothing about what plays. Choosing between them is an explicit
// act, because it moves a position and drops an alignment.
//
// A book with no designation yet takes its oldest edition, preferring one
// that can actually be played over one whose files are all missing.
func ensurePrimaryAudioEditionTx(ctx context.Context, tx *sql.Tx, bookID string) error {
	var existing int
	if err := tx.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM audio_editions WHERE book_id = ? AND is_primary = 1`,
		bookID).Scan(&existing); err != nil {
		return err
	}
	if existing > 0 {
		return nil
	}
	_, err := tx.ExecContext(ctx, `
		UPDATE audio_editions SET is_primary = 1
		WHERE id = (
			SELECT e.id FROM audio_editions e
			WHERE e.book_id = ?
			ORDER BY (SELECT COUNT(*) FROM media_files f
			          WHERE f.audio_edition_id = e.id AND f.missing_at IS NULL) = 0, e.id
			LIMIT 1)`, bookID)
	return err
}

// pruneEmptyAudioEditionsTx deletes editions left with no files — a recording
// whose last file was detached, or one whose files all moved into a new
// edition. It reports whether the book's designated edition was among them,
// which is the caller's cue that what plays has to change.
func pruneEmptyAudioEditionsTx(ctx context.Context, tx *sql.Tx, bookID string) (bool, error) {
	var lostPrimary bool
	err := tx.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM audio_editions e
			WHERE e.book_id = ? AND e.is_primary = 1
			  AND NOT EXISTS (SELECT 1 FROM media_files f WHERE f.audio_edition_id = e.id))`,
		bookID).Scan(&lostPrimary)
	if err != nil {
		return false, err
	}
	_, err = tx.ExecContext(ctx, `
		DELETE FROM audio_editions
		WHERE book_id = ?
		  AND NOT EXISTS (SELECT 1 FROM media_files f WHERE f.audio_edition_id = audio_editions.id)`,
		bookID)
	return lostPrimary, err
}

// dropAlignmentsTx clears a book's text↔audio maps and any queued job for
// them. An alignment is a dense map between one canonical text and one
// recording; when either side is replaced it would still answer every query,
// just wrongly, and a plausible wrong answer is worse than none. The worker
// re-derives it, which is what it is for.
func dropAlignmentsTx(ctx context.Context, tx *sql.Tx, bookID string) error {
	const entriesOfBook = `SELECT id FROM library_entries WHERE book_id = ? AND media_type = 'book'`
	for _, table := range []string{"alignments", "alignment_jobs"} {
		if _, err := tx.ExecContext(ctx,
			`DELETE FROM `+table+` WHERE entry_id IN (`+entriesOfBook+`)`, bookID); err != nil {
			return err
		}
	}
	return nil
}
