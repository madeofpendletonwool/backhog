package books

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/collinpendleton/backhog/api/internal/books/epub"
	"github.com/collinpendleton/backhog/api/internal/models"
	"github.com/collinpendleton/backhog/api/internal/store"
)

// ErrNoEpub reports a book entry with no EPUB media file attached (or the
// file missing from its root). The reader surfaces it as an honest "no
// ebook attached" state.
var ErrNoEpub = errors.New("books: no epub attached to this book")

// indexVersion guards the sidecar shape independently of ParserVersion
// (which guards the text itself). v2 added the per-document image list and
// the display-text spans.
const indexVersion = 2

// Ingester turns EPUB media files into canonical texts: it parses the file
// on the NAS (read-only, path-contained), canonicalizes the spine into the
// normalized text with chapter ranges and a block-offset index, writes the
// text and index as companion files, and records the DB rows. Parsing is
// on demand — never during the NAS scan — and repeats whenever
// ParserVersion moves.
type Ingester struct {
	store *store.Store
	dir   string
}

// NewIngester creates the companion-file directory alongside the database.
func NewIngester(st *store.Store, textDir string) (*Ingester, error) {
	if err := os.MkdirAll(textDir, 0o755); err != nil {
		return nil, fmt.Errorf("books: create epub text dir: %w", err)
	}
	return &Ingester{store: st, dir: textDir}, nil
}

// TextPath is where a canonical text lives.
func (ing *Ingester) TextPath(id string) string { return filepath.Join(ing.dir, id+".txt") }

// IndexPath is where a text's block-offset sidecar lives.
func (ing *Ingester) IndexPath(id string) string {
	return filepath.Join(ing.dir, id+".blocks.json")
}

// DisplayPath is where the readable companion to a canonical text lives:
// the same blocks in the same order, joined by newlines, with the book's own
// capitals and punctuation. The canonical text answers "where am I"; this
// one answers "what does it say".
func (ing *Ingester) DisplayPath(id string) string {
	return filepath.Join(ing.dir, id+".display.txt")
}

// EnsureForEntry parses (or re-parses) the EPUB attached to a user's book
// entry and returns its canonical-text row.
func (ing *Ingester) EnsureForEntry(ctx context.Context, userID, entryID string) (models.EpubText, error) {
	bookID, err := ing.store.BookIDForEntry(ctx, userID, entryID)
	if err != nil {
		return models.EpubText{}, err
	}
	file, err := ing.store.EpubMediaFileForBook(ctx, bookID)
	if err != nil {
		return models.EpubText{}, ErrNoEpub
	}
	return ing.EnsureForMediaFile(ctx, file)
}

// EnsureForMediaFile parses the EPUB behind a media file row unless a
// current parse already exists (same parser version, companion files
// present). It is the attach-time hook: MAD-403 calls it when an EPUB is
// attached, and the text endpoints call it lazily. Concurrent calls may
// both parse; the result is idempotent and the DB write is transactional.
func (ing *Ingester) EnsureForMediaFile(ctx context.Context, f models.MediaFile) (models.EpubText, error) {
	if existing, err := ing.store.GetEpubText(ctx, f.ID); err == nil {
		if existing.ParserVersion == ParserVersion &&
			fileExists(ing.TextPath(existing.ID)) && fileExists(ing.IndexPath(existing.ID)) &&
			fileExists(ing.DisplayPath(existing.ID)) {
			return existing, nil
		}
	} else if !errors.Is(err, store.ErrNotFound) {
		return models.EpubText{}, err
	}

	path, err := resolveWithinRoot(f.Root, f.Path)
	if err != nil {
		return models.EpubText{}, err
	}
	parsed, err := parseEpubFile(path)
	if err != nil {
		return models.EpubText{}, err
	}

	canonical, display, chapters, index := Canonicalize(parsed)
	et := models.EpubText{
		MediaFileID:      f.ID,
		CharCount:        len(canonical),
		WordCount:        len(strings.Fields(canonical)),
		NormalizedSHA256: sha256Hex(canonical),
		ParsedAt:         time.Now().UTC(),
		ParserVersion:    ParserVersion,
	}
	if err := ing.store.ReplaceEpubText(ctx, et, chapters); err != nil {
		return models.EpubText{}, err
	}
	// The row id settles in ReplaceEpubText (re-parses keep the old id);
	// re-read to write the companion files under the final name.
	et, err = ing.store.GetEpubText(ctx, f.ID)
	if err != nil {
		return models.EpubText{}, err
	}
	index.ParserVersion = ParserVersion
	if err := writeFileAtomic(ing.TextPath(et.ID), []byte(canonical)); err != nil {
		return models.EpubText{}, err
	}
	if err := writeFileAtomic(ing.DisplayPath(et.ID), []byte(display)); err != nil {
		return models.EpubText{}, err
	}
	if err := writeJSONAtomic(ing.IndexPath(et.ID), index); err != nil {
		return models.EpubText{}, err
	}
	return et, nil
}

