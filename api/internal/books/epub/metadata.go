package epub

import (
	"archive/zip"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"strings"

	"golang.org/x/text/encoding/htmlindex"
)

// Metadata is the OPF <metadata> block: what a book says about itself. It is
// the same structure whether it came from inside an EPUB or from a standalone
// .opf sidecar next to the files, which is why one decoder serves both.
//
// Deliberately raw. Identifiers keep their declared scheme and text exactly as
// written — deciding which one is an ISBN, and normalizing it, is the caller's
// job (internal/media), because this package stays pure parsing.
type Metadata struct {
	Title string
	// Authors are the dc:creator values, authors first: a creator carrying
	// role="aut" wins over one with no role, which wins over an illustrator
	// or a translator.
	Authors []string
	// Series and SeriesIndex come from Calibre's meta pair or from an EPUB 3
	// belongs-to-collection. SeriesIndex is kept as text: "2", "2.5" and ""
	// are all things real files contain.
	Series      string
	SeriesIndex string
	Language    string
	// Date is the raw dc:date, which ranges from "2011" to a full timestamp.
	Date        string
	Identifiers []Identifier
}

// Identifier is one dc:identifier: its declared opf:scheme (often empty) and
// its text, which may itself be a urn ("urn:isbn:9780575084070").
type Identifier struct {
	Scheme string
	Value  string
}

// Empty reports whether the block yielded nothing worth storing.
func (m Metadata) Empty() bool {
	return m.Title == "" && len(m.Authors) == 0 && m.Series == "" &&
		m.Language == "" && m.Date == "" && len(m.Identifiers) == 0
}

// ErrBadMetadata marks a package whose container opened fine but whose OPF
// could not be read — a missing rootfile, malformed XML, an exotic charset
// declaration. It exists so a caller can tell "this file is unusable" from
// "this file is fine, its metadata is not": the second is a book that still
// belongs in a library, just without what it says about itself.
var ErrBadMetadata = errors.New("epub: package metadata could not be read")

// ReadMetadata pulls the metadata block out of an EPUB held in r, resolving
// the OPF through META-INF/container.xml the same way Parse does. It reads
// exactly two zip members and never touches the spine, so a scanner can call
// it on every file in a library.
//
// ErrDRM is returned for an encrypted package, so a caller that already needs
// the DRM verdict gets both answers from one open. Errors reading the OPF
// itself wrap ErrBadMetadata; anything else means the container is unreadable
// and nothing about the file — DRM included — could be determined.
func ReadMetadata(r io.ReaderAt, size int64) (Metadata, error) {
	zr, err := zip.NewReader(r, size)
	if err != nil {
		return Metadata{}, fmt.Errorf("epub: open zip: %w", err)
	}
	return metadataFromZip(zr)
}

// metadataFromZip is the shared half of ReadMetadata, split out so a caller
// holding an open *zip.Reader does not reopen the file.
func metadataFromZip(zr *zip.Reader) (Metadata, error) {
	if f := zipLookup(zr, "META-INF/encryption.xml"); f != nil {
		return Metadata{}, ErrDRM
	}
	container, err := readContainer(zr)
	if err != nil {
		return Metadata{}, fmt.Errorf("%w: %w", ErrBadMetadata, err)
	}
	f := zipLookup(zr, container.RootfilePath)
	if f == nil {
		return Metadata{}, fmt.Errorf("%w: OPF not found at %s", ErrBadMetadata, container.RootfilePath)
	}
	rc, err := f.Open()
	if err != nil {
		return Metadata{}, fmt.Errorf("%w: open %s: %w", ErrBadMetadata, container.RootfilePath, err)
	}
	defer rc.Close()
	m, err := ParseMetadata(rc)
	if err != nil {
		return Metadata{}, fmt.Errorf("%w: %w", ErrBadMetadata, err)
	}
	return m, nil
}

