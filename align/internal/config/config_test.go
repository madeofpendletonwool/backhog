package config

import (
	"strings"
	"testing"
	"time"
)

func TestLoadRequiresTheSharedToken(t *testing.T) {
	// A worker with no token cannot claim anything, so this has to be
	// fatal at startup rather than a warning followed by a poll loop
	// that 503s forever.
	t.Setenv("ALIGN_WORKER_TOKEN", "")
	t.Setenv("ALIGN_WORKER_ID", "worker-1")

	_, err := Load()
	if err == nil {
		t.Fatal("want an error")
	}
	if !strings.Contains(err.Error(), "ALIGN_WORKER_TOKEN") {
		t.Errorf("error = %v", err)
	}
}

func TestLoadDefaults(t *testing.T) {
	t.Setenv("ALIGN_WORKER_TOKEN", "s3cret")
	t.Setenv("ALIGN_WORKER_ID", "worker-1")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.APIURL != "http://api:8080" {
		t.Errorf("APIURL = %q", cfg.APIURL)
	}
	if cfg.ChunkSeconds != 600 || cfg.OverlapSeconds != 5 {
		t.Errorf("chunking = %v/%v", cfg.ChunkSeconds, cfg.OverlapSeconds)
	}
	// The heartbeat has to fire comfortably inside the API's stale
	// window (10 minutes) or a busy worker gets reclaimed mid-book.
	if cfg.HeartbeatInterval > 2*time.Minute {
		t.Errorf("heartbeat interval = %s, too close to the stale window", cfg.HeartbeatInterval)
	}
	if cfg.ModelName != "base.en" {
		t.Errorf("ModelName = %q, want it derived from the model path", cfg.ModelName)
	}
}

func TestLoadTrimsTrailingSlashFromAPIURL(t *testing.T) {
	t.Setenv("ALIGN_WORKER_TOKEN", "s3cret")
	t.Setenv("ALIGN_WORKER_ID", "worker-1")
	t.Setenv("BACKHOG_API_URL", "http://api:8080/")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.APIURL != "http://api:8080" {
		t.Errorf("APIURL = %q", cfg.APIURL)
	}
}

func TestLoadRejectsOverlapWiderThanTheChunk(t *testing.T) {
	t.Setenv("ALIGN_WORKER_TOKEN", "s3cret")
	t.Setenv("ALIGN_WORKER_ID", "worker-1")
	t.Setenv("ALIGN_CHUNK_SECONDS", "60")
	t.Setenv("ALIGN_CHUNK_OVERLAP_SECONDS", "90")

	if _, err := Load(); err == nil {
		t.Fatal("want an error")
	}
}

func TestLoadRejectsUnparseableNumbers(t *testing.T) {
	t.Setenv("ALIGN_WORKER_TOKEN", "s3cret")
	t.Setenv("ALIGN_WORKER_ID", "worker-1")
	t.Setenv("WHISPER_THREADS", "lots")

	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "WHISPER_THREADS") {
		t.Fatalf("error = %v", err)
	}
}

func TestLoadHonoursAnExplicitModelName(t *testing.T) {
	t.Setenv("ALIGN_WORKER_TOKEN", "s3cret")
	t.Setenv("ALIGN_WORKER_ID", "worker-1")
	t.Setenv("WHISPER_MODEL", "small.en")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.ModelName != "small.en" {
		t.Errorf("ModelName = %q", cfg.ModelName)
	}
}

func TestModelNameFromPath(t *testing.T) {
	for path, want := range map[string]string{
		"/models/ggml-base.en.bin":  "base.en",
		"/models/ggml-small.en.bin": "small.en",
		"model.bin":                 "model",
		"":                          "whisper",
	} {
		if got := modelNameFromPath(path); got != want {
			t.Errorf("modelNameFromPath(%q) = %q, want %q", path, got, want)
		}
	}
}
