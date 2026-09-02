// Package align turns a timestamped transcript of an audiobook into
// alignment anchors: pairs of (byte offset into the book's canonical text,
// second on the audiobook's global timeline). It is the algorithmic heart
// of the Books arena — the thing that lets a reader stop listening in the
// car and pick the paperback up on the right sentence.
//
// # Two passes
//
// A single global alignment of half a million characters of book against a
// hundred thousand words of transcript is both slow and fragile, so the
// work is split. The coarse pass (coarse.go) locates each minute of
// narration to within a few dozen words using a word-shingle index, under
// the one constraint an audiobook really does obey: it is read front to
// back, so the located positions must never go backwards. The fine pass
// (fine.go) then runs a banded word-level alignment inside each located
// region and reads one anchor per transcript segment off the result.
//
// # Both sides speak the same language
//
// Every word on both sides goes through booktext.Normalize — the API's own
// pinned normalizer, imported rather than copied, because two normalizers
// that drifted apart would produce anchors that are silently and
// unrepairably wrong — and then through a matching fold that reconciles the
// two systematic disagreements between print and speech: numerals
// (numbers.go) and abbreviations (tokens.go).
//
// # What is expected to fail
//
// An audiobook opens with a publisher's intro and closes with an outro that
// appear in no EPUB, so unmatched head and tail regions are normal and are
// deliberately not bridged. Illustrations, footnotes and the occasional
// skipped line leave small interior gaps, which interpolation between
// anchors covers. What is not normal is an abridged reading or the wrong
// edition entirely, and the two numbers Result reports — coverage and mean
// confidence — are how that is caught: below the caller's thresholds the
// job is finished as low_confidence, keeping the anchors but labelling
// them, rather than passing a partial map off as a whole one.
package align

// Segment is one stretch of transcription in GLOBAL book seconds — the
// same timeline the player and the stored positions use.
type Segment struct {
	AudioStart float64
	AudioEnd   float64
	Text       string
}

// Anchor ties one byte offset in the canonical text to one moment on that
// global timeline, with the aligner's own belief in the pair.
type Anchor struct {
	CharOffset   int
	AudioSeconds float64
	Confidence   float64
}

// Stats are the aligner's diagnostics: enough to tell a bad alignment's
// story in a log line or a bench table without re-running it.
type Stats struct {
	CanonicalTokens  int
	TranscriptTokens int
	Segments         int
	AnchoredSegments int
	Windows          int
	LocatedWindows   int
	FirstOffset      int
	LastOffset       int
}

// Result is a finished alignment: the anchors, and the two numbers that
// say whether to trust them.
type Result struct {
	Anchors        []Anchor
	Coverage       float64
	MeanConfidence float64
	Stats          Stats
}

// Options tunes the aligner. DefaultOptions documents where each value
// came from; the zero Options is not usable.
type Options struct {
	// WindowSeconds is how much narration the coarse pass locates at a
	// time. A minute is long enough to contain shingles unique in the
	// book and short enough that the narration rate cannot drift much
	// inside it.
	WindowSeconds float64
	// ShingleSize is the n of the n-gram index. Four-word shingles repeat
	// across a novel; six miss any window with a transcription error every
	// five words. Five is the middle of that.
	ShingleSize int
	// MaxPostings drops shingles that occur more often than this, which
	// are refrains and headings rather than locations.
	MaxPostings int
	// ClusterWidth is how far apart two implied start positions can be and
	// still be the same candidate, in match tokens.
	ClusterWidth int
	// MaxCandidates and MinVotes bound how many positions a window may
	// propose and how much support each needs.
	MaxCandidates int
	MinVotes      float64
	// DriftWeight scales the penalty for a step whose canonical advance
	// disagrees with how much was spoken. See driftPenalty.
	DriftWeight float64
	// BandHalfWidth is the fine pass's band: how far the alignment may
	// wander from the diagonal the coarse pass predicted.
	BandHalfWidth int
	// RegionPad is how much canonical text either side of a located
	// window is handed to the fine pass. It must exceed BandHalfWidth so
	// the band fits inside the region.
	RegionPad int
	// ConfidencePrior blends a segment's own match ratio toward its
	// window's. Without it a four-word segment scores 0, 0.25, 0.5, 0.75
	// or 1 and nothing between, which is noise, not confidence.
	ConfidencePrior float64
	// MinAnchorConfidence is the floor for emitting an anchor at all.
	// Below it, interpolating between the neighbours is more accurate than
	// the anchor would be.
	MinAnchorConfidence float64
	// ConfidentAnchor is the floor for an anchor to count toward coverage.
	ConfidentAnchor float64
	// MaxCoverageGapChars is the largest span between two confident
	// anchors that still counts as covered. Beyond it the text between
	// them was not aligned, it was merely bracketed.
	MaxCoverageGapChars int
	// Progress, when set, is called with a fraction in [0,1] and a short
	// stage description. It is called at most a few hundred times.
	Progress func(fraction float64, stage string)
}

