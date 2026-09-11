// Package api is the OCR worker's half of the /internal/ocr contract. It
// is the only thing in this binary that talks to Backhog: the worker owns
// no database, and every fact it has about a job arrived through one of
// these calls. It mirrors the alignment worker's client deliberately —
// the two queues are the same shape — with one GET for page images, the
// one thing alignment never streams.
package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Job is the queue row the worker holds a claim on.
type Job struct {
	ID      string `json:"id"`
	EntryID string `json:"entry_id"`
	State   string `json:"state"`
	// MediaFileID is the file whose pages the job reads.
	MediaFileID int64   `json:"media_file_id"`
	Progress    float64 `json:"progress"`
	Attempts    int     `json:"attempts"`
}

// LedgerEntry pins one already-read page: the image hash its text was
// read from, and the OCR pipeline version that read it.
type LedgerEntry struct {
	ImageSHA256 string `json:"image_sha256"`
	OCRVersion  string `json:"ocr_version"`
}

// Claim is a successful claim: the job plus everything needed to run it.
type Claim struct {
	Job Job `json:"job"`
	// PageCount is the classified page count; pages are 1..PageCount.
	PageCount int `json:"page_count"`
	// Ledger maps decimal page numbers to their pins. A page whose
	// fetched image hash matches its entry's hash — read by this
	// pipeline version — is unchanged and never re-billed.
	Ledger map[string]LedgerEntry `json:"ledger"`
}

// Page is one page's lettering result, streamed back in batches.
type Page struct {
	PageNumber     int     `json:"page_number"`
	ImageSHA256    string  `json:"image_sha256"`
	Text           string  `json:"text"`
	MeanConfidence float64 `json:"mean_confidence"`
}

// PageImage is one fetched page raster plus the hash the API computed
// over the exact bytes it served — the ledger's comparison key.
type PageImage struct {
	ContentType string
	Data        []byte
	SHA256      string
}

// Worker pipeline states, mirroring the API's own vocabulary.
const (
	StateOcring = "ocring"
	StateFailed = "failed"
)

// ErrNoLettering reports the API's named refusal for a page with nothing
// to read: vector art, a blank page, a codec it cannot serve. The page is
// skipped and counted against coverage — never a job failure.
var ErrNoLettering = errors.New("page carries no readable lettering")

// Error is a non-2xx answer from the API, carrying the status so the
// worker can tell "your claim is gone" from "the server is unwell".
type Error struct {
	Status  int
	Message string
}

func (e *Error) Error() string {
	if e.Message == "" {
		return fmt.Sprintf("backhog api: http %d", e.Status)
	}
	return fmt.Sprintf("backhog api: http %d: %s", e.Status, e.Message)
}

// ClaimLost reports the answers that mean this worker no longer owns the
// job it is working on: the job vanished (404), or it is terminal or held
// by someone else after a stale reclaim (409). The only correct response
// is to drop the work and go back to polling.
func ClaimLost(err error) bool {
	var apiErr *Error
	if !errors.As(err, &apiErr) {
		return false
	}
	return apiErr.Status == http.StatusNotFound || apiErr.Status == http.StatusConflict
}

// Client talks to the API's /internal/ocr endpoints with the shared
// token. The token is held here and never logged, formatted or returned
// in an error — the only place it appears is the Authorization header.
type Client struct {
	baseURL  string
	token    string
	workerID string
	http     *http.Client

	// pageHTTP is the client page images ride on: a page can be tens of
	// megabytes, so it gets a longer timeout than the JSON calls.
	pageHTTP *http.Client

	// Attempts and Backoff shape the retry of transient failures; a 4xx
	// is never retried.
	Attempts int
	Backoff  time.Duration
}

// New builds a client for one worker identity. Every request carries that
// identity, because the API refuses writes from anyone but the worker
// holding the claim.
func New(baseURL, token, workerID string) *Client {
	return &Client{
		baseURL:  strings.TrimRight(baseURL, "/"),
		token:    token,
		workerID: workerID,
		http: &http.Client{
			Timeout: 2 * time.Minute,
		},
		pageHTTP: &http.Client{
			Timeout: 10 * time.Minute,
		},
		Attempts: 4,
		Backoff:  2 * time.Second,
	}
}

// WorkerID is this client's identity on the queue.
func (c *Client) WorkerID() string { return c.workerID }

// Claim asks for the oldest queued job. A nil claim with a nil error is
// the empty queue — the normal answer, not a problem.
func (c *Client) Claim(ctx context.Context) (*Claim, error) {
	var out Claim
	status, err := c.post(ctx, "/internal/ocr/claim", map[string]any{
		"worker": c.workerID,
	}, &out)
	if err != nil {
		return nil, err
	}
	if status == http.StatusNoContent {
		return nil, nil
	}
	return &out, nil
}

// Progress heartbeats the job. An empty state leaves the state alone and
// only refreshes the heartbeat; nil progress or detail likewise.
func (c *Client) Progress(ctx context.Context, jobID, state string, progress *float64, detail *string) error {
	body := map[string]any{"worker": c.workerID}
	if state != "" {
		body["state"] = state
	}
	if progress != nil {
		body["progress"] = *progress
	}
	if detail != nil {
		body["stage_detail"] = *detail
	}
	_, err := c.post(ctx, "/internal/ocr/"+jobID+"/progress", body, nil)
	return err
}

