package store

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/collinpendleton/backhog/api/internal/models"
)

// seedPagedEntry builds one comic book entry: a book, a user's library
// entry, an attached PDF as the designated primary text, and its
// image-native classification row. idx keeps multiple entries distinct.
func seedPagedEntry(t *testing.T, s *Store, userID string, idx int, classification string) (entryID string, mediaFileID int64) {
	t.Helper()
	ctx := context.Background()
	suffix := fmt.Sprintf("C%d", idx)
	bookID := "OL" + suffix
	entryID = "comic-" + suffix
	mediaFileID = int64(3000 + idx)

	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := s.db.ExecContext(ctx, q, args...); err != nil {
			t.Fatalf("seed %q: %v", q, err)
		}
	}
	exec(`INSERT INTO books (id, title) VALUES (?, 'Pictures `+suffix+`')`, bookID)
	exec(`INSERT INTO library_entries (id, user_id, media_type, book_id, status)
	      VALUES (?, ?, 'book', ?, 'backlog')`, entryID, userID, bookID)
	exec(`INSERT INTO media_files (id, root, path, kind, size_bytes, mtime, book_id, is_primary_text, scanned_at)
	      VALUES (?, '/nas', 'x/`+suffix+`.pdf', 'epub', 10, 1, ?, 1, CURRENT_TIMESTAMP)`, mediaFileID, bookID)
	exec(`INSERT INTO pdf_files (id, media_file_id, classification, page_count, has_text_layer, reason, parser_version)
	      VALUES (?, ?, ?, 4, 0, '', 'v1')`, "pdf-"+suffix, mediaFileID, classification)
	return entryID, mediaFileID
}

func TestEnqueueOCRIsIdempotentPerFile(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	userID := newTestUser(t, s, "ocr@example.com", "ocrer")
	entry, fileID := seedPagedEntry(t, s, userID, 1, models.PDFImageNative)

	job, existed, err := s.EnqueueOCR(ctx, userID, entry, fileID, "v1")
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if existed || job.State != models.OCRQueued || job.Attempts != 0 {
		t.Fatalf("first enqueue = (existed %v, %s), want a fresh queued job", existed, job.State)
	}

	// Hammering the button cannot stack runs.
	again, existed, err := s.EnqueueOCR(ctx, userID, entry, fileID, "v1")
	if err != nil || !existed || again.ID != job.ID {
		t.Fatalf("re-enqueue = (%s, existed %v, %v), want the same active job", again.ID, existed, err)
	}

	// Someone else's entry is an unknown one.
	stranger := newTestUser(t, s, "ocrstranger@example.com", "stranger")
	if _, _, err := s.EnqueueOCR(ctx, stranger, entry, fileID, "v1"); !errors.Is(err, ErrNotFound) {
		t.Errorf("enqueue on another user's entry = %v, want ErrNotFound", err)
	}
}

func TestClaimOCRJobAndWorkerWrites(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	userID := newTestUser(t, s, "ocr@example.com", "ocrer")
	entry, fileID := seedPagedEntry(t, s, userID, 1, models.PDFImageNative)
	if _, _, err := s.EnqueueOCR(ctx, userID, entry, fileID, "v1"); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	job, err := s.ClaimOCRJob(ctx, "worker-a")
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if job.State != models.OCRClaimed || job.Attempts != 1 || job.ClaimedBy == nil || *job.ClaimedBy != "worker-a" {
		t.Fatalf("claim = %s/%d/%v, want claimed/1/worker-a", job.State, job.Attempts, job.ClaimedBy)
	}

	// The queue is empty for a second worker while the job is active.
	if _, err := s.ClaimOCRJob(ctx, "worker-b"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second claim = %v, want ErrNotFound", err)
	}

	// Only the claim holder may write.
	if _, err := s.OCRJobProgress(ctx, job.ID, "worker-b", models.OCROcring, nil, nil); !errors.Is(err, ErrJobNotClaimedBy) {
		t.Fatalf("stranger progress = %v, want ErrJobNotClaimedBy", err)
	}

	stage := "reading page 2/4"
	half := 0.5
	job, err = s.OCRJobProgress(ctx, job.ID, "worker-a", models.OCROcring, &half, &stage)
	if err != nil {
		t.Fatalf("progress: %v", err)
	}
	if job.State != models.OCROcring || job.Progress != half || job.StageDetail != stage {
		t.Fatalf("progress = %s/%v/%q", job.State, job.Progress, job.StageDetail)
	}
	if job.HeartbeatAt == nil {
		t.Fatal("progress did not refresh the heartbeat")
	}

	// Pages upsert: sending page 2 twice replaces rather than stacks.
	pages := []models.OCRPage{
		{PageNumber: 1, ImageSHA256: "sha-1", OCRVersion: "1", Text: "WE WERE LEAN", MeanConfidence: 0.9},
		{PageNumber: 2, ImageSHA256: "sha-2", OCRVersion: "1", Text: "AND HUNGRY", MeanConfidence: 0.8},
	}
	if _, err := s.AppendOCRPages(ctx, job.ID, "worker-a", pages); err != nil {
		t.Fatalf("append pages: %v", err)
	}
	replaced := []models.OCRPage{{PageNumber: 2, ImageSHA256: "sha-2b", OCRVersion: "1", Text: "AND VERY HUNGRY", MeanConfidence: 0.7}}
	if _, err := s.AppendOCRPages(ctx, job.ID, "worker-a", replaced); err != nil {
		t.Fatalf("replace page: %v", err)
	}
	stored, err := s.OCRPagesForMediaFile(ctx, fileID)
	if err != nil {
		t.Fatalf("load pages: %v", err)
	}
	if len(stored) != 2 || stored[1].Text != "AND VERY HUNGRY" || stored[1].ImageSHA256 != "sha-2b" {
		t.Fatalf("stored pages = %#v", stored)
	}

	// A stranger cannot complete either.
	if _, err := s.CompleteOCRJob(ctx, job.ID, "worker-b", "tesseract 5 (eng)", "", 0.3, 0.6); !errors.Is(err, ErrJobNotClaimedBy) {
		t.Fatalf("stranger complete = %v, want ErrJobNotClaimedBy", err)
	}
}

