// Package passage finds where in a book's canonical text a chunk of text
// read off a paper page sits: the reusable half of the physical-copy
// bridge. Given OCR output (or a reader typing a line back in), it
// returns the canonical character offset the passage starts at, so a
// printed page can be pinned into the same address space the reader and
// player already use.
//
// The query is normalized with the pinned booktext.Normalize — the same
// function that produced the canonical text — so both sides of the
// comparison live in the same folded space before any matching happens.
//
// Matching runs in two stages. A word-shingle index over the canonical
// text (four consecutive tokens per shingle) votes on where the query
// starts: a shingle that survives OCR noise anywhere in the query pins
// the whole window, so losing a few words to garbage only loses a few
// votes, never the location. The top candidates are then confirmed with
// an edit-distance ratio over the full query, which both filters hash
// collisions and produces the confidence the caller stores on the anchor.
//
// A novel is well under a megabyte of canonical text, so the index is
// built on demand in milliseconds and kept in a small LRU keyed by text
// id, rather than persisted anywhere. A passage that recurs genuinely is
// ambiguous, and Find says so by returning alternatives rather than
// guessing: the UI asks which occurrence the reader is holding.
package passage

import (
	"container/list"
	"context"
	"errors"
	"sort"
	"strings"
	"sync"

	"github.com/collinpendleton/backhog/api/booktext"
)

const (
	// shingleSize is how many consecutive tokens form one shingle. Four
	// words is long enough that a shingle is unique inside a novel and
	// short enough that OCR noise only kills the shingles it touches.
	shingleSize = 4

	// minQueryTokens is the floor below which a query is refused rather
	// than guessed at. Short phrases recur constantly in prose ("he said
	// nothing" can sit in a thousand places); a match on one is a coin
	// toss wearing a confidence score.
	minQueryTokens = 10

	// maxQueryTokens caps the query used for voting and verification at
	// roughly a dense page. The request body cap bounds the input anyway;
	// this keeps the edit-distance confirmation bounded with it.
	maxQueryTokens = 400

	// minAcceptRatio is the edit-distance ratio a verified candidate must
	// reach to count as a match. Well below what 10–15% OCR garbage
	// produces, well above what two unrelated passages of prose score.
	minAcceptRatio = 0.5

	// alternativeGap is how close a runner-up's ratio may sit to the
	// winner's before the match is reported as ambiguous. An exact repeat
	// scores identically in both places; a near-gap usually means one
	// good match and one poor one, which is not ambiguity.
	alternativeGap = 0.05

	// maxVerified is how many distinct candidate locations are confirmed
	// with edit distance. Votes concentrate hard on the true location, so
	// five is generous.
	maxVerified = 5

	// cacheCapacity is how many books' indexes the LRU holds. Each index
	// costs a few times the text's size; eight books covers a scanning
	// session without approaching the memory of the texts themselves.
	cacheCapacity = 8
)

var (
	// ErrTooShort reports a query below minQueryTokens: refused, not
	// guessed.
	ErrTooShort = errors.New("passage: too few words to match reliably")

	// ErrNoMatch reports a query that no place in the text resembles
	// closely enough.
	ErrNoMatch = errors.New("passage: no place in this text matches")
)

// Match is one placed passage: where it starts in the canonical text
// (byte offsets, the arena's address space), where its last word ends,
// and how confidently it was placed.
type Match struct {
	CharOffset int
	CharEnd    int
	Confidence float64
}

// Result is Find's answer: the best match, plus the alternatives that
// scored close enough that the passage may genuinely recur — a chapter
// epigraph, a refrain. Alternatives is empty when the answer is
// unambiguous.
type Result struct {
	Match        Match
	Alternatives []Match
}

// Matcher places passages in canonical texts. It is safe for concurrent
// use; the loader is whatever the server uses to read a canonical text
// by its id (the ingester's companion file).
type Matcher struct {
	load  func(ctx context.Context, textID string) (string, error)
	mu    sync.Mutex
	cache *lru
}

// New builds a matcher over a canonical-text loader.
func New(load func(ctx context.Context, textID string) (string, error)) *Matcher {
	return &Matcher{load: load, cache: newLRU(cacheCapacity)}
}

