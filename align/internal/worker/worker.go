// Package worker is the alignment worker's loop: claim a job, decode and
// transcribe its audiobook chunk by chunk, stream the timestamped
// transcript back, and report a terminal state. It owns no database and
// writes nothing to the media it reads.
package worker

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/collinpendleton/backhog/align/internal/api"
	"github.com/collinpendleton/backhog/align/internal/audio"
	"github.com/collinpendleton/backhog/align/internal/config"
	"github.com/collinpendleton/backhog/align/internal/transcribe"
)

// Worker is one claim-holding process. Exactly one job runs at a time:
// transcription already saturates the CPU, and a second concurrent book
// would only make both slower while doubling the chance of a stale
// heartbeat.
type Worker struct {
	cfg     config.Config
	api     *api.Client
	ffmpeg  audio.FFmpeg
	whisper transcribe.Whisper
	log     *slog.Logger
	tracker *tracker
}

// New wires a worker from its configuration.
func New(cfg config.Config, log *slog.Logger) *Worker {
	return &Worker{
		cfg: cfg,
		api: api.New(cfg.APIURL, cfg.Token, cfg.WorkerID),
		ffmpeg: audio.FFmpeg{
			Bin:   cfg.FFmpegBin,
			Probe: cfg.FFprobeBin,
		},
		whisper: transcribe.Whisper{
			Bin:       cfg.WhisperBin,
			ModelPath: cfg.WhisperModel,
			Language:  cfg.Language,
			Threads:   cfg.Threads,
			BeamSize:  cfg.BeamSize,
		},
		log:     log,
		tracker: newTracker(cfg.WorkerID, cfg.ModelName),
	}
}

