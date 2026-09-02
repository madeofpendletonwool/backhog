// Package audio is the decode half of the worker: it turns whatever the
// library holds — mp3, m4a, m4b — into the 16 kHz mono PCM Whisper
// insists on, one bounded slice at a time. A 10-hour audiobook is never
// materialized as a WAV; only the chunk being transcribed exists on
// disk, and it is deleted as soon as it has been read.
package audio

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// The three ways decoding fails, kept apart because they need different
// answers from whoever reads the job's error: a mount that isn't there,
// a file that is there but broken, and ffmpeg itself being absent.
var (
	// ErrUnreadable means the path does not exist or cannot be opened —
	// nearly always a media mount missing from the worker container.
	ErrUnreadable = errors.New("audio file is not readable")
	// ErrDecode means ffmpeg opened the file and could not decode it.
	ErrDecode = errors.New("audio file could not be decoded")
	// ErrToolMissing means ffmpeg or ffprobe is not on PATH.
	ErrToolMissing = errors.New("ffmpeg tooling is unavailable")
)

// SampleRate is what Whisper requires; there is no other supported rate.
const SampleRate = 16000

// FFmpeg runs the two binaries by path. It holds no state, so the same
// value is safe to share.
type FFmpeg struct {
	Bin   string
	Probe string
}

// Check verifies both binaries exist before any job is claimed, so a
// broken image fails at startup instead of halfway through a book.
func (f FFmpeg) Check() error {
	for _, bin := range []string{f.Bin, f.Probe} {
		if _, err := exec.LookPath(bin); err != nil {
			return fmt.Errorf("%w: %s: %w", ErrToolMissing, bin, err)
		}
	}
	return nil
}

// Duration measures a file with ffprobe. It is only ever used for the
// chunking loop of a track whose duration the API could not measure —
// the book's global timeline is always the API's, never this.
func (f FFmpeg) Duration(ctx context.Context, path string) (float64, error) {
	if err := readable(path); err != nil {
		return 0, err
	}
	cmd := exec.CommandContext(ctx, f.Probe,
		"-v", "error",
		"-show_entries", "format=duration",
		"-of", "default=noprint_wrappers=1:nokey=1",
		path,
	)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return 0, fmt.Errorf("%w: %s: %s", ErrDecode, path, tail(stderr.String()))
	}
	return ParseDuration(string(out))
}

// ParseDuration reads ffprobe's bare-number output. ffprobe prints
// "N/A" for a stream it cannot measure, which is a decode failure and
// not a zero-length file.
func ParseDuration(raw string) (float64, error) {
	value := strings.TrimSpace(raw)
	seconds, err := strconv.ParseFloat(value, 64)
	if err != nil || seconds <= 0 {
		return 0, fmt.Errorf("%w: ffprobe reported no usable duration (%q)", ErrDecode, value)
	}
	return seconds, nil
}

// ExtractArgs is the ffmpeg command line for one chunk, split out so the
// shape of it is testable without running ffmpeg. Seeking before -i uses
// the container index instead of decoding from the start of the file,
// which is the difference between seconds and minutes on the last chunk
// of an 11-hour m4b.
func ExtractArgs(src, dst string, start, duration float64) []string {
	return []string{
		"-nostdin", "-hide_banner", "-loglevel", "error", "-y",
		"-ss", formatSeconds(start),
		"-i", src,
		"-t", formatSeconds(duration),
		"-vn", "-sn", "-dn",
		"-map", "0:a:0",
		"-ac", "1",
		"-ar", strconv.Itoa(SampleRate),
		"-c:a", "pcm_s16le",
		"-f", "wav",
		dst,
	}
}

// ExtractPCM writes [start, start+duration) of src to dst as 16 kHz mono
// PCM. dst is one chunk's worth — around 19 MB for ten minutes — and the
// caller deletes it once Whisper has read it.
func (f FFmpeg) ExtractPCM(ctx context.Context, src, dst string, start, duration float64) error {
	if err := readable(src); err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, f.Bin, ExtractArgs(src, dst, start, duration)...)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		_ = os.Remove(dst)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("%w: %s at %.1fs: %s", ErrDecode, src, start, tail(stderr.String()))
	}
	info, err := os.Stat(dst)
	if err != nil || info.Size() == 0 {
		_ = os.Remove(dst)
		return fmt.Errorf("%w: %s at %.1fs produced no audio", ErrDecode, src, start)
	}
	return nil
}

// readable separates "the mount is not there" from every other decode
// problem, because those two have completely different fixes.
func readable(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("%w: %s: %w", ErrUnreadable, path, err)
	}
	return file.Close()
}

func formatSeconds(v float64) string {
	if v < 0 {
		v = 0
	}
	return strconv.FormatFloat(v, 'f', 3, 64)
}

// tail keeps the last few lines of a tool's stderr: the useful part of
// an ffmpeg failure is at the end, and the whole thing is too long to
// put in a job's error column.
func tail(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if len(lines) == 0 {
		return "no output"
	}
	if len(lines) > 3 {
		lines = lines[len(lines)-3:]
	}
	joined := strings.TrimSpace(strings.Join(lines, "; "))
	if joined == "" {
		return "no output"
	}
	return joined
}
