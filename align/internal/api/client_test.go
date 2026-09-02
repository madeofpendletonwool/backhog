package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func testClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	c := New(srv.URL, "s3cret", "worker-1")
	c.Backoff = time.Millisecond
	return c
}

func TestClaimEmptyQueueIsNotAnError(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	claim, err := c.Claim(context.Background())
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	if claim != nil {
		t.Fatalf("want no claim, got %+v", claim)
	}
}

func TestClaimSendsTokenAndWorkerIdentity(t *testing.T) {
	var gotAuth string
	var gotBody map[string]any
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		writeJSON(w, http.StatusOK, Claim{
			Job:          Job{ID: "job-1", EntryID: "entry-1"},
			EpubTextPath: "/data/epub_text/text-1.txt",
			Tracks:       []TrackFile{{Path: "/media/one.mp3", Duration: 12.5}},
		})
	})

	claim, err := c.Claim(context.Background())
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	if gotAuth != "Bearer s3cret" {
		t.Errorf("Authorization = %q", gotAuth)
	}
	if gotBody["worker"] != "worker-1" {
		t.Errorf("worker = %v", gotBody["worker"])
	}
	if claim.Job.ID != "job-1" || len(claim.Tracks) != 1 || claim.Tracks[0].Duration != 12.5 {
		t.Errorf("claim = %+v", claim)
	}
}

func TestProgressOmitsUnsetFields(t *testing.T) {
	// The API rejects unknown fields and treats an absent state as
	// "leave it alone", so an unset value has to be absent rather than
	// sent as a zero.
	var body map[string]any
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		writeJSON(w, http.StatusOK, map[string]any{"job": Job{ID: "job-1"}})
	})

	if err := c.Progress(context.Background(), "job-1", "", nil, nil); err != nil {
		t.Fatalf("Progress: %v", err)
	}
	for _, key := range []string{"state", "progress", "stage_detail"} {
		if _, ok := body[key]; ok {
			t.Errorf("%s should be absent, got %v", key, body[key])
		}
	}
	if body["worker"] != "worker-1" {
		t.Errorf("worker = %v", body["worker"])
	}
}

func TestClaimLostRecognisesTheQueuesTwoRefusals(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		want   bool
	}{
		{"reclaimed by another worker", http.StatusConflict, true},
		{"job deleted", http.StatusNotFound, true},
		{"bad token", http.StatusUnauthorized, false},
		{"worker api disabled", http.StatusServiceUnavailable, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
				writeJSON(w, tc.status, map[string]string{"error": "nope"})
			})
			c.Attempts = 1

			err := c.Segments(context.Background(), "job-1", []Segment{{Text: "x"}})
			if err == nil {
				t.Fatal("want an error")
			}
			if got := ClaimLost(err); got != tc.want {
				t.Errorf("ClaimLost = %v, want %v (err: %v)", got, tc.want, err)
			}
		})
	}
}

func TestPostRetriesServerErrorsButNotClientErrors(t *testing.T) {
	var calls atomic.Int32
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) < 3 {
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": "restarting"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"job": Job{ID: "job-1"}})
	})

	if err := c.Segments(context.Background(), "job-1", nil); err != nil {
		t.Fatalf("Segments: %v", err)
	}
	if got := calls.Load(); got != 3 {
		t.Errorf("calls = %d, want 3", got)
	}

	calls.Store(0)
	bad := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad body"})
	})
	if err := bad.Segments(context.Background(), "job-1", nil); err == nil {
		t.Fatal("want an error")
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("a 4xx was retried %d times", got)
	}
}

func TestErrorMessageComesFromTheAPIEnvelope(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "job is claimed by another worker"})
	})
	c.Attempts = 1

	err := c.Complete(context.Background(), "job-1", StateReady, 1, 1, "base.en", "")
	if err == nil {
		t.Fatal("want an error")
	}
	if got := err.Error(); got != "backhog api: http 409: job is claimed by another worker" {
		t.Errorf("error = %q", got)
	}
}

func TestAnchorsPostsTheBatchAsTheAPIExpectsIt(t *testing.T) {
	var body struct {
		Worker  string   `json:"worker"`
		Anchors []Anchor `json:"anchors"`
	}
	var gotPath string
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		writeJSON(w, http.StatusOK, map[string]any{"job": Job{ID: "job-1"}})
	})

	anchors := []Anchor{
		{CharOffset: 0, AudioSeconds: 12.5, Confidence: 0.91},
		{CharOffset: 480, AudioSeconds: 18, Confidence: 0.47},
	}
	if err := c.Anchors(context.Background(), "job-1", anchors); err != nil {
		t.Fatalf("Anchors: %v", err)
	}
	if gotPath != "/internal/align/job-1/anchors" {
		t.Errorf("posted to %q", gotPath)
	}
	if body.Worker != "worker-1" {
		t.Errorf("worker = %q", body.Worker)
	}
	if len(body.Anchors) != 2 {
		t.Fatalf("sent %d anchors, want 2", len(body.Anchors))
	}
	// The wire names are the API's, not Go's: a rename here would be
	// silently dropped by the server's decoder.
	if body.Anchors[1].CharOffset != 480 || body.Anchors[1].AudioSeconds != 18 ||
		body.Anchors[1].Confidence != 0.47 {
		t.Errorf("anchor round-tripped as %+v", body.Anchors[1])
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if status != http.StatusNoContent {
		_ = json.NewEncoder(w).Encode(v)
	}
}
