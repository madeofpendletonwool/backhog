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

// Opus duration is the last page's granule minus the encoder's pre-skip, over
// the fixed 48 kHz granule clock. The pre-skip cases are the point: counting
// the priming samples as audio is the easy way to get this subtly wrong.
func TestDurationFromOpus(t *testing.T) {
	for _, tc := range []struct {
		name    string
		seconds float64
		preSkip uint16
	}{
		{"no pre-skip", 90, 0},
		{"typical libopus lookahead", 3671.5, 312},
		{"large pre-skip", 60, 6528},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := DurationFrom(bytes.NewReader(buildOpus(tc.seconds, tc.preSkip)), ".opus")
			if err != nil {
				t.Fatalf("duration: %v", err)
			}
			if math.Abs(got-tc.seconds) > 0.001 {
				t.Errorf("duration = %v, want %v", got, tc.seconds)
			}
		})
	}
}

// An Ogg stream this tool cannot measure is refused rather than guessed at,
// the same way a broken MP4 is.
func TestDurationRefusesUnmeasurableOpus(t *testing.T) {
	cases := []struct {
		name string
		data []byte
	}{
		{"not an ogg stream", []byte("this is not an audiobook at all")},
		{"ogg that is not opus", append([]byte("OggS"), make([]byte, 64)...)},
		{"whole stream is pre-skip", buildOpus(0, 312)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got, err := DurationFrom(bytes.NewReader(c.data), ".opus"); err == nil {
				t.Errorf("got %v, want an error rather than a guess", got)
			}
		})
	}
}

// The streamer hands the browser a type rather than letting it sniff one, so
// every inventoried extension needs an entry here and nothing else may get a
// media type by accident.
func TestContentType(t *testing.T) {
	for _, tc := range []struct{ path, want string }{
		{"Book/01 - Chapter.mp3", "audio/mpeg"},
		{"Book/whole.m4a", "audio/mp4"},
		{"Book/whole.m4b", "audio/mp4"},
		{"Book/whole.opus", "audio/ogg"},
		{"Book/WHOLE.OPUS", "audio/ogg"},
		{"Book/cover.jpg", "application/octet-stream"},
	} {
		if got := ContentType(tc.path); got != tc.want {
			t.Errorf("ContentType(%q) = %q, want %q", tc.path, got, tc.want)
		}
	}
}
