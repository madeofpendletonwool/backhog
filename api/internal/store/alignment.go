package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/collinpendleton/backhog/api/internal/models"
)

// Alignment queue tuning. Attempts are capped so a genuinely broken
// file fails loudly instead of looping between workers forever, and a
// claimed job whose heartbeat goes silent is reclaimed so one crashed
// worker cannot wedge the queue. The stale window is generous on
// purpose: transcribing an 11-hour book takes hours, but the worker
// heartbeats through it — silence means the worker is gone, not busy.
const (
	AlignmentMaxAttempts = 3
	AlignmentStaleAfter  = 10 * time.Minute
)

var (
	// ErrNoAlignmentAudio reports a book with no audiobook attached, so
	// there is nothing to align the text against.
	ErrNoAlignmentAudio = errors.New("no audiobook attached to this book")
	// ErrNoAlignmentText reports a book with no parsed canonical text,
	// which is the half of the alignment that must already exist.
	ErrNoAlignmentText = errors.New("no ebook text parsed for this book")
	// ErrJobNotClaimedBy reports a worker touching a job another worker
	// holds — the hand-off mess a stale reclaim can create, refused
	// rather than interleaved.
	ErrJobNotClaimedBy = errors.New("job is claimed by another worker")
	// ErrJobTerminal reports a write to a job that already finished.
	ErrJobTerminal = errors.New("job already finished")
)

const alignmentJobColumns = `id, entry_id, epub_text_id, audio_timeline_hash, state,
	progress, stage_detail, error, attempts, claimed_by, claimed_at,
	heartbeat_at, created_at, updated_at`

const activeJobStates = `'queued','claimed','transcribing','aligning'`

// EnqueueAlignment queues an alignment for one of the caller's book
// entries. The second return is true when an active job already existed
// and was returned as-is: enqueueing is idempotent while a job is in
// flight, so a user hammering the button cannot stack duplicates.
func (s *Store) EnqueueAlignment(ctx context.Context, userID, entryID string) (models.AlignmentJob, bool, error) {
	if _, err := s.BookIDForEntry(ctx, userID, entryID); err != nil {
		return models.AlignmentJob{}, false, err
	}
	text, audio, err := s.alignmentInputs(ctx, entryID)
	if err != nil {
		return models.AlignmentJob{}, false, err
	}

	if job, err := s.activeAlignmentJob(ctx, entryID); err != nil || job.ID != "" {
		return job, true, err
	}

	job := models.AlignmentJob{
		ID:                newID(),
		EntryID:           entryID,
		EpubTextID:        text.ID,
		AudioTimelineHash: audioTimelineHash(audio),
	}
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO alignment_jobs (id, entry_id, epub_text_id, audio_timeline_hash)
		VALUES (?, ?, ?, ?)`, job.ID, job.EntryID, job.EpubTextID, job.AudioTimelineHash)
	if err != nil {
		// The one-active-job index fires only when a concurrent enqueue
		// won the race; hand back the winner's job.
		if existing, lookupErr := s.activeAlignmentJob(ctx, entryID); lookupErr == nil && existing.ID != "" {
			return existing, true, nil
		}
		return models.AlignmentJob{}, false, err
	}
	out, err := s.AlignmentJob(ctx, job.ID)
	return out, false, err
}

// alignmentInputs resolves the two halves an alignment joins: the parsed
// canonical text and the attached audio in timeline order.
func (s *Store) alignmentInputs(ctx context.Context, entryID string) (models.EpubText, []models.MediaFile, error) {
	bookID, err := s.BookIDForEntryAny(ctx, entryID)
	if err != nil {
		return models.EpubText{}, nil, err
	}
	epubFile, err := s.EpubMediaFileForBook(ctx, bookID)
	if err != nil {
		return models.EpubText{}, nil, ErrNoAlignmentText
	}
	text, err := s.GetEpubText(ctx, epubFile.ID)
	if err != nil {
		return models.EpubText{}, nil, ErrNoAlignmentText
	}
	audio, err := s.AudioMediaFilesForBook(ctx, bookID)
	if err != nil {
		return models.EpubText{}, nil, err
	}
	if len(audio) == 0 {
		return models.EpubText{}, nil, ErrNoAlignmentAudio
	}
	return text, audio, nil
}

// audioTimelineHash fingerprints the ordered audiobook a job was
// enqueued against: the (id, path, duration) sequence in track order.
// A re-attach or re-measure changes it, which is what makes a stored
// alignment auditable against the tape it was made from.
func audioTimelineHash(files []models.MediaFile) string {
	h := sha256.New()
	for _, f := range files {
		duration := "-"
		if f.DurationSeconds != nil {
			duration = fmt.Sprintf("%.3f", *f.DurationSeconds)
		}
		fmt.Fprintf(h, "%d|%s|%s\n", f.ID, f.Path, duration)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// activeAlignmentJob returns the entry's in-flight job, or nil.
func (s *Store) activeAlignmentJob(ctx context.Context, entryID string) (models.AlignmentJob, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT `+alignmentJobColumns+`
		FROM alignment_jobs
		WHERE entry_id = ? AND state IN (`+activeJobStates+`)
		ORDER BY created_at, id LIMIT 1`, entryID)
	job, err := scanAlignmentJob(row)
	if errors.Is(err, sql.ErrNoRows) {
		return models.AlignmentJob{}, nil
	}
	return job, err
}

