// Package config parses the OCR lettering worker's environment. The worker
// owns no database, mounts no volumes and holds no user credentials: it
// needs the API's address, the shared OCR worker token, and where the
// tesseract binary lives. Everything else is tuning.
package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config is the fully resolved worker environment. Token is the one secret
// in here and is never logged, printed or included in an error.
type Config struct {
	APIURL string
	Token  string
	// WorkerID identifies this process to the queue: stable for the life
	// of a claim, distinct between workers. The container hostname is
	// both by default.
	WorkerID string

	TesseractBin string
	// Language is the tesseract model name (a tessdata traineddata
	// installed in the image), recorded on the corpus beside the version.
	Language string
	// PSM is tesseract's page segmentation mode. The default is 11 —
	// sparse text, find as much as possible in no particular order —
	// because balloon and caption lettering is exactly that; a deployment
	// full of prose-scanned PDFs may prefer 3 (the tool's own default).
	PSM int

	// ModelName is what gets recorded on the corpus ("tesseract 5.3.0
	// (eng)"), resolved at startup from the binary's own version string
	// so a stored result says which pipeline produced it.
	ModelName string

	// PageBatch is how many per-page results go up per request. A page's
	// lettering is a few hundred bytes, so batches are cheap and chunky.
	PageBatch int
	// PageTimeout bounds one page's fetch+OCR; a page that takes longer
	// is a problem worth naming, not a wall to lean on.
	PageTimeout time.Duration

	WorkDir           string
	PollInterval      time.Duration
	HeartbeatInterval time.Duration
	StatusAddr        string
}

// Load reads the environment. A missing token is fatal rather than a
// warning — a worker that cannot authenticate has nothing to do but fail
// loudly.
func Load() (Config, error) {
	cfg := Config{
		APIURL:            strings.TrimRight(env("BACKHOG_API_URL", "http://api:8080"), "/"),
		Token:             os.Getenv("OCR_WORKER_TOKEN"),
		WorkerID:          env("OCR_WORKER_ID", defaultWorkerID()),
		TesseractBin:      env("TESSERACT_BIN", "tesseract"),
		Language:          env("OCR_LANGUAGE", "eng"),
		PSM:               11,
		PageBatch:         32,
		PageTimeout:       10 * time.Minute,
		WorkDir:           env("OCR_WORK_DIR", "/tmp/backhog-ocr"),
		StatusAddr:        env("OCR_STATUS_ADDR", ":8091"),
		PollInterval:      15 * time.Second,
		HeartbeatInterval: 60 * time.Second,
	}

	var errs []error
	num := func(key string, dst *int, minimum, maximum int) {
		raw := strings.TrimSpace(os.Getenv(key))
		if raw == "" {
			return
		}
		v, err := strconv.Atoi(raw)
		if err != nil || v < minimum || v > maximum {
			errs = append(errs, fmt.Errorf("%s must be an integer between %d and %d", key, minimum, maximum))
			return
		}
		*dst = v
	}
	duration := func(key string, dst *time.Duration, minimum time.Duration) {
		raw := strings.TrimSpace(os.Getenv(key))
		if raw == "" {
			return
		}
		v, err := time.ParseDuration(raw)
		if err != nil || v < minimum {
			errs = append(errs, fmt.Errorf("%s must be a duration >= %s", key, minimum))
			return
		}
		*dst = v
	}

	num("OCR_TESSERACT_PSM", &cfg.PSM, 0, 13)
	num("OCR_PAGE_BATCH", &cfg.PageBatch, 1, 512)
	duration("OCR_PAGE_TIMEOUT", &cfg.PageTimeout, time.Second)
	duration("OCR_POLL_INTERVAL", &cfg.PollInterval, time.Second)
	duration("OCR_HEARTBEAT_INTERVAL", &cfg.HeartbeatInterval, time.Second)

	if strings.TrimSpace(cfg.Token) == "" {
		errs = append(errs, errors.New("OCR_WORKER_TOKEN is required; set the same value here and on the api service"))
	}
	if cfg.APIURL == "" {
		errs = append(errs, errors.New("BACKHOG_API_URL is required"))
	}
	if strings.TrimSpace(cfg.WorkerID) == "" {
		errs = append(errs, errors.New("OCR_WORKER_ID resolved to empty and no hostname is available"))
	}
	if err := errors.Join(errs...); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func defaultWorkerID() string {
	host, err := os.Hostname()
	if err != nil || strings.TrimSpace(host) == "" {
		return ""
	}
	return host
}

func env(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}
