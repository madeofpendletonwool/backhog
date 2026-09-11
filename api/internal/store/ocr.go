package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/collinpendleton/backhog/api/internal/models"
)

// OCR queue tuning, the alignment queue's own numbers: attempts are capped so
// a genuinely broken comic fails loudly instead of looping between workers,
// and a claimed job whose heartbeat goes silent is reclaimed. OCR is seconds
// per page rather than hours per book, but the silence still means the worker
// is gone, not busy.
const (
	OCRMaxAttempts = 3
	OCRStaleAfter  = 10 * time.Minute
)

const ocrJobColumns = `id, entry_id, media_file_id, parser_version, state,
	progress, stage_detail, error, coverage, mean_confidence, model,
	attempts, claimed_by, claimed_at, heartbeat_at, created_at, updated_at`

const activeOCRJobStates = `'queued','claimed','ocring'`

// EnqueueOCR queues a lettering pass for one of the caller's book entries,
// against the media file the handler resolved to an image-native PDF primary.
// The second return is true when an active job already existed for the file
// and was returned as-is: the corpus belongs to the media file, so a second
// reader of the same comic hammering the button cannot stack runs.
func (s *Store) EnqueueOCR(ctx context.Context, userID, entryID string, mediaFileID int64, parserVersion string) (models.OCRJob, bool, error) {
	if _, err := s.BookFilesForEntry(ctx, userID, entryID); err != nil {
		return models.OCRJob{}, false, err
	}
	if job, err := s.activeOCRJob(ctx, mediaFileID); err != nil || job.ID != "" {
		return job, true, err
	}

	job := models.OCRJob{
		ID:            newID(),
		EntryID:       entryID,
		MediaFileID:   mediaFileID,
		ParserVersion: parserVersion,
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO ocr_jobs (id, entry_id, media_file_id, parser_version)
		VALUES (?, ?, ?, ?)`, job.ID, job.EntryID, job.MediaFileID, job.ParserVersion)
	if err != nil {
		// The one-active-job index fires only when a concurrent enqueue won
		// the race; hand back the winner's job.
		if existing, lookupErr := s.activeOCRJob(ctx, mediaFileID); lookupErr == nil && existing.ID != "" {
			return existing, true, nil
		}
		return models.OCRJob{}, false, err
	}
	out, err := s.OCRJob(ctx, job.ID)
	return out, false, err
}

// MediaFileByID loads one media file row. It exists for the internal worker
// API, whose callers hold the shared token instead of a session: everything
// they touch was ownership-checked when the job was enqueued.
func (s *Store) MediaFileByID(ctx context.Context, id int64) (models.MediaFile, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT `+mediaFileColumns+`
		FROM media_files WHERE id = ?`, id)
	f, err := scanMediaFile(row)
	if errors.Is(err, sql.ErrNoRows) {
		return models.MediaFile{}, ErrNotFound
	}
	return f, err
}

// activeOCRJob returns the media file's in-flight job, or nil.
func (s *Store) activeOCRJob(ctx context.Context, mediaFileID int64) (models.OCRJob, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT `+ocrJobColumns+`
		FROM ocr_jobs
		WHERE media_file_id = ? AND state IN (`+activeOCRJobStates+`)
		ORDER BY created_at, id LIMIT 1`, mediaFileID)
	job, err := scanOCRJob(row)
	if errors.Is(err, sql.ErrNoRows) {
		return models.OCRJob{}, nil
	}
	return job, err
}

// OCRJob loads one job by id.
func (s *Store) OCRJob(ctx context.Context, jobID string) (models.OCRJob, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT `+ocrJobColumns+` FROM ocr_jobs WHERE id = ?`, jobID)
	job, err := scanOCRJob(row)
	if errors.Is(err, sql.ErrNoRows) {
		return models.OCRJob{}, ErrNotFound
	}
	return job, err
}

