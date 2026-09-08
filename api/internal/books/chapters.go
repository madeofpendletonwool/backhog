package books

import (
	"regexp"
	"strings"
	"unicode"

	"github.com/collinpendleton/backhog/api/internal/books/epub"
	"github.com/collinpendleton/backhog/api/internal/models"
)

// This file decides what a *chapter* is, which is not the same question as
// what a spine document is.
//
// The arena used to answer both with one row: chapter = spine document,
// titled by the first TOC entry pointing at it. That is only right for
// EPUBs whose TOC names every document exactly once, and across this
// library that was four percent of them. The other ninety-six percent break
// it in three ways:
//
//   - A chapter split across several documents. Calibre's converter emits
//     "part7_split_000.xhtml" holding the chapter heading and
//     "part7_split_001.xhtml" holding the chapter, and points the TOC at the
//     first. The reader then showed "CHAPTER ONE" containing one line,
//     followed by fifteen thousand characters of unnamed chapter.
//   - A TOC that names nothing. The Stand's NCX has a single navPoint
//     labelled "Start"; A Man Without a Country is a MOBI with no usable TOC
//     positions at all. Every chapter came out unnamed even though the
//     headings were sitting in the documents.
//   - Documents that are not chapters — covers, title pages, half-titles —
//     which own no text and should not be numbered as if they were.
//
// So a chapter here is a *run of consecutive spine documents*: the one that
// carries the title, plus everything after it that does not start a chapter
// of its own. Grouping happens before the canonical text is built, and it
// changes no offsets: the canonical text is still the same blocks in the
// same order, so every stored reading position survives this regrouping
// untouched.

// Title sources, most trustworthy first. They are stored per chapter so the
// reader can be honest about which titles the book asserted and which ones
// were inferred from its markup.
const (
	// TitleSourceTOC is the book's own table of contents.
	TitleSourceTOC = "toc"
	// TitleSourceHeading is a heading element inside the document — an
	// <h1>-<h6>, an epub:type="title", or a chapter-title class. The book
	// still asserted it, just in the markup rather than the TOC.
	TitleSourceHeading = "heading"
	// TitleSourceText is the document's opening line, taken because it
	// reads like a heading (all capitals, a bare numeral, "Chapter Nine").
	// This is a guess, and the only source that can be wrong about what the
	// chapter is called rather than merely terse.
	TitleSourceText = "text"
	// TitleSourceNone means nothing named it; the reader numbers it by its
	// position among the chapters that actually hold text.
	TitleSourceNone = "none"
)

// stubChars is the size below which a "chapter" is really a heading file.
// Calibre's split heading documents run to a few dozen characters — the
// chapter title and a running header — and nothing that short is a chapter
// anybody wants to open. A document under this size never ends a chapter:
// whatever follows joins it.
const stubChars = 300

// absorbChars bounds how much text one chapter may take on from documents
// that name themselves nothing. A chapter genuinely split across files runs
// to tens of thousands of characters, not hundreds of thousands; past this
// the run is no longer a chapter continuing, it is the grouping having lost
// the thread. Our Mutual Friend's TOC names nothing and its first document
// happens to open with the words "Chapter 1" — without a bound here that one
// guess absorbed all 1.7 million characters of the novel.
const absorbChars = 150_000

// maxTitleLen bounds a derived title. A heading is a label; anything longer
// than this is a paragraph that happens to be first.
const maxTitleLen = 120

// chapterWord opens a heading that names its own kind, which is strong
// evidence regardless of how it is capitalised.
var chapterWord = regexp.MustCompile(`(?i)^(chapter|part|book|section|prologue|epilogue|interlude|foreword|preface|afterword|appendix)\b`)

// romanNumeral matches a bare roman numeral, with or without a full stop.
var romanNumeral = regexp.MustCompile(`^[IVXLCDM]{1,8}\.?$`)

// group is one chapter under construction: the run of spine documents it
// covers and what named it.
type group struct {
	first, last int // inclusive indexes into doc.Docs
	title       string
	source      string
	depth       int
	textLen     int
}