// DefaultOptions are the shipped settings. The thresholds the worker
// applies to Coverage and MeanConfidence are its own, not the aligner's —
// see the worker package.
func DefaultOptions() Options {
	return Options{
		WindowSeconds:       60,
		ShingleSize:         5,
		MaxPostings:         24,
		ClusterWidth:        32,
		MaxCandidates:       6,
		MinVotes:            3,
		DriftWeight:         2,
		BandHalfWidth:       96,
		RegionPad:           160,
		ConfidencePrior:     2,
		MinAnchorConfidence: 0.35,
		ConfidentAnchor:     0.5,
		MaxCoverageGapChars: 8000,
	}
}

// Align maps the transcript onto the canonical text. canonical must be the
// bytes of the canonical text exactly as stored — it is not re-normalized,
// because those bytes are what every offset in the arena is measured in.
func Align(canonical string, segments []Segment, opts Options) Result {
	report := opts.Progress
	if report == nil {
		report = func(float64, string) {}
	}

	tokens := tokenizeCanonical(canonical)
	trans := newTranscript(segments)
	res := Result{Stats: Stats{
		CanonicalTokens:  len(tokens),
		TranscriptTokens: len(trans.toks),
		Segments:         len(segments),
	}}
	if len(tokens) == 0 || len(trans.toks) == 0 {
		return res
	}

	canon := foldCanonical(tokens)
	idx := newShingleIndex(canon, opts.ShingleSize, opts.MaxPostings)
	windows := buildWindows(segments, trans, opts.WindowSeconds)
	res.Stats.Windows = len(windows)

	report(0, "locating narration in the book")
	cands := make([][]candidate, len(windows))
	for i, w := range windows {
		cands[i] = idx.candidates(trans.toks, w.firstTok, w.lastTok, opts)
		if i%64 == 0 {
			report(0.5*float64(i)/float64(len(windows)), "locating narration in the book")
		}
	}

	offsets := monotonicPath(windows, cands, opts)
	bridgeGaps(windows, offsets)
	for _, off := range offsets {
		if off >= 0 {
			res.Stats.LocatedWindows++
		}
	}

	report(0.5, "aligning words")
	anchors := make([]Anchor, 0, len(segments))
	for i, w := range windows {
		if i%64 == 0 {
			report(0.5+0.5*float64(i)/float64(len(windows)), "aligning words")
		}
		if offsets[i] < 0 {
			continue
		}
		anchors = alignWindow(anchors, canonical, tokens, canon, trans, segments, w, offsets[i], opts)
	}
	report(1, "alignment complete")

	res.Anchors = monotone(anchors, opts)
	res.Stats.AnchoredSegments = len(res.Anchors)
	if len(res.Anchors) > 0 {
		res.Stats.FirstOffset = res.Anchors[0].CharOffset
		res.Stats.LastOffset = res.Anchors[len(res.Anchors)-1].CharOffset
	}
	res.Coverage = coverage(res.Anchors, len(canonical), opts)
	res.MeanConfidence = meanConfidence(res.Anchors)
	return res
}