// Preflight checks everything the worker needs before it claims work:
// the two external binaries, the model file, and a writable scratch
// directory. Finding any of these missing an hour into a book would be a
// waste of an hour.
func (w *Worker) Preflight() error {
	if err := w.ffmpeg.Check(); err != nil {
		return err
	}
	if err := w.whisper.Check(); err != nil {
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
	w.log.Info("alignment worker ready",
		"worker", w.cfg.WorkerID,
		"api", w.cfg.APIURL,
		"model", w.cfg.ModelName,
		"threads", w.cfg.Threads,
		"chunk_seconds", w.cfg.ChunkSeconds)

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
	log.Info("claimed alignment job", "tracks", len(claim.Tracks))
	w.tracker.startJob(job.ID, job.EntryID)

	started := time.Now()
	segments, err := w.transcribe(ctx, log, claim)
	switch {
	case err == nil:
		// Transcription is the expensive half and it is done. The
		// forced-alignment stage that turns this transcript into
		// char-offset anchors is not part of this worker yet, so the job
		// finishes as a usable-but-unmapped result rather than sitting
		// claimed until the reclaim pass burns its attempts.
		detail := fmt.Sprintf(
			"transcribed %d segments with %s; forced alignment is not available in this worker build, so no anchors were produced",
			segments, w.cfg.ModelName)
		log.Info("transcription complete",
			"segments", segments, "elapsed", time.Since(started).Round(time.Second))
		w.complete(ctx, log, job.ID, api.StateLowConfidence, 0, 0, detail)
		w.tracker.finishJob("")

	case ctx.Err() != nil:
		// Shutting down mid-book. Say nothing to the API: leaving the
		// claim to go stale is exactly what the reclaim pass is for, and
		// it keeps the attempt available for the next worker.
		log.Info("shutting down mid-job; leaving the claim to be reclaimed")
		w.tracker.finishJob("interrupted")

	case api.ClaimLost(err):
		log.Warn("claim lost mid-job; abandoning", "error", err)
		w.tracker.finishJob("claim lost")

	default:
		f := toFailure(err, "transcription")
		log.Error("alignment job failed", "code", f.Code, "detail", f.Detail)
		w.complete(ctx, log, job.ID, api.StateFailed, 0, 0, f.Error())
		w.tracker.finishJob(f.Error())
	}
}

// complete is the last call of a job. A failure to report completion is
// logged and dropped: the job is already finished on this side, and the
// reclaim pass will tidy up the queue's view of it.
func (w *Worker) complete(ctx context.Context, log *slog.Logger, jobID, state string, coverage, confidence float64, detail string) {
	// Completion runs on its own deadline so a cancelled run still gets
	// a chance to close the book it just spent an hour on.
	closeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()

	failureText := ""
	if state == api.StateFailed {
		failureText = detail
	}
	err := w.api.Complete(closeCtx, jobID, state, coverage, confidence, w.cfg.ModelName, failureText)
	if err != nil {
		log.Error("could not report job completion", "state", state, "error", err)
		return
	}
	if state != api.StateFailed {
		// The detail is worth keeping in the log even though the job
		// row only carries an error string on failure.
		log.Info("job completed", "state", state, "detail", detail)
	}
}

// transcribe walks the book: every track, every chunk, decode then
// transcribe then upload. It returns the number of segments stored.
func (w *Worker) transcribe(ctx context.Context, log *slog.Logger, claim *api.Claim) (int, error) {
	tracks, err := planTracks(claim.Tracks)
	if err != nil {
		return 0, err
	}

	jobDir := filepath.Join(w.cfg.WorkDir, claim.Job.ID)
	if err := os.MkdirAll(jobDir, 0o755); err != nil {
		return 0, failure{Code: failInternal, Detail: "could not create a scratch directory: " + err.Error()}
	}
	// Every chunk cleans up after itself; this is the belt-and-braces
	// pass for a job that failed partway through one.
	defer os.RemoveAll(jobDir)

	jobCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	beat := w.startHeartbeat(jobCtx, cancel, log, claim.Job.ID)
	defer beat.stop()

	total := 0.0
	for _, t := range tracks {
		total += t.Duration
	}

	uploader := &batcher{
		api:   w.api,
		jobID: claim.Job.ID,
		size:  w.cfg.SegmentBatch,
	}

	w.setStage(jobCtx, log, claim.Job.ID, api.StateTranscribing, "starting transcription", 0)

	done := 0.0
	for _, t := range tracks {
		duration := t.Duration
		if !t.Measured {
			// Only the final track can reach here (planTracks refuses
			// any earlier one). Its global offset is already correct, so
			// probing locally to drive the chunk loop is safe.
			probed, err := w.ffmpeg.Duration(jobCtx, t.Path)
			if err != nil {
				return uploader.count, err
			}
			duration = probed
			total += probed
			log.Warn("final track had no measured duration; probed it locally",
				"track", t.Number, "seconds", probed)
		}

		chunks := planChunks(duration, w.cfg.ChunkSeconds, w.cfg.OverlapSeconds)
		for _, c := range chunks {
			if err := jobCtx.Err(); err != nil {
				return uploader.count, err
			}
			stage := fmt.Sprintf("transcribing track %d/%d, chunk %d/%d",
				t.Number, len(tracks), c.Index+1, c.Count)
			w.setStage(jobCtx, log, claim.Job.ID, api.StateTranscribing, stage, fraction(done, total))

			segments, err := w.chunk(jobCtx, jobDir, t, c)
			if err != nil {
				return uploader.count, err
			}
			mapped := globalSegments(segments, t, c, duration)
			if err := uploader.add(jobCtx, mapped); err != nil {
				return uploader.count, err
			}

			done += c.Kept()
			log.Debug("chunk transcribed",
				"track", t.Number, "chunk", c.Index+1, "segments", len(mapped))
		}
	}

	if err := uploader.flush(jobCtx); err != nil {
		return uploader.count, err
	}
	w.setStage(jobCtx, log, claim.Job.ID, api.StateTranscribing, "transcription complete", 1)
	return uploader.count, nil
}

// chunk decodes and transcribes one slice, deleting the PCM as soon as
// Whisper is done with it. Peak scratch usage is therefore one chunk —
// about 19 MB at the default ten minutes — regardless of book length.
func (w *Worker) chunk(ctx context.Context, jobDir string, t track, c chunk) ([]transcribe.Segment, error) {
	prefix := filepath.Join(jobDir, fmt.Sprintf("t%03d-c%05d", t.Number, c.Index))
	wav := prefix + ".wav"
	defer os.Remove(wav)

	if err := w.ffmpeg.ExtractPCM(ctx, t.Path, wav, c.DecodeStart, c.DecodeDuration); err != nil {
		return nil, err
	}

	runCtx, cancel := context.WithTimeout(ctx, w.cfg.WhisperTimeout)
	defer cancel()
	segments, err := w.whisper.Transcribe(runCtx, wav, prefix)
	if err != nil {
		if ctx.Err() == nil && errors.Is(err, context.DeadlineExceeded) {
			return nil, failure{
				Code: failTranscribe,
				Detail: fmt.Sprintf("track %d chunk %d took longer than %s to transcribe",
					t.Number, c.Index+1, w.cfg.WhisperTimeout),
			}
		}
		return nil, err
	}
	return segments, nil
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

func fraction(done, total float64) float64 {
	if total <= 0 {
		return 0
	}
	return clamp(done/total, 0, 1)
}
