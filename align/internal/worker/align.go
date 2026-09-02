package worker

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/collinpendleton/backhog/align/internal/align"
	"github.com/collinpendleton/backhog/align/internal/api"
)

// maxCanonicalBytes bounds what will be read as a book. The largest
// canonical texts in a library are a couple of megabytes; anything past
// this is not an EPUB's text and reading it would only be a way to run the
// container out of memory.
const maxCanonicalBytes = 64 << 20

// outcome is what a finished job has to say for itself: the terminal
// state, the two numbers the API stores on the alignment, and a line of
// detail for the log.
type outcome struct {
	State      string
	Coverage   float64
	Confidence float64
	Segments   int
	Anchors    int
	Detail     string
}

// alignTranscript maps the transcript onto the book and streams the
// resulting anchors back. Whether the result is published as 'ready' or
// kept as 'low_confidence' is decided here, from coverage and mean
// confidence, and a result below the thresholds still has its anchors
// stored: a partial map of a book is genuinely useful, it simply must not
// be presented as a whole one.
func (w *Worker) alignTranscript(ctx context.Context, log *slog.Logger, claim *api.Claim, segments []api.Segment) (outcome, error) {
	canonical, err := readCanonical(claim.EpubTextPath)
	if err != nil {
		return outcome{}, err
	}
	if len(segments) == 0 {
		return outcome{}, failure{
			Code:   failTranscribe,
			Detail: "the transcript came back empty, so there is nothing to align",
		}
	}

	w.setStage(ctx, log, claim.Job.ID, api.StateAligning, "locating narration in the book", 0)

	in := make([]align.Segment, len(segments))
	for i, s := range segments {
		in[i] = align.Segment{AudioStart: s.AudioStart, AudioEnd: s.AudioEnd, Text: s.Text}
	}

	opts := align.DefaultOptions()
	opts.Progress = func(fraction float64, stage string) {
		w.setStage(ctx, log, claim.Job.ID, api.StateAligning, stage, fraction)
	}

	started := time.Now()
	result := align.Align(canonical, in, opts)
	log.Info("aligned transcript",
		"anchors", len(result.Anchors),
		"coverage", result.Coverage,
		"confidence", result.MeanConfidence,
		"windows", result.Stats.Windows,
		"located", result.Stats.LocatedWindows,
		"elapsed", time.Since(started).Round(time.Millisecond))

	if err := w.uploadAnchors(ctx, claim.Job.ID, result.Anchors); err != nil {
		return outcome{}, err
	}

	out := outcome{
		State:      api.StateReady,
		Coverage:   result.Coverage,
		Confidence: result.MeanConfidence,
		Segments:   len(segments),
		Anchors:    len(result.Anchors),
	}
	switch {
	case len(result.Anchors) == 0:
		out.State = api.StateLowConfidence
		out.Detail = fmt.Sprintf(
			"transcribed %d segments with %s, but none of the narration could be located in the book — "+
				"the audio and the ebook are probably not the same edition",
			len(segments), w.cfg.ModelName)
	case result.Coverage < w.cfg.MinCoverage:
		out.State = api.StateLowConfidence
		out.Detail = fmt.Sprintf(
			"%d anchors covering %.1f%% of the book, below the %.0f%% needed to publish an alignment — "+
				"an abridged reading or a different edition would look like this",
			len(result.Anchors), result.Coverage*100, w.cfg.MinCoverage*100)
	case result.MeanConfidence < w.cfg.MinConfidence:
		out.State = api.StateLowConfidence
		out.Detail = fmt.Sprintf(
			"%d anchors at %.2f mean confidence, below the %.2f needed to publish an alignment — "+
				"the transcript is probably too poor to trust word for word",
			len(result.Anchors), result.MeanConfidence, w.cfg.MinConfidence)
	default:
		out.Detail = fmt.Sprintf(
			"%d anchors from %d segments with %s, covering %.1f%% of the book at %.2f mean confidence",
			len(result.Anchors), len(segments), w.cfg.ModelName,
			result.Coverage*100, result.MeanConfidence)
	}
	return out, nil
}

// uploadAnchors streams the anchor map back in batches, the same way the
// transcript went up: each batch is a single transaction on the API's side
// and refreshes the heartbeat on the way through.
func (w *Worker) uploadAnchors(ctx context.Context, jobID string, anchors []align.Anchor) error {
	size := max(w.cfg.AnchorBatch, 1)
	batch := make([]api.Anchor, 0, min(size, len(anchors)))
	for start := 0; start < len(anchors); start += size {
		if err := ctx.Err(); err != nil {
			return err
		}
		batch = batch[:0]
		for _, a := range anchors[start:min(start+size, len(anchors))] {
			batch = append(batch, api.Anchor{
				CharOffset:   a.CharOffset,
				AudioSeconds: a.AudioSeconds,
				Confidence:   a.Confidence,
			})
		}
		if err := w.api.Anchors(ctx, jobID, batch); err != nil {
			return err
		}
	}
	return nil
}

// readCanonical loads the canonical text the API pointed at. The bytes are
// used exactly as they are found — not re-normalized, not trimmed —
// because every anchor offset is measured in them, and so is every reader
// position already stored against this book.
func readCanonical(path string) (string, error) {
	if path == "" {
		return "", failure{
			Code:   failInternal,
			Detail: "the claim carried no canonical text path",
		}
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", failure{
			Code:   failEpubText,
			Detail: fmt.Sprintf("canonical text %s is not readable: %v", path, err),
		}
	}
	if info.Size() > maxCanonicalBytes {
		return "", failure{
			Code:   failEpubText,
			Detail: fmt.Sprintf("canonical text %s is %d bytes, which is not a book", path, info.Size()),
		}
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", failure{
			Code:   failEpubText,
			Detail: fmt.Sprintf("canonical text %s is not readable: %v", path, err),
		}
	}
	if len(raw) == 0 {
		return "", failure{
			Code:   failEpubText,
			Detail: fmt.Sprintf("canonical text %s is empty; the EPUB may not have been parsed yet", path),
		}
	}
	return string(raw), nil
}
