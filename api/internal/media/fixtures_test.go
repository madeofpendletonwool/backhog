package media

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
)

// Fixture builders: minimal but structurally valid mp3, m4b and epub files so
// the scanner tests exercise the real tag and container parsers.

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
func buildEPUB(encrypted bool) []byte {
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
		"OEBPS/content.opf": `<?xml version="1.0"?>
<package xmlns="http://www.idpf.org/2007/opf" version="3.0" unique-identifier="id">
  <metadata xmlns:dc="http://purl.org/dc/elements/1.1/">
    <dc:identifier id="id">urn:uuid:test</dc:identifier>
    <dc:title>Fixture Book</dc:title>
  </metadata>
</package>`,
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
