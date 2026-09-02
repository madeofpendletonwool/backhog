// Package bench builds synthetic book/transcript pairs with known truth,
// so the aligner can be measured instead of eyeballed. It exists because
// the numbers that decide whether an alignment is published as ready or as
// low_confidence have to come from somewhere, and "it looked about right on
// one book" is not somewhere.
//
// The generator is deliberately unkind. Its books are drawn from a small
// vocabulary with a Zipf distribution, so common phrases genuinely repeat
// and the shingle index has to cope with ambiguity; its transcripts carry a
// word error rate, a publisher's intro and outro that appear in no book,
// numerals written the other way round, and — in the cases that matter
// most — an abridgement or an entirely different book.
package bench

import (
	"fmt"
	"math/rand"
	"sort"
	"strings"

	"github.com/collinpendleton/backhog/align/internal/align"
	"github.com/collinpendleton/backhog/api/booktext"
)

// Params describes one synthetic pair.
type Params struct {
	Name        string
	Description string
	Seed        int64
	// Words is the book's length. A real novel is 90k-150k words.
	Words int
	// WordErrorRate is the fraction of spoken words Whisper gets wrong,
	// as a substitution, a deletion or an insertion. base.en on clean
	// narration sits around 8-12%.
	WordErrorRate float64
	// WordsPerSecond is the narration rate. Audiobooks are read at
	// 150-160 words per minute.
	WordsPerSecond float64
	// SegmentSeconds is how long a Whisper segment runs.
	SegmentSeconds float64
	// IntroWords and OutroWords are the narration that exists in no EPUB:
	// the publisher's front matter and the "end of book" outro.
	IntroWords, OutroWords int
	// ReadFraction is how much of the book the narration actually covers.
	// Less than 1 is an abridgement.
	ReadFraction float64
	// SkipRuns and SkipWords model illustrations, footnotes and captions:
	// SkipRuns evenly spaced runs of SkipWords the narrator passes over.
	SkipRuns, SkipWords int
	// DifferentBook narrates a book generated from another seed entirely —
	// the wrong-edition case the confidence thresholds exist to catch.
	DifferentBook bool
}

// Truth is one known (audio second, canonical byte offset) pair.
type Truth struct {
	AudioSeconds float64
	CharOffset   int
}

// Case is a generated pair plus its ground truth.
type Case struct {
	Params    Params
	Canonical string
	Segments  []align.Segment
	Truth     []Truth
}

// Cases are the scenarios the thresholds were chosen from. They run in a
// second or two each, so the whole table is cheap enough to regenerate
// whenever the aligner changes.
func Cases() []Params {
	return []Params{
		{
			Name:           "clean",
			Description:    "unabridged, 8% WER, intro and outro",
			Seed:           1,
			Words:          40000,
			WordErrorRate:  0.08,
			WordsPerSecond: 2.6,
			SegmentSeconds: 6,
			IntroWords:     220,
			OutroWords:     90,
			ReadFraction:   1,
			SkipRuns:       6,
			SkipWords:      40,
		},
		{
			Name:           "noisy",
			Description:    "unabridged, 20% WER (a hard narrator or a small model)",
			Seed:           2,
			Words:          40000,
			WordErrorRate:  0.20,
			WordsPerSecond: 2.6,
			SegmentSeconds: 6,
			IntroWords:     220,
			OutroWords:     90,
			ReadFraction:   1,
			SkipRuns:       6,
			SkipWords:      40,
		},
		{
			Name:           "very-noisy",
			Description:    "unabridged, 32% WER (the floor of a usable transcript)",
			Seed:           3,
			Words:          40000,
			WordErrorRate:  0.32,
			WordsPerSecond: 2.6,
			SegmentSeconds: 6,
			IntroWords:     220,
			OutroWords:     90,
			ReadFraction:   1,
			SkipRuns:       6,
			SkipWords:      40,
		},
		{
			Name:           "garbled",
			Description:    "unabridged but 50% WER: a transcript past saving",
			Seed:           6,
			Words:          40000,
			WordErrorRate:  0.50,
			WordsPerSecond: 2.6,
			SegmentSeconds: 6,
			IntroWords:     220,
			OutroWords:     90,
			ReadFraction:   1,
			SkipRuns:       6,
			SkipWords:      40,
		},
		{
			Name:           "abridged",
			Description:    "abridged reading: 55% of the book, 10% WER",
			Seed:           4,
			Words:          40000,
			WordErrorRate:  0.10,
			WordsPerSecond: 2.6,
			SegmentSeconds: 6,
			IntroWords:     220,
			OutroWords:     90,
			ReadFraction:   0.55,
			SkipRuns:       6,
			SkipWords:      40,
		},
		{
			Name:           "wrong-book",
			Description:    "the audio is a different book entirely",
			Seed:           5,
			Words:          40000,
			WordErrorRate:  0.10,
			WordsPerSecond: 2.6,
			SegmentSeconds: 6,
			IntroWords:     220,
			OutroWords:     90,
			ReadFraction:   1,
			DifferentBook:  true,
		},
	}
}