func TestCompleteOCRJobGradesTheCorpus(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	userID := newTestUser(t, s, "ocr@example.com", "ocrer")
	entry, fileID := seedPagedEntry(t, s, userID, 1, models.PDFImageNative)
	if _, _, err := s.EnqueueOCR(ctx, userID, entry, fileID, "v1"); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	job, err := s.ClaimOCRJob(ctx, "worker-a")
	if err != nil {
		t.Fatalf("claim: %v", err)
	}

	// 2 of 4 pages carry lettering; mean confidence 0.85. Coverage 0.5
	// and confidence 0.85 against thresholds (0.3, 0.6): ready.
	lettered := []models.OCRPage{
		{PageNumber: 1, ImageSHA256: "a", OCRVersion: "1", Text: "words", MeanConfidence: 0.9},
		{PageNumber: 2, ImageSHA256: "b", OCRVersion: "1", Text: "more words", MeanConfidence: 0.8},
		{PageNumber: 3, ImageSHA256: "c", OCRVersion: "1", Text: "", MeanConfidence: 0},
	}
	if _, err := s.AppendOCRPages(ctx, job.ID, "worker-a", lettered); err != nil {
		t.Fatalf("append: %v", err)
	}
	done, err := s.CompleteOCRJob(ctx, job.ID, "worker-a", "tesseract 5.3.0 (eng)", "", 0.3, 0.6)
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	if done.State != models.OCRReady {
		t.Fatalf("state = %s, want ready", done.State)
	}
	if done.Coverage != 0.5 {
		t.Errorf("coverage = %v, want 0.5", done.Coverage)
	}
	if done.MeanConfidence < 0.84 || done.MeanConfidence > 0.86 {
		t.Errorf("mean confidence = %v, want 0.85", done.MeanConfidence)
	}
	if done.Model != "tesseract 5.3.0 (eng)" {
		t.Errorf("model = %q", done.Model)
	}

	// A terminal job refuses further writes.
	if _, err := s.OCRJobProgress(ctx, job.ID, "worker-a", "", nil, nil); !errors.Is(err, ErrJobTerminal) {
		t.Fatalf("write after terminal = %v, want ErrJobTerminal", err)
	}

	// Below the confidence threshold the corpus stays usable but flagged.
	entry2, file2 := seedPagedEntry(t, s, userID, 2, models.PDFImageNative)
	if _, _, err := s.EnqueueOCR(ctx, userID, entry2, file2, "v1"); err != nil {
		t.Fatalf("enqueue 2: %v", err)
	}
	job2, err := s.ClaimOCRJob(ctx, "worker-a")
	if err != nil {
		t.Fatalf("claim 2: %v", err)
	}
	if _, err := s.AppendOCRPages(ctx, job2.ID, "worker-a", []models.OCRPage{
		{PageNumber: 1, ImageSHA256: "a", OCRVersion: "1", Text: "mumble", MeanConfidence: 0.4},
	}); err != nil {
		t.Fatalf("append 2: %v", err)
	}
	done2, err := s.CompleteOCRJob(ctx, job2.ID, "worker-a", "tesseract 5.3.0 (eng)", "", 0.3, 0.6)
	if err != nil {
		t.Fatalf("complete 2: %v", err)
	}
	if done2.State != models.OCRLowConfidence {
		t.Fatalf("state = %s, want low_confidence", done2.State)
	}

	// An error message fails the run with the reason kept.
	entry3, file3 := seedPagedEntry(t, s, userID, 3, models.PDFImageNative)
	if _, _, err := s.EnqueueOCR(ctx, userID, entry3, file3, "v1"); err != nil {
		t.Fatalf("enqueue 3: %v", err)
	}
	job3, err := s.ClaimOCRJob(ctx, "worker-a")
	if err != nil {
		t.Fatalf("claim 3: %v", err)
	}
	done3, err := s.CompleteOCRJob(ctx, job3.ID, "worker-a", "", "the pages vanished", 0.3, 0.6)
	if err != nil {
		t.Fatalf("complete 3: %v", err)
	}
	if done3.State != models.OCRFailed || done3.Error == nil || *done3.Error != "the pages vanished" {
		t.Fatalf("failed job = %s / %v", done3.State, done3.Error)
	}

	// A usable completion must name its model.
	entry4, file4 := seedPagedEntry(t, s, userID, 4, models.PDFImageNative)
	if _, _, err := s.EnqueueOCR(ctx, userID, entry4, file4, "v1"); err != nil {
		t.Fatalf("enqueue 4: %v", err)
	}
	job4, err := s.ClaimOCRJob(ctx, "worker-a")
	if err != nil {
		t.Fatalf("claim 4: %v", err)
	}
	if _, err := s.CompleteOCRJob(ctx, job4.ID, "worker-a", "  ", "", 0.3, 0.6); err == nil {
		t.Fatal("empty model accepted for a usable corpus")
	}
}

