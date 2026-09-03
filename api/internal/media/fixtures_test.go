package media

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
)

// Fixture builders: minimal but structurally valid mp3, m4b, opus and epub
// files so the scanner tests exercise the real tag and container parsers.

// buildMP3 writes an ID3v2.3 tag followed by n synthetic MPEG1 Layer III
// frames (128kbps, 44.1kHz, 1152 samples per frame ≈ 26.12ms each).
func buildMP3(title, artist, album string, frames int) []byte {
	var tags bytes.Buffer
	textFrame := func(id, text string) {
		payload := append([]byte{0x00}, []byte(text)...) // 0x00 = ISO-8859-1
		tags.WriteString(id)
		_ = binary.Write(&tags, binary.BigEndian, uint32(len(payload)))
		tags.Write([]byte{0x00, 0x00}) // frame flags
		tags.Write(payload)
	}
	textFrame("TIT2", title)
	textFrame("TPE1", artist)
	textFrame("TALB", album)

	var out bytes.Buffer
	out.WriteString("ID3")
	out.Write([]byte{0x03, 0x00, 0x00}) // v2.3, no flags
	n := tags.Len()
	out.Write([]byte{byte(n >> 21 & 0x7f), byte(n >> 14 & 0x7f), byte(n >> 7 & 0x7f), byte(n & 0x7f)})
	out.Write(tags.Bytes())

	// 0xFF 0xFB 0x90 0x00: MPEG1, Layer III, 128kbps, 44100Hz, no padding.
	// Frame length = 144 * 128000 / 44100 = 417 bytes.
	frame := make([]byte, 417)
	frame[0], frame[1], frame[2], frame[3] = 0xFF, 0xFB, 0x90, 0x00
	for i := 0; i < frames; i++ {
		out.Write(frame)
	}
	return out.Bytes()
}

// buildM4B writes an MP4 audio book: ftyp(M4B) + moov(mvhd + udta/meta/ilst).
// Duration comes from the mvhd timescale/duration pair; tags from the ilst
// atoms, exactly where a real audiobook rip keeps them.
func buildM4B(title, albumArtist string, seconds float64) []byte {
	box := func(typ string, body ...[]byte) []byte {
		var payload bytes.Buffer
		for _, b := range body {
			payload.Write(b)
		}
		size := uint32(payload.Len() + 8)
		out := make([]byte, 4, int(size))
		binary.BigEndian.PutUint32(out, size)
		out = append(out, typ...)
		return append(out, payload.Bytes()...)
	}
	be32 := func(v uint32) []byte {
		b := make([]byte, 4)
		binary.BigEndian.PutUint32(b, v)
		return b
	}
	// data atom body: version/flags (type code 1 = text) + locale + text
	textAtom := func(typ, text string) []byte {
		dataBody := append([]byte{0, 0, 0, 1, 0, 0, 0, 0}, []byte(text)...)
		return box(typ, box("data", dataBody))
	}

	mvhd := box("mvhd",
		[]byte{0, 0, 0, 0},         // version 0 + flags
		be32(0),                    // creation
		be32(0),                    // modification
		be32(1000),                 // timescale
		be32(uint32(seconds*1000)), // duration
	)

	ilst := box("ilst", textAtom("\xa9nam", title), textAtom("aART", albumArtist))
	meta := box("meta", append([]byte{0, 0, 0, 0}, ilst...)) // version/flags word
	udta := box("udta", meta)
	moov := box("moov", mvhd, udta)
	ftyp := box("ftyp", append(append([]byte("M4B "), be32(0)...), []byte("M4B mp42")...))

	return append(ftyp, moov...)
}

// buildEPUB writes a minimal OCF container: mimetype, container.xml, one
// package document, one chapter. encrypted adds META-INF/encryption.xml —
// the DRM marker the scanner refuses.
// defaultOPF is the package document buildEPUB writes: a well-formed UTF-8
// EPUB 3 metadata block with a credited author and an ISBN.
const defaultOPF = `<?xml version="1.0"?>
<package xmlns="http://www.idpf.org/2007/opf" version="3.0" unique-identifier="id">
  <metadata xmlns:dc="http://purl.org/dc/elements/1.1/" xmlns:opf="http://www.idpf.org/2007/opf">
    <dc:identifier id="id">urn:uuid:test</dc:identifier>
    <dc:identifier opf:scheme="ISBN">9780306406157</dc:identifier>
    <dc:title>Fixture Book</dc:title>
    <dc:creator opf:role="ill">Fixture Illustrator</dc:creator>
    <dc:creator opf:role="aut">Fixture Author</dc:creator>
    <dc:language>en</dc:language>
  </metadata>
</package>`

// legacyOPF declares a non-UTF-8 charset, the way anything converted by an
// older tool does. Its metadata must still be read.
const legacyOPF = `<?xml version="1.0" encoding="iso-8859-1"?>
<package xmlns="http://www.idpf.org/2007/opf" version="2.0" unique-identifier="id">
  <metadata xmlns:dc="http://purl.org/dc/elements/1.1/" xmlns:opf="http://www.idpf.org/2007/opf">
    <dc:identifier id="id">urn:uuid:legacy</dc:identifier>
    <dc:title>Of Mice and Men</dc:title>
    <dc:creator opf:role="aut">John Steinbeck</dc:creator>
  </metadata>
</package>`