// ReadText returns the canonical text slice [from, to) as byte offsets.
// Callers validate bounds against EpubText.CharCount first.
func (ing *Ingester) ReadText(ctx context.Context, et models.EpubText, from, to int) (string, error) {
	data, err := os.ReadFile(ing.TextPath(et.ID))
	if err != nil {
		return "", fmt.Errorf("books: read canonical text: %w", err)
	}
	if from < 0 || to < from || to > len(data) {
		return "", fmt.Errorf("books: range [%d,%d) outside text of %d bytes", from, to, len(data))
	}
	return string(data[from:to]), nil
}

// ReadDisplayBlocks returns one spine document's blocks as the reader shows
// them, in the same order as the document's canonical Blocks offsets — so
// block i of this slice starts at doc.Blocks[i]. Blocks never contain a
// newline (Canonicalize collapses whitespace), which is what makes the file
// splittable and the correspondence exact.
func (ing *Ingester) ReadDisplayBlocks(ctx context.Context, et models.EpubText, doc IndexedDoc) ([]string, error) {
	data, err := os.ReadFile(ing.DisplayPath(et.ID))
	if err != nil {
		return nil, fmt.Errorf("books: read display text: %w", err)
	}
	if doc.DisplayStart < 0 || doc.DisplayEnd < doc.DisplayStart || doc.DisplayEnd > len(data) {
		return nil, fmt.Errorf("books: display range [%d,%d) outside text of %d bytes",
			doc.DisplayStart, doc.DisplayEnd, len(data))
	}
	if doc.DisplayEnd == doc.DisplayStart {
		return nil, nil
	}
	return strings.Split(string(data[doc.DisplayStart:doc.DisplayEnd]), "\n"), nil
}

// LoadIndex reads a canonical text's block-offset sidecar.
func (ing *Ingester) LoadIndex(ctx context.Context, et models.EpubText) (*BlockIndex, error) {
	data, err := os.ReadFile(ing.IndexPath(et.ID))
	if err != nil {
		return nil, fmt.Errorf("books: read block index: %w", err)
	}
	var index BlockIndex
	if err := json.Unmarshal(data, &index); err != nil {
		return nil, fmt.Errorf("books: parse block index: %w", err)
	}
	return &index, nil
}

