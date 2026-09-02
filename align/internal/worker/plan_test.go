package worker

import (
	"errors"
	"math"
	"testing"

	"github.com/collinpendleton/backhog/align/internal/api"
	"github.com/collinpendleton/backhog/align/internal/transcribe"
)

func TestPlanTracksLaysOutGlobalOffsets(t *testing.T) {
	tracks, err := planTracks([]api.TrackFile{
		{Path: "/media/one.mp3", Duration: 100},
		{Path: "/media/two.mp3", Duration: 250.5},
		{Path: "/media/three.mp3", Duration: 40},
	})
	if err != nil {
		t.Fatalf("planTracks: %v", err)
	}

	want := []float64{0, 100, 350.5}
	for i, track := range tracks {
		if track.Offset != want[i] {
			t.Errorf("track %d offset = %v, want %v", i+1, track.Offset, want[i])
		}
		if track.Number != i+1 {
			t.Errorf("track %d number = %d", i+1, track.Number)
		}
	}
}

func TestPlanTracksRejectsMissingMedia(t *testing.T) {
	_, err := planTracks([]api.TrackFile{
		{Path: "/media/one.mp3", Duration: 100},
		{Path: "/media/two.mp3", Duration: 100, Missing: true},
	})

	var f failure
	if !errors.As(err, &f) {
		t.Fatalf("want a failure, got %v", err)
	}
	if f.Code != failMediaMissing {
		t.Errorf("code = %q, want %q", f.Code, failMediaMissing)
	}
}

func TestPlanTracksRejectsUnmeasuredTrackBeforeTheEnd(t *testing.T) {
	// An unmeasured track in the middle shifts every later track's
	// global offset, so the whole transcript would be wrong. That has to
	// be a distinct, loud failure rather than silently bad timestamps.
	_, err := planTracks([]api.TrackFile{
		{Path: "/media/one.mp3", Duration: 0},
		{Path: "/media/two.mp3", Duration: 100},
	})

	var f failure
	if !errors.As(err, &f) {
		t.Fatalf("want a failure, got %v", err)
	}
	if f.Code != failTimelineDegraded {
		t.Errorf("code = %q, want %q", f.Code, failTimelineDegraded)
	}
}

func TestPlanTracksAllowsUnmeasuredFinalTrack(t *testing.T) {
	tracks, err := planTracks([]api.TrackFile{
		{Path: "/media/one.mp3", Duration: 100},
		{Path: "/media/two.mp3", Duration: 0},
	})
	if err != nil {
		t.Fatalf("planTracks: %v", err)
	}
	if tracks[1].Measured {
		t.Error("final track should be marked unmeasured")
	}
	if tracks[1].Offset != 100 {
		t.Errorf("final track offset = %v, want 100", tracks[1].Offset)
	}
}

func TestPlanTracksRejectsEmptyClaim(t *testing.T) {
	if _, err := planTracks(nil); err == nil {
		t.Fatal("want an error for a job with no tracks")
	}
}