// Generate builds one case.
func Generate(p Params) Case {
	rng := rand.New(rand.NewSource(p.Seed))
	vocab := vocabulary(rng, 1500)

	bookWords := writeBook(rng, vocab, p.Words)
	canonical := canonicalize(bookWords)
	starts := offsets(bookWords)

	spokenSource := bookWords
	spokenStarts := starts
	if p.DifferentBook {
		other := rand.New(rand.NewSource(p.Seed + 9973))
		spokenSource = writeBook(other, vocabulary(other, 1500), p.Words)
		spokenStarts = nil
	}

	read := int(float64(len(spokenSource)) * clampFraction(p.ReadFraction))
	skips := skipRanges(len(spokenSource), read, p.SkipRuns, p.SkipWords)

	// Narrate: walk the book in order, corrupting as Whisper would, and
	// remember for each spoken word which canonical word it came from.
	type spoken struct {
		word string
		src  int // -1 for a word that is in no book
	}
	said := make([]spoken, 0, read+p.IntroWords+p.OutroWords)
	front := frontMatter(rng, p.IntroWords)
	for _, w := range front {
		said = append(said, spoken{word: w, src: -1})
	}
	for i := 0; i < read; i++ {
		if skips.contains(i) {
			continue
		}
		switch {
		case rng.Float64() < p.WordErrorRate/3:
			// Deletion.
			continue
		case rng.Float64() < p.WordErrorRate/2:
			// Substitution.
			said = append(said, spoken{word: vocab[rng.Intn(len(vocab))], src: -1})
		default:
			said = append(said, spoken{word: spokenSource[i], src: i})
			if rng.Float64() < p.WordErrorRate/4 {
				// Insertion, after the word it follows.
				said = append(said, spoken{word: vocab[rng.Intn(len(vocab))], src: -1})
			}
		}
	}
	for _, w := range frontMatter(rng, p.OutroWords) {
		said = append(said, spoken{word: w, src: -1})
	}

	// Cut the narration into segments at the narration rate.
	perSegment := max(int(p.SegmentSeconds*p.WordsPerSecond), 1)
	c := Case{Params: p, Canonical: canonical}
	at := 0.0
	for i := 0; i < len(said); i += perSegment {
		end := min(i+perSegment, len(said))
		words := make([]string, 0, end-i)
		truth := -1
		for _, s := range said[i:end] {
			words = append(words, s.word)
			if truth < 0 && s.src >= 0 && spokenStarts != nil {
				truth = spokenStarts[s.src]
			}
		}
		duration := float64(end-i) / p.WordsPerSecond
		c.Segments = append(c.Segments, align.Segment{
			AudioStart: at,
			AudioEnd:   at + duration,
			Text:       speak(words),
		})
		if truth >= 0 {
			c.Truth = append(c.Truth, Truth{AudioSeconds: at, CharOffset: truth})
		}
		at += duration
	}
	return c
}

// Report is what one measured case says about the aligner.
type Report struct {
	Case           Params
	Result         align.Result
	Coverage       float64
	MeanConfidence float64
	Anchors        int
	// TruthPoints is how many ground-truth pairs fell inside the anchored
	// span and could therefore be scored at all.
	TruthPoints int
	// MedianErrChars, P95ErrChars and MaxErrChars are the distance between
	// where the aligner puts a moment and where it really is.
	MedianErrChars, P95ErrChars, MaxErrChars int
	// CharsPerSecond converts those into the number a reader would feel.
	CharsPerSecond float64
}