// alignWindow runs the fine pass over one located window and appends one
// anchor per transcript segment it could place.
func alignWindow(dst []Anchor, canonical string, tokens []canonToken, canon []matchTok,
	trans *transcript, segments []Segment, w window, offset int, opts Options) []Anchor {

	q := trans.toks[w.firstTok:w.lastTok]
	if len(q) == 0 {
		return dst
	}
	lo := max(0, offset-opts.RegionPad)
	hi := min(len(canon), offset+len(q)+opts.RegionPad)
	if hi-lo < 1 {
		return dst
	}
	region := canon[lo:hi]
	aligned := fitAlign(q, region, offset-lo, opts.BandHalfWidth)

	// The window's own match ratio is the prior every segment inside it is
	// blended toward, so a three-word segment is not asked to carry a
	// confidence estimate on its own.
	windowMatched := 0
	for i, j := range aligned {
		if j >= 0 && q[i].Word == region[j].Word {
			windowMatched++
		}
	}
	windowRatio := float64(windowMatched) / float64(len(q))

	for seg := w.firstSeg; seg < w.lastSeg; seg++ {
		a := trans.bounds[seg] - w.firstTok
		b := trans.bounds[seg+1] - w.firstTok
		if b <= a {
			continue
		}
		matched, first := 0, -1
		for i := a; i < b; i++ {
			j := aligned[i]
			if j < 0 {
				continue
			}
			if first < 0 {
				first = i
			}
			if q[i].Word == region[j].Word {
				matched++
			}
		}
		if first < 0 {
			continue
		}
		// The anchor belongs at the segment's first spoken word, so walk
		// back over however many of its words went unaligned.
		c := clampInt(lo+aligned[first]-(first-a), 0, len(canon)-1)
		conf := (float64(matched) + opts.ConfidencePrior*windowRatio) /
			(float64(b-a) + opts.ConfidencePrior)
		dst = append(dst, Anchor{
			CharOffset:   tokens[canon[c].Src].Start,
			AudioSeconds: segments[seg].AudioStart,
			Confidence:   clamp01(conf),
		})
	}
	return dst
}

// monotone drops the anchors that cannot be true given the ones around
// them: anything under the confidence floor, and anything that would move
// the reader backwards through the book while the narration moved forward.
// The database's primary key would silently swallow a repeated offset; a
// backwards one it would happily store, and the position translator would
// interpolate straight through it.
func monotone(anchors []Anchor, opts Options) []Anchor {
	out := make([]Anchor, 0, len(anchors))
	lastOffset, lastSeconds := -1, -1.0
	for _, a := range anchors {
		if a.Confidence < opts.MinAnchorConfidence {
			continue
		}
		if a.CharOffset <= lastOffset || a.AudioSeconds < lastSeconds {
			continue
		}
		out = append(out, a)
		lastOffset, lastSeconds = a.CharOffset, a.AudioSeconds
	}
	return out
}

// coverage is the fraction of the canonical text that confident anchors
// actually span. Text before the first anchor and after the last is not
// covered — which is what makes an abridgement visible — and neither is a
// hole in the middle wider than a plausible skipped illustration.
func coverage(anchors []Anchor, chars int, opts Options) float64 {
	if chars <= 0 {
		return 0
	}
	covered, prev := 0, -1
	for _, a := range anchors {
		if a.Confidence < opts.ConfidentAnchor {
			continue
		}
		if prev >= 0 {
			if gap := a.CharOffset - prev; gap <= opts.MaxCoverageGapChars {
				covered += gap
			}
		}
		prev = a.CharOffset
	}
	return clamp01(float64(covered) / float64(chars))
}

func meanConfidence(anchors []Anchor) float64 {
	if len(anchors) == 0 {
		return 0
	}
	sum := 0.0
	for _, a := range anchors {
		sum += a.Confidence
	}
	return clamp01(sum / float64(len(anchors)))
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}