// Find locates the query inside the named canonical text. The query is
// raw passage text — OCR output, a typed line, whatever the paper page
// yielded — and is normalized here with the same pinned rules as the
// canonical text itself.
func (m *Matcher) Find(ctx context.Context, textID, query string) (Result, error) {
	qTokens := strings.Fields(booktext.Normalize(query))
	if len(qTokens) < minQueryTokens {
		return Result{}, ErrTooShort
	}
	if len(qTokens) > maxQueryTokens {
		qTokens = qTokens[:maxQueryTokens]
	}

	idx, err := m.index(ctx, textID)
	if err != nil {
		return Result{}, err
	}

	// Stage one: shingle votes on the window's start token. A query
	// shingle matching at text position p implies the window began
	// p-i tokens earlier, so every surviving shingle — wherever inside
	// the query it sits — votes for the same start on a clean hit, and
	// noise only abstains.
	votes := make(map[int]int, 64)
	for i := 0; i+shingleSize <= len(qTokens); i++ {
		h := hashShingle(qTokens[i : i+shingleSize])
		for _, pos := range idx.shingles[h] {
			start := int(pos) - i
			if start >= 0 {
				votes[start]++
			}
		}
	}
	if len(votes) == 0 {
		return Result{}, ErrNoMatch
	}

	starts := make([]int, 0, len(votes))
	for s := range votes {
		starts = append(starts, s)
	}
	// Best-voted first, and nearest-the-front first among equals, so the
	// result is deterministic for text a passage repeats in.
	sort.Slice(starts, func(a, b int) bool {
		if votes[starts[a]] != votes[starts[b]] {
			return votes[starts[a]] > votes[starts[b]]
		}
		return starts[a] < starts[b]
	})

	// Candidates a few tokens apart are the same location seen through
	// noise, not alternatives; keep one per cluster.
	candidates := make([]int, 0, maxVerified)
	for _, s := range starts {
		if len(candidates) >= maxVerified {
			break
		}
		near := false
		for _, c := range candidates {
			d := s - c
			if d < 0 {
				d = -d
			}
			if d <= shingleSize {
				near = true
				break
			}
		}
		if !near {
			candidates = append(candidates, s)
		}
	}

	// Stage two: confirm with an edit-distance ratio over the full query.
	qNorm := strings.Join(qTokens, " ")
	type scored struct {
		start, votes int
		ratio        float64
	}
	var verified []scored
	for _, s := range candidates {
		end := s + len(qTokens)
		if end > len(idx.tokens) {
			end = len(idx.tokens)
		}
		if end <= s {
			continue
		}
		window := strings.Join(idx.tokens[s:end], " ")
		r := similarity(qNorm, window)
		if r >= minAcceptRatio {
			verified = append(verified, scored{s, votes[s], r})
		}
	}
	if len(verified) == 0 {
		return Result{}, ErrNoMatch
	}
	sort.Slice(verified, func(a, b int) bool {
		x, y := verified[a], verified[b]
		if x.ratio != y.ratio {
			return x.ratio > y.ratio
		}
		if x.votes != y.votes {
			return x.votes > y.votes
		}
		return x.start < y.start
	})

	out := Result{Match: idx.matchAt(verified[0].start, len(qTokens), verified[0].ratio)}
	for _, v := range verified[1:] {
		if verified[0].ratio-v.ratio <= alternativeGap {
			out.Alternatives = append(out.Alternatives, idx.matchAt(v.start, len(qTokens), v.ratio))
		}
	}
	return out, nil
}

// index is one book's shingle index. Offsets are byte offsets into the
// canonical text, which is what every stored anchor in the arena means
// by "where".
type index struct {
	tokens   []string
	offsets  []int
	shingles map[uint64][]int32
}

// matchAt converts a verified start token into a Match over canonical
// bytes. CharOffset is the first token's first byte; CharEnd is the last
// token's last byte.
func (ix *index) matchAt(start, n int, confidence float64) Match {
	end := start + n
	if end > len(ix.tokens) {
		end = len(ix.tokens)
	}
	if start >= len(ix.tokens) || end <= start {
		return Match{Confidence: confidence}
	}
	last := end - 1
	return Match{
		CharOffset: ix.offsets[start],
		CharEnd:    ix.offsets[last] + len(ix.tokens[last]),
		Confidence: confidence,
	}
}

// index loads (and on first use builds) a text's shingle index. The
// expensive half happens outside the cache lock: two concurrent first
// lookups build the same deterministic index twice and the last put
// wins, which is cheaper than serializing every scan behind one mutex.
func (m *Matcher) index(ctx context.Context, textID string) (*index, error) {
	m.mu.Lock()
	idx := m.cache.get(textID)
	m.mu.Unlock()
	if idx != nil {
		return idx, nil
	}

	text, err := m.load(ctx, textID)
	if err != nil {
		return nil, err
	}
	idx = buildIndex(text)

	m.mu.Lock()
	m.cache.put(textID, idx)
	m.mu.Unlock()
	return idx, nil
}