// GroupChapters folds a parsed book's spine documents into chapters.
//
// textLen[i] must be the number of canonical characters spine document i
// contributes; a zero means the document holds no text (a cover, a plate)
// and owns no chapter of its own. The caller computes it while normalizing,
// so the emptiness test here is exactly the one the canonical text uses.
//
// The returned groups cover every document in order, and every group holds
// at least one document with text — except when the book has no text at
// all, which returns nothing.
func GroupChapters(doc *epub.Document, textLen []int) []group {
	// Resolve every document's own claim to a title first: the TOC's if it
	// has one, otherwise whatever the markup asserts.
	titles := make([]string, len(doc.Docs))
	sources := make([]string, len(doc.Docs))
	for i := range doc.Docs {
		if textLen[i] == 0 {
			continue
		}
		if t := strings.TrimSpace(doc.Docs[i].Title); t != "" {
			titles[i], sources[i] = t, TitleSourceTOC
		} else {
			titles[i], sources[i] = derivedTitle(doc.Docs[i])
		}
	}

	var groups []group
	for i := range doc.Docs {
		if textLen[i] == 0 {
			// An empty document belongs to whatever chapter is open, so
			// that a cover plate mid-chapter does not split it. With no
			// chapter open yet it is leading front matter — the cover, the
			// half-title — and the first chapter swallows it below, so its
			// illustrations still reach the reader.
			if len(groups) > 0 {
				groups[len(groups)-1].last = i
			}
			continue
		}

		// A document is a chapter unless there is a specific reason to
		// believe it continues the one before it. Defaulting the other way
		// is what let a single named document swallow an entire novel:
		// Our Mutual Friend came out as one chapter of 1.7 million
		// characters, and Lullaby as one of eighty-nine documents.
		start := true
		if len(groups) > 0 {
			cur := &groups[len(groups)-1]
			switch {
			case titles[i] != "" && sameTitle(titles[i], cur.title):
				// The open chapter's own title, repeated. This is calibre's
				// split: "part2_split_000.xhtml" holds the heading and the
				// TOC points at it, "part2_split_001.xhtml" holds the
				// chapter and opens by printing the same heading again.
				start = false
			case titles[i] == "" && cur.textLen < stubChars:
				// The open chapter is a heading's worth of text and nothing
				// else, so it has not had its body yet. This one is it.
				start = false
			case titles[i] == "" && cur.title != "" && cur.textLen < absorbChars:
				// An unnamed document following a named chapter is that
				// chapter continuing: the second half of a split file, or
				// one of the <mbp:pagebreak> runs a MOBI with no usable TOC
				// gets cut into — page breaks, not chapter breaks, which is
				// what scattered A Man Without a Country across twenty-six
				// numbered fragments. The size bound is
				// what stops one lucky heading from swallowing a novel —
				// past it, an unnamed document is a chapter of its own even
				// though we cannot name it.
				start = false
			}
		}

		if start {
			// The opening chapter reaches back over the cover and title
			// pages, which hold no text but do hold the book's art.
			first := i
			if len(groups) == 0 {
				first = 0
			}
			groups = append(groups, group{
				first: first, last: i, title: titles[i], source: sources[i],
				depth: doc.Docs[i].Depth, textLen: textLen[i],
			})
			continue
		}

		cur := &groups[len(groups)-1]
		cur.last = i
		cur.textLen += textLen[i]
		// A run that opened unnamed takes the first name it is offered: a
		// chapter whose heading sits in its second document.
		if cur.title == "" && titles[i] != "" {
			cur.title, cur.source, cur.depth = titles[i], sources[i], doc.Docs[i].Depth
		}
	}

	// A book with no text anywhere is still a book — an all-plate facsimile,
	// a fixture — and it keeps one chapter so the ranges cover it and its
	// illustrations stay reachable.
	if len(groups) == 0 && len(doc.Docs) > 0 {
		groups = append(groups, group{first: 0, last: len(doc.Docs) - 1})
	}
	for i := range groups {
		if groups[i].title == "" {
			groups[i].source = TitleSourceNone
		}
	}
	return groups
}

// derivedTitle reads a chapter title out of the document itself, for the
// books whose TOC does not supply one.
//
// Structural markup is tried first and trusted anywhere near the top of the
// document: an <h1>, an epub:type="title", a <p class="ct">. Only when the
// document carries no such markup at all does the opening line get judged on
// its shape, and that judgement is confined to the very first block.
//
// The confinement is the point. Letter case is a terrible chapter signal in
// prose — Terry Pratchett writes DEATH's dialogue in capitals, and a rule
// that trusted shape anywhere in a document would find three hundred and
// ninety-seven chapter headings inside Reaper Man. At block zero the worst
// case is one chapter with a silly name; anywhere else it is a shredded book.
func derivedTitle(d epub.Doc) (string, string) {
	for _, b := range d.Headings {
		// Only a heading at the top of the document is that document's
		// title; one further down belongs to a section inside it.
		if b > 3 || b >= len(d.Blocks) {
			break
		}
		if t := cleanTitle(d.Blocks[b]); t != "" {
			return t, TitleSourceHeading
		}
	}
	if len(d.Headings) == 0 && len(d.Blocks) > 0 {
		if t := cleanTitle(d.Blocks[0]); t != "" && looksLikeHeading(t) {
			return t, TitleSourceText
		}
	}
	return "", TitleSourceNone
}

// cleanTitle collapses a block's whitespace and rejects anything too long
// to be a label.
func cleanTitle(raw string) string {
	t := strings.Join(strings.Fields(raw), " ")
	if t == "" || len(t) > maxTitleLen {
		return ""
	}
	return t
}

// looksLikeHeading judges a line on shape alone: it names its own kind
// ("Chapter Four"), it is a bare numeral or roman numeral, or it is short
// and predominantly capitals.
func looksLikeHeading(s string) bool {
	if len(s) > 70 {
		return false
	}
	if chapterWord.MatchString(s) || romanNumeral.MatchString(s) {
		return true
	}
	var digits, letters, upper int
	for _, r := range s {
		switch {
		case unicode.IsDigit(r):
			digits++
		case unicode.IsLetter(r):
			letters++
			if unicode.IsUpper(r) {
				upper++
			}
		}
	}
	if letters == 0 {
		return digits > 0
	}
	// A sentence that ends in a full stop is prose, however loud it is.
	if strings.HasSuffix(s, ".") && len(s) > 30 {
		return false
	}
	return upper*100/letters >= 70
}

// sameTitle compares two titles the way a reader would: ignoring case,
// spacing and trailing punctuation. It is what recognises that the TOC's
// "CHAPTER ONE" and the chapter body's own "Chapter One" are one chapter,
// not two.
func sameTitle(a, b string) bool {
	return foldTitle(a) == foldTitle(b) && foldTitle(a) != ""
}

func foldTitle(s string) string {
	s = strings.ToLower(strings.Join(strings.Fields(s), " "))
	return strings.TrimRight(s, " .:—-")
}

// chapterRow turns a group into its stored row. The row's SpineIndex is the
// first document of the run, which is the index the reader routes on and the
// block index keys on.
func (g group) chapterRow(doc *epub.Document) models.EpubChapter {
	return models.EpubChapter{
		SpineIndex:  g.first,
		Href:        doc.Docs[g.first].Href,
		Title:       g.title,
		TitleSource: g.source,
		Depth:       g.depth,
	}
}
