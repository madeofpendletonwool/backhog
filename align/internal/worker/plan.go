package worker

import (
	"fmt"
	"math"

	"github.com/collinpendleton/backhog/align/internal/api"
	"github.com/collinpendleton/backhog/align/internal/transcribe"
)

// track is one audio file placed on the book's global timeline. Offset
// is the sum of every earlier track's duration — the same arithmetic the
// API's Timeline does, deliberately, because a transcript timestamped
// against a different timeline than the player's is worse than no
// transcript at all.
type track struct {
	Number   int
	Path     string
	Offset   float64
	Duration float64
	// Measured is false when the API could not measure this track. Only
	// the final track may be unmeasured; anything earlier would shift
	// every track after it.
	Measured bool
}

// chunk is one slice of a track: the window ffmpeg decodes, and the
// narrower window whose segments are kept. The two differ by the overlap
// that gives Whisper context across a boundary without letting two
// chunks both claim the same sentence.
type chunk struct {
	Index          int
	Count          int
	DecodeStart    float64
	DecodeDuration float64
	ContentStart   float64
	ContentEnd     float64
	Last           bool
}

// Kept reports how much of the book this chunk is responsible for, which
// is what drives the progress fraction.
func (c chunk) Kept() float64 { return math.Max(0, c.ContentEnd-c.ContentStart) }

// planTracks lays the claimed tracks out on the global timeline and
// refuses the layouts that would produce wrong timestamps. A missing
// file and an unmeasurable one are both fatal, but for different
// reasons, and the job's error says which.
func planTracks(tracks []api.TrackFile) ([]track, error) {
	if len(tracks) == 0 {
		return nil, failure{Code: failMediaMissing, Detail: "the job has no audio tracks"}
	}

	out := make([]track, 0, len(tracks))
	offset := 0.0
	for i, t := range tracks {
		number := i + 1
		if t.Missing || t.Path == "" {
			return nil, failure{
				Code: failMediaMissing,
				Detail: fmt.Sprintf("track %d of %d is not on the media mount (%s)",
					number, len(tracks), describePath(t.Path)),
			}
		}
		measured := t.Duration > 0
		if !measured && i != len(tracks)-1 {
			return nil, failure{
				Code: failTimelineDegraded,
				Detail: fmt.Sprintf(
					"track %d of %d has no measured duration, so every later track's global timestamp would be wrong",
					number, len(tracks)),
			}
		}
		out = append(out, track{
			Number:   number,
			Path:     t.Path,
			Offset:   offset,
			Duration: t.Duration,
			Measured: measured,
		})
		offset += t.Duration
	}
	return out, nil
}

// planChunks slices one track. Chunk i owns exactly
// [i*length, (i+1)*length) of the track, and decodes overlap seconds on
// each side of that purely as context — so the windows tile the track
// with no gap and no double-claimed second.
func planChunks(duration, length, overlap float64) []chunk {
	if duration <= 0 || length <= 0 {
		return nil
	}
	count := int(math.Ceil(duration / length))
	chunks := make([]chunk, 0, count)
	for i := range count {
		contentStart := float64(i) * length
		contentEnd := math.Min(duration, contentStart+length)
		decodeStart := math.Max(0, contentStart-overlap)
		decodeEnd := math.Min(duration, contentEnd+overlap)
		chunks = append(chunks, chunk{
			Index:          i,
			Count:          count,
			DecodeStart:    decodeStart,
			DecodeDuration: decodeEnd - decodeStart,
			ContentStart:   contentStart,
			ContentEnd:     contentEnd,
			Last:           i == count-1,
		})
	}
	return chunks
}

// globalSegments puts one chunk's output on the book's timeline. A
// segment belongs to the chunk whose content window contains its
// midpoint, which is what makes the overlap free: a sentence straddling
// a boundary is transcribed twice, with full context both times, and
// stored once.
func globalSegments(segments []transcribe.Segment, t track, c chunk, trackDuration float64) []api.Segment {
	out := make([]api.Segment, 0, len(segments))
	for _, s := range segments {
		start := c.DecodeStart + s.Start
		end := c.DecodeStart + s.End
		if end < start {
			end = start
		}
		mid := (start + end) / 2
		if mid < c.ContentStart {
			continue
		}
		// The final chunk keeps everything past its start: Whisper can
		// round a closing segment a hair beyond the measured end, and
		// dropping the last sentence of a track is a real loss.
		if !c.Last && mid >= c.ContentEnd {
			continue
		}
		start = clamp(start, 0, trackDuration)
		end = clamp(end, start, trackDuration)
		if !transcribe.Speech(s.Text) {
			continue
		}
		out = append(out, api.Segment{
			AudioStart: t.Offset + start,
			AudioEnd:   t.Offset + end,
			Text:       s.Text,
		})
	}
	return out
}

func clamp(v, lo, hi float64) float64 {
	if hi < lo {
		hi = lo
	}
	return math.Min(math.Max(v, lo), hi)
}

// describePath keeps a path out of the "" case in an operator-facing
// message without pretending the API told us one.
func describePath(path string) string {
	if path == "" {
		return "no resolved path"
	}
	return path
}