// AlignmentJob loads one job by id.
func (s *Store) AlignmentJob(ctx context.Context, jobID string) (models.AlignmentJob, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT `+alignmentJobColumns+` FROM alignment_jobs WHERE id = ?`, jobID)
	job, err := scanAlignmentJob(row)
	if errors.Is(err, sql.ErrNoRows) {
		return models.AlignmentJob{}, ErrNotFound
	}
	return job, err
}

// AlignmentJobForEntry returns the entry's most recent job of any
// state, or nil when the entry has never been aligned. User-scoped:
// someone else's entry is indistinguishable from an unknown one.
func (s *Store) AlignmentJobForEntry(ctx context.Context, userID, entryID string) (models.AlignmentJob, error) {
	if _, err := s.BookIDForEntry(ctx, userID, entryID); err != nil {
		return models.AlignmentJob{}, err
	}
	row := s.db.QueryRowContext(ctx, `
		SELECT `+alignmentJobColumns+`
		FROM alignment_jobs WHERE entry_id = ?
		ORDER BY created_at DESC, id DESC LIMIT 1`, entryID)
	job, err := scanAlignmentJob(row)
	if errors.Is(err, sql.ErrNoRows) {
		return models.AlignmentJob{}, nil
	}
	return job, err
}

// ClaimAlignmentJob atomically claims the oldest queued job for a
// worker. Stale claims are reclaimed first (see reclaimStaleJobs), so a
// crashed worker's job comes back around instead of wedging the queue.
// The claim, its attempt bump and the reclaim all run in one
// transaction: two concurrent claims can never hand out the same job.
// ErrNotFound means the queue is empty.
func (s *Store) ClaimAlignmentJob(ctx context.Context, workerID string) (models.AlignmentJob, error) {
	if strings.TrimSpace(workerID) == "" {
		return models.AlignmentJob{}, errors.New("worker id must not be empty")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return models.AlignmentJob{}, err
	}
	defer tx.Rollback()

	if _, err := reclaimStaleJobs(ctx, tx); err != nil {
		return models.AlignmentJob{}, err
	}

	var jobID string
	err = tx.QueryRowContext(ctx, `
		SELECT id FROM alignment_jobs WHERE state = 'queued'
		ORDER BY created_at, id LIMIT 1`).Scan(&jobID)
	if errors.Is(err, sql.ErrNoRows) {
		return models.AlignmentJob{}, ErrNotFound
	}
	if err != nil {
		return models.AlignmentJob{}, err
	}

	// A re-claim starts the job over, so whatever the previous attempt
	// streamed has to go with it: a worker restarted mid-book
	// re-transcribes from the beginning, and keeping the old partial
	// output would interleave two runs into one duplicated transcript.
	// The unfinished alignment row is the handle for all of it — its
	// anchors and segments cascade. A finalized alignment is never
	// touched, so a previous good result survives a fresh attempt.
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM alignments WHERE id = ? AND state = 'aligning'`, jobID); err != nil {
		return models.AlignmentJob{}, err
	}

	now := time.Now().UTC()
	if _, err := tx.ExecContext(ctx, `
		UPDATE alignment_jobs SET
			state = 'claimed', claimed_by = ?, claimed_at = ?,
			heartbeat_at = ?, attempts = attempts + 1,
			stage_detail = '', error = NULL, updated_at = ?
		WHERE id = ?`, workerID, now, now, now, jobID); err != nil {
		return models.AlignmentJob{}, err
	}

	job, err := alignmentJobTx(ctx, tx, jobID)
	if err != nil {
		return models.AlignmentJob{}, err
	}
	return job, tx.Commit()
}

