// Package worker is the OCR lettering worker's loop: claim a job, fetch
// each page's image through the API's own streaming endpoint, read its
// lettering with tesseract, stream the per-page results back, and close
// the job. It owns no database, mounts no volumes, and writes nothing
// anywhere but its own scratch directory.
package worker

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/collinpendleton/backhog/ocr/internal/api"
	"github.com/collinpendleton/backhog/ocr/internal/config"
	"github.com/collinpendleton/backhog/ocr/internal/tesseract"
)

// ocrVersion names this worker's reading pipeline, independent of the
// tesseract version it records per corpus: bump it when anything about
// HOW pages are read changes (preprocessing, PSM policy), and every page
// in every ledger becomes billable again even though the images did not
// change.
const ocrVersion = "1"

// The failure codes a job can end on, the alignment worker's vocabulary.
const (
	failTool     = "tesseract_unavailable"
	failRead     = "ocr_failed"
	failPage     = "page_unreadable"
	failInternal = "worker_error"
)

// failure is a terminal job error with a machine-readable code — what
// gets written to ocr_jobs.error, so it reads well to a person staring at
// a stuck comic and greps in a log.
type failure struct {
	Code   string
	Detail string
}

func (f failure) Error() string { return f.Code + ": " + f.Detail }

func toFailure(err error, context string) failure {
	var known failure
	if errors.As(err, &known) {
		return known
	}
	detail := fmt.Sprintf("%s: %v", context, err)
	switch {
	case errors.Is(err, tesseract.ErrToolMissing):
		return failure{Code: failTool, Detail: detail}
	case errors.Is(err, tesseract.ErrRead):
		return failure{Code: failRead, Detail: detail}
	default:
		return failure{Code: failInternal, Detail: detail}
	}
}

// Worker is one claim-holding process. Exactly one job runs at a time:
// tesseract already saturates a core per page, and a second concurrent
// comic would only blur both progress bars.
type Worker struct {
	cfg   config.Config
	api   *api.Client
	ocr   tesseract.Tesseract
	log   *slog.Logger
	model string
	// tracker holds the current status for the heartbeat and the
	// /status endpoint.
	tracker *tracker
	// batcher groups per-page results before they go up.
	batcher *batcher
}

// New wires a worker from its configuration. model is the checked
// tesseract identity ("tesseract 5.3.0 (eng)") recorded on every corpus.
func New(cfg config.Config, model string, log *slog.Logger) *Worker {
	return &Worker{
		cfg: cfg,
		api: api.New(cfg.APIURL, cfg.Token, cfg.WorkerID),
		ocr: tesseract.Tesseract{
			Bin:      cfg.TesseractBin,
			Language: cfg.Language,
			PSM:      cfg.PSM,
		},
		log:     log,
		model:   model,
		tracker: newTracker(cfg.WorkerID, model),
	}
}

// Preflight checks the one external dependency before any job is
// claimed: discovering a missing tesseract an hour into a comic would be
// a waste of an hour.
func (w *Worker) Preflight() error {
	if _, err := w.ocr.Check(); err != nil {
		return err
	}
	if err := os.MkdirAll(w.cfg.WorkDir, 0o755); err != nil {
		return fmt.Errorf("work directory %s is not usable: %w", w.cfg.WorkDir, err)
	}
	probe, err := os.CreateTemp(w.cfg.WorkDir, "preflight-*")
	if err != nil {
		return fmt.Errorf("work directory %s is not writable: %w", w.cfg.WorkDir, err)
	}
	name := probe.Name()
	_ = probe.Close()
	_ = os.Remove(name)
	return nil
}

// Run polls the queue until the context is cancelled. A claim failure is
// logged and retried on the next tick rather than killing the container:
// the API restarting under a running worker is ordinary, not fatal.
func (w *Worker) Run(ctx context.Context) error {
	w.log.Info("ocr lettering worker ready",
		"worker", w.cfg.WorkerID,
		"api", w.cfg.APIURL,
		"model", w.model,
		"psm", w.cfg.PSM)

	for {
		claim, err := w.api.Claim(ctx)
		switch {
		case ctx.Err() != nil:
			return nil
		case err != nil:
			w.log.Warn("claim failed", "error", err)
		case claim == nil:
			// Empty queue. Nothing to say every fifteen seconds.
		default:
			w.runClaim(ctx, claim)
			continue
		}

		select {
		case <-ctx.Done():
			return nil
		case <-time.After(w.cfg.PollInterval):
		}
	}
}

// runClaim executes one claimed job and reports its outcome. Every path
// out of here either completes the job or deliberately abandons it to
// the API's reclaim pass; a job is never left half-owned.
func (w *Worker) runClaim(ctx context.Context, claim *api.Claim) {
	job := claim.Job
	log := w.log.With("job", job.ID, "entry", job.EntryID, "attempt", job.Attempts)
	log.Info("claimed ocr job", "pages", claim.PageCount, "ledgered", len(claim.Ledger))
	w.tracker.startJob(job.ID, job.EntryID)
	w.batcher = newBatcher(w.api, job.ID, w.cfg.PageBatch)

	jobCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	beat := w.startHeartbeat(jobCtx, cancel, log, job.ID)
	defer beat.stop()

	started := time.Now()
	err := w.process(jobCtx, log, claim)
	switch {
	case err == nil:
		log.Info("ocr complete", "elapsed", time.Since(started).Round(time.Second))
		w.complete(ctx, log, job.ID, "")
		w.tracker.finishJob("")

	case ctx.Err() != nil:
		// Shutting down mid-run. Say nothing to the API: leaving the
		// claim to go stale is exactly what the reclaim pass is for, and
		// every page already uploaded is pinned in the ledger for the
		// next attempt.
		log.Info("shutting down mid-job; leaving the claim to be reclaimed")
		w.tracker.finishJob("interrupted")

	case api.ClaimLost(err), errors.Is(err, context.Canceled):
		log.Warn("claim lost mid-job; abandoning", "error", err)
		w.tracker.finishJob("claim lost")

	default:
		f := toFailure(err, "ocr pass")
		log.Error("ocr job failed", "code", f.Code, "detail", f.Detail)
		w.complete(ctx, log, job.ID, f.Error())
		w.tracker.finishJob(f.Error())
	}
	w.batcher = nil
}

