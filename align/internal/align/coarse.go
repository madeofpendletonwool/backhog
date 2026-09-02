package align

import "sort"

// Pass 1: coarse location.
//
// The transcript is cut into windows of about a minute. Each window votes,
// through the shingle index, for the canonical positions it might have come
// from, and a dynamic program picks one position per window subject to the
// only constraint an audiobook actually guarantees: it is read front to
// back. The chosen positions must therefore be non-decreasing, and a
// candidate that would require the narrator to go backwards is rejected no
// matter how many votes it collected.

// window is one stretch of transcript treated as a unit by the coarse
// pass: a half-open range of segments and the match tokens they cover.
type window struct {
	firstSeg, lastSeg int
	firstTok, lastTok int
	start, end        float64
}

// buildWindows cuts the transcript on segment boundaries, closing a window
// once it has covered windowSeconds of audio. Segments are never split:
// an anchor is emitted per segment, so a segment straddling two windows
// would be aligned twice and have to be reconciled.
func buildWindows(segments []Segment, t *transcript, windowSeconds float64) []window {
	var out []window
	i := 0
	for i < len(segments) {
		w := window{
			firstSeg: i,
			firstTok: t.bounds[i],
			start:    segments[i].AudioStart,
			end:      segments[i].AudioEnd,
		}
		for i < len(segments) && segments[i].AudioEnd-w.start < windowSeconds {
			w.end = max(w.end, segments[i].AudioEnd)
			i++
		}
		if i == w.firstSeg {
			// One segment longer than the whole window. Take it alone
			// rather than looping forever on it.
			w.end = max(w.end, segments[i].AudioEnd)
			i++
		}
		w.lastSeg = i
		w.lastTok = t.bounds[i]
		out = append(out, w)
	}
	return out
}

// candidate is one canonical position a window might start at, with the
// number of shingle hits that voted for it.
type candidate struct {
	offset int
	votes  float64
}

// candidates finds the best few canonical positions for one window. Every
// shingle hit implies a start position (the canonical position of the hit
// minus the window-relative position of the shingle); those implied starts
// are clustered, because a real match scatters them across a few dozen
// tokens as the two texts insert and delete words relative to each other.
func (idx *shingleIndex) candidates(toks []matchTok, from, to int, opts Options) []candidate {
	if idx.n <= 0 || to-from < idx.n {
		return nil
	}
	votes := make(map[int]int32)
	for i := from; i+idx.n <= to; i++ {
		for _, p := range idx.postings[shingleHash(toks[i:i+idx.n])] {
			votes[int(p)-(i-from)]++
		}
	}
	if len(votes) == 0 {
		return nil
	}

	// Cluster the implied starts into buckets, keeping the single
	// best-supported exact offset in each: the fine pass searches a band
	// around whatever it is given, so a representative within a few tokens
	// is all this has to be.
	type cluster struct {
		best      int
		bestVotes int32
		total     float64
	}
	clusters := make(map[int]*cluster, len(votes))
	for off, v := range votes {
		if off < 0 {
			continue
		}
		key := off / opts.ClusterWidth
		c := clusters[key]
		if c == nil {
			c = &cluster{best: off, bestVotes: v}
			clusters[key] = c
		} else if v > c.bestVotes || (v == c.bestVotes && off < c.best) {
			c.best, c.bestVotes = off, v
		}
		c.total += float64(v)
	}

	out := make([]candidate, 0, len(clusters))
	for _, c := range clusters {
		if c.total >= opts.MinVotes {
			out = append(out, candidate{offset: c.best, votes: c.total})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].votes != out[j].votes {
			return out[i].votes > out[j].votes
		}
		return out[i].offset < out[j].offset
	})
	if len(out) > opts.MaxCandidates {
		out = out[:opts.MaxCandidates]
	}
	return out
}

// monotonicPath picks at most one candidate per window so that the chosen
// canonical offsets never decrease, maximizing total votes less a drift
// penalty. Returns one offset per window, or -1 for a window that no
// candidate could be reconciled with.
//
// The drift penalty is what stops a lucky burst of votes in the wrong
// chapter from winning. Between two chosen windows the canonical text
// should advance by about as many tokens as the narration did; the penalty
// grows with the relative disagreement and saturates, because a large
// honest jump — a skipped illustration, a chapter of front matter — must
// stay possible.
func monotonicPath(windows []window, cands [][]candidate, opts Options) []int {
	type node struct {
		w, c  int
		score float64
		prev  int
	}
	nodes := make([]node, 0, len(windows)*opts.MaxCandidates)
	for w := range cands {
		for c := range cands[w] {
			nodes = append(nodes, node{w: w, c: c, score: cands[w][c].votes, prev: -1})
		}
	}
	if len(nodes) == 0 {
		return filled(len(windows), -1)
	}

	best, bestScore := -1, 0.0
	for i := range nodes {
		n := &nodes[i]
		off := cands[n.w][n.c].offset
		for j := 0; j < i; j++ {
			p := &nodes[j]
			if p.w >= n.w {
				continue
			}
			prevOff := cands[p.w][p.c].offset
			if prevOff > off {
				continue
			}
			score := p.score - driftPenalty(windows[p.w], windows[n.w], prevOff, off, opts)
			if score+cands[n.w][n.c].votes > n.score {
				n.score = score + cands[n.w][n.c].votes
				n.prev = j
			}
		}
		if n.score > bestScore {
			best, bestScore = i, n.score
		}
	}

	offsets := filled(len(windows), -1)
	for i := best; i >= 0; i = nodes[i].prev {
		offsets[nodes[i].w] = cands[nodes[i].w][nodes[i].c].offset
	}
	return offsets
}

// driftPenalty scores how much a step disagrees with the narration rate.
func driftPenalty(from, to window, fromOff, toOff int, opts Options) float64 {
	spoken := to.firstTok - from.firstTok
	if spoken <= 0 {
		return 0
	}
	advanced := toOff - fromOff
	dev := float64(abs(advanced-spoken)) / float64(max(spoken, 64))
	return opts.DriftWeight * min(dev, 4)
}

// bridgeGaps gives an offset to windows the vote left unplaced, by
// interpolating between the placed windows on either side. A window can
// come up empty for dull reasons — a passage of dialogue whose every
// shingle is too common to index, a stretch Whisper heard badly — and
// leaving it unplaced would throw away a minute of perfectly alignable
// audio. Only interior gaps are bridged: an unplaced run at the head or
// the tail is the audiobook's own intro and outro, which exist in no EPUB
// and must not be dragged onto one.
func bridgeGaps(windows []window, offsets []int) {
	first, last := -1, -1
	for i, off := range offsets {
		if off < 0 {
			continue
		}
		if first < 0 {
			first = i
		}
		last = i
	}
	if first < 0 || first == last {
		return
	}
	for i := first; i < last; i++ {
		if offsets[i] < 0 {
			continue
		}
		next := i + 1
		for offsets[next] < 0 {
			next++
		}
		if next == i+1 {
			continue
		}
		spoken := windows[next].firstTok - windows[i].firstTok
		advanced := offsets[next] - offsets[i]
		for j := i + 1; j < next; j++ {
			frac := 0.0
			if spoken > 0 {
				frac = float64(windows[j].firstTok-windows[i].firstTok) / float64(spoken)
			}
			offsets[j] = offsets[i] + int(frac*float64(advanced))
		}
	}
}

func filled(n, v int) []int {
	out := make([]int, n)
	for i := range out {
		out[i] = v
	}
	return out
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
