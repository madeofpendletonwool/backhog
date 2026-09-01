package audio

import (
	"bytes"
	"encoding/binary"
)

// buildM4B writes a minimal but structurally valid MP4 audiobook:
// ftyp(M4B) + moov(mvhd) + an mdat of padBytes zeros, so a fixture can be
// made large enough to exercise real byte ranges. v1 selects the 64-bit mvhd
// layout, which real long audiobooks use.
func buildM4B(seconds float64, padBytes int, v1 bool) []byte {
	var mvhdBody []byte
	if v1 {
		mvhdBody = concat(
			[]byte{1, 0, 0, 0}, // version 1 + flags
			be64(0), be64(0),   // creation, modification
			be32(1000),                 // timescale
			be64(uint64(seconds*1000)), // duration
		)
	} else {
		mvhdBody = concat(
			[]byte{0, 0, 0, 0}, // version 0 + flags
			be32(0), be32(0),   // creation, modification
			be32(1000),                 // timescale
			be32(uint32(seconds*1000)), // duration
		)
	}

	moov := box("moov", box("mvhd", mvhdBody))
	ftyp := box("ftyp", concat([]byte("M4B "), be32(0), []byte("M4B mp42")))
	mdat := box("mdat", bytes.Repeat([]byte{0xAB}, padBytes))
	return concat(ftyp, moov, mdat)
}

// buildMP3 writes n synthetic MPEG1 Layer III frames (128kbps, 44.1kHz,
// 1152 samples per frame ≈ 26.12ms each, 417 bytes each).
func buildMP3(frames int) []byte {
	var out bytes.Buffer
	frame := make([]byte, 417)
	frame[0], frame[1], frame[2], frame[3] = 0xFF, 0xFB, 0x90, 0x00
	for range frames {
		out.Write(frame)
	}
	return out.Bytes()
}

func box(typ string, body ...[]byte) []byte {
	payload := concat(body...)
	out := make([]byte, 4, len(payload)+8)
	binary.BigEndian.PutUint32(out, uint32(len(payload)+8))
	out = append(out, typ...)
	return append(out, payload...)
}

func concat(parts ...[]byte) []byte {
	var buf bytes.Buffer
	for _, p := range parts {
		buf.Write(p)
	}
	return buf.Bytes()
}

func be32(v uint32) []byte {
	b := make([]byte, 4)
	binary.BigEndian.PutUint32(b, v)
	return b
}

func be64(v uint64) []byte {
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, v)
	return b
}
