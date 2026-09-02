package http

import (
	"context"
	"crypto/subtle"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	booktext "github.com/collinpendleton/backhog/api/internal/books"
	bookaudio "github.com/collinpendleton/backhog/api/internal/books/audio"
	"github.com/collinpendleton/backhog/api/internal/books/position"
	"github.com/collinpendleton/backhog/api/internal/models"
	"github.com/collinpendleton/backhog/api/internal/store"
)

// The alignment queue's two audiences. Users enqueue and watch through
// /api/books/{entryID}/align with their session; the worker container
// pulls jobs through /internal/align/* with the shared ALIGN_WORKER_TOKEN.
// The internal half is mounted outside /api so the public nginx vhost —
// which proxies exactly /api/ — cannot reach it: a job's inputs include
// absolute library paths, and those are nobody else's business.

// alignmentAnchors is the position translator's live anchor source: the
// audio map is read from the entry's newest usable alignment, and the
// page map stays empty until the page-scan stage lands. An entry with
// no alignment is the normal case, reported as no anchors — not an
// error — exactly as the Provider contract promises.
type alignmentAnchors struct{ store *store.Store }

func (a alignmentAnchors) AudioAnchors(ctx context.Context, entryID string) ([]position.Anchor, error) {
	alignment, err := a.store.AlignmentForEntry(ctx, entryID)
	if err != nil || alignment.ID == "" {
		return nil, err
	}
	stored, err := a.store.AlignmentAnchors(ctx, alignment.ID)
	if err != nil {
		return nil, err
	}
	out := make([]position.Anchor, 0, len(stored))
	for _, s := range stored {
		out = append(out, position.Anchor{
			CharOffset: s.CharOffset,
			Value:      s.AudioSeconds,
			Confidence: s.Confidence,
		})
	}
	return out, nil
}

func (a alignmentAnchors) PageAnchors(context.Context, string) ([]position.Anchor, error) {
	return nil, nil
}

// handleBookAlignEnqueue queues an alignment for one of the caller's
// book entries. The canonical text must exist (parsing it on demand if
// this is the first thing to need it) and an audiobook must be
// attached; alignment joins the two, so an entry missing either half is
// a 422 with the reason, not a job that can never run.
func (s *Server) handleBookAlignEnqueue(w http.ResponseWriter, r *http.Request) {
	userID, entryID, _, ok := s.bookEntry(w, r)
	if !ok {
		return
	}

	if s.epubs == nil {
		fail(w, errorf(http.StatusServiceUnavailable, "epub text storage is unavailable"))
		return
	}
	if _, err := s.epubs.EnsureForEntry(r.Context(), userID, entryID); err != nil {
		if errors.Is(err, booktext.ErrNoEpub) {
			fail(w, errorf(http.StatusUnprocessableEntity,
				"no ebook is attached to this book, so there is no text to align"))
			return
		}
		fail(w, errorf(http.StatusUnprocessableEntity, "the ebook could not be parsed: "+err.Error()))
		return
	}

	job, existed, err := s.store.EnqueueAlignment(r.Context(), userID, entryID)
	switch {
	case errors.Is(err, store.ErrNoAlignmentText):
		fail(w, errorf(http.StatusUnprocessableEntity,
			"no ebook text is parsed for this book, so there is no text to align"))
		return
	case errors.Is(err, store.ErrNoAlignmentAudio):
		fail(w, errorf(http.StatusUnprocessableEntity,
			"no audiobook is attached to this book, so there is nothing to align against"))
		return
	case errors.Is(err, store.ErrNotFound):
		fail(w, errNotFound)
		return
	case err != nil:
		fail(w, err)
		return
	}

	status := http.StatusCreated
	if existed {
		status = http.StatusOK
	}
	writeJSON(w, status, map[string]any{"job": job})
}

