package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/collinpendleton/backhog/api/internal/models"
)

// FinishOfferPercent is how far through a book counts as "you're basically
// done". Crossing it *offers* the finish rather than taking it: the last few
// percent of an ebook are acknowledgements, endnotes and the author's other
// titles, so a hard auto-finish would mark books read that the reader is
// still in the middle of the epilogue of.
const FinishOfferPercent = 97.0

// maxReadingSessionSeconds caps one logged session at 24 hours, matching the
// database CHECK. A longer one is a client that forgot to stop its timer.
const maxReadingSessionSeconds = 86400

// ProgressWrite is a position to store. Exactly one of the shapes is
// meaningful at a time: a canonical CharOffset (the normal case, and the only
// one that survives a re-alignment), the RawAudio pair for a listening
// position on a book that has no alignment yet, or a PageIndex for a paged
// book — one whose designated text file is an image-native PDF and whose
// position therefore lives on the page axis.
type ProgressWrite struct {
	Mode            string
	CharOffset      int
	Source          string
	PageIndex       *int
	RawAudioSeconds *float64
	RawAudioFileID  *int64
	PercentComplete float64
}

// ProgressResult is a stored position plus what the write did to the entry
// around it.
type ProgressResult struct {
	Progress models.BookProgress
	// Status is the entry's status after the write, and StatusChanged reports
	// whether this write is what moved it.
	Status        string
	StatusChanged bool
	// OfferFinished reports that the position crossed FinishOfferPercent, so
	// the client can ask whether the book is done. Nothing is auto-finished.
	OfferFinished bool
}

// BookProgress returns a book entry's stored position. An entry that has
// never been opened has no row yet and comes back as a zero position rather
// than ErrNotFound — "page one" is the right answer for a book you own and
// have not started, and it keeps the read path from having to special-case
// the first write.
//
// ErrNotFound means the entry does not exist, is not a book, or is not the
// caller's; the three are deliberately indistinguishable.
func (s *Store) BookProgress(ctx context.Context, userID, entryID string) (models.BookProgress, error) {
	if _, err := s.BookIDForEntry(ctx, userID, entryID); err != nil {
		return models.BookProgress{}, err
	}

	p := models.BookProgress{
		EntryID:          entryID,
		PositionMode:     models.PositionModeText,
		CharOffsetSource: models.PositionSourceManual,
	}
	if err := scanProgress(s.db.QueryRowContext(ctx, `
		SELECT char_offset, char_offset_source, position_mode, page_index,
		       raw_audio_seconds, raw_audio_file_id,
		       percent_complete, updated_at
		FROM book_progress WHERE entry_id = ?`, entryID), &p); errors.Is(err, sql.ErrNoRows) {
		return p, nil
	} else if err != nil {
		return models.BookProgress{}, err
	}
	return p, nil
}

// scanProgress reads one book_progress row, including the page axis. The
// store enforces the pairing the schema only implies: a page row carries a
// page index, a text row carries none.
func scanProgress(row interface{ Scan(...any) error }, p *models.BookProgress) error {
	var rawSeconds sql.NullFloat64
	var rawFileID sql.NullInt64
	var pageIndex sql.NullInt64
	if err := row.Scan(&p.CharOffset, &p.CharOffsetSource, &p.PositionMode, &pageIndex,
		&rawSeconds, &rawFileID, &p.PercentComplete, &p.UpdatedAt); err != nil {
		return err
	}
	if pageIndex.Valid {
		i := int(pageIndex.Int64)
		p.PageIndex = &i
	}
	if rawSeconds.Valid {
		p.RawAudioSeconds = &rawSeconds.Float64
	}
	if rawFileID.Valid {
		p.RawAudioFileID = &rawFileID.Int64
	}
	return nil
}

