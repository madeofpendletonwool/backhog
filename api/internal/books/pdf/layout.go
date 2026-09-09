package pdf

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	gopdf "github.com/ledongthuc/pdf"

	"github.com/collinpendleton/backhog/api/internal/books/epub"
)

// This file owns reading order: the run-to-line, line-to-paragraph
// clustering that turns positioned glyph runs into the paragraph-ish blocks
// the spine contract speaks. It is the PDF counterpart of the block rules
// epub.ExtractBlocksHTML applies to markup — there is no markup to share
// code with, but the block granularity and the Headings semantics match, so
// the canonicalizer and the chapter rules treat all three formats alike.

// pageBox is the page's crop rectangle in PDF coordinates (y grows upward).
type pageBox struct {
	x0, y0, x1, y1 float64
}

func (b pageBox) width() float64  { return b.x1 - b.x0 }
func (b pageBox) height() float64 { return b.y1 - b.y0 }

// word is one whitespace-delimited token with its extent, its dominant
// glyph size, and the quality gate's per-word verdicts.
type word struct {
	text     string
	x0, x1   float64
	size     float64
	hasRunes bool // any letter or digit
	hasJunk  bool // any replacement or control rune
	dictHit  bool // a common word once folded
}

// line is one visual row of words on a shared baseline.
type line struct {
	words   []word
	x0, x1  float64
	y       float64
	size    float64
	heading bool
	dropped bool
	// geomOK reports whether the line's horizontal extent is real. Fonts
	// that carry no glyph widths (core-font PDFs from minimal writers)
	// extract with every glyph at the same x and zero width; their
	// position-driven block rules would be noise, so they are skipped.
	geomOK bool
}

func (l *line) text() string {
	var sb strings.Builder
	for i, w := range l.words {
		if i > 0 {
			sb.WriteByte(' ')
		}
		sb.WriteString(w.text)
	}
	return sb.String()
}

// block is one paragraph-ish output unit; heading records whether its
// first line was set in display type.
type block struct {
	text    string
	heading bool
}

// pageText is one page's clustering: lines in y/x order until blocks are
// built, then blocks in reading order.
type pageText struct {
	box    pageBox
	lines  []line
	blocks []block
}

// book is a fully extracted PDF before it is shaped into spine documents.
type book struct {
	pages    []pageText
	bodySize float64 // median line size across the book
	gate     gateStats
}

// extractBook walks every page and clusters text runs into lines, gathering
// the statistics the quality gate needs. Blocks are built later (buildDocs)
// once the book-wide body size is known.
func extractBook(rd *gopdf.Reader, pageCount int) *book {
	b := &book{pages: make([]pageText, pageCount)}
	for i := 1; i <= pageCount; i++ {
		p := rd.Page(i)
		if p.V.IsNull() {
			continue // a hole in the page tree stays an empty document
		}
		b.pages[i-1] = pageText{box: pageBoxOf(p)}
		b.pages[i-1].lines = buildLines(p.Content().Text, b.pages[i-1].box.width())
	}
	for i := range b.pages {
		for _, l := range b.pages[i].lines {
			b.gate.observe(l.words)
		}
		if len(b.pages[i].lines) > 0 {
			b.gate.pagesWithText++
		}
	}
	b.gate.pages = pageCount
	b.stripRunningHeads()
	return b
}

// pageBoxOf returns the page's crop box, falling back to its media box and
// then to US Letter, so geometry-dependent rules never divide by zero.
// Page boxes are inheritable, so the parent chain is walked the way the
// reader's own (unexported) helpers would.
func pageBoxOf(p gopdf.Page) pageBox {
	for _, key := range []string{"CropBox", "MediaBox"} {
		if v := inheritedKey(p, key); v.Kind() == gopdf.Array && v.Len() >= 4 {
			box := pageBox{v.Index(0).Float64(), v.Index(1).Float64(),
				v.Index(2).Float64(), v.Index(3).Float64()}
			if box.width() > 0 && box.height() > 0 {
				return box
			}
		}
	}
	return pageBox{0, 0, 612, 792}
}

// inheritedKey walks a page's Parent chain for an inheritable key.
func inheritedKey(p gopdf.Page, key string) gopdf.Value {
	for v := p.V; !v.IsNull(); v = v.Key("Parent") {
		if r := v.Key(key); !r.IsNull() {
			return r
		}
	}
	return gopdf.Value{}
}

