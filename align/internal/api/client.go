// Package api is the worker's half of the /internal alignment contract.
// It is the only thing in this binary that talks to Backhog: the worker
// owns no database, and every fact it has about a job arrived through
// one of these five calls.
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

// TrackFile is one audio track as the API resolved it: an absolute path
// on the media mount the worker shares with the API, plus the duration
// the book timeline was built from.
type TrackFile struct {
	MediaFileID int64   `json:"id"`
	Path        string  `json:"path"`
	Duration    float64 `json:"duration_seconds"`
	Missing     bool    `json:"missing"`
}

// Job is the queue row the worker holds a claim on.
type Job struct {
	ID                string  `json:"id"`
	EntryID           string  `json:"entry_id"`
	EpubTextID        string  `json:"epub_text_id"`
	AudioTimelineHash string  `json:"audio_timeline_hash"`
	State             string  `json:"state"`
	Progress          float64 `json:"progress"`
	Attempts          int     `json:"attempts"`
}

// Claim is a successful claim: the job and everything needed to run it.
type Claim struct {
	Job          Job         `json:"job"`
	EpubTextPath string      `json:"epub_text_path"`
	Tracks       []TrackFile `json:"tracks"`
}

// Segment is one stretch of transcription, in GLOBAL book seconds — the
// same timeline the player and stored positions use. Converting from
// per-track times is the worker's job, not the API's.
type Segment struct {
	AudioStart float64 `json:"audio_start"`
	AudioEnd   float64 `json:"audio_end"`
	Text       string  `json:"text"`
}

// Worker pipeline states, mirroring the API's own vocabulary.
const (
	StateTranscribing  = "transcribing"
	StateAligning      = "aligning"
	StateReady         = "ready"
	StateLowConfidence = "low_confidence"
	StateFailed        = "failed"
)

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

// ClaimLost reports the two answers that mean this worker no longer owns
// the job it is writing to: the job vanished (404), or it is terminal or
// held by someone else after a stale reclaim (409). Either way the only
// correct response is to drop the work and go back to polling — pushing
// harder would interleave two workers' output.
func ClaimLost(err error) bool {
	var apiErr *Error
	if !errors.As(err, &apiErr) {
		return false
	}
	return apiErr.Status == http.StatusNotFound || apiErr.Status == http.StatusConflict
}

// Client talks to the API's /internal endpoints with the shared token.
// The token is held here and never logged, formatted or returned in an
// error — the only place it appears is the Authorization header.
type Client struct {
	baseURL  string
	token    string
	workerID string
	http     *http.Client

	// Attempts and Backoff shape the retry of transient failures. A
	// book is hours of compute, so losing it to one dropped connection
	// would be a poor trade; a 4xx is never retried.
	Attempts int
	Backoff  time.Duration
}

// New builds a client for one worker identity. Every request carries
// that identity, because the API refuses writes from anyone but the
// worker holding the claim.
func New(baseURL, token, workerID string) *Client {
	return &Client{
		baseURL:  strings.TrimRight(baseURL, "/"),
		token:    token,
		workerID: workerID,
		http: &http.Client{
			Timeout: 2 * time.Minute,
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
	status, err := c.post(ctx, "/internal/align/claim", map[string]any{
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
	_, err := c.post(ctx, "/internal/align/"+jobID+"/progress", body, nil)
	return err
}

// Segments uploads one batch of transcript. Batches are append-only and
// each one refreshes the heartbeat, which is what keeps a long
// transcription from looking dead to the reclaim pass.
func (c *Client) Segments(ctx context.Context, jobID string, segments []Segment) error {
	_, err := c.post(ctx, "/internal/align/"+jobID+"/segments", map[string]any{
		"worker":   c.workerID,
		"segments": segments,
	}, nil)
	return err
}

// Complete closes the job with its terminal state. A usable result must
// name the model that produced it; a failure should say why.
func (c *Client) Complete(ctx context.Context, jobID, state string, coverage, meanConfidence float64, model, failure string) error {
	_, err := c.post(ctx, "/internal/align/"+jobID+"/complete", map[string]any{
		"worker":          c.workerID,
		"state":           state,
		"coverage":        coverage,
		"mean_confidence": meanConfidence,
		"model":           model,
		"error":           failure,
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
		// net/http puts the request URL in this error, never the
		// Authorization header, so it is safe to surface.
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
// there, falling back to whatever the body says. Bounded, because an
// error page is not something to buffer without limit.
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