func TestPlanChunksTilesTheTrackExactly(t *testing.T) {
	chunks := planChunks(150, 60, 5)
	if len(chunks) != 3 {
		t.Fatalf("chunk count = %d, want 3", len(chunks))
	}

	// The content windows must tile [0, duration) with no gap and no
	// overlap: that is what makes the decode overlap free of duplicates.
	if chunks[0].ContentStart != 0 || chunks[0].ContentEnd != 60 {
		t.Errorf("chunk 0 content = [%v,%v)", chunks[0].ContentStart, chunks[0].ContentEnd)
	}
	if chunks[1].ContentStart != 60 || chunks[1].ContentEnd != 120 {
		t.Errorf("chunk 1 content = [%v,%v)", chunks[1].ContentStart, chunks[1].ContentEnd)
	}
	if chunks[2].ContentStart != 120 || chunks[2].ContentEnd != 150 {
		t.Errorf("chunk 2 content = [%v,%v)", chunks[2].ContentStart, chunks[2].ContentEnd)
	}
	if !chunks[2].Last {
		t.Error("final chunk should be marked last")
	}

	// The decode window adds context on both sides without running off
	// either end of the track.
	if chunks[0].DecodeStart != 0 || chunks[0].DecodeDuration != 65 {
		t.Errorf("chunk 0 decode = %v+%v", chunks[0].DecodeStart, chunks[0].DecodeDuration)
	}
	if chunks[1].DecodeStart != 55 || chunks[1].DecodeDuration != 70 {
		t.Errorf("chunk 1 decode = %v+%v", chunks[1].DecodeStart, chunks[1].DecodeDuration)
	}
	if chunks[2].DecodeStart != 115 || chunks[2].DecodeDuration != 35 {
		t.Errorf("chunk 2 decode = %v+%v", chunks[2].DecodeStart, chunks[2].DecodeDuration)
	}

	total := 0.0
	for _, c := range chunks {
		total += c.Kept()
	}
	if math.Abs(total-150) > 1e-9 {
		t.Errorf("kept windows total %v, want 150", total)
	}
}

func TestPlanChunksHandlesShortAndEmptyTracks(t *testing.T) {
	if got := planChunks(30, 600, 5); len(got) != 1 || got[0].DecodeDuration != 30 {
		t.Errorf("short track = %+v", got)
	}
	if got := planChunks(0, 600, 5); got != nil {
		t.Errorf("zero-length track = %+v, want nil", got)
	}
}

func TestGlobalSegmentsMapsOntoTheBookTimeline(t *testing.T) {
	tr := track{Number: 2, Offset: 1000, Duration: 150, Measured: true}
	c := planChunks(150, 60, 5)[1] // decodes from 55, keeps [60, 120)

	got := globalSegments([]transcribe.Segment{
		{Start: 2, End: 4, Text: "before the window"},  // 57-59, dropped
		{Start: 6, End: 8, Text: "inside the window"},  // 61-63, kept
		{Start: 64, End: 68, Text: "after the window"}, // 119-123, mid 121, dropped
	}, tr, c, 150)

	if len(got) != 1 {
		t.Fatalf("kept %d segments, want 1: %+v", len(got), got)
	}
	if got[0].Text != "inside the window" {
		t.Errorf("kept %q", got[0].Text)
	}
	if got[0].AudioStart != 1061 || got[0].AudioEnd != 1063 {
		t.Errorf("global span = [%v,%v], want [1061,1063]", got[0].AudioStart, got[0].AudioEnd)
	}
}

func TestGlobalSegmentsKeepsTheTailOfTheFinalChunk(t *testing.T) {
	// Whisper can round a closing segment a hair past the measured end
	// of a track. Dropping it would lose the last sentence of the book,
	// so the final chunk keeps everything and clamps instead.
	tr := track{Number: 1, Offset: 0, Duration: 100, Measured: true}
	c := planChunks(100, 60, 5)[1]

	got := globalSegments([]transcribe.Segment{
		{Start: 44, End: 47, Text: "the end"}, // 99-102 on a 100s track
	}, tr, c, 100)

	if len(got) != 1 {
		t.Fatalf("kept %d segments, want 1", len(got))
	}
	if got[0].AudioEnd != 100 {
		t.Errorf("end = %v, want clamped to 100", got[0].AudioEnd)
	}
}

func TestGlobalSegmentsDropsNonSpeech(t *testing.T) {
	tr := track{Number: 1, Duration: 60, Measured: true}
	c := planChunks(60, 60, 5)[0]

	got := globalSegments([]transcribe.Segment{
		{Start: 1, End: 2, Text: "[BLANK_AUDIO]"},
		{Start: 3, End: 4, Text: "  "},
		{Start: 5, End: 6, Text: "real words"},
	}, tr, c, 60)

	if len(got) != 1 || got[0].Text != "real words" {
		t.Errorf("kept %+v", got)
	}
}
