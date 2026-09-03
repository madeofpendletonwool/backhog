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

// buildOpus writes a structurally valid Ogg Opus stream: an OpusHead page and
// an end-of-stream page whose granule position carries the length. preSkip is
// a parameter so a test can prove the priming samples are subtracted rather
// than counted as audio.
func buildOpus(seconds float64, preSkip uint16) []byte {
	granule := uint64(seconds*48000) + uint64(preSkip)

	head := make([]byte, 19)
	copy(head, "OpusHead")
	head[8], head[9] = 1, 2 // version, channel count
	binary.LittleEndian.PutUint16(head[10:12], preSkip)
	binary.LittleEndian.PutUint32(head[12:16], 48000)

	var out bytes.Buffer
	out.Write(oggPage(head, 0x02, 0, 0))
	out.Write(oggPage([]byte("audio"), 0x04, granule, 1))
	return out.Bytes()
}

// oggPage frames one packet as a single Ogg page with a real CRC. Packets
// must fit one lacing segment, which every header here does.
func oggPage(packet []byte, flags byte, granule uint64, sequence uint32) []byte {
	page := make([]byte, 0, 28+len(packet))
	page = append(page, "OggS"...)
	page = append(page, 0, flags)
	page = binary.LittleEndian.AppendUint64(page, granule)
	page = binary.LittleEndian.AppendUint32(page, 0xB4C4C0DE) // stream serial
	page = binary.LittleEndian.AppendUint32(page, sequence)
	page = binary.LittleEndian.AppendUint32(page, 0) // CRC placeholder
	page = append(page, 1, byte(len(packet)))
	page = append(page, packet...)

	// Ogg's CRC32: polynomial 0x04c11db7, no reflection, no final inversion,
	// over the page with its CRC field zeroed.
	var crc uint32
	for _, b := range page {
		crc ^= uint32(b) << 24
		for i := 0; i < 8; i++ {
			if crc&0x80000000 != 0 {
				crc = (crc << 1) ^ 0x04c11db7
			} else {
				crc <<= 1
			}
		}
	}
	binary.LittleEndian.PutUint32(page[22:26], crc)
	return page
}
