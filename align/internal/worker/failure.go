package worker

import (
	"errors"
	"fmt"

	"github.com/collinpendleton/backhog/align/internal/audio"
	"github.com/collinpendleton/backhog/align/internal/transcribe"
)

// The failure codes a job can end on. They exist so that the things that
// actually go wrong here — a mount that isn't mounted, a book whose
// timeline cannot be trusted, a file ffmpeg cannot read, a model that will
// not load, a canonical text that was never parsed — are distinguishable
// in the job's error column instead of collapsing into one unhelpful
// "alignment failed".
const (
	failMediaMissing     = "media_missing"
	failTimelineDegraded = "timeline_degraded"
	failDecode           = "decode_failed"
	failModelUnavailable = "model_unavailable"
	failTranscribe       = "transcribe_failed"
	failEpubText         = "epub_text_unreadable"
	failInternal         = "worker_error"
)

// failure is a terminal job error with a machine-readable code. It is
// what gets written to alignment_jobs.error, so it has to read well to a
// person looking at a stuck book and be greppable in a log.
type failure struct {
	Code   string
	Detail string
}

func (f failure) Error() string { return f.Code + ": " + f.Detail }

// toFailure maps the errors the decode and transcription packages
// produce onto job failures, and gives anything unrecognised a code of
// its own rather than mislabelling it.
func toFailure(err error, context string) failure {
	var known failure
	if errors.As(err, &known) {
		return known
	}
	detail := fmt.Sprintf("%s: %v", context, err)
	switch {
	case errors.Is(err, audio.ErrUnreadable):
		return failure{Code: failMediaMissing, Detail: detail}
	case errors.Is(err, audio.ErrDecode), errors.Is(err, audio.ErrToolMissing):
		return failure{Code: failDecode, Detail: detail}
	case errors.Is(err, transcribe.ErrModelLoad), errors.Is(err, transcribe.ErrToolMissing):
		return failure{Code: failModelUnavailable, Detail: detail}
	case errors.Is(err, transcribe.ErrTranscribe):
		return failure{Code: failTranscribe, Detail: detail}
	default:
		return failure{Code: failInternal, Detail: detail}
	}
}
