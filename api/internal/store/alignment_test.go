package store

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/collinpendleton/backhog/api/internal/models"
)

// seedAlignmentEntry builds one alignable book entry: a book, a library
// entry for the user, an attached EPUB with a parsed canonical text, and
// two attached audio tracks in order. idx keeps multiple entries in one
// fixture distinct (it feeds the media-file and text ids).
func seedAlignmentEntry(t *testing.T, s *Store, userID string, idx int) string {
	t.Helper()
	ctx := context.Background()
	suffix := fmt.Sprintf("E%d", idx)
	bookID := "OL" + suffix
	entryID := "align-" + suffix

	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := s.db.ExecContext(ctx, q, args...); err != nil {
			t.Fatalf("seed %q: %v", q, err)
		}
	}
	exec(`INSERT INTO books (id, title) VALUES (?, 'Anathem `+suffix+`')`, bookID)
	exec(`INSERT INTO library_entries (id, user_id, media_type, book_id, status)
	      VALUES (?, ?, 'book', ?, 'backlog')`, entryID, userID, bookID)
	exec(`INSERT INTO media_files (id, root, path, kind, size_bytes, mtime, book_id, scanned_at)
	      VALUES (?, '/nas', 'x/`+suffix+`.epub', 'epub', 10, 1, ?, CURRENT_TIMESTAMP)`, 1000+idx, bookID)
	exec(`INSERT INTO epub_texts (id, media_file_id, char_count, word_count, normalized_sha256, parser_version)
	      VALUES (?, ?, 400000, 70000, 'sha-`+suffix+`', 'v1')`, "text-"+suffix, 1000+idx)
	exec(`INSERT INTO media_files (id, root, path, kind, size_bytes, mtime, book_id, track_number, duration_seconds, scanned_at)
	      VALUES (?, '/nas', 'x/`+suffix+`-01.m4b', 'audio', 10, 1, ?, 1, 19800.0, CURRENT_TIMESTAMP),
	             (?, '/nas', 'x/`+suffix+`-02.m4b', 'audio', 10, 1, ?, 2, 20200.0, CURRENT_TIMESTAMP)`,
		2000+2*idx, bookID, 2001+2*idx, bookID)
	return entryID
}

func TestEnqueueAlignmentRequiresBothHalves(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	userID := newTestUser(t, s, "align@example.com", "aligner")
	entry := seedAlignmentEntry(t, s, userID, 1)

	job, existed, err := s.EnqueueAlignment(ctx, userID, entry)
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if existed || job.State != models.AlignmentQueued || job.Attempts != 0 {
		t.Fatalf("first enqueue = (existed %v, %s, %d attempts), want fresh queued job", existed, job.State, job.Attempts)
	}
	if job.AudioTimelineHash == "" {
		t.Error("enqueue did not pin an audio timeline hash")
	}

	// The second enqueue is idempotent while the job is in flight.
	again, existed, err := s.EnqueueAlignment(ctx, userID, entry)
	if err != nil || !existed || again.ID != job.ID {
		t.Fatalf("re-enqueue = (%s, existed %v, %v), want the same active job", again.ID, existed, err)
	}

	// Someone else's entry is an unknown one.
	stranger := newTestUser(t, s, "stranger@example.com", "stranger")
	if _, _, err := s.EnqueueAlignment(ctx, stranger, entry); !errors.Is(err, ErrNotFound) {
		t.Errorf("enqueue on another user's entry = %v, want ErrNotFound", err)
	}

	// An entry with no parsed text cannot be aligned.
	textless := seedAlignmentEntry(t, s, userID, 2)
	if _, err := s.db.ExecContext(ctx, `DELETE FROM epub_texts`); err != nil {
		t.Fatalf("drop texts: %v", err)
	}
	if _, _, err := s.EnqueueAlignment(ctx, userID, textless); !errors.Is(err, ErrNoAlignmentText) {
		t.Errorf("enqueue without text = %v, want ErrNoAlignmentText", err)
	}

	// An entry with no audio has nothing to align against.
	seedAlignmentEntry(t, s, userID, 3)
	if _, err := s.db.ExecContext(ctx, `UPDATE media_files SET book_id = NULL WHERE kind = 'audio'`); err != nil {
		t.Fatalf("detach audio: %v", err)
	}
	if _, _, err := s.EnqueueAlignment(ctx, userID, "align-E3"); !errors.Is(err, ErrNoAlignmentAudio) {
		t.Errorf("enqueue without audio = %v, want ErrNoAlignmentAudio", err)
	}
}