// buildLines clusters per-glyph text entries into visual lines and words.
// The entries arrive in content-stream order; geometry, not draw order, is
// the only trustworthy reading-order signal, so lines come out sorted by
// descending y (top of the page first — PDF y grows upward) then ascending
// x, with word boundaries inferred from gaps wider than a fraction of the
// glyph size. A gap wider than a fraction of the page is a column
// boundary: the line splits there, because two columns sharing a baseline
// are two lines as far as reading order is concerned.
func buildLines(entries []gopdf.Text, pageW float64) []line {
	// Empty strings are dropped; whitespace glyphs stay, because an
	// explicit space is a word boundary when glyph widths are absent and
	// gaps cannot signal one.
	ents := make([]gopdf.Text, 0, len(entries))
	for _, e := range entries {
		if e.S != "" {
			ents = append(ents, e)
		}
	}
	if len(ents) == 0 {
		return nil
	}
	sort.SliceStable(ents, func(i, j int) bool {
		if ents[i].Y != ents[j].Y {
			return ents[i].Y > ents[j].Y
		}
		return ents[i].X < ents[j].X
	})

	var lines []line
	var run []gopdf.Text
	flush := func() {
		if len(run) > 0 {
			lines = append(lines, lineFromRun(run, pageW)...)
			run = run[:0]
		}
	}
	for _, e := range ents {
		if len(run) > 0 {
			tol := 0.45 * math.Max(run[0].FontSize, e.FontSize)
			if math.Abs(e.Y-run[0].Y) > tol {
				flush()
			}
		}
		run = append(run, e)
	}
	flush()
	return lines
}

// lineFromRun turns one baseline's glyph entries (already x-ordered) into
// one or more column-segment lines of gap-separated words. A whitespace
// glyph ends the current word; otherwise a gap wider than a fifth of the
// glyph size does; a gap wider than a twenty-fifth of the page ends the
// segment.
func lineFromRun(run []gopdf.Text, pageW float64) []line {
	seed := line{y: run[0].Y, size: run[0].FontSize}
	var words []word
	var w word
	started := false
	prevEnd := math.NaN()
	flushWord := func() {
		if started {
			w.text = strings.TrimSpace(w.text)
			w.x1 = prevEnd
			for _, r := range w.text {
				if unicode.IsLetter(r) || unicode.IsDigit(r) {
					w.hasRunes = true
				}
				if isJunkRune(r) {
					w.hasJunk = true
				}
			}
			// A junk-bearing token survives even without letters: unmapped
			// glyphs are the quality gate's evidence, not noise to filter.
			if w.hasRunes || w.hasJunk {
				w.dictHit = w.hasRunes && commonWords[foldWord(w.text)]
				words = append(words, w)
			}
			w = word{}
			started = false
		}
	}
	for _, e := range run {
		if e.FontSize > seed.size {
			seed.size = e.FontSize
		}
		if strings.TrimSpace(e.S) == "" {
			// An explicit space glyph is a word boundary by construction.
			flushWord()
			prevEnd = e.X + e.W
			continue
		}
		if started && !math.IsNaN(prevEnd) {
			gap := e.X - prevEnd
			if gap > 0.2*math.Max(w.size, e.FontSize) {
				flushWord()
			}
		}
		if !started {
			w.x0 = e.X
			w.size = e.FontSize
			started = true
		}
		w.text += e.S
		if e.FontSize > w.size {
			w.size = e.FontSize
		}
		prevEnd = e.X + e.W
	}
	flushWord()
	if len(words) == 0 {
		return nil
	}
	return segments(words, seed, pageW)
}

// segments splits one baseline's words into line segments at column-scale
// gaps, in x order, each carrying its own extent, geometry verdict and
// junk evidence.
func segments(words []word, seed line, pageW float64) []line {
	var out []line
	var cur []word
	flush := func() {
		if len(cur) == 0 {
			return
		}
		l := line{
			words: cur,
			y:     seed.y,
			size:  seed.size,
			x0:    cur[0].x0,
			x1:    cur[len(cur)-1].x1,
		}
		for _, w := range cur {
			if w.size > l.size {
				l.size = w.size
			}
		}
		l.geomOK = l.x1 > l.x0+0.6*l.size
		out = append(out, l)
		cur = nil
	}
	for _, w := range words {
		if len(cur) > 0 && w.x0-cur[len(cur)-1].x1 > 0.04*pageW {
			flush()
		}
		cur = append(cur, w)
	}
	flush()
	return out
}