// Measure runs the aligner over a case and scores it against the truth.
func Measure(c Case, opts align.Options) Report {
	res := align.Align(c.Canonical, c.Segments, opts)
	r := Report{
		Case:           c.Params,
		Result:         res,
		Coverage:       res.Coverage,
		MeanConfidence: res.MeanConfidence,
		Anchors:        len(res.Anchors),
	}
	if len(c.Segments) > 0 {
		total := c.Segments[len(c.Segments)-1].AudioEnd
		if total > 0 {
			r.CharsPerSecond = float64(len(c.Canonical)) / total
		}
	}
	if len(res.Anchors) < 2 {
		return r
	}

	errs := make([]int, 0, len(c.Truth))
	for _, t := range c.Truth {
		if t.AudioSeconds < res.Anchors[0].AudioSeconds ||
			t.AudioSeconds > res.Anchors[len(res.Anchors)-1].AudioSeconds {
			continue
		}
		errs = append(errs, abs(interpolate(res.Anchors, t.AudioSeconds)-t.CharOffset))
	}
	if len(errs) == 0 {
		return r
	}
	sort.Ints(errs)
	r.TruthPoints = len(errs)
	r.MedianErrChars = errs[len(errs)/2]
	r.P95ErrChars = errs[min(int(float64(len(errs))*0.95), len(errs)-1)]
	r.MaxErrChars = errs[len(errs)-1]
	return r
}

// interpolate reads a char offset off the anchor map the way the API's
// position translator does: linearly between the two surrounding anchors.
func interpolate(anchors []align.Anchor, seconds float64) int {
	i := sort.Search(len(anchors), func(i int) bool {
		return anchors[i].AudioSeconds >= seconds
	})
	if i == 0 {
		return anchors[0].CharOffset
	}
	if i >= len(anchors) {
		return anchors[len(anchors)-1].CharOffset
	}
	lo, hi := anchors[i-1], anchors[i]
	span := hi.AudioSeconds - lo.AudioSeconds
	if span <= 0 {
		return lo.CharOffset
	}
	frac := (seconds - lo.AudioSeconds) / span
	return lo.CharOffset + int(frac*float64(hi.CharOffset-lo.CharOffset))
}

// String renders one report as a table row.
func (r Report) String() string {
	secs := func(chars int) string {
		if r.CharsPerSecond <= 0 {
			return "-"
		}
		return fmt.Sprintf("%.1fs", float64(chars)/r.CharsPerSecond)
	}
	return fmt.Sprintf("%-12s cov=%.3f conf=%.3f anchors=%-6d truth=%-5d median=%-6d p95=%-6d max=%-7d (p95 %s, max %s)",
		r.Case.Name, r.Coverage, r.MeanConfidence, r.Anchors, r.TruthPoints,
		r.MedianErrChars, r.P95ErrChars, r.MaxErrChars, secs(r.P95ErrChars), secs(r.MaxErrChars))
}

// --- generation helpers -------------------------------------------------

var syllables = []string{
	"ba", "ken", "tor", "mil", "sa", "dre", "lun", "vor", "tha", "rin",
	"pel", "gos", "una", "wex", "fal", "nid", "cor", "yel", "bra", "shen",
	"ott", "vil", "quen", "mar", "del", "hos", "urn", "kip", "lav", "tez",
}

// vocabulary builds a deterministic pseudo-English word list.
func vocabulary(rng *rand.Rand, n int) []string {
	seen := make(map[string]bool, n)
	out := make([]string, 0, n)
	for len(out) < n {
		w := syllables[rng.Intn(len(syllables))] + syllables[rng.Intn(len(syllables))]
		if rng.Float64() < 0.4 {
			w += syllables[rng.Intn(len(syllables))]
		}
		if seen[w] {
			continue
		}
		seen[w] = true
		out = append(out, w)
	}
	return out
}

