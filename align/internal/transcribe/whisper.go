// Package transcribe drives whisper.cpp's CLI over one prepared chunk of
// PCM and parses its timestamped output. Everything here works in
// chunk-local seconds; putting a segment on the book's global timeline
// is the worker's job, because only the worker knows the track offsets.
package transcribe

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// The failure modes worth telling apart. A model that will not load is
// an operator problem that no retry fixes; a chunk that will not
// transcribe may just be a bad stretch of audio.
var (
	// ErrModelLoad means whisper.cpp could not initialize with the
	// configured model file — missing, truncated, or the wrong format.
	ErrModelLoad = errors.New("whisper model could not be loaded")
	// ErrTranscribe means whisper.cpp ran and failed on the audio.
	ErrTranscribe = errors.New("transcription failed")
	// ErrToolMissing means the whisper binary is not on PATH.
	ErrToolMissing = errors.New("whisper binary is unavailable")
)

// Segment is one stretch of speech in seconds relative to the start of
// the audio that was handed to Whisper.
type Segment struct {
	Start float64
	End   float64
	Text  string
}

// Whisper runs the whisper.cpp CLI. Threads and BeamSize are the two
// knobs that trade transcription time against quality; the defaults are
// tuned for CPU-only transcription of a whole audiobook.
type Whisper struct {
	Bin       string
	ModelPath string
	Language  string
	Threads   int
	BeamSize  int
}

// Check verifies the binary and the model file before any job is
// claimed. Loading a multi-hundred-megabyte model is the slowest way to
// discover it is missing, so the cheap checks happen at startup.
func (w Whisper) Check() error {
	if _, err := exec.LookPath(w.Bin); err != nil {
		return fmt.Errorf("%w: %s: %w", ErrToolMissing, w.Bin, err)
	}
	info, err := os.Stat(w.ModelPath)
	if err != nil {
		return fmt.Errorf("%w: %s: %w", ErrModelLoad, w.ModelPath, err)
	}
	if info.Size() == 0 {
		return fmt.Errorf("%w: %s is empty", ErrModelLoad, w.ModelPath)
	}
	return nil
}

// Args is the whisper.cpp command line for one chunk, split out so it
// can be asserted on without a model in the picture. Output goes to a
// JSON sidecar rather than stdout: stdout carries progress chatter, and
// parsing a file the tool wrote is more robust than parsing a stream it
// shares with its own logging.
func (w Whisper) Args(wavPath, outPrefix string) []string {
	return []string{
		"-m", w.ModelPath,
		"-f", wavPath,
		"-l", w.Language,
		"-t", strconv.Itoa(max(w.Threads, 1)),
		"-bs", strconv.Itoa(max(w.BeamSize, 1)),
		"-np",
		"-oj",
		"-of", outPrefix,
	}
}

// Transcribe runs Whisper over one WAV and returns its segments. The
// JSON sidecar is removed before returning either way, so a long book
// does not leave a trail of per-chunk files behind.
func (w Whisper) Transcribe(ctx context.Context, wavPath, outPrefix string) ([]Segment, error) {
	jsonPath := outPrefix + ".json"
	defer os.Remove(jsonPath)

	cmd := exec.CommandContext(ctx, w.Bin, w.Args(wavPath, outPrefix)...)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	cmd.Stdout = nil
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, classify(stderr.String(), filepath.Base(wavPath), err)
	}

	raw, err := os.ReadFile(jsonPath)
	if err != nil {
		return nil, fmt.Errorf("%w: whisper wrote no output for %s: %w",
			ErrTranscribe, filepath.Base(wavPath), err)
	}
	segments, err := ParseJSON(raw)
	if err != nil {
		return nil, fmt.Errorf("%w: %s: %w", ErrTranscribe, filepath.Base(wavPath), err)
	}
	return segments, nil
}

// classify turns a whisper.cpp exit into one of this package's errors. A
// model that will not load says so on stderr, and it is worth catching
// by name: it is the difference between "fix your image" and "this one
// file is bad".
func classify(stderr, name string, runErr error) error {
	lower := strings.ToLower(stderr)
	for _, marker := range []string{
		"failed to initialize whisper context",
		"failed to load model",
		"invalid model",
		"unable to load model",
		"no such file or directory",
	} {
		if strings.Contains(lower, marker) {
			return fmt.Errorf("%w: %s", ErrModelLoad, tail(stderr))
		}
	}
	return fmt.Errorf("%w: %s: %s (%w)", ErrTranscribe, name, tail(stderr), runErr)
}

// whisperOutput is the shape of whisper.cpp's --output-json. Offsets are
// milliseconds from the start of the transcribed audio.
type whisperOutput struct {
	Transcription []struct {
		Offsets struct {
			From int64 `json:"from"`
			To   int64 `json:"to"`
		} `json:"offsets"`
		Text string `json:"text"`
	} `json:"transcription"`
}

// ParseJSON reads whisper.cpp's JSON output into segments. Segments with
// no usable text are dropped here rather than stored: whisper marks
// silence and music with bracketed placeholders, and a transcript full
// of "[BLANK_AUDIO]" would be noise in the anchor step and worse than
// nothing when debugging an alignment.
func ParseJSON(raw []byte) ([]Segment, error) {
	var parsed whisperOutput
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("parse whisper json: %w", err)
	}
	out := make([]Segment, 0, len(parsed.Transcription))
	for _, item := range parsed.Transcription {
		text := strings.TrimSpace(item.Text)
		if !Speech(text) {
			continue
		}
		start := float64(item.Offsets.From) / 1000
		end := float64(item.Offsets.To) / 1000
		if start < 0 {
			start = 0
		}
		if end < start {
			end = start
		}
		out = append(out, Segment{Start: start, End: end, Text: text})
	}
	return out, nil
}

// nonSpeech is whisper.cpp's vocabulary for "nothing was said here".
// The list is deliberately short and exact: anything broader would start
// eating real dialogue that happens to be bracketed.
var nonSpeech = map[string]bool{
	"[blank_audio]": true,
	"[silence]":     true,
	"(silence)":     true,
	"[music]":       true,
	"(music)":       true,
	"[no speech]":   true,
	"[inaudible]":   true,
	"[sound]":       true,
}

// Speech reports whether a segment's text is worth storing.
func Speech(text string) bool {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return false
	}
	return !nonSpeech[strings.ToLower(trimmed)]
}

func tail(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if len(lines) > 3 {
		lines = lines[len(lines)-3:]
	}
	joined := strings.TrimSpace(strings.Join(lines, "; "))
	if joined == "" {
		return "no output"
	}
	return joined
}