func TestReclaimStaleOCRJobs(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	userID := newTestUser(t, s, "ocr@example.com", "ocrer")
	entry, fileID := seedPagedEntry(t, s, userID, 1, models.PDFImageNative)
	if _, _, err := s.EnqueueOCR(ctx, userID, entry, fileID, "v1"); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	job, err := s.ClaimOCRJob(ctx, "worker-a")
	if err != nil {
		t.Fatalf("claim: %v", err)
	}

	// Age the heartbeat past the cutoff; the next claim reclaims it.
	past := time.Now().UTC().Add(-2 * OCRStaleAfter)
	if _, err := s.db.ExecContext(ctx,
		`UPDATE ocr_jobs SET heartbeat_at = ? WHERE id = ?`, past, job.ID); err != nil {
		t.Fatalf("age heartbeat: %v", err)
	}
	reclaimed, err := s.ClaimOCRJob(ctx, "worker-b")
	if err != nil {
		t.Fatalf("reclaim claim: %v", err)
	}
	if reclaimed.ID != job.ID || reclaimed.Attempts != 2 || reclaimed.ClaimedBy == nil || *reclaimed.ClaimedBy != "worker-b" {
		t.Fatalf("reclaimed = %s/%d/%v", reclaimed.ID, reclaimed.Attempts, reclaimed.ClaimedBy)
	}

	// The ledger rows from the dead attempt survive the reclaim: each is
	// pinned to its image hash and stays valid whoever re-reads it.
	if _, err := s.AppendOCRPages(ctx, reclaimed.ID, "worker-b", []models.OCRPage{
		{PageNumber: 1, ImageSHA256: "a", OCRVersion: "1", Text: "kept", MeanConfidence: 0.9},
	}); err != nil {
		t.Fatalf("append after reclaim: %v", err)
	}
	pages, err := s.OCRPagesForMediaFile(ctx, fileID)
	if err != nil || len(pages) != 1 || pages[0].Text != "kept" {
		t.Fatalf("pages after reclaim = %#v err %v", pages, err)
	}

	// Out of attempts, a stale claim fails loudly. Walk the remaining
	// attempts: age the claim, let the next claim reclaim it, until the
	// cap refuses to hand it back.
	job = reclaimed
	for attempt := job.Attempts; attempt < OCRMaxAttempts; attempt++ {
		if _, err := s.db.ExecContext(ctx,
			`UPDATE ocr_jobs SET heartbeat_at = ? WHERE id = ?`, past, job.ID); err != nil {
			t.Fatalf("age %d: %v", attempt, err)
		}
		j, err := s.ClaimOCRJob(ctx, "worker-x")
		if err != nil {
			t.Fatalf("claim %d: %v", attempt, err)
		}
		if j.ID != job.ID {
			t.Fatalf("claim %d picked %s, want the same job", attempt, j.ID)
		}
		job = j
	}
	if _, err := s.db.ExecContext(ctx,
		`UPDATE ocr_jobs SET heartbeat_at = ? WHERE id = ?`, past, job.ID); err != nil {
		t.Fatalf("final age: %v", err)
	}
	if n, err := s.ReclaimStaleOCRJobs(ctx); err != nil || n == 0 {
		t.Fatalf("final reclaim = %d, %v", n, err)
	}
	job, err = s.OCRJob(ctx, job.ID)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if job.State != models.OCRFailed || job.Error == nil {
		t.Fatalf("exhausted job = %s / %v", job.State, job.Error)
	}
	if job.Attempts != OCRMaxAttempts {
		t.Fatalf("attempts = %d, want %d", job.Attempts, OCRMaxAttempts)
	}
}