// maskLine folds a line's text into the shape its recurrence is judged on:
// lowercase, digits collapsed to a single placeholder, everything else
// dropped. "Chapter 12" and "chapter 47" are the same running head; a
// sentence of prose never is.
func maskLine(s string) string {
	var sb strings.Builder
	space := false
	for _, r := range strings.ToLower(s) {
		switch {
		case unicode.IsDigit(r):
			if !space && sb.Len() > 0 {
				sb.WriteByte(' ')
			}
			sb.WriteByte('#')
			space = false
		case unicode.IsLetter(r):
			if space && sb.Len() > 0 {
				sb.WriteByte(' ')
			}
			sb.WriteRune(r)
			space = false
		default:
			space = sb.Len() > 0
		}
	}
	return sb.String()
}

// stripRunningHeads drops lines that recur at the same vertical position
// across pages: running heads and folios, the apparatus a printed page
// carries that a book's text does not. Only a page's first and last line
// are candidates, and only when the same masked shape occupies that edge on
// at least three pages and at least half the book — prose repeating that
// uniformly across a book's top edge is not prose.
func (b *book) stripRunningHeads() {
	n := len(b.pages)
	if n < 4 {
		return
	}
	tops := map[string]int{}
	bottoms := map[string]int{}
	edges := make([][2]string, n) // per page: the top and bottom candidate masks
	for i := range b.pages {
		lines := b.pages[i].lines
		if len(lines) == 0 {
			continue
		}
		box := b.pages[i].box
		first, last := lines[0], lines[len(lines)-1]
		if first.y >= box.y1-0.12*box.height() {
			if m := maskLine(first.text()); m != "" {
				tops[m]++
				edges[i][0] = m
			}
		}
		if last.y <= box.y0+0.12*box.height() {
			if m := maskLine(last.text()); m != "" {
				bottoms[m]++
				edges[i][1] = m
			}
		}
	}
	recurring := func(m string, counts map[string]int) bool {
		c := counts[m]
		return c >= 3 && 2*c >= n
	}
	for i := range b.pages {
		lines := b.pages[i].lines
		if len(lines) == 0 {
			continue
		}
		if m := edges[i][0]; m != "" && recurring(m, tops) {
			lines[0].dropped = true
		}
		if m := edges[i][1]; m != "" && recurring(m, bottoms) {
			lines[len(lines)-1].dropped = true
		}
	}
}

// sortLines orders lines top-to-bottom, left-to-right within a row.
func sortLines(lines []line) {
	sort.SliceStable(lines, func(i, j int) bool {
		if lines[i].y != lines[j].y {
			return lines[i].y > lines[j].y
		}
		return lines[i].x0 < lines[j].x0
	})
}

// orderLines returns a page's lines in reading order. A two-column layout
// is detected by a clear vertical gutter — a band no line crosses, with at
// least three lines wholly on each side — and reads the left column
// top-to-bottom before the right; anything else reads straight down the
// page. Full-width lines crossing the middle suppress the detection, which
// is the conservative failure: a headered two-column page still reads its
// columns in order once the header is the page's own first line.
func orderLines(lines []line, box pageBox) []line {
	sortLines(lines)
	if len(lines) < 6 {
		return lines
	}
	w := box.width()
	if w <= 0 {
		return lines
	}
	crosses := func(x float64) bool {
		for _, l := range lines {
			if l.x0 < x-1 && l.x1 > x+1 {
				return true
			}
		}
		return false
	}
	step := w / 200
	bestA, bestB := 0.0, 0.0 // widest clear band, empty when zero
	a := -1.0
	for x := 0.28 * w; x <= 0.72*w+step; x += step {
		if !crosses(x) {
			if a < 0 {
				a = x
			}
			if x-a > bestB-bestA {
				bestA, bestB = a, x
			}
		} else {
			a = -1
		}
	}
	if bestB-bestA < 0.04*w {
		return lines
	}
	mid := (bestA + bestB) / 2
	var left, right []line
	for _, l := range lines {
		switch {
		case l.x1 <= bestA+1, l.x0 < mid && l.x1 < bestB:
			left = append(left, l)
		default:
			right = append(right, l)
		}
	}
	if len(left) < 3 || len(right) < 3 {
		return lines
	}
	sortLines(left)
	sortLines(right)
	return append(left, right...)
}