// writeBook draws words with a Zipf distribution so the common ones really
// are common, which is what makes shingles ambiguous in the way a real
// book's are.
func writeBook(rng *rand.Rand, vocab []string, words int) []string {
	zipf := rand.NewZipf(rng, 1.1, 2, uint64(len(vocab)-1))
	out := make([]string, 0, words)
	for len(out) < words {
		switch {
		case rng.Float64() < 0.004:
			// A number, printed as digits the way a book prints it.
			out = append(out, fmt.Sprintf("%d", rng.Intn(400)+1))
		case rng.Float64() < 0.004:
			// An abbreviated title, printed the way a book prints it.
			out = append(out, "Mr.", vocab[zipf.Uint64()])
		default:
			out = append(out, vocab[zipf.Uint64()])
		}
	}
	return out[:words]
}

// frontMatter is narration that appears in no EPUB.
func frontMatter(rng *rand.Rand, words int) []string {
	boiler := strings.Fields(`this is an audio production presented by the publisher
		all rights reserved unauthorized duplication is prohibited the following
		recording is read by the author end of book thank you for listening`)
	out := make([]string, 0, words)
	for len(out) < words {
		out = append(out, boiler[rng.Intn(len(boiler))])
	}
	return out
}

// canonicalize turns the printed book into its canonical text exactly the
// way the EPUB ingester does.
func canonicalize(words []string) string {
	return booktext.Normalize(strings.Join(words, " "))
}

// offsets maps each printed word to its byte offset in the canonical text.
// Words that normalize away own no offset and take the next one's.
func offsets(words []string) []int {
	var b strings.Builder
	out := make([]int, len(words))
	for i, w := range words {
		n := booktext.Normalize(w)
		if n == "" {
			out[i] = b.Len()
			continue
		}
		if b.Len() > 0 {
			b.WriteByte(' ')
		}
		out[i] = b.Len()
		b.WriteString(n)
	}
	return out
}

// speak renders spoken words the way Whisper would write them down: the
// numbers spelled out, the titles unabbreviated, sentence punctuation the
// normalizer will throw away again.
func speak(words []string) string {
	var b strings.Builder
	for i, w := range words {
		if i > 0 {
			b.WriteByte(' ')
		}
		switch {
		case w == "Mr.":
			b.WriteString("Mister")
		case isNumber(w):
			b.WriteString(spellNumber(w))
		default:
			b.WriteString(w)
		}
	}
	b.WriteString(".")
	return b.String()
}

func isNumber(w string) bool {
	for i := 0; i < len(w); i++ {
		if w[i] < '0' || w[i] > '9' {
			return false
		}
	}
	return w != ""
}

// spellNumber writes a number the way a narrator says it, hyphenated the
// way a transcript writes it down.
func spellNumber(digits string) string {
	n := 0
	for i := 0; i < len(digits); i++ {
		n = n*10 + int(digits[i]-'0')
	}
	ones := []string{"zero", "one", "two", "three", "four", "five", "six",
		"seven", "eight", "nine", "ten", "eleven", "twelve", "thirteen",
		"fourteen", "fifteen", "sixteen", "seventeen", "eighteen", "nineteen"}
	tens := []string{"", "", "twenty", "thirty", "forty", "fifty", "sixty",
		"seventy", "eighty", "ninety"}
	switch {
	case n >= 100:
		rest := ""
		if n%100 > 0 {
			rest = " " + spellNumber(fmt.Sprintf("%d", n%100))
		}
		return ones[n/100] + " hundred" + rest
	case n >= 20:
		if n%10 > 0 {
			return tens[n/10] + "-" + ones[n%10]
		}
		return tens[n/10]
	default:
		return ones[n]
	}
}

type ranges struct{ spans [][2]int }

func (r ranges) contains(i int) bool {
	for _, s := range r.spans {
		if i >= s[0] && i < s[1] {
			return true
		}
	}
	return false
}

// skipRanges spaces the skipped runs evenly through the part that is read.
func skipRanges(total, read, runs, words int) ranges {
	if runs <= 0 || words <= 0 || read <= 0 {
		return ranges{}
	}
	var out ranges
	step := read / (runs + 1)
	for i := 1; i <= runs; i++ {
		start := i * step
		if start+words >= read {
			break
		}
		out.spans = append(out.spans, [2]int{start, start + words})
	}
	return out
}

func clampFraction(v float64) float64 {
	if v <= 0 || v > 1 {
		return 1
	}
	return v
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
