// windows-1252 decoding for MOBI6 chapter slices.
//
// The MOBI header declares one of two encodings: UTF-8 (65001) or
// windows-1252. Chapter boundaries are byte offsets into the raw text and
// every slice decodes independently, so this is the one translation the
// parser does itself. The table is transcribed from the Unicode
// consortium's public-domain mapping file
// (https://unicode.org/Public/MAPPINGS/VENDORS/MICSFT/WINDOWS/CP1252.TXT),
// with the five unassigned slots mapped to their C1 control counterparts —
// the same per-byte result mobi-go's decoder produces, so the canonical
// text never depends on which layer happened to decode a string.
package mobi

import "strings"

// cp1252High maps bytes 0x80–0x9F to Unicode code points; everything else
// in windows-1252 (ASCII and the 0xA0–0xFF Latin-1 range) is its own code
// point.
var cp1252High = [32]rune{
	0x20AC, 0x0081, 0x201A, 0x0192, 0x201E, 0x2026, 0x2020, 0x2021,
	0x02C6, 0x2030, 0x0160, 0x2039, 0x0152, 0x008D, 0x017D, 0x008F,
	0x0090, 0x2018, 0x2019, 0x201C, 0x201D, 0x2022, 0x2013, 0x2014,
	0x02DC, 0x2122, 0x0161, 0x203A, 0x0153, 0x009D, 0x017E, 0x0178,
}

// decodeCP1252 decodes a raw chapter slice stored in windows-1252. It is
// only called for books whose decoded text is longer than its raw bytes —
// a cp1252 book holding nothing but ASCII decodes byte-for-byte and takes
// the string() fast path instead.
func decodeCP1252(b []byte) string {
	ascii := true
	for _, c := range b {
		if c >= 0x80 {
			ascii = false
			break
		}
	}
	if ascii {
		return string(b)
	}
	var sb strings.Builder
	sb.Grow(len(b))
	for _, c := range b {
		if c >= 0x80 && c <= 0x9F {
			sb.WriteRune(cp1252High[c-0x80])
		} else {
			sb.WriteRune(rune(c))
		}
	}
	return sb.String()
}
