package worker

import (
	"encoding/json"
	"net/http"
	"sync"
	"time"
)

// Status is what the worker is doing right now. One value serves two
// readers: the heartbeat that pushes it to the API, and the /status
// endpoint an operator (or a compose healthcheck) reads. Keeping them on
// the same source means the container can never disagree with the queue
// about what it is working on.
type Status struct {
	Worker    string    `json:"worker"`
	State     string    `json:"state"`
	JobID     string    `json:"job_id,omitempty"`
	EntryID   string    `json:"entry_id,omitempty"`
	Stage     string    `json:"stage_detail,omitempty"`
	Progress  float64   `json:"progress"`
	Model     string    `json:"model"`
	LastError string    `json:"last_error,omitempty"`
	Since     time.Time `json:"since"`
}

// Idle is the state of a worker with no claim, which is where a healthy
// worker spends most of its life.
const Idle = "idle"

// tracker holds the current Status behind a mutex.
type tracker struct {
	mu     sync.Mutex
	status Status
}

func newTracker(workerID, model string) *tracker {
	return &tracker{status: Status{
		Worker: workerID,
		State:  Idle,
		Model:  model,
		Since:  time.Now().UTC(),
	}}
}

func (t *tracker) Get() Status {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.status
}

// startJob resets the tracker onto a freshly claimed job, clearing the
// previous job's error so a stale failure never looks current.
func (t *tracker) startJob(jobID, entryID string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.status.JobID = jobID
	t.status.EntryID = entryID
	t.status.State = "claimed"
	t.status.Stage = ""
	t.status.Progress = 0
	t.status.LastError = ""
	t.status.Since = time.Now().UTC()
}

func (t *tracker) setStage(state, stage string, progress float64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.status.State = state
	t.status.Stage = stage
	t.status.Progress = progress
}

// finishJob returns the worker to idle, keeping the last failure visible
// so a crash loop is legible without digging through logs.
func (t *tracker) finishJob(lastError string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.status.JobID = ""
	t.status.EntryID = ""
	t.status.State = Idle
	t.status.Stage = ""
	t.status.Progress = 0
	t.status.LastError = lastError
	t.status.Since = time.Now().UTC()
}

// StatusHandler serves the worker's own state. It is bound to the
// compose network only and exposes no library paths, no job inputs and
// certainly no token — just what this container is doing.
func (w *Worker) StatusHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(rw http.ResponseWriter, _ *http.Request) {
		writeJSON(rw, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /status", func(rw http.ResponseWriter, _ *http.Request) {
		writeJSON(rw, w.tracker.Get())
	})
	return mux
}

func writeJSON(rw http.ResponseWriter, v any) {
	rw.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(rw).Encode(v)
}
