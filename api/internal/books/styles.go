package books

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"

	"github.com/collinpendleton/backhog/api/internal/books/epub"
	"github.com/collinpendleton/backhog/api/internal/models"
)

// StylesVersion guards the typography sidecar on its own clock: moving it
// re-derives every book's styles on next open without touching the
// canonical text, so no position, alignment or page anchor is disturbed.
//
// v2 keys documents the way the block index does — by folded chapter, not
// raw spine file — so books whose short documents fold together (a cover
// into the next file, say) line up; v1 sidecars are rebuilt.
const StylesVersion = "2"

// BookStyles is the typography sidecar: per indexed document (keyed by its
// SpineIndex, as the display endpoint asks for it), one entry per display
// block (nil for a plain paragraph), parallel to the display text and the
// block index. Documents with nothing but plain paragraphs are omitted.
type BookStyles struct {
	Version string                        `json:"version"`
	Docs    map[string][]*epub.BlockStyle `json:"docs"`
}

// StylesPath is where a text's typography sidecar lives. The version is in
// the name, so "is it current" is a stat, not a read — the check runs on
// every text request.
func (ing *Ingester) StylesPath(id string) string {
	return filepath.Join(ing.dir, id+".styles.v"+StylesVersion+".json")
}

// BuildStyles derives the sidecar from a parse, walking blocks exactly as
// Canonicalize does — a block dropped there is dropped here — and folding
// raw spine documents into the index's documents the way foldChapters did:
// each indexed document starts at its SpineIndex and runs up to the next
// one's. Entry i of a document then belongs to its display block i. A
// document whose block count does not match the index gets no styles; the
// rest of the book still does.
//
// A block that normalizes to nothing is usually a typed scene break ("* * *",
// "⁂"): punctuation only, which the canonical text rightly drops. The page
// should still show the pause, so it becomes a Break on the next block.
func BuildStyles(doc *epub.Document, index *BlockIndex) BookStyles {
	out := BookStyles{Version: StylesVersion, Docs: map[string][]*epub.BlockStyle{}}
	raw := make([][]*epub.BlockStyle, len(doc.Docs))
	for i, d := range doc.Docs {
		var styles []*epub.BlockStyle
		breakNext := false
		for j, raw := range d.Blocks {
			var style epub.BlockStyle
			if j < len(d.Styles) {
				style = d.Styles[j]
			}
			if Normalize(raw) == "" {
				if style.Break || displayText(raw) != "" {
					breakNext = true
				}
				continue
			}
			style.Break = style.Break || breakNext
			breakNext = false
			style.Runs = displayRuns(style.Runs, displayText(raw))
			if style.Kind == "" && !style.Break && style.Runs == nil {
				styles = append(styles, nil)
				continue
			}
			s := style
			styles = append(styles, &s)
		}
		raw[i] = styles
	}

	if index == nil {
		return out
	}
	for k, d := range index.Documents {
		end := len(raw)
		if k+1 < len(index.Documents) {
			end = index.Documents[k+1].SpineIndex
		}
		if d.SpineIndex < 0 || d.SpineIndex > end || end > len(raw) {
			continue
		}
		var styles []*epub.BlockStyle
		for i := d.SpineIndex; i < end; i++ {
			styles = append(styles, raw[i]...)
		}
		if len(styles) != len(d.Blocks) {
			continue
		}
		for _, st := range styles {
			if st != nil {
				out.Docs[strconv.Itoa(d.SpineIndex)] = styles
				break
			}
		}
	}
	return out
}

