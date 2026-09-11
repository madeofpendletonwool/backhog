package http

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/collinpendleton/backhog/api/internal/books"
	"github.com/collinpendleton/backhog/api/internal/books/pdf"
	"github.com/collinpendleton/backhog/api/internal/models"
	"github.com/collinpendleton/backhog/api/internal/store"
)

// The OCR lettering queue's two audiences, the alignment queue's shape
// exactly. Users enqueue and watch through /api/books/{entryID}/ocr with
// their session; the optional OCR worker container pulls through
// /internal/ocr/* with the shared OCR_WORKER_TOKEN — a separate token from
// the alignment one, because a deployment may want either without the
// other, and an unset one disables these endpoints outright.

// handleOCREnqueue queues a lettering pass for one of the caller's book
// entries: POST /api/books/{entryID}/ocr.
//
// The book must answer on the page axis — its designated text file an
// image-native PDF — because the corpus is a search layer over that file's
// page images, nothing more. Text-mode books are refused by name: they have
// a canonical text, and searching that is strictly the better answer.
func (s *Server) handleOCREnqueue(w http.ResponseWriter, r *http.Request) {
	if s.epubs == nil {
		fail(w, errorf(http.StatusServiceUnavailable, "canonical text storage unavailable"))
		return
	}
	userID, entryID, _, ok := s.bookEntry(w, r)
	if !ok {
		return
	}
	f, pf, err := s.epubs.PagedBookForEntry(r.Context(), userID, entryID)
	if !failOCRPreflight(w, r, err) {
		return
	}

	job, existed, err := s.store.EnqueueOCR(r.Context(), userID, entryID, f.ID, pf.ParserVersion)
	switch {
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

// handleOCRStatus reports where a paged book's lettering stands: the newest
// job (whatever its state, so a failure is visible with its reason), the
// newest usable corpus with its honesty pair, and whether a worker can
// exist to pick the job up.
func (s *Server) handleOCRStatus(w http.ResponseWriter, r *http.Request) {
	if s.epubs == nil {
		fail(w, errorf(http.StatusServiceUnavailable, "canonical text storage unavailable"))
		return
	}
	userID, entryID, _, ok := s.bookEntry(w, r)
	if !ok {
		return
	}
	f, _, err := s.epubs.PagedBookForEntry(r.Context(), userID, entryID)
	if !failOCRPreflight(w, r, err) {
		return
	}

	job, err := s.store.OCRJobForMediaFile(r.Context(), f.ID)
	if err != nil {
		fail(w, err)
		return
	}
	corpus, err := s.store.OCRCorpusForMediaFile(r.Context(), f.ID)
	if err != nil {
		fail(w, err)
		return
	}
	var jobAny any
	if job.ID != "" {
		jobAny = job
	}
	var corpusAny any
	if corpus.State != "" {
		corpusAny = corpus
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"job":            jobAny,
		"corpus":         corpusAny,
		"worker_enabled": s.cfg.OCRWorkerToken != "",
	})
}

// handleOCRDelete cancels any in-flight lettering pass and clears the
// stored corpus — pages and job history with them. The book itself is
// untouched: pages still serve, positions still hold.
func (s *Server) handleOCRDelete(w http.ResponseWriter, r *http.Request) {
	userID, entryID, _, ok := s.bookEntry(w, r)
	if !ok {
		return
	}
	if err := s.store.ClearOCR(r.Context(), userID, entryID); err != nil {
		switch {
		case errors.Is(err, store.ErrNotFound):
			fail(w, errNotFound)
		default:
			fail(w, err)
		}
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// failOCRPreflight maps the paged-book resolution's errors for the OCR
// surface. Every refusal is the one the pages endpoints give, verbatim:
// the two surfaces ask the same question of the same file.
func failOCRPreflight(w http.ResponseWriter, r *http.Request, err error) bool {
	var notPaged *books.NotPagedError
	switch {
	case err == nil:
		return true
	case errors.Is(err, store.ErrNotFound):
		fail(w, errNotFound)
	case errors.Is(err, books.ErrNoEpub):
		fail(w, errorf(http.StatusNotFound, "no ebook is attached to this book"))
	case errors.As(err, &notPaged):
		fail(w, errorf(http.StatusUnprocessableEntity, notPaged.Reason))
	case errors.Is(err, pdf.ErrDRM):
		fail(w, errorf(http.StatusUnprocessableEntity,
			"this PDF is DRM-protected (/Encrypt) and cannot be read"))
	case errors.Is(err, pdf.ErrCorrupt):
		fail(w, errorf(http.StatusUnprocessableEntity, "this PDF could not be read structurally"))
	default:
		fail(w, err)
	}
	return false
}

// requireOCRWorker guards the /internal half of the lettering queue. The
// alignment token's contract, one feature over: constant-time compare,
// never logged, and an unset token disables the endpoints outright (503)
// rather than leaving them open — OCR is strictly optional.
func (s *Server) requireOCRWorker(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.cfg.OCRWorkerToken == "" {
			fail(w, errorf(http.StatusServiceUnavailable, "the OCR worker API is not enabled"))
			return
		}
		header := r.Header.Get("Authorization")
		const prefix = "Bearer "
		token := ""
		if strings.HasPrefix(header, prefix) {
			token = strings.TrimSpace(strings.TrimPrefix(header, prefix))
		}
		if token == "" || subtle.ConstantTimeCompare([]byte(token), []byte(s.cfg.OCRWorkerToken)) != 1 {
			fail(w, errorf(http.StatusUnauthorized, "not authenticated"))
			return
		}
		next.ServeHTTP(w, r)
	})
}

// ocrLedgerEntry is one page the corpus already holds: the image hash its
// text was read from, and the OCR pipeline version that read it. A fetched
// page matching both is an unchanged page — never re-billed.
type ocrLedgerEntry struct {
	ImageSHA256 string `json:"image_sha256"`
	OCRVersion  string `json:"ocr_version"`
}

type ocrClaimResponse struct {
	Job models.OCRJob `json:"job"`
	// PageCount is the classified page count: pages 1..PageCount exist.
	PageCount int `json:"page_count"`
	// Ledger maps the page numbers already read (decimal keys) to their
	// pins. A worker skips any page whose fetched image hash and its own
	// pipeline version match the entry.
	Ledger map[string]ocrLedgerEntry `json:"ledger"`
}

// handleOCRClaim hands the oldest queued job to a worker with everything it
// needs: the page count and the corpus ledger. An empty queue is 204, not
// an error.
func (s *Server) handleOCRClaim(w http.ResponseWriter, r *http.Request) {
	var body workerRequest
	if err := decodeMax(r, &body, 1<<20); err != nil {
		fail(w, err)
		return
	}
	if strings.TrimSpace(body.Worker) == "" {
		fail(w, errorf(http.StatusBadRequest, "worker is required"))
		return
	}
	if s.epubs == nil {
		fail(w, errorf(http.StatusServiceUnavailable, "canonical text storage unavailable"))
		return
	}

	job, err := s.store.ClaimOCRJob(r.Context(), body.Worker)
	if errors.Is(err, store.ErrNotFound) {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err != nil {
		fail(w, err)
		return
	}

	// The claim is made; if the file's classification has moved under it
	// (re-parsed, new verdict), the honest completion is a named failure
	// — the corpus it would read is not the one that was enqueued.
	pf, err := s.store.GetPDFFile(r.Context(), job.MediaFileID)
	if err != nil || pf.Classification != models.PDFImageNative || pf.ParserVersion != job.ParserVersion {
		if _, cerr := s.store.CompleteOCRJob(r.Context(), job.ID, body.Worker, "",
			"the file was re-classified while queued; enqueue it again",
			s.cfg.OCRMinCoverage, s.cfg.OCRMinConfidence); cerr != nil {
			fail(w, cerr)
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}

	ledger := map[string]ocrLedgerEntry{}
	if pages, err := s.store.OCRPagesForMediaFile(r.Context(), job.MediaFileID); err == nil {
		for _, p := range pages {
			ledger[strconv.Itoa(p.PageNumber)] = ocrLedgerEntry{
				ImageSHA256: p.ImageSHA256,
				OCRVersion:  p.OCRVersion,
			}
		}
	}
	writeJSON(w, http.StatusOK, ocrClaimResponse{
		Job:       job,
		PageCount: pf.PageCount,
		Ledger:    ledger,
	})
}

// handleOCRProgress is the worker's heartbeat: optionally a new state,
// progress fraction and stage detail, always a fresh heartbeat timestamp.
func (s *Server) handleOCRProgress(w http.ResponseWriter, r *http.Request) {
	var body alignProgressRequest
	if err := decodeMax(r, &body, 1<<20); err != nil {
		fail(w, err)
		return
	}
	job, err := s.store.OCRJobProgress(r.Context(), chi.URLParam(r, "jobID"),
		body.Worker, body.State, body.Progress, body.StageDetail)
	if !failWorkerWrite(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"job": job})
}

type ocrPagesRequest struct {
	workerRequest
	// OCRVersion names the pipeline that read these pages, so a future
	// reader change can re-bill every page without touching the images.
	OCRVersion string        `json:"ocr_version"`
	Pages      []models.OCRPage `json:"pages"`
}

// handleOCRPages stores one streamed batch of per-page lettering. Batches
// are idempotent per page: re-sending a page replaces it wholesale, and
// each carries the heartbeat.
func (s *Server) handleOCRPages(w http.ResponseWriter, r *http.Request) {
	var body ocrPagesRequest
	if err := decodeMax(r, &body, 16<<20); err != nil {
		fail(w, err)
		return
	}
	if strings.TrimSpace(body.OCRVersion) == "" {
		fail(w, errorf(http.StatusBadRequest, "ocr_version is required"))
		return
	}
	for i := range body.Pages {
		body.Pages[i].OCRVersion = body.OCRVersion
		if strings.TrimSpace(body.Pages[i].ImageSHA256) == "" {
			fail(w, errorf(http.StatusBadRequest, "every page needs its image hash"))
			return
		}
	}
	job, err := s.store.AppendOCRPages(r.Context(), chi.URLParam(r, "jobID"), body.Worker, body.Pages)
	if !failWorkerWrite(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"job": job})
}

// handleOCRPageImage streams one page's raster to the worker:
// GET /internal/ocr/{jobID}/page/{page}, page 1-based (the corpus's own
// numbering). The bytes come from the exact companion-or-extract machinery
// the reader's endpoint uses — there is no second decode path — and the
// response carries the image's sha256 so the worker can check the ledger
// against nothing but the bytes it already pulled.
//
// A page with no servable image (vector art, an unservable codec) is a 422
// the worker reads as "no lettering possible here": skipped, counted
// against coverage, never a failure.
func (s *Server) handleOCRPageImage(w http.ResponseWriter, r *http.Request) {
	if s.epubs == nil {
		fail(w, errorf(http.StatusServiceUnavailable, "canonical text storage unavailable"))
		return
	}
	jobID := chi.URLParam(r, "jobID")
	page, err := strconv.Atoi(chi.URLParam(r, "page"))
	if err != nil || page < 1 {
		fail(w, errorf(http.StatusBadRequest, "page must be a 1-based page number"))
		return
	}
	// A GET carries no body, so the worker identifies itself by query
	// parameter — the same id every POST body carries, and the claim
	// check below refuses anyone else's.
	worker := strings.TrimSpace(r.URL.Query().Get("worker"))
	if worker == "" {
		fail(w, errorf(http.StatusBadRequest, "worker is required"))
		return
	}

	job, err := s.store.OCRJob(r.Context(), jobID)
	if errors.Is(err, store.ErrNotFound) {
		fail(w, errNotFound)
		return
	}
	if err != nil {
		fail(w, err)
		return
	}
	if models.OCRJobTerminal(job.State) || job.ClaimedBy == nil {
		fail(w, errorf(http.StatusConflict, "job is not held by a worker"))
		return
	}
	if *job.ClaimedBy != worker {
		fail(w, errorf(http.StatusConflict, "job is claimed by another worker"))
		return
	}

	f, err := s.store.MediaFileByID(r.Context(), job.MediaFileID)
	if err != nil {
		fail(w, errNotFound)
		return
	}
	pf, err := s.store.GetPDFFile(r.Context(), job.MediaFileID)
	if err != nil {
		fail(w, err)
		return
	}

	asset, err := s.epubs.PageImageByFile(r.Context(), f, pf, page-1)
	if err != nil {
		var notPaged *books.NotPagedError
		switch {
		case errors.Is(err, books.ErrPageOutOfRange), errors.Is(err, pdf.ErrPageOutOfRange):
			fail(w, errorf(http.StatusBadRequest, "page is outside this book's pages"))
		case errors.Is(err, pdf.ErrNoPageImage):
			fail(w, errorf(http.StatusUnprocessableEntity,
				"this page carries no image — it is vector art or blank, so it has no lettering to read"))
		case errors.Is(err, pdf.ErrUnsupportedPageImage):
			fail(w, errorf(http.StatusUnprocessableEntity,
				"this page's image uses a codec the reader cannot serve (JPX, JBIG2 or TIFF)"))
		case errors.Is(err, pdf.ErrDRM):
			fail(w, errorf(http.StatusUnprocessableEntity, "this PDF is DRM-protected (/Encrypt)"))
		case errors.Is(err, pdf.ErrCorrupt):
			fail(w, errorf(http.StatusUnprocessableEntity, "this PDF could not be read structurally"))
		case errors.As(err, &notPaged):
			fail(w, errorf(http.StatusUnprocessableEntity, notPaged.Reason))
		default:
			fail(w, err)
		}
		return
	}

	sum := sha256.Sum256(asset.Data)
	w.Header().Set("Content-Type", asset.ContentType)
	w.Header().Set("X-Backhog-Page-Sha256", hex.EncodeToString(sum[:]))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; sandbox")
	w.Header().Set("Content-Length", strconv.Itoa(len(asset.Data)))
	if r.Method == http.MethodHead {
		w.WriteHeader(http.StatusOK)
		return
	}
	w.Write(asset.Data)
}

type ocrCompleteRequest struct {
	workerRequest
	// Model names what read the lettering ("tesseract 5.3.0 (eng)"), so a
	// stored corpus says which pipeline produced it. Required for a
	// usable result.
	Model string `json:"model"`
	// Error, when non-empty, fails the run with the reason.
	Error string `json:"error"`
}

// handleOCRComplete finalizes a run. The API — not the worker — computes
// the honesty pair from the corpus it owns, and grades it against the
// deployment's thresholds: 'ready', or usable-but-flagged
// 'low_confidence', or failed.
func (s *Server) handleOCRComplete(w http.ResponseWriter, r *http.Request) {
	var body ocrCompleteRequest
	if err := decodeMax(r, &body, 1<<20); err != nil {
		fail(w, err)
		return
	}
	job, err := s.store.CompleteOCRJob(r.Context(), chi.URLParam(r, "jobID"),
		body.Worker, body.Model, body.Error, s.cfg.OCRMinCoverage, s.cfg.OCRMinConfidence)
	if !failWorkerWrite(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"job": job})
}