// TestClaimAlignmentJobNeverHandsOutTheSameJobTwice is the queue's core
// safety property: however many workers claim at once, each job is
// claimed by exactly one of them.
func TestClaimAlignmentJobNeverHandsOutTheSameJobTwice(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	userID := newTestUser(t, s, "align@example.com", "aligner")

	const jobs = 12
	entryIDs := make([]string, jobs)
	for i := range jobs {
		entryIDs[i] = seedAlignmentEntry(t, s, userID, i)
		if _, _, err := s.EnqueueAlignment(ctx, userID, entryIDs[i]); err != nil {
			t.Fatalf("enqueue %d: %v", i, err)
		}
	}

	const workers = 12
	claimed := make([]models.AlignmentJob, workers)
	errs := make([]error, workers)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := range workers {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			job, err := s.ClaimAlignmentJob(ctx, fmt.Sprintf("worker-%d", i))
			claimed[i], errs[i] = job, err
		}(i)
	}
	close(start)
	wg.Wait()

	seen := map[string]bool{}
	for i := range workers {
		if errs[i] != nil {
			t.Fatalf("worker %d claim: %v", i, errs[i])
		}
		if seen[claimed[i].ID] {
			t.Fatalf("job %s was claimed by two workers", claimed[i].ID)
		}
		seen[claimed[i].ID] = true
		if claimed[i].State != models.AlignmentClaimed {
			t.Errorf("job %s state = %s, want claimed", claimed[i].ID, claimed[i].State)
		}
		if claimed[i].Attempts != 1 {
			t.Errorf("job %s attempts = %d, want 1", claimed[i].ID, claimed[i].Attempts)
		}
		if claimed[i].ClaimedBy == nil || *claimed[i].ClaimedBy != fmt.Sprintf("worker-%d", i) {
			t.Errorf("job %s claimed_by = %v, want worker-%d", claimed[i].ID, claimed[i].ClaimedBy, i)
		}
	}
	if len(seen) != workers {
		t.Errorf("%d distinct jobs claimed by %d workers", len(seen), workers)
	}

	// The queue is now empty: a further claim says so, not an error job.
	if _, err := s.ClaimAlignmentJob(ctx, "worker-0"); !errors.Is(err, ErrNotFound) {
		t.Errorf("claim on empty queue = %v, want ErrNotFound", err)
	}
}

func TestClaimReclaimsStaleHeartbeat(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	userID := newTestUser(t, s, "align@example.com", "aligner")
	entry := seedAlignmentEntry(t, s, userID, 1)
	if _, _, err := s.EnqueueAlignment(ctx, userID, entry); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	first, err := s.ClaimAlignmentJob(ctx, "slow-worker")
	if err != nil {
		t.Fatalf("claim: %v", err)
	}

	// A healthy heartbeat is not stale.
	if _, err := s.ReclaimStaleAlignmentJobs(ctx); err != nil {
		t.Fatalf("reclaim: %v", err)
	}
	job, err := s.AlignmentJob(ctx, first.ID)
	if err != nil || job.State != models.AlignmentClaimed {
		t.Fatalf("fresh claim was reclaimed: (%s, %v)", job.State, err)
	}

	// The worker goes silent: backdate its heartbeat past the cutoff.
	stale := time.Now().UTC().Add(-(AlignmentStaleAfter + time.Minute))
	if _, err := s.db.ExecContext(ctx,
		`UPDATE alignment_jobs SET heartbeat_at = ? WHERE id = ?`, stale, first.ID); err != nil {
		t.Fatalf("backdate: %v", err)
	}

	second, err := s.ClaimAlignmentJob(ctx, "replacement-worker")
	if err != nil {
		t.Fatalf("reclaiming claim: %v", err)
	}
	if second.ID != first.ID {
		t.Fatalf("stale job %s was not requeued; claim got %s", first.ID, second.ID)
	}
	if second.Attempts != 2 {
		t.Errorf("attempts = %d, want 2", second.Attempts)
	}
	if second.ClaimedBy == nil || *second.ClaimedBy != "replacement-worker" {
		t.Errorf("claimed_by = %v, want the replacement", second.ClaimedBy)
	}

	// The old worker waking up must not be able to write to the job it
	// lost — its output would interleave with the new owner's.
	if _, err := s.AlignmentJobProgress(ctx, first.ID, "slow-worker",
		models.AlignmentTranscribing, nil, nil); !errors.Is(err, ErrJobNotClaimedBy) {
		t.Errorf("stale worker progress = %v, want ErrJobNotClaimedBy", err)
	}
	if _, err := s.AppendAlignmentAnchors(ctx, first.ID, "slow-worker",
		[]models.AlignmentAnchor{{CharOffset: 0, AudioSeconds: 0}}); !errors.Is(err, ErrJobNotClaimedBy) {
		t.Errorf("stale worker anchors = %v, want ErrJobNotClaimedBy", err)
	}
}