// OCRJobForMediaFile returns the file's most recent job of any state, or nil
// when it has never been OCR'd.
func (s *Store) OCRJobForMediaFile(ctx context.Context, mediaFileID int64) (models.OCRJob, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT `+ocrJobColumns+`
		FROM ocr_jobs WHERE media_file_id = ?
		ORDER BY created_at DESC, id DESC LIMIT 1`, mediaFileID)
	job, err := scanOCRJob(row)
	if errors.Is(err, sql.ErrNoRows) {
		return models.OCRJob{}, nil
	}
	return job, err
}

// ClaimOCRJob atomically claims the oldest queued job for a worker, stale
// claims first. The ledger rows a previous attempt wrote are deliberately
// kept: each is pinned to its page image's hash, so a restarted run re-uses
// (or overwrites) them page by page rather than needing a clean slate.
func (s *Store) ClaimOCRJob(ctx context.Context, workerID string) (models.OCRJob, error) {
	if strings.TrimSpace(workerID) == "" {
		return models.OCRJob{}, errors.New("worker id must not be empty")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return models.OCRJob{}, err
	}
	defer tx.Rollback()

	if _, err := reclaimStaleOCRJobs(ctx, tx); err != nil {
		return models.OCRJob{}, err
	}

	var jobID string
	err = tx.QueryRowContext(ctx, `
		SELECT id FROM ocr_jobs WHERE state = 'queued'
		ORDER BY created_at, id LIMIT 1`).Scan(&jobID)
	if errors.Is(err, sql.ErrNoRows) {
		return models.OCRJob{}, ErrNotFound
	}
	if err != nil {
		return models.OCRJob{}, err
	}

	now := time.Now().UTC()
	if _, err := tx.ExecContext(ctx, `
		UPDATE ocr_jobs SET
			state = 'claimed', claimed_by = ?, claimed_at = ?,
			heartbeat_at = ?, attempts = attempts + 1,
			progress = 0, stage_detail = '', error = NULL,
			coverage = 0, mean_confidence = 0, model = '', updated_at = ?
		WHERE id = ?`, workerID, now, now, now, jobID); err != nil {
		return models.OCRJob{}, err
	}

	job, err := ocrJobTx(ctx, tx, jobID)
	if err != nil {
		return models.OCRJob{}, err
	}
	return job, tx.Commit()
}

// OCRJobProgress records a worker heartbeat: optionally a new state, progress
// and stage detail, always a fresh heartbeat_at. Only the worker holding the
// claim may write, and never a terminal job.
func (s *Store) OCRJobProgress(ctx context.Context, jobID, workerID, state string, progress *float64, stageDetail *string) (models.OCRJob, error) {
	if state != "" && !models.OCRJobActive(state) {
		return models.OCRJob{}, fmt.Errorf("state %q is not a worker pipeline state", state)
	}
	if progress != nil && (*progress < 0 || *progress > 1) {
		return models.OCRJob{}, errors.New("progress must be within [0,1]")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return models.OCRJob{}, err
	}
	defer tx.Rollback()

	job, err := requireClaimedOCRJobTx(ctx, tx, jobID, workerID)
	if err != nil {
		return models.OCRJob{}, err
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
		`UPDATE ocr_jobs SET `+strings.Join(sets, ", ")+` WHERE id = ?`, args...); err != nil {
		return models.OCRJob{}, err
	}

	job, err = ocrJobTx(ctx, tx, jobID)
	if err != nil {
		return models.OCRJob{}, err
	}
	return job, tx.Commit()
}

// AppendOCRPages upserts one streamed batch of per-page lettering into the
// corpus. A page is keyed by its number and pinned to the image hash the
// text was read from; re-sending a page (a worker retrying a batch) replaces
// it wholesale rather than interleaving.
func (s *Store) AppendOCRPages(ctx context.Context, jobID, workerID string, pages []models.OCRPage) (models.OCRJob, error) {
	for _, p := range pages {
		if p.PageNumber < 1 {
			return models.OCRJob{}, errors.New("page numbers are 1-based")
		}
		if len(p.Text) > 64<<10 {
			return models.OCRJob{}, errors.New("page text over the size limit")
		}
		p.MeanConfidence = min(1, max(0, p.MeanConfidence))
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return models.OCRJob{}, err
	}
	defer tx.Rollback()

	job, err := requireClaimedOCRJobTx(ctx, tx, jobID, workerID)
	if err != nil {
		return models.OCRJob{}, err
	}

	now := time.Now().UTC()
	const batchSize = 64
	for start := 0; start < len(pages); start += batchSize {
		end := min(start+batchSize, len(pages))
		var b strings.Builder
		args := make([]any, 0, (end-start)*7+1)
		b.WriteString(`INSERT INTO ocr_pages
			(media_file_id, page_number, image_sha256, ocr_version, text, mean_confidence, updated_at)
			VALUES `)
		for i, p := range pages[start:end] {
			if i > 0 {
				b.WriteByte(',')
			}
			b.WriteString("(?,?,?,?,?,?,?)")
			args = append(args, job.MediaFileID, p.PageNumber, p.ImageSHA256,
				p.OCRVersion, p.Text, min(1, max(0, p.MeanConfidence)), now)
		}
		// The composite PK is the idempotency: re-sending a page replaces
		// it wholesale — a retried batch interleaves nothing.
		b.WriteString(` ON CONFLICT(media_file_id, page_number) DO UPDATE SET
			image_sha256 = excluded.image_sha256,
			ocr_version = excluded.ocr_version,
			text = excluded.text,
			mean_confidence = excluded.mean_confidence,
			updated_at = excluded.updated_at`)
		if _, err := tx.ExecContext(ctx, b.String(), args...); err != nil {
			return models.OCRJob{}, fmt.Errorf("upsert ocr pages: %w", err)
		}
	}

	out, err := s.touchOCRHeartbeatTx(ctx, tx, jobID)
	if err != nil {
		return models.OCRJob{}, err
	}
	return out, tx.Commit()
}

// CompleteOCRJob finalizes a job. The API — not the worker — computes the
// honesty pair from the corpus it owns: coverage is the share of the
// classified pages that yielded any lettering, mean confidence the average
// over those pages. Against the deployment's thresholds the corpus is
// 'ready' or stays usable as 'low_confidence'; an error message fails it.
func (s *Store) CompleteOCRJob(ctx context.Context, jobID, workerID, model, errMsg string, minCoverage, minConfidence float64) (models.OCRJob, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return models.OCRJob{}, err
	}
	defer tx.Rollback()

	job, err := requireClaimedOCRJobTx(ctx, tx, jobID, workerID)
	if err != nil {
		return models.OCRJob{}, err
	}

	var pageCount int
	err = tx.QueryRowContext(ctx, `
		SELECT pf.page_count FROM pdf_files pf WHERE pf.media_file_id = ?`,
		job.MediaFileID).Scan(&pageCount)
	if err != nil {
		return models.OCRJob{}, fmt.Errorf("complete ocr job: classification: %w", err)
	}

	var withText int
	var meanConfidence sql.NullFloat64
	if err := tx.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(CASE WHEN text <> '' THEN 1 ELSE 0 END), 0),
		       (SELECT AVG(mean_confidence) FROM ocr_pages
		        WHERE media_file_id = ? AND text <> '')
		FROM ocr_pages WHERE media_file_id = ?`, job.MediaFileID, job.MediaFileID).
		Scan(&withText, &meanConfidence); err != nil {
		return models.OCRJob{}, err
	}

	state := models.OCRReady
	progress := 1.0
	coverage := 0.0
	mean := 0.0
	if pageCount > 0 && withText > 0 {
		coverage = float64(withText) / float64(pageCount)
	}
	if meanConfidence.Valid {
		mean = meanConfidence.Float64
	}
	var jobErr any
	if msg := strings.TrimSpace(errMsg); msg != "" {
		state = models.OCRFailed
		progress = job.Progress
		jobErr = msg
		if strings.TrimSpace(model) == "" {
			model = job.Model
		}
	} else {
		if strings.TrimSpace(model) == "" {
			return models.OCRJob{}, errors.New("model is required for a usable corpus")
		}
		if coverage < minCoverage || mean < minConfidence {
			state = models.OCRLowConfidence
		}
	}

	now := time.Now().UTC()
	if _, err := tx.ExecContext(ctx, `
		UPDATE ocr_jobs SET state = ?, progress = ?, error = ?, coverage = ?,
			mean_confidence = ?, model = ?, heartbeat_at = ?, updated_at = ?
		WHERE id = ?`, state, progress, jobErr, coverage, mean, model, now, now, jobID); err != nil {
		return models.OCRJob{}, err
	}

	out, err := ocrJobTx(ctx, tx, jobID)
	if err != nil {
		return models.OCRJob{}, err
	}
	return out, tx.Commit()
}

