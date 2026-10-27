package books

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
const StylesVersion = "1"

// BookStyles is the typography sidecar: per spine document, one entry per
// display block (nil for a plain paragraph), parallel to the display text
// and the block index. Documents with nothing but plain paragraphs are
// omitted.
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
// Canonicalize does — a block dropped there is dropped here — so entry i
// of a document belongs to its display block i.
//
// A block that normalizes to nothing is usually a typed scene break ("* * *",
// "⁂"): punctuation only, which the canonical text rightly drops. The page
// should still show the pause, so it becomes a Break on the next block.
func BuildStyles(doc *epub.Document) BookStyles {
	out := BookStyles{Version: StylesVersion, Docs: map[string][]*epub.BlockStyle{}}
	for i, d := range doc.Docs {
		var styles []*epub.BlockStyle
		styled := false
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
			styled = true
		}
		if styled {
			out.Docs[strconv.Itoa(i)] = styles
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

// writeStyles persists a parse's typography beside its text.
func (ing *Ingester) writeStyles(et models.EpubText, doc *epub.Document) error {
	return writeJSONAtomic(ing.StylesPath(et.ID), BuildStyles(doc))
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
	}
}

// healStyles gives a text parsed before the sidecar existed (or under an
// older StylesVersion) its typography: the file is parsed again, but only
// the sidecar is written, and only if the parse still lines up block for
// block with the stored index — a styles file describing some other block
// list would put headings on the wrong paragraphs. Best effort: the reader
// renders plain paragraphs without it, exactly as before.
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
	if err := sameBlocks(parsed, index); err != nil {
		slog.WarnContext(ctx, "styles: parse no longer matches the stored text; skipped", "file", f.ID, "error", err)
		ing.markStylesless(ctx, f, et)
		return
	}
	if err := ing.writeStyles(et, parsed); err != nil {
		slog.WarnContext(ctx, "styles: write", "file", f.ID, "error", err)
	}
}

// sameBlocks checks a fresh parse keeps the stored index's block counts.
func sameBlocks(doc *epub.Document, index *BlockIndex) error {
	kept := map[int]int{}
	for i, d := range doc.Docs {
		for _, raw := range d.Blocks {
			if Normalize(raw) != "" {
				kept[i]++
			}
		}
	}
	for _, d := range index.Documents {
		if kept[d.SpineIndex] != len(d.Blocks) {
			return fmt.Errorf("spine %d: %d blocks parsed, %d stored", d.SpineIndex, kept[d.SpineIndex], len(d.Blocks))
		}
	}
	return nil
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