// handleBookAlignStatus reports where an entry's alignment stands: the
// newest job (whatever its state, so a failure is visible with its
// reason) and the newest usable alignment. Either may be null.
func (s *Server) handleBookAlignStatus(w http.ResponseWriter, r *http.Request) {
	userID, entryID, _, ok := s.bookEntry(w, r)
	if !ok {
		return
	}

	job, err := s.store.AlignmentJobForEntry(r.Context(), userID, entryID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			fail(w, errNotFound)
			return
		}
		fail(w, err)
		return
	}
	alignment, err := s.store.AlignmentForEntry(r.Context(), entryID)
	if err != nil {
		fail(w, err)
		return
	}
	var jobAny any
	if job.ID != "" {
		jobAny = job
	}
	var alignmentAny any
	if alignment.ID != "" {
		alignmentAny = alignment
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"job":            jobAny,
		"alignment":      alignmentAny,
		"worker_enabled": s.cfg.AlignWorkerToken != "",
	})
}

// handleBookAlignDelete cancels any in-flight alignment for one of the
// caller's entries and clears its stored alignments — anchors, segments
// and job history with them. Positions stored as character offsets are
// untouched: they were always the truth; only the derived views lose
// their map.
func (s *Server) handleBookAlignDelete(w http.ResponseWriter, r *http.Request) {
	userID, entryID, _, ok := s.bookEntry(w, r)
	if !ok {
		return
	}
	if err := s.store.ClearAlignment(r.Context(), userID, entryID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			fail(w, errNotFound)
			return
		}
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// requireAlignWorker guards the /internal half of the queue. The token
// compare is constant-time and the token never appears in a log line;
// an unset token disables the endpoints outright (503) rather than
// leaving them open, because alignment is strictly optional.
func (s *Server) requireAlignWorker(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.cfg.AlignWorkerToken == "" {
			fail(w, errorf(http.StatusServiceUnavailable,
				"the alignment worker API is not enabled"))
			return
		}
		header := r.Header.Get("Authorization")
		const prefix = "Bearer "
		token := ""
		if strings.HasPrefix(header, prefix) {
			token = strings.TrimSpace(strings.TrimPrefix(header, prefix))
		}
		if token == "" || subtle.ConstantTimeCompare([]byte(token), []byte(s.cfg.AlignWorkerToken)) != 1 {
			fail(w, errorf(http.StatusUnauthorized, "not authenticated"))
			return
		}
		next.ServeHTTP(w, r)
	})
}

// workerRequest is the field every /internal body carries: the worker's
// own id, which must hold the claim it is writing to. That check is
// what makes a stale reclaim safe — the old owner's late writes are
// refused instead of interleaving with the new owner's.
type workerRequest struct {
	Worker string `json:"worker"`
}

type alignClaimResponse struct {
	Job models.AlignmentJob `json:"job"`
	// EpubTextPath is the absolute path of the canonical text the worker
	// aligns against. The worker mounts the same volumes the API does.
	EpubTextPath string                `json:"epub_text_path"`
	Tracks       []bookaudio.TrackFile `json:"tracks"`
}