// Page fetches one page's raster (1-based) through the same streaming
// path the paged reader serves from — there is no second decode path. A
// 422 is ErrNoLettering, the named refusal for a page with nothing to
// read; anything else fails as written.
func (c *Client) Page(ctx context.Context, jobID string, page int) (PageImage, error) {
	var lastErr error
	for attempt := 0; attempt < max(c.Attempts, 1); attempt++ {
		if attempt > 0 {
			delay := c.Backoff * time.Duration(1<<(attempt-1))
			select {
			case <-ctx.Done():
				return PageImage{}, ctx.Err()
			case <-time.After(delay):
			}
		}
		img, err := c.pageOnce(ctx, jobID, page)
		if err == nil {
			return img, nil
		}
		lastErr = err
		if ctx.Err() != nil {
			return PageImage{}, ctx.Err()
		}
		// A named refusal is the page's answer, not a transient fault —
		// retrying it would only re-read the same refusal.
		if errors.Is(err, ErrNoLettering) {
			return PageImage{}, err
		}
		var apiErr *Error
		if errors.As(err, &apiErr) && apiErr.Status < 500 {
			return PageImage{}, err
		}
	}
	return PageImage{}, lastErr
}

func (c *Client) pageOnce(ctx context.Context, jobID string, page int) (PageImage, error) {
	url := fmt.Sprintf("%s/internal/ocr/%s/page/%d?worker=%s", c.baseURL, jobID, page, c.workerID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return PageImage{}, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)

	resp, err := c.pageHTTP.Do(req)
	if err != nil {
		// net/http puts the request URL in this error, never the
		// Authorization header, so it is safe to surface.
		return PageImage{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		if resp.StatusCode == http.StatusUnprocessableEntity {
			// The named refusal: this page carries nothing to read.
			return PageImage{}, fmt.Errorf("%w: page %d: %s",
				ErrNoLettering, page, readMessage(resp.Body))
		}
		return PageImage{}, &Error{Status: resp.StatusCode, Message: readMessage(resp.Body)}
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 80<<20))
	if err != nil {
		return PageImage{}, fmt.Errorf("read page %d: %w", page, err)
	}
	sum := resp.Header.Get("X-Backhog-Page-Sha256")
	if sum == "" {
		return PageImage{}, fmt.Errorf("page %d arrived without its image hash", page)
	}
	return PageImage{
		ContentType: resp.Header.Get("Content-Type"),
		Data:        data,
		SHA256:      sum,
	}, nil
}

// Pages uploads one batch of per-page lettering. Batches are idempotent
// per page and each refreshes the heartbeat, which is what keeps a long
// pass from looking dead to the reclaim pass.
func (c *Client) Pages(ctx context.Context, jobID, ocrVersion string, pages []Page) error {
	_, err := c.post(ctx, "/internal/ocr/"+jobID+"/pages", map[string]any{
		"worker":      c.workerID,
		"ocr_version": ocrVersion,
		"pages":       pages,
	}, nil)
	return err
}

// Complete closes the job. A usable result must name the model that read
// the lettering; a failure should say why. The honesty pair — coverage
// and mean confidence — is computed by the API from the corpus it owns.
func (c *Client) Complete(ctx context.Context, jobID, model, failure string) error {
	_, err := c.post(ctx, "/internal/ocr/"+jobID+"/complete", map[string]any{
		"worker": c.workerID,
		"model":  model,
		"error":  failure,
	}, nil)
	return err
}

// post sends one JSON request, retrying only what is worth retrying: a
// transport error or a 5xx. A 4xx is the API telling the worker it is
// wrong, and repeating it would not make it right.
func (c *Client) post(ctx context.Context, path string, body any, out any) (int, error) {
	encoded, err := json.Marshal(body)
	if err != nil {
		return 0, err
	}

	attempts := max(c.Attempts, 1)
	var lastErr error
	for attempt := range attempts {
		if attempt > 0 {
			delay := c.Backoff * time.Duration(1<<(attempt-1))
			select {
			case <-ctx.Done():
				return 0, ctx.Err()
			case <-time.After(delay):
			}
		}
		status, err := c.do(ctx, path, encoded, out)
		if err == nil {
			return status, nil
		}
		lastErr = err
		if ctx.Err() != nil {
			return 0, ctx.Err()
		}
		var apiErr *Error
		if errors.As(err, &apiErr) && apiErr.Status < 500 {
			return status, err
		}
	}
	return 0, lastErr
}

func (c *Client) do(ctx context.Context, path string, body []byte, out any) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.token)

	resp, err := c.http.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return resp.StatusCode, &Error{Status: resp.StatusCode, Message: readMessage(resp.Body)}
	}
	if resp.StatusCode == http.StatusNoContent || out == nil {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
		return resp.StatusCode, nil
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 32<<20)).Decode(out); err != nil {
		return resp.StatusCode, fmt.Errorf("decode %s response: %w", path, err)
	}
	return resp.StatusCode, nil
}

// readMessage pulls the API's own error text out of the body when it is
// there. Bounded, because an error page is not something to buffer
// without limit.
func readMessage(r io.Reader) string {
	raw, err := io.ReadAll(io.LimitReader(r, 8<<10))
	if err != nil || len(raw) == 0 {
		return ""
	}
	var payload struct {
		Error   string `json:"error"`
		Message string `json:"message"`
	}
	if json.Unmarshal(raw, &payload) == nil {
		if payload.Error != "" {
			return payload.Error
		}
		if payload.Message != "" {
			return payload.Message
		}
	}
	return strings.TrimSpace(string(raw))
}
