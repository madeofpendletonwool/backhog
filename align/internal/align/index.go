package align

// The coarse pass needs to answer "roughly where in this book does this
// minute of narration come from" without comparing the minute against all
// half-million characters of it. A word-shingle index does that: every run
// of shingleSize consecutive words in the canonical text is hashed to the
// positions it occurs at, so a window of transcript can look its own
// shingles up and read the answer off the hits.

// shingleIndex maps a shingle hash to the canonical match-token positions
// it starts at.
type shingleIndex struct {
	n        int
	postings map[uint64][]int32
}

// newShingleIndex indexes every n-gram of toks, then throws away the ones
// that occur more than maxPostings times. A shingle that appears fifty
// times is a chapter heading, a refrain or a stock phrase: it votes for
// fifty places at once, which is noise in every one of them, and keeping
// it would cost the coarse pass far more time than it is worth.
func newShingleIndex(toks []matchTok, n, maxPostings int) *shingleIndex {
	idx := &shingleIndex{n: n, postings: make(map[uint64][]int32, max(len(toks)-n+1, 1))}
	if n <= 0 || len(toks) < n {
		return idx
	}
	for i := 0; i+n <= len(toks); i++ {
		h := shingleHash(toks[i : i+n])
		idx.postings[h] = append(idx.postings[h], int32(i))
	}
	for h, p := range idx.postings {
		if len(p) > maxPostings {
			delete(idx.postings, h)
		}
	}
	return idx
}

// shingleHash is FNV-1a over the shingle's words with a separator, so
// ("a", "bc") and ("ab", "c") do not collide.
func shingleHash(toks []matchTok) uint64 {
	const (
		offset64 = 14695981039346656037
		prime64  = 1099511628211
	)
	h := uint64(offset64)
	for i, t := range toks {
		if i > 0 {
			h = (h ^ ' ') * prime64
		}
		for j := 0; j < len(t.Word); j++ {
			h = (h ^ uint64(t.Word[j])) * prime64
		}
	}
	return h
}