// buildDocs shapes the extracted book into spine documents: one per page,
// blocks in reading order, headings marked where the type is larger than
// the book's body text, and the page's image XObjects inventoried behind
// its blocks.
func buildDocs(rd *gopdf.Reader, b *book) []epub.Doc {
	var sizes []float64
	for i := range b.pages {
		for _, l := range b.pages[i].lines {
			if !l.dropped {
				sizes = append(sizes, l.size)
			}
		}
	}
	sort.Float64s(sizes)
	if len(sizes) > 0 {
		b.bodySize = sizes[len(sizes)/2]
	}

	docs := make([]epub.Doc, len(b.pages))
	for i := range b.pages {
		pt := &b.pages[i]
		var kept []line
		for _, l := range pt.lines {
			if l.dropped {
				continue
			}
			if b.bodySize > 0 && l.size >= 1.18*b.bodySize {
				l.heading = true
			}
			kept = append(kept, l)
		}
		ordered := orderLines(kept, pt.box)
		blocks := assembleBlocks(ordered, pt.box)

		d := epub.Doc{
			SpineIndex: i,
			Href:       fmt.Sprintf("pdf:page:%d", i+1),
		}
		for _, bl := range blocks {
			if bl.heading {
				d.Headings = append(d.Headings, len(d.Blocks))
			}
			d.Blocks = append(d.Blocks, bl.text)
		}
		b.gate.imagePages += pageImages(rd.Page(i+1), &d)
		docs[i] = d
	}
	return docs
}

// assembleBlocks merges consecutive lines into paragraph blocks. A block
// breaks on a vertical gap past normal leading, a change in type size, a
// paragraph indent, or a short line followed by a flush restart — the
// signals a printed page actually carries. Line joins dehyphenate: a line
// ending in "-" followed by a lowercase continuation rejoins into one word,
// so the shared normalizer's dash rule can never freeze the split.
func assembleBlocks(lines []line, box pageBox) []block {
	w := box.width()
	leftEdge, rightEdge := math.Inf(1), math.Inf(-1)
	for _, l := range lines {
		leftEdge = math.Min(leftEdge, l.x0)
		rightEdge = math.Max(rightEdge, l.x1)
	}

	var blocks []block
	var sb strings.Builder
	open := false
	heading := false
	var prev line
	flush := func() {
		if open {
			if t := strings.TrimSpace(sb.String()); t != "" {
				blocks = append(blocks, block{text: t, heading: heading})
			}
		}
		sb.Reset()
		open, heading = false, false
	}
	for _, l := range lines {
		if open {
			split := false
			gap := prev.y - l.y
			maxSize := math.Max(prev.size, l.size)
			switch {
			case gap > 1.8*maxSize:
				split = true // paragraph spacing
			case l.size > prev.size*1.3 || l.size*1.3 < prev.size:
				split = true // display type or a size change mid-page
			case !prev.geomOK || !l.geomOK:
				// Widthless extraction: horizontal rules would be noise.
			case l.x0-prev.x0 > 0.25*w:
				split = true // a column or pull-quote transition, not an indent
			case l.x0-prev.x0 > 0.015*w:
				split = true // indented paragraph start
			case prev.x1 < rightEdge-0.15*w && endsSentence(prev) && l.x0 <= leftEdge+0.01*w:
				split = true // a finished sentence short of the margin, flush restart
			}
			if split {
				flush()
			}
		}
		if !open {
			open, heading = true, l.heading
			sb.WriteString(l.text())
		} else {
			joinLine(&sb, l.text())
		}
		prev = l
	}
	flush()
	return blocks
}