// AlignmentJobProgress records a worker heartbeat: optionally a new
// pipeline state, progress and stage detail, always a fresh
// heartbeat_at. Only the worker holding the claim may write, and never
// a terminal job.
func (s *Store) AlignmentJobProgress(ctx context.Context, jobID, workerID, state string, progress *float64, stageDetail *string) (models.AlignmentJob, error) {
	if state != "" && !models.AlignmentJobActive(state) {
		return models.AlignmentJob{}, fmt.Errorf("state %q is not a worker pipeline state", state)
	}
	if progress != nil && (*progress < 0 || *progress > 1) {
		return models.AlignmentJob{}, errors.New("progress must be within [0,1]")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return models.AlignmentJob{}, err
	}
	defer tx.Rollback()

	job, err := requireClaimedJobTx(ctx, tx, jobID, workerID)
	if err != nil {
		return models.AlignmentJob{}, err
	}

	sets := []string{"heartbeat_at = ?", "updated_at = ?"}
	args := []any{time.Now().UTC(), time.Now().UTC()}
	if state != "" {
		sets = append(sets, "state = ?")
		args = append(args, state)
	}
	if progress != nil {
		sets = append(sets, "progress = ?")
		args = append(args, *progress)
	}
	if stageDetail != nil {
		sets = append(sets, "stage_detail = ?")
		args = append(args, *stageDetail)
	}
	args = append(args, jobID)
	if _, err := tx.ExecContext(ctx,
		`UPDATE alignment_jobs SET `+strings.Join(sets, ", ")+` WHERE id = ?`, args...); err != nil {
		return models.AlignmentJob{}, err
	}

	job, err = alignmentJobTx(ctx, tx, jobID)
	if err != nil {
		return models.AlignmentJob{}, err
	}
	return job, tx.Commit()
}

// AppendTranscriptSegments stores one streamed batch of transcription
// output against the job's alignment. Batches may arrive in any size,
// including empty (a no-op that still refreshes the heartbeat).
func (s *Store) AppendTranscriptSegments(ctx context.Context, jobID, workerID string, segments []models.TranscriptSegment) (models.AlignmentJob, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return models.AlignmentJob{}, err
	}
	defer tx.Rollback()

	job, err := requireClaimedJobTx(ctx, tx, jobID, workerID)
	if err != nil {
		return models.AlignmentJob{}, err
	}
	if err := ensureAlignmentTx(ctx, tx, job); err != nil {
		return models.AlignmentJob{}, err
	}

	const batchSize = 128
	for start := 0; start < len(segments); start += batchSize {
		end := min(start+batchSize, len(segments))
		var b strings.Builder
		args := make([]any, 0, (end-start)*4+1)
		b.WriteString(`INSERT INTO transcript_segments (alignment_id, audio_start, audio_end, text) VALUES `)
		for i, seg := range segments[start:end] {
			if i > 0 {
				b.WriteByte(',')
			}
			b.WriteString("(?,?,?,?)")
			args = append(args, job.ID, seg.AudioStart, seg.AudioEnd, seg.Text)
		}
		if _, err := tx.ExecContext(ctx, b.String(), args...); err != nil {
			return models.AlignmentJob{}, fmt.Errorf("insert segments: %w", err)
		}
	}

	out, err := s.touchHeartbeatTx(ctx, tx, jobID)
	if err != nil {
		return models.AlignmentJob{}, err
	}
	return out, tx.Commit()
}