// ReclaimStaleOCRJobs requeues or fails every claimed OCR job whose
// heartbeat has gone silent, and returns how many it touched.
func (s *Store) ReclaimStaleOCRJobs(ctx context.Context) (int, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	n, err := reclaimStaleOCRJobs(ctx, tx)
	if err != nil {
		return 0, err
	}
	return n, tx.Commit()
}

// reclaimStaleOCRJobs is the reclaim pass, the alignment queue's rule exactly:
// a claimed job whose heartbeat predates the cutoff goes back to queued
// (attempts permitting) or, out of attempts, fails loudly.
func reclaimStaleOCRJobs(ctx context.Context, tx *sql.Tx) (int, error) {
	cutoff := time.Now().UTC().Add(-OCRStaleAfter)

	rows, err := tx.QueryContext(ctx, `
		SELECT id, attempts, heartbeat_at, claimed_at
		FROM ocr_jobs
		WHERE state IN ('claimed','ocring')`)
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
		if j.attempts >= OCRMaxAttempts {
			if _, err := tx.ExecContext(ctx, `
				UPDATE ocr_jobs SET state = 'failed',
					error = ?, claimed_by = NULL, claimed_at = NULL, heartbeat_at = NULL,
					updated_at = ?
				WHERE id = ?`,
				fmt.Sprintf("worker heartbeat went stale; giving up after %d attempts", j.attempts), now, j.id); err != nil {
				return 0, err
			}
			continue
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE ocr_jobs SET state = 'queued',
				claimed_by = NULL, claimed_at = NULL, heartbeat_at = NULL,
				updated_at = ?
			WHERE id = ?`, now, j.id); err != nil {
			return 0, err
		}
	}
	return len(out), nil
}

// OCRPagesForMediaFile returns the file's whole lettering corpus in page
// order — the searcher's load. Only usable-corpus files are asked.
func (s *Store) OCRPagesForMediaFile(ctx context.Context, mediaFileID int64) ([]models.OCRPage, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT page_number, image_sha256, ocr_version, text, mean_confidence, updated_at
		FROM ocr_pages WHERE media_file_id = ? ORDER BY page_number`, mediaFileID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []models.OCRPage{}
	for rows.Next() {
		var p models.OCRPage
		if err := rows.Scan(&p.PageNumber, &p.ImageSHA256, &p.OCRVersion, &p.Text,
			&p.MeanConfidence, &p.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// OCRCorpusForMediaFile grades the file's newest usable corpus — the newest
// ready or low_confidence job — with its coverage/confidence pair recomputed
// from the pages as they stand now. A failed run never feeds search.
func (s *Store) OCRCorpusForMediaFile(ctx context.Context, mediaFileID int64) (models.OCRCorpus, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT `+ocrJobColumns+`
		FROM ocr_jobs
		WHERE media_file_id = ? AND state IN ('ready','low_confidence')
		ORDER BY created_at DESC, id DESC LIMIT 1`, mediaFileID)
	job, err := scanOCRJob(row)
	if errors.Is(err, sql.ErrNoRows) {
		return models.OCRCorpus{}, nil
	}
	if err != nil {
		return models.OCRCorpus{}, err
	}

	var pageCount int
	if err := s.db.QueryRowContext(ctx,
		`SELECT page_count FROM pdf_files WHERE media_file_id = ?`, mediaFileID).
		Scan(&pageCount); err != nil {
		return models.OCRCorpus{}, err
	}
	var withText int
	if err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM ocr_pages
		WHERE media_file_id = ? AND text <> ''`, mediaFileID).Scan(&withText); err != nil {
		return models.OCRCorpus{}, err
	}
	return models.OCRCorpus{
		State:          job.State,
		Coverage:       job.Coverage,
		MeanConfidence: job.MeanConfidence,
		Model:          job.Model,
		PagesWithText:  withText,
		PageCount:      pageCount,
	}, nil
}

// OCRCorpusRevision is a cheap digest of a file's corpus as it stands —
// row count, total text length, newest update. It is what the searcher
// folds into its cache key so a re-OCR'd page invalidates the index built
// over the old lettering, for the price of one aggregate query.
func (s *Store) OCRCorpusRevision(ctx context.Context, mediaFileID int64) (string, error) {
	var count, length int
	var newest sql.NullString
	err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*), COALESCE(SUM(LENGTH(text)), 0), COALESCE(CAST(MAX(updated_at) AS TEXT), '')
		FROM ocr_pages WHERE media_file_id = ?`, mediaFileID).
		Scan(&count, &length, &newest)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%d/%d/%s", count, length, newest.String), nil
}

// ClearOCR cancels any in-flight lettering pass for one of the caller's
// entries and drops the file's corpus — pages and job history with them.
// The user-facing "stop / start over".
func (s *Store) ClearOCR(ctx context.Context, userID, entryID string) error {
	if _, err := s.BookFilesForEntry(ctx, userID, entryID); err != nil {
		return err
	}
	mediaFileID, err := s.primaryPagedMediaFile(ctx, entryID)
	if err != nil {
		return err
	}
	return s.ClearOCRForMediaFile(ctx, mediaFileID)
}

// ClearOCRForMediaFile drops a file's corpus and queue rows whole — the
// re-classification path (a new verdict means the old page images, and
// everything read off them, are stale) and the user-facing clear.
func (s *Store) ClearOCRForMediaFile(ctx context.Context, mediaFileID int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM ocr_jobs WHERE media_file_id = ?`, mediaFileID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM ocr_pages WHERE media_file_id = ?`, mediaFileID); err != nil {
		return err
	}
	return tx.Commit()
}

// PrimaryPagedMediaFile resolves an entry to its designated text file when
// that file is an image-native PDF — the file a lettering pass reads. The
// second return is false for every text-mode book: an EPUB, a MOBI, a
// text-native PDF. The caller owns the honest refusal.
func (s *Store) PrimaryPagedMediaFile(ctx context.Context, userID, entryID string) (models.MediaFile, bool, error) {
	if _, err := s.BookFilesForEntry(ctx, userID, entryID); err != nil {
		return models.MediaFile{}, false, err
	}
	mediaFileID, err := s.primaryPagedMediaFile(ctx, entryID)
	if err != nil {
		return models.MediaFile{}, false, err
	}
	f, err := s.MediaFileByID(ctx, mediaFileID)
	return f, true, err
}

// primaryPagedMediaFile is the unscoped half of PrimaryPagedMediaFile: the
// primary text media file id of an entry whose book answers on the page
// axis. ErrNotFound means the book is text-mode — the caller's 422.
func (s *Store) primaryPagedMediaFile(ctx context.Context, entryID string) (int64, error) {
	var mediaFileID int64
	err := s.db.QueryRowContext(ctx, `
		SELECT mf.id
		FROM library_entries e
		JOIN media_files mf ON mf.book_id = e.book_id AND `+primaryTextIs+`
		JOIN pdf_files pf ON pf.media_file_id = mf.id
		     AND pf.classification = 'image-native'
		WHERE e.id = ? AND e.media_type = 'book'
		ORDER BY `+TextFormatRank+`, mf.id
		LIMIT 1`, entryID).Scan(&mediaFileID)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrNotFound
	}
	return mediaFileID, err
}

// requireClaimedOCRJobTx loads a job and enforces the two invariants of
// every worker write: not terminal, and held by this worker — the check
// that keeps a stale worker's late pages out of the new owner's corpus.
func requireClaimedOCRJobTx(ctx context.Context, tx *sql.Tx, jobID, workerID string) (models.OCRJob, error) {
	job, err := ocrJobTx(ctx, tx, jobID)
	if err != nil {
		return models.OCRJob{}, err
	}
	if models.OCRJobTerminal(job.State) {
		return models.OCRJob{}, ErrJobTerminal
	}
	if job.ClaimedBy == nil || *job.ClaimedBy != workerID {
		return models.OCRJob{}, ErrJobNotClaimedBy
	}
	return job, nil
}

// touchOCRHeartbeatTx refreshes a job's heartbeat and returns the job.
func (s *Store) touchOCRHeartbeatTx(ctx context.Context, tx *sql.Tx, jobID string) (models.OCRJob, error) {
	now := time.Now().UTC()
	if _, err := tx.ExecContext(ctx, `
		UPDATE ocr_jobs SET heartbeat_at = ?, updated_at = ? WHERE id = ?`,
		now, now, jobID); err != nil {
		return models.OCRJob{}, err
	}
	return ocrJobTx(ctx, tx, jobID)
}

func ocrJobTx(ctx context.Context, tx *sql.Tx, jobID string) (models.OCRJob, error) {
	row := tx.QueryRowContext(ctx,
		`SELECT `+ocrJobColumns+` FROM ocr_jobs WHERE id = ?`, jobID)
	job, err := scanOCRJob(row)
	if errors.Is(err, sql.ErrNoRows) {
		return models.OCRJob{}, ErrNotFound
	}
	return job, err
}

func scanOCRJob(row interface{ Scan(...any) error }) (models.OCRJob, error) {
	var j models.OCRJob
	var jobErr sql.NullString
	var claimedBy sql.NullString
	var claimedAt, heartbeatAt sql.NullTime
	err := row.Scan(&j.ID, &j.EntryID, &j.MediaFileID, &j.ParserVersion, &j.State,
		&j.Progress, &j.StageDetail, &jobErr, &j.Coverage, &j.MeanConfidence, &j.Model,
		&j.Attempts, &claimedBy, &claimedAt, &heartbeatAt, &j.CreatedAt, &j.UpdatedAt)
	if err != nil {
		return models.OCRJob{}, err
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