// endsSentence reports whether a line's last visible rune closes a
// sentence — the evidence that a short line finished its paragraph rather
// than being hyphenated or cut mid-thought.
func endsSentence(l line) bool {
	s := strings.TrimSpace(l.text())
	if s == "" {
		return false
	}
	last, _ := utf8.DecodeLastRuneInString(s)
	switch last {
	case '.', '!', '?', '"', '\u201d', '\u2019':
		return true
	}
	return false
}

// joinLine appends one line's text to a block under construction, joining
// hyphenated line-ends into single words when the next line continues the
// word (it starts with a lowercase letter or a digit). A heading-shaped
// continuation ("...number-" before "One") keeps its hyphen; the normalizer
// treats it as the dash it probably is.
func joinLine(sb *strings.Builder, text string) {
	if sb.Len() == 0 {
		sb.WriteString(text)
		return
	}
	cur := sb.String()
	if strings.HasSuffix(cur, "-") && continuesWord(text) {
		first := text
		rest := ""
		if i := strings.IndexAny(text, " \t"); i >= 0 {
			first, rest = text[:i], strings.TrimSpace(text[i+1:])
		}
		sb.Reset()
		sb.WriteString(strings.TrimSuffix(cur, "-"))
		sb.WriteString(first)
		if rest != "" {
			sb.WriteByte(' ')
			sb.WriteString(rest)
		}
		return
	}
	sb.WriteByte(' ')
	sb.WriteString(text)
}

// continuesWord reports whether s begins a hyphenated continuation.
func continuesWord(s string) bool {
	if s == "" {
		return false
	}
	first, _ := utf8.DecodeRuneInString(s)
	return unicode.IsLower(first) || unicode.IsDigit(first)
}

// rejoinCrossPageHyphens carries dehyphenation across page boundaries: a
// word split at a page break belongs, typographically, to the earlier
// page, so the continuation's first word moves up into the previous
// document and the split never reaches the canonical text. Without this,
// every hyphenated page break in a book would freeze as two words forever.
func rejoinCrossPageHyphens(docs []epub.Doc) {
	for i := 0; i+1 < len(docs); i++ {
		cur := &docs[i]
		next := &docs[i+1]
		if len(cur.Blocks) == 0 || len(next.Blocks) == 0 {
			continue
		}
		last := cur.Blocks[len(cur.Blocks)-1]
		if !strings.HasSuffix(last, "-") {
			continue
		}
		firstBlock := next.Blocks[0]
		firstWord := firstBlock
		rest := ""
		if j := strings.IndexAny(firstBlock, " \t"); j >= 0 {
			firstWord, rest = firstBlock[:j], strings.TrimSpace(firstBlock[j+1:])
		}
		if !continuesWord(firstWord) {
			continue
		}
		cur.Blocks[len(cur.Blocks)-1] = strings.TrimSuffix(last, "-") + firstWord
		if rest == "" {
			next.Blocks = next.Blocks[1:]
			shiftHeadings(&next.Headings)
		} else {
			next.Blocks[0] = rest
		}
	}
}

// shiftHeadings drops index 0 and moves the rest down after a leading
// block was removed.
func shiftHeadings(headings *[]int) {
	out := (*headings)[:0]
	for _, h := range *headings {
		if h > 0 {
			out = append(out, h-1)
		}
	}
	*headings = out
}

// pageImages inventories a page's image XObjects onto the document being
// built. Content-stream position is not tracked at this layer, so images
// anchor behind their page's blocks — len(Blocks), trailing everything on
// the page — which is the honest placement; an image-only page anchors at
// zero because it has no blocks to trail. Inline images (BI/ID/EI) are not
// resource XObjects and are not inventoried.
func pageImages(p gopdf.Page, d *epub.Doc) int {
	res := p.Resources()
	if res.Kind() != gopdf.Dict {
		return 0
	}
	xo := res.Key("XObject")
	if xo.Kind() != gopdf.Dict {
		return 0
	}
	n := 0
	for _, k := range xo.Keys() {
		v := xo.Key(k)
		if v.Kind() != gopdf.Stream && v.Kind() != gopdf.Dict {
			continue
		}
		if v.Key("Subtype").Name() != "Image" {
			continue
		}
		d.Images = append(d.Images, epub.Image{
			Href:        fmt.Sprintf("pdf:page:%d:%s", d.SpineIndex+1, k),
			BeforeBlock: len(d.Blocks),
		})
		n++
	}
	return n
}