func TestOCRCorpusAndClear(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	userID := newTestUser(t, s, "ocr@example.com", "ocrer")
	entry, fileID := seedPagedEntry(t, s, userID, 1, models.PDFImageNative)

	// No corpus yet: the honest nothing.
	corpus, err := s.OCRCorpusForMediaFile(ctx, fileID)
	if err != nil || corpus.State != "" {
		t.Fatalf("empty corpus = %#v err %v", corpus, err)
	}

	if _, _, err := s.EnqueueOCR(ctx, userID, entry, fileID, "v1"); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	job, err := s.ClaimOCRJob(ctx, "worker-a")
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if _, err := s.AppendOCRPages(ctx, job.ID, "worker-a", []models.OCRPage{
		{PageNumber: 1, ImageSHA256: "a", OCRVersion: "1", Text: "words", MeanConfidence: 0.9},
		{PageNumber: 2, ImageSHA256: "b", OCRVersion: "1", Text: "more", MeanConfidence: 0.9},
	}); err != nil {
		t.Fatalf("append: %v", err)
	}
	// A revision exists and changes with the corpus.
	rev1, err := s.OCRCorpusRevision(ctx, fileID)
	if err != nil || rev1 == "" {
		t.Fatalf("revision = %q err %v", rev1, err)
	}
	if _, err := s.AppendOCRPages(ctx, job.ID, "worker-a", []models.OCRPage{
		{PageNumber: 3, ImageSHA256: "c", OCRVersion: "1", Text: "another page", MeanConfidence: 0.9},
	}); err != nil {
		t.Fatalf("second append: %v", err)
	}
	rev2, err := s.OCRCorpusRevision(ctx, fileID)
	if err != nil {
		t.Fatalf("revision 2: %v", err)
	}
	if rev1 == rev2 {
		t.Fatalf("revision did not change after a corpus write: %q", rev1)
	}

	if _, err := s.CompleteOCRJob(ctx, job.ID, "worker-a", "tesseract 5.3.0 (eng)", "", 0.3, 0.6); err != nil {
		t.Fatalf("complete: %v", err)
	}

	corpus, err = s.OCRCorpusForMediaFile(ctx, fileID)
	if err != nil {
		t.Fatalf("corpus: %v", err)
	}
	if corpus.State != models.OCRReady || corpus.PagesWithText != 3 || corpus.PageCount != 4 {
		t.Fatalf("corpus = %#v", corpus)
	}

	// The clear drops jobs and pages both.
	if err := s.ClearOCR(ctx, userID, entry); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if pages, err := s.OCRPagesForMediaFile(ctx, fileID); err != nil || len(pages) != 0 {
		t.Fatalf("pages after clear = %d err %v", len(pages), err)
	}
	if j, err := s.OCRJobForMediaFile(ctx, fileID); err != nil || j.ID != "" {
		t.Fatalf("job after clear = %s err %v", j.ID, err)
	}
}

func TestPrimaryPagedMediaFile(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	userID := newTestUser(t, s, "ocr@example.com", "ocrer")
	entry, fileID := seedPagedEntry(t, s, userID, 1, models.PDFImageNative)

	f, ok, err := s.PrimaryPagedMediaFile(ctx, userID, entry)
	if err != nil || !ok || f.ID != fileID {
		t.Fatalf("paged primary = (file %d, %v, %v), want (%d, true)", f.ID, ok, err, fileID)
	}

	// A text-native classification answers "no paged primary" — ErrNotFound
	// is the shared honest no for both a text-mode book and an unknown
	// entry; the caller's 422 says which.
	textEntry, _ := seedPagedEntry(t, s, userID, 2, models.PDFTextNative)
	if _, ok, err := s.PrimaryPagedMediaFile(ctx, userID, textEntry); ok || !errors.Is(err, ErrNotFound) {
		t.Fatalf("text-native primary = ok %v err %v, want false/ErrNotFound", ok, err)
	}

	// So does an unknown entry.
	if _, ok, err := s.PrimaryPagedMediaFile(ctx, userID, "nope"); ok || !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown entry = (%v, %v), want false/ErrNotFound", ok, err)
	}
}
