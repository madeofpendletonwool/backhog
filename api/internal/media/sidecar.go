package media

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path"
	"regexp"
	"strings"

	mobigo "github.com/madeofpendletonwool/mobi-go"

	"github.com/collinpendleton/backhog/api/internal/books/epub"
	"github.com/collinpendleton/backhog/api/internal/metadata"
	"github.com/collinpendleton/backhog/api/internal/models"
)

// sidecarExtension is the OPF metadata sidecar Calibre and friends write next
// to a library's books: a per-book "metadata.opf", or a "Title - Author.opf"
// beside a single file. It is not a book and is never inventoried — but it is
// the most reliable thing in the directory about *which* book is there, so it
// is parsed once per scan and handed to the matcher.
const sidecarExtension = ".opf"

// knownUnhandled are ebook formats this tool recognises and deliberately does
// not parse. MOBI, AZW and AZW3 are parsed (see mobiExtensions and
// internal/books/mobi); KFX — Amazon's newer container — is out of scope
// permanently: mobi-go, the pure-Go Kindle reader this project builds on,
// does not read it by design, and no other open reader exists. Naming the
// format and pointing at the EPUB or MOBI of the same book is the honest
// answer, and it keeps these files from reading as a broken scan.
var knownUnhandled = map[string]bool{
	".kfx": true,
}

// mobiExtensions are the text-side extensions the mobi parser owns. They
// share kind 'epub' — the text-side slot — and the same epub_texts canonical
// model once attached.
var mobiExtensions = map[string]bool{
	".mobi": true,
	".azw":  true,
	".azw3": true,
}

// bookTags is the JSON shape of the container_metadata column for an epub:
// what the book's own OPF package says about itself. It is the text-side
// counterpart of audioTags, stored in the same column — the readers that
// unmarshal audioTags are all scoped to kind 'audio', so the two shapes never
// meet.
type bookTags struct {
	Title       string   `json:"title,omitempty"`
	Authors     []string `json:"authors,omitempty"`
	Series      string   `json:"series,omitempty"`
	SeriesIndex string   `json:"series_index,omitempty"`
	Language    string   `json:"language,omitempty"`
	Date        string   `json:"date,omitempty"`
	// ISBN is already normalized and validated; a non-empty value is safe to
	// hand straight to an ISBN lookup.
	ISBN string `json:"isbn,omitempty"`
	// WorkKey is an Open Library work key (OL12345W): an exact identity
	// rather than a search term.
	WorkKey string `json:"work_key,omitempty"`
}

func (b bookTags) empty() bool {
	return b.Title == "" && len(b.Authors) == 0 && b.Series == "" &&
		b.Language == "" && b.Date == "" && b.ISBN == "" && b.WorkKey == ""
}

// Author returns the first credited author, which is what the matcher scores
// against; the rest are kept for display.
func (b bookTags) Author() string {
	if len(b.Authors) == 0 {
		return ""
	}
	return b.Authors[0]
}

// workKeyPattern matches an Open Library work key, with or without the
// "/works/" path Open Library's own identifiers carry.
var workKeyPattern = regexp.MustCompile(`(?i)(?:^|/)(OL\d+W)$`)

// tagsFromMetadata folds a parsed OPF metadata block into the stored shape,
// resolving the one thing epub.Metadata deliberately leaves raw: which of the
// declared identifiers is a usable ISBN and which is an Open Library work key.
// Both callers — the epub scan and the .opf sidecar — go through here, so an
// identifier is recognised the same way wherever it was written.
func tagsFromMetadata(m epub.Metadata) bookTags {
	tags := bookTags{
		Title:       strings.TrimSpace(m.Title),
		Series:      strings.TrimSpace(m.Series),
		SeriesIndex: strings.TrimSpace(m.SeriesIndex),
		Language:    strings.TrimSpace(m.Language),
		Date:        strings.TrimSpace(m.Date),
	}
	for _, a := range m.Authors {
		if a = strings.TrimSpace(a); a != "" {
			tags.Authors = append(tags.Authors, a)
		}
	}
	for _, id := range m.Identifiers {
		value := strings.TrimSpace(id.Value)
		// A urn-shaped identifier ("urn:isbn:9780575084070") carries its
		// scheme in the value; a bare one declares it in opf:scheme, and
		// plenty of files declare nothing at all — so the value is always
		// tested on its own merits.
		if i := strings.LastIndex(value, ":"); i >= 0 {
			value = value[i+1:]
		}
		if tags.ISBN == "" {
			if isbn := metadata.NormalizeISBN(value); metadata.ValidISBN(isbn) {
				tags.ISBN = isbn
			}
		}
		if tags.WorkKey == "" {
			if m := workKeyPattern.FindStringSubmatch(strings.TrimSpace(id.Value)); m != nil {
				tags.WorkKey = strings.ToUpper(m[1])
			}
		}
	}
	return tags
}

