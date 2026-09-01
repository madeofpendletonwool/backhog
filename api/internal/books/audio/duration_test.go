package audio

import (
	"bytes"
	"math"
	"os"
	"path/filepath"
	"testing"
)

func TestDurationFromMP4(t *testing.T) {
	for _, tc := range []struct {
		name string
		v1   bool
	}{
		{"mvhd version 0", false},
		{"mvhd version 1 (64-bit)", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := buildM4B(3671.5, 2048, tc.v1)
			got, err := DurationFrom(bytes.NewReader(data), ".m4b")
			if err != nil {
				t.Fatalf("duration: %v", err)
			}
			if math.Abs(got-3671.5) > 0.001 {
				t.Errorf("duration = %v, want 3671.5", got)
			}
		})
	}
}

func TestDurationFromMP3(t *testing.T) {
	// 1000 frames of 1152 samples at 44.1kHz.
	want := 1000 * 1152 / 44100.0
	got, err := DurationFrom(bytes.NewReader(buildMP3(1000)), ".mp3")
	if err != nil {
		t.Fatalf("duration: %v", err)
	}
	if math.Abs(got-want) > 0.01 {
		t.Errorf("duration = %v, want ~%v", got, want)
	}
}

// The reader is rewound by DurationFrom, so a caller that already read tags
// off the same handle gets the right answer without seeking itself.
func TestDurationFromRewinds(t *testing.T) {
	r := bytes.NewReader(buildM4B(60, 0, false))
	if _, err := r.Seek(32, 0); err != nil {
		t.Fatal(err)
	}
	got, err := DurationFrom(r, ".m4b")
	if err != nil || math.Abs(got-60) > 0.001 {
		t.Errorf("duration = %v, %v; want 60", got, err)
	}
}

func TestDurationRefusesWhatItCannotMeasure(t *testing.T) {
	cases := []struct {
		name string
		data []byte
		ext  string
	}{
		{"unsupported extension", buildM4B(60, 0, false), ".flac"},
		{"not a container", []byte("this is not an audiobook"), ".m4b"},
		{"empty file", nil, ".mp3"},
		{"mp4 with no moov", box("ftyp", []byte("M4B ")), ".m4b"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got, err := DurationFrom(bytes.NewReader(c.data), c.ext); err == nil {
				t.Errorf("got %v, want an error rather than a guess", got)
			}
		})
	}
}

func TestProbeDuration(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "track.m4b")
	if err := os.WriteFile(path, buildM4B(120, 1024, false), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := ProbeDuration(path)
	if err != nil || math.Abs(got-120) > 0.001 {
		t.Errorf("ProbeDuration = %v, %v; want 120", got, err)
	}

	if _, err := ProbeDuration(filepath.Join(dir, "absent.m4b")); err == nil {
		t.Error("probing a missing file should fail")
	}
}
