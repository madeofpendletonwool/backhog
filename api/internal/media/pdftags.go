package media

import (
	"errors"
	"html"
	"io"
	"os"
	"regexp"
	"strings"

	gopdf "github.com/ledongthuc/pdf"

	"github.com/collinpendleton/backhog/api/internal/metadata"
)

// readPDFMetadata opens a PDF once and answers the two questions the
// scanner asks of every text-side container: whether it is DRM-wrapped
// (any /Encrypt dictionary, including owner-password-only "restrictions"
// files that would open with an empty user password), and what its own
// metadata says it is — the trailer /Info dictionary plus the catalog's
// XMP packet, the two places PDF producers write a book's identity. The
// mobi EXTH pattern one container over.
//
// The quality gate is deliberately NOT consulted here: classifying a PDF
// text-native or image-native means extracting its pages, which is parse
// work, and the scanner stays a cheap inventory pass (the roadmap's rule).
// A comic inventories like a novel; its parse refuses it later, honestly.
func readPDFMetadata(p string) (encrypted bool, tags bookTags, err error) {
	f, err := os.Open(p) // O_RDONLY: the media roots are read-only
	if err != nil {
		return false, bookTags{}, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return false, bookTags{}, err
	}
	rd, err := gopdf.NewReader(f, info.Size())
	if err != nil {
		if isPDFEncryptionError(err) {
			return true, bookTags{}, nil
		}
		// A PDF that will not open is one whose DRM status could not be
		// determined — the same answer an unopenable epub or mobi gets:
		// a scan failure, never an inventory entry.
		return false, bookTags{}, err
	}
	if !rd.Trailer().Key("Encrypt").IsNull() {
		return true, bookTags{}, nil
	}

	tags = pdfInfoTags(rd.Trailer().Key("Info"))
	tags.Language = firstNonEmpty(tags.Language, strings.TrimSpace(rd.Trailer().Key("Root").Key("Lang").Text()))

	xmp := pdfXMP(rd.Trailer().Key("Root").Key("Metadata"))
	if xmp.title != "" {
		tags.Title = xmp.title
	}
	if len(xmp.authors) > 0 {
		tags.Authors = xmp.authors
	}
	if xmp.language != "" {
		tags.Language = xmp.language
	}
	// Same treatment an OPF or EXTH identifier gets: normalize, validate,
	// and only then may the matcher treat it as an identity rather than a
	// guess.
	if xmp.identifier != "" {
		if isbn := metadata.NormalizeISBN(xmp.identifier); metadata.ValidISBN(isbn) {
			tags.ISBN = isbn
		}
	}
	return false, tags, nil
}

// isPDFEncryptionError recognizes the reader's encrypted-file failures,
// which arrive as ErrInvalidPassword or as version-specific messages.
func isPDFEncryptionError(err error) bool {
	return errors.Is(err, gopdf.ErrInvalidPassword) ||
		strings.Contains(err.Error(), "encrypted PDF")
}

// pdfInfoTags reads the classic trailer /Info dictionary: Title, Author
// and CreationDate, the fields every producer from pdfTeX to Word writes.
func pdfInfoTags(info gopdf.Value) bookTags {
	var tags bookTags
	tags.Title = strings.TrimSpace(info.Key("Title").Text())
	if a := strings.TrimSpace(info.Key("Author").Text()); a != "" {
		tags.Authors = []string{a}
	}
	// "D:20060102030405Z" is the PDF date shape; the D: prefix is
	// formatting, not information.
	tags.Date = strings.TrimPrefix(strings.TrimSpace(info.Key("CreationDate").Text()), "D:")
	return tags
}

// pdfXMP reads the catalog /Metadata XMP packet, when there is one. XMP is
// RDF-in-XML; the fields a book's identity needs are four (title, creator,
// language, identifier), each a container of rdf:li items, and a tolerant
// targeted scan answers all four without pretending to be an RDF engine —
// a packet this reader cannot understand degrades to the Info dictionary,
// never to an error.
func pdfXMP(v gopdf.Value) xmpMeta {
	var out xmpMeta
	if v.Kind() != gopdf.Stream {
		return out
	}
	rc := v.Reader()
	defer rc.Close()
	// Metadata packets are kilobytes; a "packet" claiming to be a gigabyte
	// is a bomb, and reading it would trade one scan for the server's
	// memory.
	data, err := io.ReadAll(io.LimitReader(rc, 4<<20))
	if err != nil {
		return out
	}
	out = parseXMP(string(data))
	return out
}

// xmpMeta is what the scanner wants out of an XMP packet.
type xmpMeta struct {
	title      string
	authors    []string
	language   string
	identifier string
}

// xmpField extracts one dc field: the values of every rdf:li inside the
// element, whatever namespace prefix the producer bound dc to.
func xmpField(data, field string) []string {
	block := regexp.MustCompile(`(?is)<(?:[\w.-]+:)?` + field + `>(.*?)</(?:[\w.-]+:)?` + field + `>`).
		FindStringSubmatch(data)
	if block == nil {
		return nil
	}
	li := regexp.MustCompile(`(?is)<rdf:li[^>]*>(.*?)</rdf:li>`).FindAllStringSubmatch(block[1], -1)
	var out []string
	for _, m := range li {
		if s := cleanXMPText(m[1]); s != "" {
			out = append(out, s)
		}
	}
	if len(out) == 0 {
		// The simple forms: <dc:language>en</dc:language> and
		// <dc:identifier>urn:isbn:…</dc:identifier> carry their value
		// directly.
		if s := cleanXMPText(block[1]); s != "" {
			out = append(out, s)
		}
	}
	return out
}

var xmpISBNPattern = regexp.MustCompile(`(?i)urn:isbn:([0-9Xx][0-9Xx -]*)`)

func parseXMP(data string) xmpMeta {
	var out xmpMeta
	if v := xmpField(data, "title"); len(v) > 0 {
		out.title = v[0]
	}
	out.authors = xmpField(data, "creator")
	if v := xmpField(data, "language"); len(v) > 0 {
		out.language = v[0]
	}
	ids := xmpField(data, "identifier")
	// Producers write identifiers as bare values, urn:isbn: values, or
	// both; the urn form says its scheme out loud, so it wins.
	if m := xmpISBNPattern.FindStringSubmatch(data); m != nil {
		ids = append([]string{m[1]}, ids...)
	}
	for _, id := range ids {
		if id != "" {
			out.identifier = id
			break
		}
	}
	return out
}

// cleanXMPText trims and unescapes one XMP text node.
func cleanXMPText(s string) string {
	return strings.TrimSpace(html.UnescapeString(strings.TrimSpace(s)))
}
