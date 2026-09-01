// Package audio turns the ordered media files attached to a book into one
// continuous listening timeline and serves their bytes.
//
// An audiobook is N files that must behave like a single tape. Everything
// outside this package — the player, the alignment anchors, stored progress —
// works in **global seconds**: one number from 0 to the book's total
// duration. Track boundaries are reasoned about here and nowhere else, so
// re-ordering, re-attaching or re-measuring files cannot leave two different
// notions of "where am I" in the codebase.
package audio

import "errors"

var (
	// ErrEmptyTimeline reports a book with no audio attached.
	ErrEmptyTimeline = errors.New("audio: no audio attached to this book")
	// ErrOutOfRange reports a position outside the timeline (or outside a
	// track whose length is actually known).
	ErrOutOfRange = errors.New("audio: position outside the timeline")
	// ErrUnknownTrack reports a track id that is not on this timeline.
	ErrUnknownTrack = errors.New("audio: track is not on this timeline")
)

// Track is one file's slot on the timeline. Duration is in seconds and is 0
// exactly when Measured is false — a file whose length could not be derived
// from its container headers still holds its place in the running order, it
// just contributes no time.
type Track struct {
	MediaFileID int64   `json:"id"`
	Number      int     `json:"track_number"`
	Path        string  `json:"path"`
	Title       string  `json:"title"`
	SizeBytes   int64   `json:"size_bytes"`
	Duration    float64 `json:"duration_seconds"`
	GlobalStart float64 `json:"global_start"`
	Measured    bool    `json:"measured"`
	// Missing marks a file whose path is currently absent from its root —
	// an unmounted NAS, not a deleted book. Its bytes cannot be served.
	Missing bool `json:"missing"`
}

// Timeline is a book's audio as one continuous span of global seconds.
// Degraded means at least one track's duration is unknown, so the offsets of
// every later track are wrong by that track's real length. It is surfaced
// rather than papered over with a guess: a silently wrong timeline would
// mis-place every alignment anchor after the first bad file.
type Timeline struct {
	Tracks        []Track `json:"tracks"`
	TotalDuration float64 `json:"total_duration"`
	Degraded      bool    `json:"degraded"`
}

// Position is a point on the timeline expressed against one track.
type Position struct {
	MediaFileID  int64   `json:"id"`
	Number       int     `json:"track_number"`
	TrackSeconds float64 `json:"track_seconds"`
}

// NewTimeline lays the given tracks out in the order supplied — which is the
// attach order (track_number), not the order the filesystem happened to hand
// them over. It fills in Number, GlobalStart, the total and the degraded
// flag; callers supply only the per-file facts.
func NewTimeline(tracks []Track) Timeline {
	tl := Timeline{Tracks: make([]Track, len(tracks))}
	offset := 0.0
	for i, t := range tracks {
		if !t.Measured || t.Duration <= 0 {
			t.Measured = false
			t.Duration = 0
			tl.Degraded = true
		}
		t.Number = i + 1
		t.GlobalStart = offset
		offset += t.Duration
		tl.Tracks[i] = t
	}
	tl.TotalDuration = offset
	return tl
}

// Locate maps a global second to the track that owns it. Tracks own
// [GlobalStart, GlobalStart+Duration), so a boundary second belongs to the
// track that is starting, and the very end of the book resolves to the end of
// the last track that holds any time.
func (t Timeline) Locate(global float64) (Position, error) {
	if len(t.Tracks) == 0 {
		return Position{}, ErrEmptyTimeline
	}
	if global < 0 || global > t.TotalDuration {
		return Position{}, ErrOutOfRange
	}
	for _, tr := range t.Tracks {
		if tr.Duration > 0 && global < tr.GlobalStart+tr.Duration {
			return Position{
				MediaFileID:  tr.MediaFileID,
				Number:       tr.Number,
				TrackSeconds: global - tr.GlobalStart,
			}, nil
		}
	}
	// global == TotalDuration, or nothing on this timeline is measured: the
	// end of the last track that owns any time, else the start of the book.
	last := t.Tracks[0]
	for _, tr := range t.Tracks {
		if tr.Duration > 0 {
			last = tr
		}
	}
	return Position{MediaFileID: last.MediaFileID, Number: last.Number, TrackSeconds: last.Duration}, nil
}

// Global is Locate's inverse: an offset within one track becomes a global
// second. Offsets past a measured track's end are rejected; on an unmeasured
// track there is no end to check against, so the sum is returned as the best
// available answer and the timeline's Degraded flag is the caller's warning.
func (t Timeline) Global(mediaFileID int64, trackSeconds float64) (float64, error) {
	if trackSeconds < 0 {
		return 0, ErrOutOfRange
	}
	for _, tr := range t.Tracks {
		if tr.MediaFileID != mediaFileID {
			continue
		}
		if tr.Measured && trackSeconds > tr.Duration {
			return 0, ErrOutOfRange
		}
		return tr.GlobalStart + trackSeconds, nil
	}
	return 0, ErrUnknownTrack
}

// Track returns the timeline slot for one media file id.
func (t Timeline) Track(mediaFileID int64) (Track, bool) {
	for _, tr := range t.Tracks {
		if tr.MediaFileID == mediaFileID {
			return tr, true
		}
	}
	return Track{}, false
}