// Canonicalize applies the pinned normalizer to every block of every spine
// document and assembles the canonical text:
//
//   - each block is normalized (books.Normalize) and empty results dropped;
//   - a document's text is its blocks joined by single spaces;
//   - documents are joined by single spaces in spine order;
//   - chapter ranges partition [0, len) exactly: every non-empty document
//     except the last owns its trailing separator space inside CharEnd, and
//     empty (image-only) documents contribute empty ranges at the boundary.
//
// Offsets are byte offsets into the returned canonical string.
//
// It builds the *display* text in the same pass and returns it second. The
// canonical text is folded for matching — lowercased, quotes and punctuation
// dropped — so it is an address space, not prose; the display text is the
// same blocks with the book's own characters, joined by newlines, and is
// what the reader puts on the page. One pass because the two must agree
// block for block: a block dropped from one is dropped from the other, or
// every offset the reader reports lands on the wrong paragraph.
func Canonicalize(doc *epub.Document) (string, string, []models.EpubChapter, *BlockIndex) {
	var canonical, display strings.Builder
	chapters := make([]models.EpubChapter, len(doc.Docs))
	index := &BlockIndex{
		Version:   indexVersion,
		Documents: make([]IndexedDoc, len(doc.Docs)),
	}

	// span records where a document's own text sits; hasText is false for
	// image-only documents, which own no bytes.
	type span struct {
		hasText bool
		first   int
		last    int
	}
	spans := make([]span, len(doc.Docs))

	for i, d := range doc.Docs {
		blocks := make([]string, 0, len(d.Blocks))
		starts := make([]int, 0, len(d.Blocks))
		// An empty document owns no display bytes either, so it collapses
		// onto whatever boundary the previous one left behind.
		displayStart := display.Len()
		// kept[j] is how many blocks survived normalization before raw
		// block j, which is the canonical index an image anchored before
		// j belongs at. It is monotone, so a dropped block simply moves
		// its images onto the next surviving one.
		kept := make([]int, len(d.Blocks)+1)
		for j, raw := range d.Blocks {
			kept[j] = len(starts)
			norm := Normalize(raw)
			if norm == "" {
				continue
			}
			if canonical.Len() > 0 {
				canonical.WriteByte(' ')
			}
			if display.Len() > 0 {
				display.WriteByte('\n')
			}
			if len(starts) == 0 {
				displayStart = display.Len()
			}
			starts = append(starts, canonical.Len())
			canonical.WriteString(norm)
			display.WriteString(displayText(raw))
			blocks = append(blocks, norm)
		}
		kept[len(d.Blocks)] = len(starts)

		spans[i] = span{hasText: len(starts) > 0}
		if spans[i].hasText {
			spans[i].first = starts[0]
			spans[i].last = canonical.Len()
		}
		chapters[i] = models.EpubChapter{
			SpineIndex: i,
			Href:       d.Href,
			Title:      d.Title,
			Depth:      d.Depth,
		}
		index.Documents[i] = IndexedDoc{
			Href:         d.Href,
			SpineIndex:   i,
			Blocks:       starts,
			Images:       anchorImages(d.Images, kept),
			DisplayStart: displayStart,
			DisplayEnd:   display.Len(),
		}
	}

	// Hand each non-empty document except the last its trailing separator
	// space; empty documents sit on the boundary between their neighbours.
	lastNonEmpty := -1
	for i := range spans {
		if spans[i].hasText {
			lastNonEmpty = i
		}
	}
	boundary := 0
	for i := range spans {
		switch {
		case spans[i].hasText:
			chapters[i].CharStart = spans[i].first
			chapters[i].CharEnd = spans[i].last
			if i != lastNonEmpty {
				chapters[i].CharEnd++
			}
			index.Documents[i].CharStart = chapters[i].CharStart
			index.Documents[i].CharEnd = chapters[i].CharEnd
			boundary = chapters[i].CharEnd
		default:
			chapters[i].CharStart = boundary
			chapters[i].CharEnd = boundary
			index.Documents[i].CharStart = boundary
			index.Documents[i].CharEnd = boundary
		}
	}

	index.CharCount = canonical.Len()
	return canonical.String(), display.String(), chapters, index
}

// displayText prepares one block for reading: the book's own characters,
// with every run of whitespace collapsed to a single space and the edges
// trimmed. Nothing else is folded — case, quotes, dashes and ligatures
// belong to the book, and the folding lives in Normalize.
//
// Collapsing is structural, not cosmetic: the display file joins blocks with
// newlines, so a block that kept one of its own would come back out as two.
func displayText(raw string) string {
	return strings.Join(strings.Fields(raw), " ")
}

// anchorImages re-anchors a document's images from raw block indexes onto
// canonical ones. An image trailing every block keeps that position, which
// is what len(kept)-1 maps to.
func anchorImages(images []epub.Image, kept []int) []IndexedImage {
	if len(images) == 0 {
		return nil
	}
	out := make([]IndexedImage, 0, len(images))
	for _, img := range images {
		at := img.BeforeBlock
		if at < 0 {
			at = 0
		}
		if at > len(kept)-1 {
			at = len(kept) - 1
		}
		out = append(out, IndexedImage{Href: img.Href, Alt: img.Alt, BeforeBlock: kept[at]})
	}
	return out
}

// parseEpubFile opens and parses an EPUB from disk.
func parseEpubFile(path string) (*epub.Document, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("books: open epub: %w", err)
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("books: stat epub: %w", err)
	}
	doc, err := epub.Parse(f, info.Size())
	if err != nil {
		return nil, err
	}
	return doc, nil
}

// resolveWithinRoot joins a media file's root-relative path and verifies
// with symlinks resolved that the result stays inside the root. The NAS
// mount is read-only, but a crafted row must not turn parsing into an
// arbitrary-file read.
func resolveWithinRoot(root, rel string) (string, error) {
	abs := filepath.Join(root, rel)
	rootReal, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", fmt.Errorf("books: resolve media root: %w", err)
	}
	real, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", fmt.Errorf("books: resolve media file: %w", err)
	}
	if real != rootReal && !strings.HasPrefix(real+string(filepath.Separator), rootReal+string(filepath.Separator)) {
		return "", fmt.Errorf("books: media path escapes its root")
	}
	return real, nil
}

// writeFileAtomic writes via a temp file in the same directory so readers
// never observe a half-written canonical text.
func writeFileAtomic(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".epubtext-*")
	if err != nil {
		return fmt.Errorf("books: temp file: %w", err)
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("books: write text: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("books: close text: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("books: rename text: %w", err)
	}
	return nil
}

func writeJSONAtomic(path string, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("books: marshal index: %w", err)
	}
	return writeFileAtomic(path, data)
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}