// brokenOPF is a good container holding an unparseable package document. The
// book must still be inventoried; it just loses its metadata.
const brokenOPF = `<?xml version="1.0"?><package><metadata>`

func buildEPUB(encrypted bool) []byte {
	return buildEPUBWithOPF(encrypted, defaultOPF)
}

func buildEPUBWithOPF(encrypted bool, opf string) []byte {
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)

	mimetype, err := w.CreateHeader(&zip.FileHeader{Name: "mimetype", Method: zip.Store})
	if err != nil {
		panic(err)
	}
	if _, err := mimetype.Write([]byte("application/epub+zip")); err != nil {
		panic(err)
	}

	files := map[string]string{
		"META-INF/container.xml": `<?xml version="1.0"?>
<container version="1.0" xmlns="urn:oasis:names:tc:opendocument:xmlns:container">
  <rootfiles><rootfile full-path="OEBPS/content.opf" media-type="application/oebps-package+xml"/></rootfiles>
</container>`,
		"OEBPS/content.opf": opf,
	}
	if encrypted {
		files["META-INF/encryption.xml"] = `<?xml version="1.0"?>
<enc:encryption xmlns:enc="urn:oasis:names:tc:opendocument:xmlns:container"/>`
	}
	for name, body := range files {
		f, err := w.Create(name)
		if err != nil {
			panic(err)
		}
		if _, err := f.Write([]byte(body)); err != nil {
			panic(err)
		}
	}
	if err := w.Close(); err != nil {
		panic(err)
	}
	return buf.Bytes()
}

// buildOpus writes a structurally valid Ogg Opus stream: an OpusHead page, an
// OpusTags page carrying Vorbis comments, and an audio page whose granule
// position sets the duration. Real pages with real CRCs, so both the tag
// library and opusDuration parse this the way they parse a Libro.fm download.
//
// seconds is what the file should measure. The granule is computed back from
// it, with a deliberately non-zero pre-skip so a parser that forgets to
// subtract the priming samples reads a measurably wrong length.
func buildOpus(title, artist, album string, seconds float64) []byte {
	// An Opus granule always ticks at 48 kHz whatever the source rate was,
	// and 312 is the usual libopus lookahead.
	const granuleRate, preSkip = 48000, 312
	granule := uint64(seconds*granuleRate) + preSkip

	head := make([]byte, 19)
	copy(head, "OpusHead")
	head[8] = 1 // version
	head[9] = 2 // channel count
	binary.LittleEndian.PutUint16(head[10:12], preSkip)
	binary.LittleEndian.PutUint32(head[12:16], 48000) // original sample rate
	// head[16:18] output gain, head[18] channel mapping family: all zero.

	var tags bytes.Buffer
	tags.WriteString("OpusTags")
	vendor := "backhog test fixture"
	_ = binary.Write(&tags, binary.LittleEndian, uint32(len(vendor)))
	tags.WriteString(vendor)
	comments := []string{"TITLE=" + title, "ARTIST=" + artist, "ALBUM=" + album}
	_ = binary.Write(&tags, binary.LittleEndian, uint32(len(comments)))
	for _, c := range comments {
		_ = binary.Write(&tags, binary.LittleEndian, uint32(len(c)))
		tags.WriteString(c)
	}

	var out bytes.Buffer
	// beginning-of-stream, then the tag page, then an end-of-stream audio
	// page whose granule is the whole stream's length.
	out.Write(oggPage(head, 0x02, 0, 0))
	out.Write(oggPage(tags.Bytes(), 0x00, 0, 1))
	out.Write(oggPage([]byte("not really opus audio"), 0x04, granule, 2))
	return out.Bytes()
}

// oggPage frames one packet as a single Ogg page and computes its CRC. The
// packet must be under 255 bytes so it fits in one lacing segment, which is
// true of every header this fixture writes.
func oggPage(packet []byte, flags byte, granule uint64, sequence uint32) []byte {
	if len(packet) >= 255 {
		panic("oggPage: fixture packets must fit one lacing segment")
	}
	// 27 header bytes, then a one-entry lacing table, then the packet.
	page := make([]byte, 0, 27+1+len(packet))
	page = append(page, "OggS"...)
	page = append(page, 0)     // stream structure version
	page = append(page, flags) // 0x02 BOS, 0x04 EOS
	page = binary.LittleEndian.AppendUint64(page, granule)
	page = binary.LittleEndian.AppendUint32(page, 0xB4C4C0DE) // stream serial
	page = binary.LittleEndian.AppendUint32(page, sequence)
	page = binary.LittleEndian.AppendUint32(page, 0) // CRC placeholder
	page = append(page, 1)                           // one lacing segment
	page = append(page, byte(len(packet)))
	page = append(page, packet...)

	binary.LittleEndian.PutUint32(page[22:26], oggCRC(page))
	return page
}

// oggCRC is Ogg's own CRC32: polynomial 0x04c11db7, no reflection and no
// final inversion, computed over the page with its CRC field zeroed.
func oggCRC(page []byte) uint32 {
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
	return crc
}
