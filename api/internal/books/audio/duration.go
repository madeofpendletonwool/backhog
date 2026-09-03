package audio

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/tcolgate/mp3"
)

// Duration extraction, in pure Go. The API image is distroless and CGO-free
// and stays that way, so there is no ffprobe to shell out to: MP3 duration
// comes from summing frame headers, MP4 (m4a/m4b) duration from the mvhd box,
// and Ogg Opus duration from the last page's granule position. All read
// headers only — no decoding, and only MP3 reads the whole file.
//
// The scanner (internal/media) fills duration_seconds from these same
// functions at inventory time; the timeline calls them again for the rows
// where that did not work, because a timeline without durations cannot place
// a track on it at all.

// ProbeDuration measures an audio file's play time in seconds, dispatching on
// its extension. It returns an error rather than a guess: an unmeasurable
// file makes the timeline degraded, which is a state the API reports.
func ProbeDuration(path string) (float64, error) {
	f, err := os.Open(path) // O_RDONLY: the media roots are read-only
	if err != nil {
		return 0, err
	}
	defer f.Close()
	return DurationFrom(f, strings.ToLower(filepath.Ext(path)))
}

// DurationFrom measures play time from an already-open container, for callers
// that have the file open for other reasons. ext selects the parser and is
// lower-cased with its leading dot (".mp3").
func DurationFrom(r io.ReadSeeker, ext string) (float64, error) {
	if _, err := r.Seek(0, io.SeekStart); err != nil {
		return 0, err
	}
	var (
		seconds float64
		err     error
	)
	switch ext {
	case ".mp3":
		seconds, err = mp3Duration(r)
	case ".m4a", ".m4b":
		seconds, err = mp4Duration(r)
	case ".opus":
		seconds, err = opusDuration(r)
	default:
		return 0, fmt.Errorf("audio: no duration parser for %q", ext)
	}
	if err != nil {
		return 0, err
	}
	if seconds <= 0 {
		return 0, fmt.Errorf("audio: no usable duration in container")
	}
	return seconds, nil
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

// mp4Duration reads the movie header (mvhd) box of an MP4 container (m4a,
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
	return 0, fmt.Errorf("audio: no mvhd box in moov")
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
		return 0, fmt.Errorf("audio: mvhd has no usable duration")
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
		err = fmt.Errorf("audio: box %q extends to end of file; unsupported", name)
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

// Opus in an Ogg container. Duration comes from the stream's own bookkeeping
// rather than any decoding: the last page's granule position counts elapsed
// samples, and an Opus granule is always at 48 kHz regardless of the rate the
// audio was captured at (RFC 7845 §4). The encoder's pre-skip — priming
// samples that exist only to warm the decoder up and are not part of the
// recording — is subtracted, exactly as a player does when it reports length.
const (
	// opusGranuleRate is the fixed granule clock, in Hz.
	opusGranuleRate = 48000
	// oggPageHeaderSize is the fixed part of a page header, before the
	// per-page segment table.
	oggPageHeaderSize = 27
	// opusTailWindow is how much of the file's end is searched for the final
	// page. A page's payload is at most 255*255 bytes, so 64 KiB always spans
	// at least one complete page boundary.
	opusTailWindow = 64 * 1024
)

// opusDuration measures an Ogg Opus stream's play time. Header reads only: the
// first page for the pre-skip and the last 64 KiB for the final granule.
func opusDuration(r io.ReadSeeker) (float64, error) {
	preSkip, err := opusPreSkip(r)
	if err != nil {
		return 0, err
	}
	granule, err := opusLastGranule(r)
	if err != nil {
		return 0, err
	}
	if granule <= uint64(preSkip) {
		// A stream whose whole length is priming samples has no audio in it.
		return 0, fmt.Errorf("audio: ogg opus granule %d does not exceed pre-skip %d", granule, preSkip)
	}
	return float64(granule-uint64(preSkip)) / opusGranuleRate, nil
}

// opusPreSkip reads the OpusHead identification packet, which RFC 7845
// requires to be alone in the stream's first page, and returns its pre-skip.
func opusPreSkip(r io.ReadSeeker) (uint16, error) {
	if _, err := r.Seek(0, io.SeekStart); err != nil {
		return 0, err
	}
	// The identification header is 19 bytes; it starts right after the page
	// header and its segment table.
	var head [oggPageHeaderSize]byte
	if _, err := io.ReadFull(r, head[:]); err != nil {
		return 0, fmt.Errorf("audio: ogg first page: %w", err)
	}
	if string(head[0:4]) != "OggS" {
		return 0, fmt.Errorf("audio: not an ogg stream")
	}
	segments := int(head[26])
	if _, err := r.Seek(int64(segments), io.SeekCurrent); err != nil {
		return 0, err
	}
	var packet [19]byte
	if _, err := io.ReadFull(r, packet[:]); err != nil {
		return 0, fmt.Errorf("audio: ogg opus identification header: %w", err)
	}
	if string(packet[0:8]) != "OpusHead" {
		return 0, fmt.Errorf("audio: ogg stream is not opus")
	}
	// Bytes 10-11 are the pre-skip, little-endian like every Ogg field.
	return binary.LittleEndian.Uint16(packet[10:12]), nil
}

// opusLastGranule returns the granule position of the stream's final page. It
// scans the tail for the last "OggS" capture pattern rather than walking every
// page from the start, so an eleven-hour book costs one small read.
func opusLastGranule(r io.ReadSeeker) (uint64, error) {
	size, err := r.Seek(0, io.SeekEnd)
	if err != nil {
		return 0, err
	}
	window := int64(opusTailWindow)
	if window > size {
		window = size
	}
	if _, err := r.Seek(size-window, io.SeekStart); err != nil {
		return 0, err
	}
	tail := make([]byte, window)
	if _, err := io.ReadFull(r, tail); err != nil {
		return 0, fmt.Errorf("audio: ogg tail: %w", err)
	}
	// Walk backwards to the last header that has room for its granule field.
	for i := len(tail) - oggPageHeaderSize; i >= 0; i-- {
		if string(tail[i:i+4]) != "OggS" {
			continue
		}
		return binary.LittleEndian.Uint64(tail[i+6 : i+14]), nil
	}
	return 0, fmt.Errorf("audio: no ogg page header in the last %d bytes", window)
}
