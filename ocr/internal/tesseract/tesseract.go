// Package tesseract drives the tesseract CLI over one page image and
// parses its TSV output into lettering text plus a per-page confidence.
//
// The confidence is the mean of tesseract's own per-word beliefs, the
// honest number for stylized lettering: comic fonts, speech balloons and
// hand lettering read far worse than print, and the arena's answer to
// that is to surface the number, never to fake it.
package tesseract

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

var (
	// ErrToolMissing means the tesseract binary is not on PATH.
	ErrToolMissing = errors.New("tesseract binary is unavailable")
	// ErrRead means tesseract ran and failed on the image.
	ErrRead = errors.New("tesseract failed to read the image")
)

// Tesseract runs the tesseract CLI. Language and PSM are the two knobs
// that matter for lettering; see the config package for their defaults.
type Tesseract struct {
	Bin      string
	Language string
	PSM      int
}

// Check verifies the binary exists and answers --version, and returns
// the version line ("tesseract 5.3.0") for the model name a corpus is
// recorded against.
func (t Tesseract) Check() (string, error) {
	if _, err := exec.LookPath(t.Bin); err != nil {
		return "", fmt.Errorf("%w: %s: %w", ErrToolMissing, t.Bin, err)
	}
	out, err := exec.Command(t.Bin, "--version").Output()
	if err != nil {
		// tesseract prints --version to stderr and exits nonzero on some
		// builds; a CombinedOutput fallback covers that without failing a
		// working binary.
		out, err = exec.Command(t.Bin, "--version").CombinedOutput()
		if err != nil {
			return "", fmt.Errorf("%w: %s: %w", ErrToolMissing, t.Bin, err)
		}
	}
	for _, line := range strings.Split(string(out), "\n") {
		if v, ok := strings.CutPrefix(strings.TrimSpace(line), "tesseract "); ok {
			return "tesseract " + strings.TrimSpace(v), nil
		}
	}
	return "tesseract", nil
}

// Args is the command line for one page, split out so it can be asserted
// on without a binary in the picture. TSV output on stdout keeps the
// per-word confidences, which plain text throws away.
func (t Tesseract) Args(imagePath string) []string {
	return []string{
		imagePath,
		"stdout",
		"-l", t.Language,
		"--psm", strconv.Itoa(t.PSM),
		"tsv",
	}
}

// Reading is one page's lettering: the text as tesseract read it (lines
// joined with newlines) and the mean per-word confidence in [0,1].
// A page with no words found is Text "" and Confidence 0 — an honest
// empty, not an error.
type Reading struct {
	Text           string
	MeanConfidence float64
}

// Read runs tesseract over one image file.
func (t Tesseract) Read(ctx context.Context, imagePath string) (Reading, error) {
	cmd := exec.CommandContext(ctx, t.Bin, t.Args(imagePath)...)
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return Reading{}, ctx.Err()
		}
		return Reading{}, fmt.Errorf("%w: %s: %v: %s",
			ErrRead, imagePath, err, strings.TrimSpace(stderr.String()))
	}
	return ParseTSV(stdout.String())
}

// ParseTSV turns tesseract's TSV output into a Reading. Level 5 rows are
// words; rows with a confidence of -1 are structural and dropped. Words
// are grouped back into the lines tesseract saw, because a balloon's
// line breaks are real reading order even when the normalized corpus
// will fold them away.
func ParseTSV(tsv string) (Reading, error) {
	var lines []string
	var confidences []float64
	key := ""
	started := false

	for _, row := range strings.Split(tsv, "\n") {
		fields := strings.Split(row, "\t")
		if len(fields) < 12 {
			continue
		}
		if fields[0] == "level" {
			continue
		}
		if fields[0] != "5" {
			continue
		}
		conf, err := strconv.ParseFloat(fields[10], 64)
		if err != nil || conf < 0 {
			continue
		}
		word := fields[11]
		if word == "" {
			continue
		}
		// The line a word belongs to: (block, par, line). A new key ends
		// the previous line.
		thisKey := fields[2] + "/" + fields[3] + "/" + fields[4]
		if started && thisKey != key {
			lines = append(lines, "")
		}
		key = thisKey
		started = true
		if len(lines) == 0 {
			lines = append(lines, "")
		}
		if lines[len(lines)-1] != "" {
			lines[len(lines)-1] += " "
		}
		lines[len(lines)-1] += word
		confidences = append(confidences, conf)
	}

	if len(confidences) == 0 {
		return Reading{}, nil
	}
	total := 0.0
	for _, c := range confidences {
		total += c
	}
	return Reading{
		Text:           strings.Join(lines, "\n"),
		MeanConfidence: total / float64(len(confidences)) / 100,
	}, nil
}
