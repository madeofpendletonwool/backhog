package media

import (
	"path"
	"sort"
	"strconv"
	"strings"

	"github.com/collinpendleton/backhog/api/internal/models"
)

// SeriesEvidence is what a library's own metadata declares about a book's
// series membership: the series' name and the book's rank inside it. It is
// evidence, not truth — the same stance the matcher takes everywhere — but
// it is deterministic, arrives with the files, and costs nothing to keep.
type SeriesEvidence struct {
	Name string
	// Number is Calibre's series_index — frequently fractional (1.5 for
	// the between-the-numbers novella). HasNumber reports whether the
	// sidecar carried one at all.
	Number    float64
	HasNumber bool
}

// seriesDirKey addresses a sidecar's directory exactly the way the matcher
// groups files: root plus the directory with ripper platter names
// ("Disc 2") folded into their parent, so a book split across platters is
// one directory for sidecar purposes too.
type seriesDirKey struct{ root, dir string }

// FoldDir is the matcher's platter fold, exported for the readers that
// place attached files in sidecar directories: "Book/Disc 2" is the book's
// own directory with a ripper artifact inside it, not a different shelf.
func FoldDir(dir string) string { return groupDir(dir) }

// SeriesEvidenceForFiles resolves the series a book's attached files
// declare through their Calibre sidecars. bookFiles are the files attached
// to the one book being resolved; dirFiles is every attached file in the
// directories those files occupy — the occupancy that decides whether a
// directory-level metadata.opf describes this book or merely lives beside
// several. ok is false when nothing declares a series.
//
// Two rules, strongest first, both lifted from the matcher's own
// sidecarFor so the walker and the attach flow can never disagree with the
// matching UI about whose sidecar a file answers to:
//
//   - A sidecar sharing a file's stem ("Title - Author.opf" beside
//     "Title - Author.epub") was written to describe that file.
//   - A directory's preferred sidecar (Calibre's metadata.opf first)
//     describes the book when the directory holds files of exactly one
//     attached book — the one-book-per-folder layout Calibre writes. A
//     flat shelf holding several books' files has no directory-level
//     verdict, and says nothing rather than guessing.
func SeriesEvidenceForFiles(bookFiles, dirFiles []models.MediaFile, sidecars []models.MediaSidecar) (SeriesEvidence, bool) {
	if len(bookFiles) == 0 {
		return SeriesEvidence{}, false
	}

	byDir := map[seriesDirKey][]models.MediaSidecar{}
	for _, car := range sidecars {
		if car.Series == "" {
			continue
		}
		k := seriesDirKey{car.Root, groupDir(path.Dir(car.Path))}
		byDir[k] = append(byDir[k], car)
	}
	if len(byDir) == 0 {
		return SeriesEvidence{}, false
	}
	for k := range byDir {
		in := byDir[k]
		sort.Slice(in, func(i, j int) bool {
			ri, rj := sidecarPreference(in[i].Path), sidecarPreference(in[j].Path)
			if ri != rj {
				return ri < rj
			}
			return in[i].Path < in[j].Path
		})
		byDir[k] = in
	}

	// A stem match names the very file, so it outranks everything the
	// directory might say.
	for _, f := range bookFiles {
		k := seriesDirKey{f.Root, groupDir(path.Dir(f.Path))}
		stem := strings.TrimSuffix(f.Path, path.Ext(f.Path))
		for _, car := range byDir[k] {
			if sidecarStem(car) == stem {
				return evidenceOf(car), true
			}
		}
	}

	// The directory-level fallback: a sidecar speaks for this book only
	// when every attached file in the directory is this book's.
	for _, f := range bookFiles {
		k := seriesDirKey{f.Root, groupDir(path.Dir(f.Path))}
		cars := byDir[k]
		if len(cars) == 0 {
			continue
		}
		if !dirHoldsOneBook(dirFiles, k) {
			continue
		}
		return evidenceOf(cars[0]), true
	}
	return SeriesEvidence{}, false
}

// dirHoldsOneBook reports whether every attached file under the directory
// belongs to a single book. Unattached files do not vote: the matcher has
// not said whose they are, and a directory of unattached files beside one
// attached book is the ordinary "just ripped, not reviewed yet" shelf.
func dirHoldsOneBook(dirFiles []models.MediaFile, k seriesDirKey) bool {
	var book string
	found := false
	for _, f := range dirFiles {
		if f.Root != k.root || groupDir(path.Dir(f.Path)) != k.dir {
			continue
		}
		if f.BookID == nil || *f.BookID == "" {
			continue
		}
		if found && *f.BookID != book {
			return false
		}
		book, found = *f.BookID, true
	}
	return found
}

// evidenceOf reads one sidecar's series claim, parsing the rank it carries
// when it carries one.
func evidenceOf(car models.MediaSidecar) SeriesEvidence {
	return ParseSeriesEvidence(car.Series, car.SeriesIndex)
}

// ParseSeriesEvidence parses a sidecar's series name and rank. The rank is
// Calibre's series_index; unparseable or negative values mean "unknown",
// not zero — a zero rank is a real value (the series prologue) that a bad
// parse must not be allowed to impersonate.
func ParseSeriesEvidence(name, index string) SeriesEvidence {
	e := SeriesEvidence{Name: name}
	if n, err := strconv.ParseFloat(strings.TrimSpace(index), 64); err == nil && n >= 0 {
		e.Number, e.HasNumber = n, true
	}
	return e
}
