package worker

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/collinpendleton/backhog/align/internal/api"
)

// heartbeat keeps a claim alive while the CPU is busy. Transcribing a
// ten-minute chunk can take minutes with nothing to report; without this
// the API's reclaim pass would eventually decide the worker had died and
// hand the book to someone else mid-sentence.
type heartbeat struct {
	stopOnce sync.Once
	done     chan struct{}
	finished chan struct{}
}

func (h *heartbeat) stop() {
	h.stopOnce.Do(func() { close(h.done) })
	<-h.finished
}

// startHeartbeat pushes the tracker's current stage to the API on a
// ticker. If the API says the claim is gone — reclaimed after a stall,
// or cancelled by the user — it cancels the job context, which unwinds
// the transcription instead of letting two workers write the same
// transcript.
func (w *Worker) startHeartbeat(ctx context.Context, cancel context.CancelFunc, log *slog.Logger, jobID string) *heartbeat {
	h := &heartbeat{done: make(chan struct{}), finished: make(chan struct{})}

	go func() {
		defer close(h.finished)
		ticker := time.NewTicker(w.cfg.HeartbeatInterval)
		defer ticker.Stop()

		for {
			select {
			case <-h.done:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
			}

			status := w.tracker.Get()
			state := status.State
			if state == Idle || state == "" {
				state = api.StateTranscribing
			}
			progress, stage := status.Progress, status.Stage
			if err := w.api.Progress(ctx, jobID, state, &progress, &stage); err != nil {
				if ctx.Err() != nil {
					return
				}
				if api.ClaimLost(err) {
					log.Warn("heartbeat rejected; this worker no longer holds the claim", "error", err)
					cancel()
					return
				}
				log.Warn("heartbeat failed", "error", err)
			}
		}
	}()

	return h
}