func TestStaleJobFailsAfterMaxAttempts(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	userID := newTestUser(t, s, "align@example.com", "aligner")
	entry := seedAlignmentEntry(t, s, userID, 1)
	if _, _, err := s.EnqueueAlignment(ctx, userID, entry); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	// Claim the real job and age it out at the attempt cap: this is a
	// job that has been through the claim/reclaim cycle enough times.
	job, err := s.ClaimAlignmentJob(ctx, "ghost")
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	stale := time.Now().UTC().Add(-(AlignmentStaleAfter + time.Minute))
	if _, err := s.db.ExecContext(ctx, `
		UPDATE alignment_jobs SET attempts = ?, heartbeat_at = ? WHERE id = ?`,
		AlignmentMaxAttempts, stale, job.ID); err != nil {
		t.Fatalf("age out job: %v", err)
	}

	n, err := s.ReclaimStaleAlignmentJobs(ctx)
	if err != nil || n != 1 {
		t.Fatalf("reclaim = (%d, %v), want (1, nil)", n, err)
	}
	job, err = s.AlignmentJob(ctx, job.ID)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if job.State != models.AlignmentFailed {
		t.Errorf("state = %s, want failed", job.State)
	}
	if job.Error == nil || *job.Error == "" {
		t.Error("a burned-out job failed without a reason")
	}
	if job.ClaimedBy != nil {
		t.Errorf("claimed_by = %v on a dead job, want nil", job.ClaimedBy)
	}

	// It is never handed out again.
	if _, err := s.ClaimAlignmentJob(ctx, "worker-0"); !errors.Is(err, ErrNotFound) {
		t.Errorf("claim = %v, want no claimable job left", err)
	}
}

func TestAlignmentJobProgress(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	userID := newTestUser(t, s, "align@example.com", "aligner")
	entry := seedAlignmentEntry(t, s, userID, 1)
	if _, _, err := s.EnqueueAlignment(ctx, userID, entry); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	job, err := s.ClaimAlignmentJob(ctx, "worker-1")
	if err != nil {
		t.Fatalf("claim: %v", err)
	}

	pct := 0.25
	detail := "whisper large-v3: chunk 4/16"
	job, err = s.AlignmentJobProgress(ctx, job.ID, "worker-1",
		models.AlignmentTranscribing, &pct, &detail)
	if err != nil {
		t.Fatalf("progress: %v", err)
	}
	if job.State != models.AlignmentTranscribing || job.Progress != pct || job.StageDetail != detail {
		t.Errorf("progress wrote (%s, %v, %q)", job.State, job.Progress, job.StageDetail)
	}
	if job.HeartbeatAt == nil || job.HeartbeatAt.Before(time.Now().UTC().Add(-time.Minute)) {
		t.Error("progress did not refresh the heartbeat")
	}

	// A bare heartbeat, no state move.
	job, err = s.AlignmentJobProgress(ctx, job.ID, "worker-1", "", nil, nil)
	if err != nil || job.State != models.AlignmentTranscribing {
		t.Fatalf("bare heartbeat = (%v, %s)", err, job.State)
	}

	if _, err := s.AlignmentJobProgress(ctx, job.ID, "worker-1", "osmosis", nil, nil); err == nil {
		t.Error("a nonsense state was accepted")
	}
	over := 1.5
	if _, err := s.AlignmentJobProgress(ctx, job.ID, "worker-1", "", &over, nil); err == nil {
		t.Error("progress over 1 was accepted")
	}
	if _, err := s.AlignmentJobProgress(ctx, "nope", "worker-1", "", nil, nil); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown job = %v, want ErrNotFound", err)
	}
	if _, err := s.AlignmentJobProgress(ctx, job.ID, "worker-2", "", nil, nil); !errors.Is(err, ErrJobNotClaimedBy) {
		t.Errorf("wrong worker = %v, want ErrJobNotClaimedBy", err)
	}
}