// buildIndex tokenizes the canonical text and hashes every shingle. The
// canonical text is already Normalize output, so a plain whitespace scan
// yields exactly its tokens; each field is normalized again anyway —
// idempotent on canonical input, and a guard against being handed text
// that never went through the pinned rules. A field that folds to
// nothing (it was pure punctuation) contributes no token, and a field
// that folds to several (a dash inside a word) contributes each of them,
// anchored at the field's start: exact on canonical input, near enough
// to still match on anything else.
func buildIndex(text string) *index {
	ix := &index{shingles: make(map[uint64][]int32, 1024)}
	i := 0
	for i < len(text) {
		for i < len(text) && isSpaceByte(text[i]) {
			i++
		}
		if i >= len(text) {
			break
		}
		start := i
		for i < len(text) && !isSpaceByte(text[i]) {
			i++
		}
		for _, tok := range strings.Fields(booktext.Normalize(text[start:i])) {
			ix.tokens = append(ix.tokens, tok)
			ix.offsets = append(ix.offsets, start)
		}
	}

	for pos := 0; pos+shingleSize <= len(ix.tokens); pos++ {
		h := hashShingle(ix.tokens[pos : pos+shingleSize])
		ix.shingles[h] = append(ix.shingles[h], int32(pos))
	}
	return ix
}

// isSpaceByte matches the ASCII whitespace the canonical text is joined
// with, plus the other controls a never-normalized haystack might carry.
func isSpaceByte(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == '\r' || b == '\v' || b == '\f'
}

// FNV-1a over the shingle's tokens, one space separator between them —
// no allocations, and collisions are both vanishingly rare at a novel's
// scale and caught by the edit-distance confirmation anyway.
const (
	fnvOffset64 = 14695981039346656037
	fnvPrime64  = 1099511628211
)

func hashShingle(tokens []string) uint64 {
	h := uint64(fnvOffset64)
	for _, t := range tokens {
		for k := 0; k < len(t); k++ {
			h ^= uint64(t[k])
			h *= fnvPrime64
		}
		h ^= ' '
		h *= fnvPrime64
	}
	return h
}

// similarity is 1 minus the normalized edit distance between two strings
// over runes — the canonical text is not ASCII-only, and neither is OCR
// output. 1.0 is identical; 0.0 shares nothing.
func similarity(a, b string) float64 {
	ra, rb := []rune(a), []rune(b)
	if len(ra) == 0 && len(rb) == 0 {
		return 1
	}
	d := levenshtein(ra, rb)
	longest := len(ra)
	if len(rb) > longest {
		longest = len(rb)
	}
	if longest == 0 {
		return 1
	}
	return 1 - float64(d)/float64(longest)
}

// levenshtein is the classic two-row DP. Inputs are bounded by
// maxQueryTokens' worth of characters on one side and a window of the
// same length on the other, so this stays in the tens of microseconds.
func levenshtein(a, b []rune) int {
	if len(a) == 0 {
		return len(b)
	}
	if len(b) == 0 {
		return len(a)
	}
	prev := make([]int, len(b)+1)
	cur := make([]int, len(b)+1)
	for j := 0; j <= len(b); j++ {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = cur[j-1] + 1
			if prev[j]+1 < cur[j] {
				cur[j] = prev[j] + 1
			}
			if prev[j-1]+cost < cur[j] {
				cur[j] = prev[j-1] + cost
			}
		}
		prev, cur = cur, prev
	}
	return prev[len(b)]
}

// lru is a tiny fixed-capacity map with least-recently-used eviction,
// safe for concurrent use. It exists here rather than as a dependency
// because the need is exactly one map of eight entries.
type lru struct {
	cap     int
	entries map[string]*list.Element
	order   *list.List
}

type lruEntry struct {
	key string
	idx *index
}

func newLRU(capacity int) *lru {
	return &lru{
		cap:     capacity,
		entries: make(map[string]*list.Element, capacity),
		order:   list.New(),
	}
}

func (l *lru) get(key string) *index {
	if el, ok := l.entries[key]; ok {
		l.order.MoveToFront(el)
		return el.Value.(*lruEntry).idx
	}
	return nil
}

func (l *lru) put(key string, idx *index) {
	if el, ok := l.entries[key]; ok {
		l.order.MoveToFront(el)
		el.Value.(*lruEntry).idx = idx
		return
	}
	if l.cap > 0 && l.order.Len() >= l.cap {
		if oldest := l.order.Back(); oldest != nil {
			l.order.Remove(oldest)
			delete(l.entries, oldest.Value.(*lruEntry).key)
		}
	}
	if l.cap > 0 {
		l.entries[key] = l.order.PushFront(&lruEntry{key, idx})
	}
}