// AppendAlignmentAnchors stores one streamed batch of anchors, written
// in a single transaction so a batch is all-or-nothing. Re-sent batches
// (a worker retrying after a reconnect) are idempotent: an anchor that
// already exists is left as-is rather than failing the batch.
func (s *Store) AppendAlignmentAnchors(ctx context.Context, jobID, workerID string, anchors []models.AlignmentAnchor) (models.AlignmentJob, error) {
	for _, a := range anchors {
		if a.CharOffset < 0 || a.AudioSeconds < 0 {
			return models.AlignmentJob{}, errors.New("anchor offsets must not be negative")
		}
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return models.AlignmentJob{}, err
	}
	defer tx.Rollback()

	job, err := requireClaimedJobTx(ctx, tx, jobID, workerID)
	if err != nil {
		return models.AlignmentJob{}, err
	}
	if err := ensureAlignmentTx(ctx, tx, job); err != nil {
		return models.AlignmentJob{}, err
	}

	const batchSize = 128
	for start := 0; start < len(anchors); start += batchSize {
		end := min(start+batchSize, len(anchors))
		var b strings.Builder
		args := make([]any, 0, (end-start)*4+1)
		b.WriteString(`INSERT OR IGNORE INTO alignment_anchors
			(alignment_id, char_offset, audio_seconds, confidence) VALUES `)
		for i, a := range anchors[start:end] {
			if i > 0 {
				b.WriteByte(',')
			}
			b.WriteString("(?,?,?,?)")
			args = append(args, job.ID, a.CharOffset, a.AudioSeconds,
				min(1, max(0, a.Confidence)))
		}
		if _, err := tx.ExecContext(ctx, b.String(), args...); err != nil {
			return models.AlignmentJob{}, fmt.Errorf("insert anchors: %w", err)
		}
	}

	out, err := s.touchHeartbeatTx(ctx, tx, jobID)
	if err != nil {
		return models.AlignmentJob{}, err
	}
	return out, tx.Commit()
}

// CompleteAlignment finalizes a job: it writes the alignment's terminal
// state, coverage, mean confidence and model, closes the job, and — for
// a usable result — supersedes the entry's older alignments so exactly
// one alignment ever feeds the translator. A failed completion leaves
// any previous good alignment in place.
func (s *Store) CompleteAlignment(ctx context.Context, jobID, workerID, state string, coverage, meanConfidence float64, model, errMsg string) (models.AlignmentJob, models.Alignment, error) {
	if state != models.AlignmentReady && state != models.AlignmentLowConfidence && state != models.AlignmentFailed {
		return models.AlignmentJob{}, models.Alignment{}, fmt.Errorf("state %q is not a terminal alignment state", state)
	}
	if coverage < 0 || coverage > 1 || meanConfidence < 0 || meanConfidence > 1 {
		return models.AlignmentJob{}, models.Alignment{}, errors.New("coverage and mean_confidence must be within [0,1]")
	}
	if state != models.AlignmentFailed && strings.TrimSpace(model) == "" {
		return models.AlignmentJob{}, models.Alignment{}, errors.New("model is required for a usable alignment")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return models.AlignmentJob{}, models.Alignment{}, err
	}
	defer tx.Rollback()

	job, err := requireClaimedJobTx(ctx, tx, jobID, workerID)
	if err != nil {
		return models.AlignmentJob{}, models.Alignment{}, err
	}
	if err := ensureAlignmentTx(ctx, tx, job); err != nil {
		return models.AlignmentJob{}, models.Alignment{}, err
	}

	now := time.Now().UTC()
	if _, err := tx.ExecContext(ctx, `
		UPDATE alignments SET state = ?, coverage = ?, mean_confidence = ?, model = ?
		WHERE id = ?`, state, coverage, meanConfidence, model, jobID); err != nil {
		return models.AlignmentJob{}, models.Alignment{}, err
	}

	progress := job.Progress
	if state != models.AlignmentFailed {
		progress = 1
		// A usable alignment replaces the entry's older ones wholesale:
		// their anchors describe a superseded run.
		if _, err := tx.ExecContext(ctx,
			`DELETE FROM alignments WHERE entry_id = ? AND id != ?`, job.EntryID, jobID); err != nil {
			return models.AlignmentJob{}, models.Alignment{}, err
		}
	}
	jobErr := job.Error
	if state == models.AlignmentFailed {
		msg := strings.TrimSpace(errMsg)
		if msg == "" {
			msg = "alignment failed"
		}
		jobErr = &msg
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE alignment_jobs SET state = ?, progress = ?, error = ?,
			heartbeat_at = ?, updated_at = ?
		WHERE id = ?`, state, progress, jobErr, now, now, jobID); err != nil {
		return models.AlignmentJob{}, models.Alignment{}, err
	}

	outJob, err := alignmentJobTx(ctx, tx, jobID)
	if err != nil {
		return models.AlignmentJob{}, models.Alignment{}, err
	}
	outAlign, err := alignmentTx(ctx, tx, jobID)
	if err != nil {
		return models.AlignmentJob{}, models.Alignment{}, err
	}
	return outJob, outAlign, tx.Commit()
}

// ReclaimStaleAlignmentJobs requeues or fails every claimed job whose
// heartbeat has gone silent, and returns how many it touched. It runs
// inside every claim; exposing it lets a test (or an operator script)
// drive a reclaim without a worker in the picture.
func (s *Store) ReclaimStaleAlignmentJobs(ctx context.Context) (int, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	n, err := reclaimStaleJobs(ctx, tx)
	if err != nil {
		return 0, err
	}
	return n, tx.Commit()
}

// reclaimStaleJobs is the reclaim pass: a job in a claimed state whose
// heartbeat predates the staleness cutoff either goes back to queued
// (attempts permitting) or, out of attempts, fails loudly. A job with
// no heartbeat at all is judged by its claim time.
func reclaimStaleJobs(ctx context.Context, tx *sql.Tx) (int, error) {
	cutoff := time.Now().UTC().Add(-AlignmentStaleAfter)

	rows, err := tx.QueryContext(ctx, `
		SELECT id, attempts, heartbeat_at, claimed_at
		FROM alignment_jobs
		WHERE state IN ('claimed','transcribing','aligning')`)
	if err != nil {
		return 0, err
	}
	type stale struct {
		id       string
		attempts int
	}
	var out []stale
	for rows.Next() {
		var j stale
		var beat, claim sql.NullTime
		if err := rows.Scan(&j.id, &j.attempts, &beat, &claim); err != nil {
			rows.Close()
			return 0, err
		}
		// heartbeat_at is the liveness signal: every worker write path
		// refreshes it, so it is authoritative while present. A claimed
		// job with none at all is judged by its claim time.
		liveness := beat
		if !liveness.Valid {
			liveness = claim
		}
		if !liveness.Valid || liveness.Time.Before(cutoff) {
			out = append(out, j)
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, err
	}
	rows.Close()

	now := time.Now().UTC()
	for _, j := range out {
		if j.attempts >= AlignmentMaxAttempts {
			if _, err := tx.ExecContext(ctx, `
				UPDATE alignment_jobs SET state = 'failed',
					error = ?, claimed_by = NULL, claimed_at = NULL, heartbeat_at = NULL,
					updated_at = ?
				WHERE id = ?`,
				fmt.Sprintf("worker heartbeat went stale; giving up after %d attempts", j.attempts), now, j.id); err != nil {
				return 0, err
			}
			continue
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE alignment_jobs SET state = 'queued',
				claimed_by = NULL, claimed_at = NULL, heartbeat_at = NULL,
				updated_at = ?
			WHERE id = ?`, now, j.id); err != nil {
			return 0, err
		}
	}
	return len(out), nil
}

