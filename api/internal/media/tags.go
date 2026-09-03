package media

import (
	"encoding/json"
	"log/slog"
	"os"

	"github.com/dhowden/tag"

	"github.com/collinpendleton/backhog/api/internal/books/audio"
)

// audioTags is the JSON shape of the container_metadata column for an audio
// file: the embedded ID3/MP4/Vorbis tag set, under the tag names the
// containers actually use. Mapping those onto "author" and "narrator" is an
// interpretation the attach stage makes; the raw tags are kept faithful here.
// The epub side of the same column is bookTags, in sidecar.go.
type audioTags struct {
	Title       string `json:"title,omitempty"`
	Artist      string `json:"artist,omitempty"`
	AlbumArtist string `json:"album_artist,omitempty"`
	Album       string `json:"album,omitempty"`
	Composer    string `json:"composer,omitempty"`
	Genre       string `json:"genre,omitempty"`
	Year        int    `json:"year,omitempty"`
	Track       int    `json:"track,omitempty"`
	TrackCount  int    `json:"track_count,omitempty"`
	Comment     string `json:"comment,omitempty"`
}

// readAudioMetadata opens an audio file read-only and extracts the embedded
// tags and duration. Both results are best-effort: a file with no parsable
// tags or no readable duration still scans — the columns just stay NULL and
// the align worker fills duration in later.
func readAudioMetadata(path, ext string) (json.RawMessage, *float64) {
	f, err := os.Open(path) // O_RDONLY: the media roots are read-only
	if err != nil {
		slog.Warn("media scan open audio", "path", path, "error", err)
		return nil, nil
	}
	defer f.Close()

	metadata := readAudioTags(f)
	// readAudioDuration rewinds first: the tag read left the offset wherever
	// it finished.
	return metadata, readAudioDuration(f, ext)
}

// readAudioTags pulls the ID3 (mp3), MP4 atom (m4a/m4b) or Vorbis comment
// (opus) tag set. The dispatch is the tag library's own: it sniffs the
// container rather than trusting the extension.
func readAudioTags(f *os.File) json.RawMessage {
	m, err := tag.ReadFrom(f)
	if err != nil {
		// Untagged files are normal; anything else is worth a debug line only.
		slog.Debug("media scan tags", "path", f.Name(), "error", err)
		return nil
	}
	track, trackCount := m.Track()
	tags := audioTags{
		Title:       m.Title(),
		Artist:      m.Artist(),
		AlbumArtist: m.AlbumArtist(),
		Album:       m.Album(),
		Composer:    m.Composer(),
		Genre:       m.Genre(),
		Year:        m.Year(),
		Track:       track,
		TrackCount:  trackCount,
		Comment:     m.Comment(),
	}
	if tags == (audioTags{}) {
		return nil
	}
	raw, err := json.Marshal(tags)
	if err != nil {
		return nil
	}
	return raw
}

// readAudioDuration extracts play time in seconds. The container parsers
// live in books/audio, which needs the same numbers to place a track on a
// book's timeline; the scanner just fills the column in early so the timeline
// rarely has to re-open the file.
func readAudioDuration(f *os.File, ext string) *float64 {
	seconds, err := audio.DurationFrom(f, ext)
	if err != nil {
		return nil
	}
	return &seconds
}