// handleAlignClaim hands the oldest queued job to a worker, with the
// resolved absolute paths of everything the job needs: the canonical
// text and the ordered audio tracks. An empty queue is 204, not an
// error — a worker polling for work should read it as "nothing to do".
func (s *Server) handleAlignClaim(w http.ResponseWriter, r *http.Request) {
	var body workerRequest
	if err := decodeMax(r, &body, 8<<20); err != nil {
		fail(w, err)
		return
	}
	if strings.TrimSpace(body.Worker) == "" {
		fail(w, errorf(http.StatusBadRequest, "worker is required"))
		return
	}
	if s.epubs == nil {
		fail(w, errorf(http.StatusServiceUnavailable, "epub text storage is unavailable"))
		return
	}

	job, err := s.store.ClaimAlignmentJob(r.Context(), body.Worker)
	if errors.Is(err, store.ErrNotFound) {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err != nil {
		fail(w, err)
		return
	}

	resp := alignClaimResponse{
		Job:          job,
		EpubTextPath: s.epubs.TextPath(job.EpubTextID),
		Tracks:       []bookaudio.TrackFile{},
	}
	if bookID, err := s.store.BookIDForEntryAny(r.Context(), job.EntryID); err == nil {
		if tracks, err := s.audio.TrackFiles(r.Context(), bookID); err == nil {
			resp.Tracks = tracks
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

type alignProgressRequest struct {
	workerRequest
	State       string   `json:"state"`
	Progress    *float64 `json:"progress"`
	StageDetail *string  `json:"stage_detail"`
}

// handleAlignProgress is the worker's heartbeat: optionally a new
// pipeline state, progress fraction and stage detail, always a fresh
// heartbeat timestamp.
func (s *Server) handleAlignProgress(w http.ResponseWriter, r *http.Request) {
	var body alignProgressRequest
	if err := decodeMax(r, &body, 8<<20); err != nil {
		fail(w, err)
		return
	}
	job, err := s.store.AlignmentJobProgress(r.Context(), chi.URLParam(r, "jobID"),
		body.Worker, body.State, body.Progress, body.StageDetail)
	if !failWorkerWrite(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"job": job})
}

type alignSegmentsRequest struct {
	workerRequest
	Segments []models.TranscriptSegment `json:"segments"`
}

// handleAlignSegments stores one streamed batch of transcription
// segments. Batches are append-only and each carries the heartbeat.
func (s *Server) handleAlignSegments(w http.ResponseWriter, r *http.Request) {
	var body alignSegmentsRequest
	if err := decodeMax(r, &body, 8<<20); err != nil {
		fail(w, err)
		return
	}
	job, err := s.store.AppendTranscriptSegments(r.Context(), chi.URLParam(r, "jobID"),
		body.Worker, body.Segments)
	if !failWorkerWrite(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"job": job})
}

type alignAnchorsRequest struct {
	workerRequest
	Anchors []models.AlignmentAnchor `json:"anchors"`
}

// handleAlignAnchors stores one streamed batch of anchors; the server
// writes the batch in a single transaction so a batch is all-or-nothing.
func (s *Server) handleAlignAnchors(w http.ResponseWriter, r *http.Request) {
	var body alignAnchorsRequest
	if err := decodeMax(r, &body, 8<<20); err != nil {
		fail(w, err)
		return
	}
	job, err := s.store.AppendAlignmentAnchors(r.Context(), chi.URLParam(r, "jobID"),
		body.Worker, body.Anchors)
	if !failWorkerWrite(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"job": job})
}

type alignCompleteRequest struct {
	workerRequest
	State          string  `json:"state"`
	Coverage       float64 `json:"coverage"`
	MeanConfidence float64 `json:"mean_confidence"`
	Model          string  `json:"model"`
	Error          string  `json:"error"`
}

// handleAlignComplete finalizes a job with its terminal state. A usable
// result ('ready' or 'low_confidence') must name the model that
// produced it; a failure should say why. On success the entry's older
// alignments are superseded, and the position endpoints start
// deriving audio timestamps through the new anchors.
func (s *Server) handleAlignComplete(w http.ResponseWriter, r *http.Request) {
	var body alignCompleteRequest
	if err := decodeMax(r, &body, 8<<20); err != nil {
		fail(w, err)
		return
	}
	job, alignment, err := s.store.CompleteAlignment(r.Context(), chi.URLParam(r, "jobID"),
		body.Worker, body.State, body.Coverage, body.MeanConfidence, body.Model, body.Error)
	if !failWorkerWrite(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"job": job, "alignment": alignment})
}

// failWorkerWrite maps the store's worker-write errors onto their HTTP
// answers and reports whether the handler may continue. The two 409s
// are the queue's liveness contract: a terminal job refuses further
// writes, and a job claimed by someone else refuses yours — that is
// what it looks like when a stale worker's claim was reclaimed.
func failWorkerWrite(w http.ResponseWriter, err error) bool {
	switch {
	case err == nil:
		return true
	case errors.Is(err, store.ErrNotFound):
		fail(w, errNotFound)
	case errors.Is(err, store.ErrJobTerminal):
		fail(w, errorf(http.StatusConflict, "job already finished"))
	case errors.Is(err, store.ErrJobNotClaimedBy):
		fail(w, errorf(http.StatusConflict, "job is claimed by another worker"))
	default:
		fail(w, err)
	}
	return false
}