// displayRuns applies displayText's whitespace collapse across a block's
// runs, so their concatenation is the display block exactly. A collapsed
// stretch of whitespace becomes one plain space — a line break if any of it
// was a <br>. Returns nil when no run carries a mark (the common paragraph)
// or, defensively, when the result would not reproduce want.
func displayRuns(runs []epub.Run, want string) []epub.Run {
	var out []epub.Run
	emit := func(text, marks string) {
		if n := len(out); n > 0 && out[n-1].Marks == marks {
			out[n-1].Text += text
			return
		}
		out = append(out, epub.Run{Text: text, Marks: marks})
	}
	// A collapsed space keeps the marks of the text it sat in, so a bold
	// title stays one bold run instead of bold words between plain spaces.
	space, brk, started := false, false, false
	spaceMarks := ""
	for _, run := range runs {
		lineBreak := strings.ContainsRune(run.Marks, epub.MarkBreak)
		for _, r := range run.Text {
			if unicode.IsSpace(r) {
				if !space {
					spaceMarks = run.Marks
				}
				space = true
				brk = brk || lineBreak
				continue
			}
			if space && started {
				if brk {
					emit(" ", string(epub.MarkBreak))
				} else {
					emit(" ", spaceMarks)
				}
			}
			space, brk, started = false, false, true
			emit(string(r), run.Marks)
		}
	}

	var joined strings.Builder
	marked := false
	for _, run := range out {
		joined.WriteString(run.Text)
		marked = marked || run.Marks != ""
	}
	if !marked || joined.String() != want {
		return nil
	}
	return out
}

// writeStyles persists a parse's typography beside its text, and clears any
// sidecar an older StylesVersion left behind.
func (ing *Ingester) writeStyles(et models.EpubText, doc *epub.Document, index *BlockIndex) error {
	if err := writeJSONAtomic(ing.StylesPath(et.ID), BuildStyles(doc, index)); err != nil {
		return err
	}
	ing.removeStaleStyles(et)
	return nil
}

func (ing *Ingester) removeStaleStyles(et models.EpubText) {
	old, _ := filepath.Glob(filepath.Join(ing.dir, et.ID+".styles.v*.json"))
	for _, path := range old {
		if path != ing.StylesPath(et.ID) {
			_ = os.Remove(path)
		}
	}
}

// stylesCurrent reports whether a text's sidecar exists at this version.
func (ing *Ingester) stylesCurrent(et models.EpubText) bool {
	return fileExists(ing.StylesPath(et.ID))
}

// markStylesless records that a book has no usable typography, so the heal
// is not attempted again on every request.
func (ing *Ingester) markStylesless(ctx context.Context, f models.MediaFile, et models.EpubText) {
	if err := writeJSONAtomic(ing.StylesPath(et.ID), BookStyles{Version: StylesVersion}); err != nil {
		slog.WarnContext(ctx, "styles: write", "file", f.ID, "error", err)
		return
	}
	ing.removeStaleStyles(et)
}

// healStyles gives a text parsed before the sidecar existed (or under an
// older StylesVersion) its typography: the file is parsed again, but only
// the sidecar is written, and each document only where the parse still
// lines up block for block with the stored index — styles describing some
// other block list would put headings on the wrong paragraphs. Best
// effort: the reader renders plain paragraphs without it, exactly as before.
func (ing *Ingester) healStyles(ctx context.Context, f models.MediaFile, et models.EpubText) {
	path, err := resolveWithinRoot(f.Root, f.Path)
	if err != nil {
		return
	}
	if strings.EqualFold(filepath.Ext(path), ".pdf") {
		// A PDF has no markup to read typography from.
		ing.markStylesless(ctx, f, et)
		return
	}
	parsed, err := parseBookFile(path)
	if err != nil {
		slog.WarnContext(ctx, "styles: parse", "file", f.ID, "error", err)
		ing.markStylesless(ctx, f, et)
		return
	}
	index, err := ing.LoadIndex(ctx, et)
	if err != nil {
		return
	}
	if err := ing.writeStyles(et, parsed, index); err != nil {
		slog.WarnContext(ctx, "styles: write", "file", f.ID, "error", err)
	}
}

// ReadStyles returns one spine document's block styles, parallel to its
// display blocks, or nil when the book has none recorded.
func (ing *Ingester) ReadStyles(et models.EpubText, spine int) ([]*epub.BlockStyle, error) {
	data, err := os.ReadFile(ing.StylesPath(et.ID))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var styles BookStyles
	if err := json.Unmarshal(data, &styles); err != nil {
		return nil, err
	}
	return styles.Docs[strconv.Itoa(spine)], nil
}