// SaveBookProgress writes a position and advances the entry's status the same
// way logging playtime does: something you have started reading is no longer
// sitting in the backlog, and it leaves the queue so the queue stops
// suggesting it. The transition goes through recordStatusChangeTx like every
// other one, so the history table stays the single record of when a book was
// picked up.
//
// Books reuse the games status vocabulary — 'playing' covers reading —
// rather than adding a parallel 'reading' value. One in-progress status means
// every shared query (the queue, stats, debt, smart lists) keeps working on
// books without learning a second name for the same state; the UI is free to
// label it "Reading" for a book.
func (s *Store) SaveBookProgress(ctx context.Context, userID, entryID string, w ProgressWrite) (ProgressResult, error) {
	if !models.ValidPositionSource(w.Source) {
		return ProgressResult{}, fmt.Errorf("unknown position source %q", w.Source)
	}
	if w.Mode == "" {
		w.Mode = models.PositionModeText
	}
	if !models.ValidPositionMode(w.Mode) {
		return ProgressResult{}, fmt.Errorf("unknown position mode %q", w.Mode)
	}
	if w.Mode == models.PositionModePage {
		// The page axis is exclusive of the char axis: a paged book has no
		// text to offset into, so the char offset stays pinned at zero
		// rather than collecting a number nothing can interpret.
		if w.PageIndex == nil || *w.PageIndex < 0 {
			return ProgressResult{}, errors.New("a page position needs a non-negative page_index")
		}
		w.CharOffset = 0
	} else if w.PageIndex != nil {
		return ProgressResult{}, errors.New("a text position cannot carry a page_index")
	}
	if w.CharOffset < 0 {
		return ProgressResult{}, errors.New("char_offset must not be negative")
	}
	if (w.RawAudioSeconds == nil) != (w.RawAudioFileID == nil) {
		return ProgressResult{}, errors.New("a raw audio position needs both a timestamp and a file id")
	}
	if w.RawAudioSeconds != nil && *w.RawAudioSeconds < 0 {
		return ProgressResult{}, errors.New("audio_seconds must not be negative")
	}
	w.PercentComplete = min(100, max(0, w.PercentComplete))

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ProgressResult{}, err
	}
	defer tx.Rollback()

	var status string
	err = tx.QueryRowContext(ctx, `
		SELECT status FROM library_entries
		WHERE id = ? AND user_id = ? AND media_type = 'book'`, entryID, userID).Scan(&status)
	if errors.Is(err, sql.ErrNoRows) {
		return ProgressResult{}, ErrNotFound
	}
	if err != nil {
		return ProgressResult{}, err
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO book_progress (entry_id, position_mode, page_index,
		                           char_offset, char_offset_source,
		                           raw_audio_seconds, raw_audio_file_id,
		                           percent_complete, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(entry_id) DO UPDATE SET
			position_mode      = excluded.position_mode,
			page_index         = excluded.page_index,
			char_offset        = excluded.char_offset,
			char_offset_source = excluded.char_offset_source,
			raw_audio_seconds  = excluded.raw_audio_seconds,
			raw_audio_file_id  = excluded.raw_audio_file_id,
			percent_complete   = excluded.percent_complete,
			updated_at         = CURRENT_TIMESTAMP`,
		entryID, w.Mode, w.PageIndex, w.CharOffset, w.Source, w.RawAudioSeconds,
		w.RawAudioFileID, w.PercentComplete); err != nil {
		return ProgressResult{}, err
	}

	result := ProgressResult{
		Status:        status,
		OfferFinished: w.PercentComplete >= FinishOfferPercent && status != models.StatusPlayed,
	}
	if status == models.StatusBacklog || status == models.StatusWishlist {
		if err := startReadingTx(ctx, tx, userID, entryID, status); err != nil {
			return ProgressResult{}, err
		}
		result.Status = models.StatusPlaying
		result.StatusChanged = true
	}

	p := models.BookProgress{EntryID: entryID, PositionMode: models.PositionModeText}
	if err := scanProgress(tx.QueryRowContext(ctx, `
		SELECT char_offset, char_offset_source, position_mode, page_index,
		       raw_audio_seconds, raw_audio_file_id,
		       percent_complete, updated_at
		FROM book_progress WHERE entry_id = ?`, entryID), &p); err != nil {
		return ProgressResult{}, err
	}
	result.Progress = p

	if err := tx.Commit(); err != nil {
		return ProgressResult{}, err
	}
	return result, nil
}

// startReadingTx moves an entry out of the backlog and out of the queue, the
// same way AddSession does for a game. It is a separate helper only because
// both the position write and the session write need it.
func startReadingTx(ctx context.Context, tx *sql.Tx, userID, entryID, fromStatus string) error {
	if err := recordStatusChangeTx(ctx, tx, userID, entryID, fromStatus, models.StatusPlaying); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `
		UPDATE library_entries
		SET status = 'playing',
		    queue_position = NULL,
		    started_at = COALESCE(started_at, CURRENT_TIMESTAMP),
		    updated_at = CURRENT_TIMESTAMP
		WHERE user_id = ? AND id = ?`, userID, entryID)
	return err
}

// AddReadingSession records a stretch of reading or listening against a book
// entry, and starts the book if it was still in the backlog — the same rule
// play sessions follow, for the same reason.
func (s *Store) AddReadingSession(ctx context.Context, userID, entryID string, rs models.ReadingSession) (models.ReadingSession, error) {
	if !models.ValidReadingMode(rs.Mode) {
		return models.ReadingSession{}, fmt.Errorf("mode must be %q or %q",
			models.ReadingModeRead, models.ReadingModeListen)
	}
	if rs.StartedAt.IsZero() || rs.EndedAt.IsZero() {
		return models.ReadingSession{}, errors.New("started_at and ended_at are required")
	}
	if rs.EndedAt.Before(rs.StartedAt) {
		return models.ReadingSession{}, errors.New("ended_at must not precede started_at")
	}
	if rs.Seconds <= 0 {
		// A client that reports its endpoints but not a duration means the
		// wall-clock span between them.
		rs.Seconds = int(rs.EndedAt.Sub(rs.StartedAt).Seconds())
	}
	if rs.Seconds < 0 || rs.Seconds > maxReadingSessionSeconds {
		return models.ReadingSession{}, fmt.Errorf("seconds must be between 0 and %d", maxReadingSessionSeconds)
	}
	if rs.CharsAdvanced < 0 {
		// Re-reading is not negative progress.
		rs.CharsAdvanced = 0
	}
	if rs.PagesTurned < 0 {
		// Flipping back is not negative progress either.
		rs.PagesTurned = 0
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return models.ReadingSession{}, err
	}
	defer tx.Rollback()

	var status string
	err = tx.QueryRowContext(ctx, `
		SELECT status FROM library_entries
		WHERE id = ? AND user_id = ? AND media_type = 'book'`, entryID, userID).Scan(&status)
	if errors.Is(err, sql.ErrNoRows) {
		return models.ReadingSession{}, ErrNotFound
	}
	if err != nil {
		return models.ReadingSession{}, err
	}

	rs.ID = newID()
	rs.EntryID = entryID
	rs.StartedAt = rs.StartedAt.UTC()
	rs.EndedAt = rs.EndedAt.UTC()
	if err := tx.QueryRowContext(ctx, `
		INSERT INTO reading_sessions (id, user_id, entry_id, started_at, ended_at,
		                              mode, chars_advanced, pages_turned, seconds)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?) RETURNING created_at`,
		rs.ID, userID, entryID, rs.StartedAt, rs.EndedAt, rs.Mode,
		rs.CharsAdvanced, rs.PagesTurned, rs.Seconds).Scan(&rs.CreatedAt); err != nil {
		return models.ReadingSession{}, err
	}

	if status == models.StatusBacklog || status == models.StatusWishlist {
		if err := startReadingTx(ctx, tx, userID, entryID, status); err != nil {
			return models.ReadingSession{}, err
		}
	}

	if err := tx.Commit(); err != nil {
		return models.ReadingSession{}, err
	}
	return rs, nil
}

// ReadingSessions returns a book entry's logged sessions, newest first.
func (s *Store) ReadingSessions(ctx context.Context, userID, entryID string) ([]models.ReadingSession, error) {
	if _, err := s.BookIDForEntry(ctx, userID, entryID); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, entry_id, started_at, ended_at, mode, chars_advanced, pages_turned,
		       seconds, created_at
		FROM reading_sessions
		WHERE user_id = ? AND entry_id = ?
		ORDER BY started_at DESC, created_at DESC`, userID, entryID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []models.ReadingSession{}
	for rows.Next() {
		var rs models.ReadingSession
		if err := rows.Scan(&rs.ID, &rs.EntryID, &rs.StartedAt, &rs.EndedAt,
			&rs.Mode, &rs.CharsAdvanced, &rs.PagesTurned, &rs.Seconds, &rs.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, rs)
	}
	return out, rows.Err()
}

// ReadingTime totals a book entry's logged seconds by mode — the reading
// dashboard's "hours owed" input. Modes with nothing logged are absent.
func (s *Store) ReadingTime(ctx context.Context, userID, entryID string) (map[string]int, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT mode, COALESCE(SUM(seconds), 0) FROM reading_sessions
		WHERE user_id = ? AND entry_id = ? GROUP BY mode`, userID, entryID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	totals := map[string]int{}
	for rows.Next() {
		var mode string
		var seconds int
		if err := rows.Scan(&mode, &seconds); err != nil {
			return nil, err
		}
		totals[mode] = seconds
	}
	return totals, rows.Err()
}
