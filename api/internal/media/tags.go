package media

import (
	"archive/zip"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/dhowden/tag"
	"github.com/tcolgate/mp3"
)

// audioTags is the JSON shape of the container_metadata column: the embedded
// ID3/MP4 tag set, under the tag names the containers actually use. Mapping
// those onto "author" and "narrator" is an interpretation the attach stage
// makes; the raw tags are kept faithful here.
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

	if _, err := f.Seek(0, io.SeekStart); err != nil {
		slog.Warn("media scan rewind", "path", path, "error", err)
		return metadata, nil
	}
	duration := readAudioDuration(f, ext)
	return metadata, duration
}

// readAudioTags pulls the ID3 (mp3) or MP4 atom (m4a/m4b) tag set.
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

// readAudioDuration extracts play time in seconds. MP3 needs a frame-header
// walk (the duration is not in the tags); MP4 carries it in the mvhd atom.
func readAudioDuration(f *os.File, ext string) *float64 {
	var (
		seconds float64
		err     error
	)
	switch ext {
	case ".mp3":
		seconds, err = mp3Duration(f)
	case ".m4a", ".m4b":
		seconds, err = mp4Duration(f)
	default:
		return nil
	}
	if err != nil || seconds <= 0 {
		return nil
	}
	return &seconds
}

// mp3Duration sums frame header durations without decoding audio.
func mp3Duration(r io.Reader) (float64, error) {
	d := mp3.NewDecoder(r)
	var frame mp3.Frame
	var skipped int
	var total time.Duration
	for {
		if err := d.Decode(&frame, &skipped); err != nil {
			if err == io.EOF {
				break
			}
			return 0, err
		}
		total += frame.Duration()
	}
	return total.Seconds(), nil
}

// mp4Duration reads the movie header (mvhd) atom of an MP4 container (m4a,
// m4b): duration divided by its timescale is the play time in seconds.
func mp4Duration(r io.ReadSeeker) (float64, error) {
	for {
		name, size, err := readBoxHeader(r)
		if err != nil {
			return 0, err // io.EOF: no moov box, no duration
		}
		if name == "moov" {
			return mvhdDuration(r, size)
		}
		if _, err := r.Seek(size, io.SeekCurrent); err != nil {
			return 0, err
		}
	}
}

// mvhdDuration walks the children of a moov box looking for mvhd.
func mvhdDuration(r io.ReadSeeker, moovSize int64) (float64, error) {
	remaining := moovSize
	for remaining > 0 {
		name, size, err := readBoxHeader(r)
		if err != nil {
			return 0, err
		}
		remaining -= 8 + size
		if name == "mvhd" {
			return parseMVHD(r)
		}
		if _, err := r.Seek(size, io.SeekCurrent); err != nil {
			return 0, err
		}
	}
	return 0, fmt.Errorf("no mvhd atom in moov")
}

// parseMVHD reads version, timescale and duration from an mvhd payload that
// the stream is positioned at (right after the box header).
func parseMVHD(r io.Reader) (float64, error) {
	var version byte
	if err := binary.Read(r, binary.BigEndian, &version); err != nil {
		return 0, err
	}
	var flags [3]byte
	if _, err := io.ReadFull(r, flags[:]); err != nil {
		return 0, err
	}

	var timescale uint32
	if version == 1 {
		var created, modified uint64 // creation and modification times
		var duration uint64
		if err := binary.Read(r, binary.BigEndian, &created); err != nil {
			return 0, err
		}
		if err := binary.Read(r, binary.BigEndian, &modified); err != nil {
			return 0, err
		}
		if err := binary.Read(r, binary.BigEndian, &timescale); err != nil {
			return 0, err
		}
		if err := binary.Read(r, binary.BigEndian, &duration); err != nil {
			return 0, err
		}
		return mvhdSeconds(timescale, duration)
	}

	var created, modified, duration uint32
	if err := binary.Read(r, binary.BigEndian, &created); err != nil {
		return 0, err
	}
	if err := binary.Read(r, binary.BigEndian, &modified); err != nil {
		return 0, err
	}
	if err := binary.Read(r, binary.BigEndian, &timescale); err != nil {
		return 0, err
	}
	if err := binary.Read(r, binary.BigEndian, &duration); err != nil {
		return 0, err
	}
	return mvhdSeconds(timescale, uint64(duration))
}

func mvhdSeconds(timescale uint32, duration uint64) (float64, error) {
	if timescale == 0 || duration == 0 {
		return 0, fmt.Errorf("mvhd has no usable duration")
	}
	return float64(duration) / float64(timescale), nil
}

// readBoxHeader reads one MP4 box header, returning its type and payload
// size (box size minus the 8 header bytes).
func readBoxHeader(r io.Reader) (name string, payload int64, err error) {
	var size uint32
	if err = binary.Read(r, binary.BigEndian, &size); err != nil {
		return
	}
	var typ [4]byte
	if _, err = io.ReadFull(r, typ[:]); err != nil {
		return
	}
	name = string(typ[:])
	if size == 0 {
		err = fmt.Errorf("box %q extends to end of file; unsupported", name)
		return
	}
	payload = int64(size) - 8
	if size == 1 {
		// 64-bit largesize variant: the real size follows the header.
		var largesize uint64
		if err = binary.Read(r, binary.BigEndian, &largesize); err != nil {
			return
		}
		payload = int64(largesize) - 16
	}
	return
}

// epubEncrypted reports whether an EPUB carries META-INF/encryption.xml —
// the marker for DRM-protected books, which this tool does not support and
// does not work around.
func epubEncrypted(path string) (bool, error) {
	r, err := zip.OpenReader(path)
	if err != nil {
		return false, err
	}
	defer r.Close()
	for _, f := range r.File {
		if strings.TrimSpace(f.Name) == "META-INF/encryption.xml" {
			return true, nil
		}
	}
	return false, nil
}
