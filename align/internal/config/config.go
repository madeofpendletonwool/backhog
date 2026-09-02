// Package config parses the alignment worker's environment. The worker
// owns no database and holds no user credentials: everything it needs is
// the API's address, the shared worker token, and where its two external
// binaries and the Whisper model live.
package config

import (
	"errors"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// Config is the fully resolved worker environment. Token is the one
// secret in here and is never logged, printed or included in an error.
type Config struct {
	APIURL string
	Token  string
	// WorkerID identifies this process to the queue. It must be stable
	// for the life of a claim and distinct between workers; the
	// container hostname is both by default.
	WorkerID string

	FFmpegBin  string
	FFprobeBin string

	WhisperBin   string
	WhisperModel string
	// ModelName is what gets recorded on the alignment, so a stored
	// result says which model produced it.
	ModelName      string
	Language       string
	Threads        int
	BeamSize       int
	WhisperTimeout time.Duration

	// ChunkSeconds is how much audio is decoded and transcribed at a
	// time. Whisper works on whole files, so a 10-hour audiobook has to
	// be sliced: this is the slice, and it is what keeps peak disk and
	// memory flat regardless of book length.
	ChunkSeconds float64
	// OverlapSeconds is decoded on each side of a chunk purely as
	// context, so a sentence straddling a boundary is not clipped. Kept
	// output is still partitioned exactly on the chunk boundary.
	OverlapSeconds float64
	SegmentBatch   int

	WorkDir           string
	PollInterval      time.Duration
	HeartbeatInterval time.Duration
	StatusAddr        string
}

// Load reads the environment. Only two values have no sensible default:
// the API address and the shared token, and a missing token is fatal
// rather than a warning — a worker that cannot authenticate has nothing
// to do but fail loudly.
func Load() (Config, error) {
	cfg := Config{
		APIURL:            strings.TrimRight(env("BACKHOG_API_URL", "http://api:8080"), "/"),
		Token:             os.Getenv("ALIGN_WORKER_TOKEN"),
		WorkerID:          env("ALIGN_WORKER_ID", defaultWorkerID()),
		FFmpegBin:         env("FFMPEG_BIN", "ffmpeg"),
		FFprobeBin:        env("FFPROBE_BIN", "ffprobe"),
		WhisperBin:        env("WHISPER_BIN", "whisper-cli"),
		WhisperModel:      env("WHISPER_MODEL_PATH", "/models/ggml-base.en.bin"),
		Language:          env("WHISPER_LANGUAGE", "en"),
		WorkDir:           env("ALIGN_WORK_DIR", "/tmp/backhog-align"),
		StatusAddr:        env("ALIGN_STATUS_ADDR", ":8090"),
		ModelName:         os.Getenv("WHISPER_MODEL"),
		Threads:           runtime.NumCPU(),
		BeamSize:          2,
		ChunkSeconds:      600,
		OverlapSeconds:    5,
		SegmentBatch:      200,
		PollInterval:      15 * time.Second,
		HeartbeatInterval: 60 * time.Second,
		WhisperTimeout:    2 * time.Hour,
	}

	var errs []error
	num := func(key string, dst *int, minimum int) {
		raw := strings.TrimSpace(os.Getenv(key))
		if raw == "" {
			return
		}
		v, err := strconv.Atoi(raw)
		if err != nil || v < minimum {
			errs = append(errs, fmt.Errorf("%s must be an integer >= %d", key, minimum))
			return
		}
		*dst = v
	}
	seconds := func(key string, dst *float64, minimum float64) {
		raw := strings.TrimSpace(os.Getenv(key))
		if raw == "" {
			return
		}
		v, err := strconv.ParseFloat(raw, 64)
		if err != nil || v < minimum {
			errs = append(errs, fmt.Errorf("%s must be a number >= %g", key, minimum))
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

	num("WHISPER_THREADS", &cfg.Threads, 1)
	num("WHISPER_BEAM_SIZE", &cfg.BeamSize, 1)
	num("ALIGN_SEGMENT_BATCH", &cfg.SegmentBatch, 1)
	seconds("ALIGN_CHUNK_SECONDS", &cfg.ChunkSeconds, 30)
	seconds("ALIGN_CHUNK_OVERLAP_SECONDS", &cfg.OverlapSeconds, 0)
	duration("ALIGN_POLL_INTERVAL", &cfg.PollInterval, time.Second)
	duration("ALIGN_HEARTBEAT_INTERVAL", &cfg.HeartbeatInterval, time.Second)
	duration("WHISPER_TIMEOUT", &cfg.WhisperTimeout, time.Minute)

	if strings.TrimSpace(cfg.Token) == "" {
		errs = append(errs, errors.New("ALIGN_WORKER_TOKEN is required; set the same value here and on the api service"))
	}
	if cfg.APIURL == "" {
		errs = append(errs, errors.New("BACKHOG_API_URL is required"))
	}
	if strings.TrimSpace(cfg.WorkerID) == "" {
		errs = append(errs, errors.New("ALIGN_WORKER_ID resolved to empty and no hostname is available"))
	}
	if cfg.OverlapSeconds >= cfg.ChunkSeconds {
		errs = append(errs, errors.New("ALIGN_CHUNK_OVERLAP_SECONDS must be smaller than ALIGN_CHUNK_SECONDS"))
	}
	if cfg.ModelName == "" {
		cfg.ModelName = modelNameFromPath(cfg.WhisperModel)
	}
	if err := errors.Join(errs...); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// modelNameFromPath recovers the human model name from the ggml file
// name, so `/models/ggml-base.en.bin` records itself as `base.en`.
func modelNameFromPath(path string) string {
	name := path
	if i := strings.LastIndexByte(name, '/'); i >= 0 {
		name = name[i+1:]
	}
	name = strings.TrimSuffix(name, ".bin")
	name = strings.TrimPrefix(name, "ggml-")
	if name == "" {
		return "whisper"
	}
	return name
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