// marshalTags serialises a metadata block for the container_metadata column,
// returning nil for a block that said nothing — the column stays NULL rather
// than holding an empty object.
func marshalTags(tags bookTags) json.RawMessage {
	if tags.empty() {
		return nil
	}
	raw, err := json.Marshal(tags)
	if err != nil {
		return nil
	}
	return raw
}

// readEpubMetadata opens an EPUB once and answers both questions the scanner
// has about it: whether it is DRM-wrapped, and what its OPF package says about
// itself. One open, two zip members — the DRM check already cost this open, so
// the metadata is very nearly free.
func readEpubMetadata(p string) (encrypted bool, tags bookTags, err error) {
	f, err := os.Open(p) // O_RDONLY: the media roots are read-only
	if err != nil {
		return false, bookTags{}, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return false, bookTags{}, err
	}
	meta, err := epub.ReadMetadata(f, info.Size())
	switch {
	case err == nil:
		return false, tagsFromMetadata(meta), nil
	case errors.Is(err, epub.ErrDRM):
		return true, bookTags{}, nil
	default:
		return false, bookTags{}, err
	}
}

// readMobiMetadata opens a MOBI/AZW/AZW3 once and answers the same two
// questions the scanner asks of an EPUB: whether it is DRM-wrapped, and what
// its EXTH block says about itself. mobi-go opens eagerly (decompressing the
// whole book), so the metadata is nearly free once the DRM check has been
// paid for — and a file that will not open is one whose DRM status could not
// be determined, which the caller treats as a scan failure rather than an
// inventory entry.
func readMobiMetadata(p string) (encrypted bool, tags bookTags, err error) {
	f, err := os.Open(p) // O_RDONLY: the media roots are read-only
	if err != nil {
		return false, bookTags{}, err
	}
	defer f.Close()
	data, err := io.ReadAll(f)
	if err != nil {
		return false, bookTags{}, err
	}
	b, err := mobigo.OpenBytes(data)
	if err != nil {
		if errors.Is(err, mobigo.ErrDRM) {
			return true, bookTags{}, nil
		}
		return false, bookTags{}, err
	}
	md := b.Metadata()
	tags = bookTags{
		Title:    strings.TrimSpace(md.Title),
		Language: strings.TrimSpace(md.Language),
		Date:     strings.TrimSpace(md.Published),
	}
	for _, a := range md.Authors {
		if a = strings.TrimSpace(a); a != "" {
			tags.Authors = append(tags.Authors, a)
		}
	}
	// Same treatment an OPF identifier gets: normalize, validate, and only
	// then may the matcher treat it as an identity rather than a guess.
	if isbn := metadata.NormalizeISBN(strings.TrimSpace(md.ISBN)); metadata.ValidISBN(isbn) {
		tags.ISBN = isbn
	}
	return false, tags, nil
}

// parseSidecar reads one .opf file into a sidecar row. A file that parses but
// names no book is rejected: an empty sidecar is worse than none, because it
// would outrank the filename evidence that actually says something.
func parseSidecar(root, rel, absolute string) (models.MediaSidecar, bool) {
	f, err := os.Open(absolute) // O_RDONLY: the media roots are read-only
	if err != nil {
		return models.MediaSidecar{}, false
	}
	defer f.Close()

	meta, err := epub.ParseMetadata(f)
	if err != nil {
		return models.MediaSidecar{}, false
	}
	tags := tagsFromMetadata(meta)
	if tags.Title == "" && tags.ISBN == "" && tags.WorkKey == "" {
		return models.MediaSidecar{}, false
	}
	return models.MediaSidecar{
		Root: root, Path: rel,
		Title:       tags.Title,
		Author:      tags.Author(),
		Series:      tags.Series,
		SeriesIndex: tags.SeriesIndex,
		Language:    tags.Language,
		ISBN:        tags.ISBN,
		WorkKey:     tags.WorkKey,
	}, true
}

// sidecarPreference orders the sidecars of one directory so that picking the
// first is a stable choice: Calibre's own "metadata.opf" is the canonical
// name and wins, and everything else falls back to path order.
func sidecarPreference(p string) int {
	if strings.EqualFold(path.Base(p), "metadata.opf") {
		return 0
	}
	return 1
}
