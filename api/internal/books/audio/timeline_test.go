package audio

import (
	"errors"
	"math"
	"testing"
)

// measured is a track whose length is known.
func measured(id int64, seconds float64) Track {
	return Track{MediaFileID: id, Duration: seconds, Measured: true}
}

func TestNewTimelineLaysOutOffsets(t *testing.T) {
	tl := NewTimeline([]Track{measured(10, 100), measured(11, 50), measured(12, 25)})

	if tl.Degraded {
		t.Error("all durations known, timeline should not be degraded")
	}
	if tl.TotalDuration != 175 {
		t.Errorf("total = %v, want 175", tl.TotalDuration)
	}
	wantStarts := []float64{0, 100, 150}
	for i, tr := range tl.Tracks {
		if tr.Number != i+1 {
			t.Errorf("track %d numbered %d", i, tr.Number)
		}
		if tr.GlobalStart != wantStarts[i] {
			t.Errorf("track %d start = %v, want %v", i, tr.GlobalStart, wantStarts[i])
		}
	}
}

func TestLocateAcrossBoundaries(t *testing.T) {
	tl := NewTimeline([]Track{measured(10, 100), measured(11, 50), measured(12, 25)})

	cases := []struct {
		global   float64
		wantID   int64
		wantSecs float64
	}{
		{0, 10, 0},       // the very start
		{99.5, 10, 99.5}, // inside the first track
		{100, 11, 0},     // a boundary belongs to the track that is starting
		{149, 11, 49},    // inside the second
		{150, 12, 0},     // second boundary
		{175, 12, 25},    // the end of the book resolves to the end of the last track
	}
	for _, c := range cases {
		pos, err := tl.Locate(c.global)
		if err != nil {
			t.Fatalf("locate %v: %v", c.global, err)
		}
		if pos.MediaFileID != c.wantID || math.Abs(pos.TrackSeconds-c.wantSecs) > 1e-9 {
			t.Errorf("locate %v = track %d @ %v, want track %d @ %v",
				c.global, pos.MediaFileID, pos.TrackSeconds, c.wantID, c.wantSecs)
		}
	}

	for _, bad := range []float64{-0.1, 175.1} {
		if _, err := tl.Locate(bad); !errors.Is(err, ErrOutOfRange) {
			t.Errorf("locate %v: err = %v, want ErrOutOfRange", bad, err)
		}
	}
}

func TestGlobalIsLocatesInverse(t *testing.T) {
	tl := NewTimeline([]Track{measured(10, 100), measured(11, 50), measured(12, 25)})

	for _, global := range []float64{0, 42, 100, 149.75, 150, 175} {
		pos, err := tl.Locate(global)
		if err != nil {
			t.Fatalf("locate %v: %v", global, err)
		}
		back, err := tl.Global(pos.MediaFileID, pos.TrackSeconds)
		if err != nil {
			t.Fatalf("global %d @ %v: %v", pos.MediaFileID, pos.TrackSeconds, err)
		}
		if math.Abs(back-global) > 1e-9 {
			t.Errorf("round trip of %v came back as %v", global, back)
		}
	}

	if _, err := tl.Global(11, 50.1); !errors.Is(err, ErrOutOfRange) {
		t.Errorf("past a measured track's end: err = %v, want ErrOutOfRange", err)
	}
	if _, err := tl.Global(11, -1); !errors.Is(err, ErrOutOfRange) {
		t.Errorf("negative offset: err = %v, want ErrOutOfRange", err)
	}
	if _, err := tl.Global(999, 0); !errors.Is(err, ErrUnknownTrack) {
		t.Errorf("unknown track: err = %v, want ErrUnknownTrack", err)
	}
}

// An unmeasurable file still holds its slot in the running order; it just
// owns no time, and the timeline says so instead of guessing a length.
func TestUnmeasuredTrackDegradesTimeline(t *testing.T) {
	tl := NewTimeline([]Track{
		measured(10, 100),
		{MediaFileID: 11, Duration: 30, Measured: false}, // a duration nobody could confirm
		measured(12, 25),
	})

	if !tl.Degraded {
		t.Error("timeline with an unmeasured track should be degraded")
	}
	if tl.TotalDuration != 125 {
		t.Errorf("total = %v, want 125 (the unmeasured track contributes nothing)", tl.TotalDuration)
	}
	if tl.Tracks[1].Duration != 0 || tl.Tracks[1].Measured {
		t.Errorf("unmeasured track kept a duration: %+v", tl.Tracks[1])
	}
	if tl.Tracks[1].Number != 2 {
		t.Errorf("unmeasured track lost its place in the order: %+v", tl.Tracks[1])
	}

	// A second at the zero-length track's offset belongs to the next track
	// that actually owns time.
	pos, err := tl.Locate(100)
	if err != nil {
		t.Fatalf("locate 100: %v", err)
	}
	if pos.MediaFileID != 12 {
		t.Errorf("locate 100 = track %d, want 12", pos.MediaFileID)
	}

	// Offsets inside an unmeasured track have no end to be checked against,
	// so they resolve rather than erroring — Degraded is the warning.
	global, err := tl.Global(11, 12)
	if err != nil || global != 112 {
		t.Errorf("global on unmeasured track = %v, %v; want 112, nil", global, err)
	}
}

func TestEmptyTimeline(t *testing.T) {
	tl := NewTimeline(nil)
	if _, err := tl.Locate(0); !errors.Is(err, ErrEmptyTimeline) {
		t.Errorf("err = %v, want ErrEmptyTimeline", err)
	}
	if tl.TotalDuration != 0 || tl.Degraded {
		t.Errorf("empty timeline = %+v", tl)
	}
}

// Every track unmeasurable: the timeline is a zero-length span rather than a
// broken one, and position 0 still resolves to the first track.
func TestFullyUnmeasuredTimeline(t *testing.T) {
	tl := NewTimeline([]Track{{MediaFileID: 10}, {MediaFileID: 11}})
	if !tl.Degraded || tl.TotalDuration != 0 {
		t.Fatalf("timeline = %+v", tl)
	}
	pos, err := tl.Locate(0)
	if err != nil || pos.MediaFileID != 10 {
		t.Errorf("locate 0 = %+v, %v", pos, err)
	}
}

func TestTrackLookup(t *testing.T) {
	tl := NewTimeline([]Track{measured(10, 100), measured(11, 50)})
	if tr, ok := tl.Track(11); !ok || tr.GlobalStart != 100 {
		t.Errorf("track 11 = %+v, %v", tr, ok)
	}
	if _, ok := tl.Track(99); ok {
		t.Error("unknown track id resolved")
	}
}