func TestAppendBatchesAndComplete(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	userID := newTestUser(t, s, "align@example.com", "aligner")
	entry := seedAlignmentEntry(t, s, userID, 1)
	if _, _, err := s.EnqueueAlignment(ctx, userID, entry); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	job, err := s.ClaimAlignmentJob(ctx, "worker-1")
	if err != nil {
		t.Fatalf("claim: %v", err)
	}

	// A pre-existing good alignment from an earlier run: completing a
	// new one must supersede it, a failed one must not.
	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO alignments (id, entry_id, epub_text_id, state, coverage, mean_confidence, model)
		VALUES ('old', ?, 'text-E1', 'ready', 0.5, 0.5, 'v1')`, entry); err != nil {
		t.Fatalf("seed old alignment: %v", err)
	}

	if _, err := s.AlignmentJobProgress(ctx, job.ID, "worker-1",
		models.AlignmentTranscribing, nil, nil); err != nil {
		t.Fatalf("progress: %v", err)
	}
	segJob, err := s.AppendTranscriptSegments(ctx, job.ID, "worker-1", []models.TranscriptSegment{
		{AudioStart: 0, AudioEnd: 4.2, Text: "the clock ticks"},
		{AudioStart: 4.2, AudioEnd: 8.9, Text: "orth"},
	})
	if err != nil {
		t.Fatalf("segments: %v", err)
	}
	if segJob.HeartbeatAt == nil {
		t.Error("a segment batch did not refresh the heartbeat")
	}

	anchors := []models.AlignmentAnchor{
		{CharOffset: 0, AudioSeconds: 0, Confidence: 0.9},
		{CharOffset: 412900, AudioSeconds: 21600.5, Confidence: 0.8},
		{CharOffset: 412999, AudioSeconds: 21605.0, Confidence: 1.4}, // clamped
	}
	if _, err := s.AppendAlignmentAnchors(ctx, job.ID, "worker-1", anchors); err != nil {
		t.Fatalf("anchors: %v", err)
	}
	// A re-sent batch is idempotent, not a conflict.
	if _, err := s.AppendAlignmentAnchors(ctx, job.ID, "worker-1", anchors[:1]); err != nil {
		t.Fatalf("duplicate anchors: %v", err)
	}

	stored, err := s.AlignmentAnchors(ctx, job.ID)
	if err != nil {
		t.Fatalf("load anchors: %v", err)
	}
	if len(stored) != 3 || stored[2].Confidence != 1 {
		t.Errorf("stored anchors = %d (last confidence %v), want 3 (clamped to 1)", len(stored), stored[2].Confidence)
	}

	// Batches and writes belong to the claiming worker alone.
	if _, err := s.AppendTranscriptSegments(ctx, job.ID, "worker-2", nil); !errors.Is(err, ErrJobNotClaimedBy) {
		t.Errorf("segments from the wrong worker = %v", err)
	}

	doneJob, alignment, err := s.CompleteAlignment(ctx, job.ID, "worker-1",
		models.AlignmentReady, 0.94, 0.87, "whisper large-v3 + aeneas", "")
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	if doneJob.State != models.AlignmentReady || doneJob.Progress != 1 {
		t.Errorf("completed job = (%s, %v), want (ready, 1)", doneJob.State, doneJob.Progress)
	}
	if alignment.State != models.AlignmentReady || alignment.Coverage != 0.94 ||
		alignment.MeanConfidence != 0.87 || alignment.Model != "whisper large-v3 + aeneas" {
		t.Errorf("completed alignment = %+v", alignment)
	}

	// Terminal: no further writes of any kind.
	if _, err := s.AlignmentJobProgress(ctx, job.ID, "worker-1", "", nil, nil); !errors.Is(err, ErrJobTerminal) {
		t.Errorf("progress after complete = %v, want ErrJobTerminal", err)
	}

	// The old run's alignment is gone; the new one is what the
	// translator reads.
	usable, err := s.AlignmentForEntry(ctx, entry)
	if err != nil || usable.ID != job.ID {
		t.Errorf("usable alignment = (%s, %v), want the completed job's", usable.ID, err)
	}
	var oldCount int
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM alignments WHERE id = 'old'`).Scan(&oldCount); err != nil || oldCount != 0 {
		t.Errorf("the superseded alignment survived: (%d, %v)", oldCount, err)
	}

	// A failed completion keeps whatever good alignment existed.
	entry2 := seedAlignmentEntry(t, s, userID, 2)
	if _, _, err := s.EnqueueAlignment(ctx, userID, entry2); err != nil {
		t.Fatalf("enqueue 2: %v", err)
	}
	job2, err := s.ClaimAlignmentJob(ctx, "worker-1")
	if err != nil {
		t.Fatalf("claim 2: %v", err)
	}
	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO alignments (id, entry_id, epub_text_id, state, coverage, mean_confidence, model)
		VALUES ('good', ?, 'text-E2', 'ready', 0.9, 0.9, 'v1')`, entry2); err != nil {
		t.Fatalf("seed good alignment: %v", err)
	}
	if _, _, err := s.CompleteAlignment(ctx, job2.ID, "worker-1",
		models.AlignmentFailed, 0, 0, "", "corrupt audio"); err != nil {
		t.Fatalf("fail 2: %v", err)
	}
	usable2, err := s.AlignmentForEntry(ctx, entry2)
	if err != nil || usable2.ID != "good" {
		t.Errorf("usable alignment after a failure = (%s, %v), want the earlier good one", usable2.ID, err)
	}
	failedJob, err := s.AlignmentJob(ctx, job2.ID)
	if err != nil || failedJob.State != models.AlignmentFailed ||
		failedJob.Error == nil || *failedJob.Error != "corrupt audio" {
		t.Errorf("failed job = (%s, %v), want failed with the worker's reason", failedJob.State, err)
	}
}

func TestClearAlignment(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	userID := newTestUser(t, s, "align@example.com", "aligner")
	entry := seedAlignmentEntry(t, s, userID, 1)
	job, _, err := s.EnqueueAlignment(ctx, userID, entry)
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	claimed, err := s.ClaimAlignmentJob(ctx, "worker-1")
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if _, err := s.AppendAlignmentAnchors(ctx, claimed.ID, "worker-1",
		[]models.AlignmentAnchor{{CharOffset: 0, AudioSeconds: 0}}); err != nil {
		t.Fatalf("anchors: %v", err)
	}

	// Another user's entry is not theirs to clear.
	stranger := newTestUser(t, s, "stranger@example.com", "stranger")
	if err := s.ClearAlignment(ctx, stranger, entry); !errors.Is(err, ErrNotFound) {
		t.Errorf("clear as stranger = %v, want ErrNotFound", err)
	}

	if err := s.ClearAlignment(ctx, userID, entry); err != nil {
		t.Fatalf("clear: %v", err)
	}
	for _, table := range []string{"alignment_jobs", "alignments", "alignment_anchors", "transcript_segments"} {
		var n int
		if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+table).Scan(&n); err != nil || n != 0 {
			t.Errorf("%s after clear = (%d, %v), want empty", table, n, err)
		}
	}
	_ = job

	// And the entry can be enqueued again from scratch.
	if _, _, err := s.EnqueueAlignment(ctx, userID, entry); err != nil {
		t.Fatalf("re-enqueue after clear: %v", err)
	}
}