// process walks the book: every page fetched through the API's streaming
// endpoint, the ledger consulted for pages whose images did not change,
// tesseract run over the rest, results streamed up in batches.
func (w *Worker) process(ctx context.Context, log *slog.Logger, claim *api.Claim) error {
	jobDir := filepath.Join(w.cfg.WorkDir, claim.Job.ID)
	if err := os.MkdirAll(jobDir, 0o755); err != nil {
		return failure{Code: failInternal, Detail: "could not create a scratch directory: " + err.Error()}
	}
	// One page image lives on disk at a time; this is the belt-and-braces
	// pass for a run that failed partway through one.
	defer os.RemoveAll(jobDir)

	w.setStage(ctx, log, claim.Job.ID, api.StateOcring, "starting lettering pass", 0)

	for page := 1; page <= claim.PageCount; page++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		stage := fmt.Sprintf("reading page %d/%d", page, claim.PageCount)
		w.setStage(ctx, log, claim.Job.ID, api.StateOcring, stage, float64(page-1)/float64(max(claim.PageCount, 1)))

		img, err := w.api.Page(ctx, claim.Job.ID, page)
		if err != nil {
			if errors.Is(err, api.ErrNoLettering) {
				// A named refusal: this page is vector art, blank, or an
				// unservable codec. It has no lettering to read; the
				// coverage pair will say so honestly. (There is no row to
				// write — the corpus counts what exists, not what doesn't.)
				log.Debug("page skipped: no readable image", "page", page)
				continue
			}
			return failure{Code: failPage, Detail: fmt.Sprintf("fetch page %d: %v", page, err)}
		}

		// The ledger: an unchanged page, already read by this pipeline
		// version, is never re-billed.
		if entry, ok := claim.Ledger[fmt.Sprintf("%d", page)]; ok &&
			entry.ImageSHA256 == img.SHA256 && entry.OCRVersion == ocrVersion {
			log.Debug("page unchanged; ledger hit", "page", page)
			continue
		}

		reading, err := w.readPage(ctx, jobDir, page, img)
		if err != nil {
			return err
		}
		if err := w.batcher.add(ctx, []api.Page{{
			PageNumber:     page,
			ImageSHA256:    img.SHA256,
			Text:           reading.Text,
			MeanConfidence: reading.MeanConfidence,
		}}); err != nil {
			return err
		}
		log.Debug("page read", "page", page, "bytes", len(reading.Text), "confidence", reading.MeanConfidence)
	}

	if err := w.batcher.flush(ctx); err != nil {
		return err
	}
	w.setStage(ctx, log, claim.Job.ID, api.StateOcring, "lettering pass complete", 1)
	return nil
}

// readPage writes one fetched image to scratch and hands it to tesseract.
// The file is removed before returning either way, so a long comic leaves
// no trail of page images behind.
func (w *Worker) readPage(ctx context.Context, jobDir string, page int, img api.PageImage) (tesseract.Reading, error) {
	ext := ".png"
	if strings.Contains(img.ContentType, "jpeg") || strings.Contains(img.ContentType, "jpg") {
		ext = ".jpg"
	}
	path := filepath.Join(jobDir, fmt.Sprintf("p%06d%s", page, ext))
	defer os.Remove(path)

	if err := os.WriteFile(path, img.Data, 0o600); err != nil {
		return tesseract.Reading{}, failure{Code: failInternal, Detail: fmt.Sprintf("write page %d image: %v", page, err)}
	}

	runCtx, cancel := context.WithTimeout(ctx, w.cfg.PageTimeout)
	defer cancel()
	reading, err := w.ocr.Read(runCtx, path)
	if err != nil {
		if ctx.Err() == nil && errors.Is(runCtx.Err(), context.DeadlineExceeded) {
			return tesseract.Reading{}, failure{
				Code:   failRead,
				Detail: fmt.Sprintf("page %d took longer than %s to read", page, w.cfg.PageTimeout),
			}
		}
		return tesseract.Reading{}, err
	}
	return reading, nil
}

// complete is the last call of a job. A failure to report completion is
// logged and dropped: the job is already finished on this side, and the
// reclaim pass will tidy up the queue's view of it.
func (w *Worker) complete(ctx context.Context, log *slog.Logger, jobID, failureText string) {
	// Completion runs on its own deadline so a cancelled run still gets a
	// chance to close the book it just spent an hour on.
	closeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	if err := w.api.Complete(closeCtx, jobID, w.model, failureText); err != nil {
		log.Error("could not report job completion", "error", err)
	}
}

// setStage records progress in one place: the local tracker for the
// status endpoint, and the API for the user watching a progress bar. A
// failed push is not fatal — the heartbeat will try again shortly.
func (w *Worker) setStage(ctx context.Context, log *slog.Logger, jobID, state, stage string, progress float64) {
	w.tracker.setStage(state, stage, progress)
	if err := w.api.Progress(ctx, jobID, state, &progress, &stage); err != nil && ctx.Err() == nil {
		log.Warn("progress update failed", "error", err)
	}
}