// ClearAlignment cancels any in-flight job for one of the caller's
// entries and deletes the entry's alignments — anchors, segments and
// job history with them. This is the user-facing "stop / start over".
func (s *Store) ClearAlignment(ctx context.Context, userID, entryID string) error {
	if _, err := s.BookIDForEntry(ctx, userID, entryID); err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM alignment_jobs WHERE entry_id = ?`, entryID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM alignments WHERE entry_id = ?`, entryID); err != nil {
		return err
	}
	return tx.Commit()
}

// AlignmentForEntry returns the entry's newest usable alignment
// ('ready' or 'low_confidence'), or nil when there is none. A failed
// alignment never feeds the translator.
func (s *Store) AlignmentForEntry(ctx context.Context, entryID string) (models.Alignment, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, entry_id, epub_text_id, state, coverage, mean_confidence, model, created_at
		FROM alignments
		WHERE entry_id = ? AND state IN ('ready','low_confidence')
		ORDER BY created_at DESC, id DESC LIMIT 1`, entryID)
	align, err := scanAlignment(row)
	if errors.Is(err, sql.ErrNoRows) {
		return models.Alignment{}, nil
	}
	return align, err
}

// AlignmentAnchors returns one alignment's anchors in text order.
func (s *Store) AlignmentAnchors(ctx context.Context, alignmentID string) ([]models.AlignmentAnchor, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT alignment_id, char_offset, audio_seconds, confidence
		FROM alignment_anchors WHERE alignment_id = ? ORDER BY char_offset`, alignmentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []models.AlignmentAnchor{}
	for rows.Next() {
		var a models.AlignmentAnchor
		if err := rows.Scan(&a.AlignmentID, &a.CharOffset, &a.AudioSeconds, &a.Confidence); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// BookIDForEntryAny resolves a library entry to its book id without an
// owner check. It exists for the internal worker API, whose callers
// hold the shared token instead of a session: everything reachable
// through it was already ownership-checked when the job was enqueued.
// Not for user-facing routes.
func (s *Store) BookIDForEntryAny(ctx context.Context, entryID string) (string, error) {
	var bookID string
	err := s.db.QueryRowContext(ctx, `
		SELECT book_id FROM library_entries
		WHERE id = ? AND media_type = 'book'`, entryID).Scan(&bookID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return bookID, err
}

// requireClaimedJobTx loads a job and enforces the two invariants of
// every worker write: the job exists and is not terminal, and the
// caller holds its claim. The claim check is what keeps a worker that
// stalled, lost its job to a reclaim, and woke up from interleaving
// its output with the new owner's.
func requireClaimedJobTx(ctx context.Context, tx *sql.Tx, jobID, workerID string) (models.AlignmentJob, error) {
	job, err := alignmentJobTx(ctx, tx, jobID)
	if err != nil {
		return models.AlignmentJob{}, err
	}
	if models.AlignmentJobTerminal(job.State) {
		return models.AlignmentJob{}, ErrJobTerminal
	}
	if job.ClaimedBy == nil || *job.ClaimedBy != workerID {
		return models.AlignmentJob{}, ErrJobNotClaimedBy
	}
	return job, nil
}

// ensureAlignmentTx creates the alignment row a streaming worker writes
// against. It shares the job's id — one alignment per job, and the job
// already carries the entry and text it was enqueued with.
func ensureAlignmentTx(ctx context.Context, tx *sql.Tx, job models.AlignmentJob) error {
	_, err := tx.ExecContext(ctx, `
		INSERT OR IGNORE INTO alignments (id, entry_id, epub_text_id, state)
		VALUES (?, ?, ?, 'aligning')`, job.ID, job.EntryID, job.EpubTextID)
	return err
}

// touchHeartbeatTx refreshes a job's heartbeat and returns the job.
func (s *Store) touchHeartbeatTx(ctx context.Context, tx *sql.Tx, jobID string) (models.AlignmentJob, error) {
	now := time.Now().UTC()
	if _, err := tx.ExecContext(ctx, `
		UPDATE alignment_jobs SET heartbeat_at = ?, updated_at = ? WHERE id = ?`,
		now, now, jobID); err != nil {
		return models.AlignmentJob{}, err
	}
	return alignmentJobTx(ctx, tx, jobID)
}

func alignmentJobTx(ctx context.Context, tx *sql.Tx, jobID string) (models.AlignmentJob, error) {
	row := tx.QueryRowContext(ctx,
		`SELECT `+alignmentJobColumns+` FROM alignment_jobs WHERE id = ?`, jobID)
	job, err := scanAlignmentJob(row)
	if errors.Is(err, sql.ErrNoRows) {
		return models.AlignmentJob{}, ErrNotFound
	}
	return job, err
}

func alignmentTx(ctx context.Context, tx *sql.Tx, alignmentID string) (models.Alignment, error) {
	row := tx.QueryRowContext(ctx, `
		SELECT id, entry_id, epub_text_id, state, coverage, mean_confidence, model, created_at
		FROM alignments WHERE id = ?`, alignmentID)
	return scanAlignment(row)
}

func scanAlignmentJob(row interface{ Scan(...any) error }) (models.AlignmentJob, error) {
	var j models.AlignmentJob
	var jobErr sql.NullString
	var claimedBy sql.NullString
	var claimedAt, heartbeatAt sql.NullTime
	err := row.Scan(&j.ID, &j.EntryID, &j.EpubTextID, &j.AudioTimelineHash, &j.State,
		&j.Progress, &j.StageDetail, &jobErr, &j.Attempts,
		&claimedBy, &claimedAt, &heartbeatAt, &j.CreatedAt, &j.UpdatedAt)
	if err != nil {
		return models.AlignmentJob{}, err
	}
	if jobErr.Valid {
		j.Error = &jobErr.String
	}
	if claimedBy.Valid {
		j.ClaimedBy = &claimedBy.String
	}
	if claimedAt.Valid {
		j.ClaimedAt = &claimedAt.Time
	}
	if heartbeatAt.Valid {
		j.HeartbeatAt = &heartbeatAt.Time
	}
	return j, nil
}

func scanAlignment(row interface{ Scan(...any) error }) (models.Alignment, error) {
	var a models.Alignment
	err := row.Scan(&a.ID, &a.EntryID, &a.EpubTextID, &a.State,
		&a.Coverage, &a.MeanConfidence, &a.Model, &a.CreatedAt)
	return a, err
}