// ParseMetadata decodes an OPF package document — a standalone .opf sidecar,
// or the rootfile of an EPUB — and returns its metadata block.
func ParseMetadata(r io.Reader) (Metadata, error) {
	// Element and attribute names are matched on local name only, with no
	// namespace, exactly as readOPF matches manifest and spine: real files in
	// the wild bind the dc and opf prefixes inconsistently (and sometimes not
	// at all), and the local names are unambiguous either way.
	var raw struct {
		Metadata struct {
			Titles   []string `xml:"title"`
			Creators []struct {
				Name string `xml:",chardata"`
				Role string `xml:"role,attr"`
			} `xml:"creator"`
			Identifiers []struct {
				Value  string `xml:",chardata"`
				Scheme string `xml:"scheme,attr"`
			} `xml:"identifier"`
			Languages []string `xml:"language"`
			Dates     []string `xml:"date"`
			Metas     []struct {
				Name     string `xml:"name,attr"`
				Content  string `xml:"content,attr"`
				Property string `xml:"property,attr"`
				Value    string `xml:",chardata"`
			} `xml:"meta"`
		} `xml:"metadata"`
	}
	dec := xml.NewDecoder(r)
	// Plenty of real EPUBs — anything converted by an older tool — declare
	// iso-8859-1 or windows-1252. Without a CharsetReader the decoder
	// refuses the file outright, which would cost those books their metadata
	// for no better reason than the year they were packaged.
	dec.CharsetReader = charsetReader
	if err := dec.Decode(&raw); err != nil {
		return Metadata{}, fmt.Errorf("epub: parse OPF metadata: %w", err)
	}

	var m Metadata
	m.Title = firstNonBlank(raw.Metadata.Titles)
	m.Language = firstNonBlank(raw.Metadata.Languages)
	m.Date = firstNonBlank(raw.Metadata.Dates)

	// Authors first, then unroled creators, then everybody else: a book whose
	// only roled creator is the illustrator should still not name the
	// illustrator as its author.
	var authors, unroled []string
	for _, c := range raw.Metadata.Creators {
		name := strings.TrimSpace(c.Name)
		if name == "" {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(c.Role)) {
		case "aut":
			authors = append(authors, name)
		case "":
			unroled = append(unroled, name)
		}
	}
	m.Authors = append(authors, unroled...)

	for _, id := range raw.Metadata.Identifiers {
		value := strings.TrimSpace(id.Value)
		if value == "" {
			continue
		}
		m.Identifiers = append(m.Identifiers, Identifier{
			Scheme: strings.TrimSpace(id.Scheme), Value: value,
		})
	}

	for _, meta := range raw.Metadata.Metas {
		switch strings.ToLower(strings.TrimSpace(meta.Name)) {
		case "calibre:series":
			m.Series = firstNonBlank([]string{m.Series, meta.Content})
		case "calibre:series_index":
			m.SeriesIndex = firstNonBlank([]string{m.SeriesIndex, meta.Content})
		}
		// EPUB 3 spells the same two facts as property-carrying meta elements
		// with the value in the element body.
		switch strings.ToLower(strings.TrimSpace(meta.Property)) {
		case "belongs-to-collection":
			m.Series = firstNonBlank([]string{m.Series, meta.Value})
		case "group-position":
			m.SeriesIndex = firstNonBlank([]string{m.SeriesIndex, meta.Value})
		}
	}
	return m, nil
}

func firstNonBlank(values []string) string {
	for _, v := range values {
		if s := strings.TrimSpace(v); s != "" {
			return s
		}
	}
	return ""
}

// charsetReader decodes an OPF declared in something other than UTF-8. The
// encoding names in these files are the HTML ones, which is exactly what
// htmlindex resolves; an encoding nothing recognises is an error rather than
// a guess, and the caller degrades to no metadata.
func charsetReader(charset string, input io.Reader) (io.Reader, error) {
	enc, err := htmlindex.Get(charset)
	if err != nil {
		return nil, fmt.Errorf("epub: unsupported OPF charset %q: %w", charset, err)
	}
	return enc.NewDecoder().Reader(input), nil
}
