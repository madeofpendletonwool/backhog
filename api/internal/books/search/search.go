// Package search finds a remembered phrase inside a book's canonical text.
//
// It is the cheapest feature in the arena and the one that shows off what the
// arena is. A hit is a canonical character offset, and a canonical character
// offset is the only position Backhog stores — so the position translator turns
// one search result into the page of the reader's own printing and the second of
// the audiobook, without this package knowing either exists.
//
// The corpus is the canonical text, not the Whisper transcript. Both describe
// the same book, but the canonical text is the author's words rather than a
// machine's best guess at a narrator reading them, and a transcript can only
// exist for a book that already has one (alignment needs an EPUB). Searching the
// better text is strictly the better trade.
//
// # Why substring search is enough
//
// The canonical text has already had case, punctuation, quotes, dashes and
// apostrophes folded out of it by the pinned booktext.Normalize. Folding the
// query through the same function puts both sides in that space, so
//
//	“Don’t,” he said—finally.
//
// and what a reader actually types,
//
//	dont he said finally
//
// are the same string, and finding one inside the other is strings.Index over
// well under a megabyte. Every hard part of "search that ignores punctuation"
// was already paid for by the alignment pipeline.
//
// # Two tiers
//
// Phrase is the primary pass and the one that answers the question people
// actually have ("what was that line?"). Loose is the fallback for a query
// whose words are right and whose wording is not: it finds a window of the text
// holding all but one of the query's terms, in any order. The mode is reported
// so a UI can say which one it is looking at rather than quietly degrading.
//
// # Why the index is in memory and not in SQLite
//
// The same reason the canonical text itself is a file: the database runs on a
// single connection, and this is a keystroke path. An FTS5 table would put every
// character typed into a search box in line behind whatever else wants to write.
// A novel's token index builds in a few milliseconds and is kept in a small LRU,
// the way passage.Matcher keeps its shingle indexes.
package search

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
	// minQueryBytes is the floor below which a query is refused. Two folded
	// characters match somewhere on nearly every page of a novel, and a
	// result set that large is noise wearing a list's clothing.
	minQueryBytes = 3

	// maxQueryTerms caps the loose pass's term count. Somebody pasting a
	// paragraph into the box wants the phrase pass anyway.
	maxQueryTerms = 24

	// looseWindowFloor is the smallest window, in tokens, that the loose pass
	// will look for all of a query's terms inside. Wider than a short query
	// needs, so remembered-but-reordered wording still lands.
	looseWindowFloor = 12

	// looseWindowPerTerm widens that window with the query, because a longer
	// remembered sentence is spread over more of the page.
	looseWindowPerTerm = 3

	// maxDriverPostings bounds the loose pass's work on a query whose rarest
	// term is still common. Well past the point where the answer is useful.
	maxDriverPostings = 4000

	// cacheCapacity is how many books' token indexes are held. Each costs a
	// few times its text's size and the texts are the largest thing in the
	// arena, so this is deliberately smaller than the passage matcher's.
	cacheCapacity = 4
)

// ErrTooShort reports a query below minQueryBytes once folded: refused rather
// than answered with most of the book.
var ErrTooShort = errors.New("search: query too short")

// Mode says which pass produced a result, so a caller can tell the reader
// whether it found their words or something close to them.
type Mode string

const (
	// ModePhrase means the query was found verbatim (in folded space).
	ModePhrase Mode = "phrase"
	// ModeLoose means no verbatim hit existed and these are passages holding
	// the query's words, possibly reordered and possibly missing one.
	ModeLoose Mode = "loose"
)

// Hit is one place in the canonical text, in the arena's byte offsets.
type Hit struct {
	CharOffset int
	CharEnd    int
	// Score orders the loose pass. It is meaningless for phrase hits, which
	// are all equally exact and come back in book order.
	Score float64
}

// Result is one search. Total counts every hit found, which may exceed the
// hits returned — a reader searching "the" should be told it appears four
// thousand times, not handed four thousand rows.
type Result struct {
	Mode  Mode
	Hits  []Hit
	Total int
}

// Searcher searches canonical texts. It is safe for concurrent use; load is
// whatever the server uses to read a canonical text by its id.
type Searcher struct {
	load  func(ctx context.Context, textID string) (string, error)
	mu    sync.Mutex
	cache *lru
}

// New builds a searcher over a canonical-text loader.
func New(load func(ctx context.Context, textID string) (string, error)) *Searcher {
	return &Searcher{load: load, cache: newLRU(cacheCapacity)}
}

// Search finds query inside the named canonical text, returning at most limit
// hits.
//
// revision identifies the exact text behind textID — the normalized SHA is what
// the callers have. It is part of the cache key because a re-parse can reuse an
// epub_texts id, and an index built over the old text would hand back offsets
// into a book that no longer exists.
func (s *Searcher) Search(ctx context.Context, textID, revision, query string, limit int) (Result, error) {
	// Whether the reader has finished typing the last word decides whether it
	// has to match one, so this is read before the fold eats the space.
	complete := query != strings.TrimRight(query, " \t\n\r")

	q := booktext.Normalize(query)
	if len(q) < minQueryBytes {
		return Result{}, ErrTooShort
	}
	if limit <= 0 {
		limit = 20
	}

	idx, err := s.index(ctx, textID, revision)
	if err != nil {
		return Result{}, err
	}

	// Whole-word first. Only if the book does not contain the phrase as typed
	// does the last word get to be a prefix, so "the door" is not buried under
	// every "theatre" in the book — but "the door clos" still finds it.
	if res := idx.phrase(q, true, limit); res.Total > 0 {
		return res, nil
	}
	if !complete {
		if res := idx.phrase(q, false, limit); res.Total > 0 {
			return res, nil
		}
	}
	return idx.loose(q, limit), nil
}

// index returns the token index for a text, building it if the LRU does not
// hold it. The lock is held across the build so two searches racing on the same
// cold book do the work once.
func (s *Searcher) index(ctx context.Context, textID, revision string) (*index, error) {
	key := textID + "@" + revision

	s.mu.Lock()
	defer s.mu.Unlock()
	if idx := s.cache.get(key); idx != nil {
		return idx, nil
	}
	text, err := s.load(ctx, textID)
	if err != nil {
		return nil, err
	}
	idx := buildIndex(text)
	s.cache.put(key, idx)
	return idx, nil
}

// index is one book's canonical text plus the token postings the loose pass
// needs. The phrase pass needs only the text.
type index struct {
	text string
	// starts and ends bound each token in text, in book order.
	starts, ends []int32
	// postings maps a token to the token positions it occupies, ascending.
	postings map[string][]int32
}

// buildIndex tokenizes a canonical text. The canonical form contains only
// letters, digits and single spaces, so splitting on the space is the whole
// tokenizer — no normalization, because the text is already the output of it.
func buildIndex(text string) *index {
	ix := &index{text: text, postings: make(map[string][]int32)}
	for at := 0; at < len(text); {
		if text[at] == ' ' {
			at++
			continue
		}
		start := at
		for at < len(text) && text[at] != ' ' {
			at++
		}
		pos := int32(len(ix.starts))
		ix.starts = append(ix.starts, int32(start))
		ix.ends = append(ix.ends, int32(at))
		tok := text[start:at]
		ix.postings[tok] = append(ix.postings[tok], pos)
	}
	return ix
}

// phrase finds every occurrence of the folded query at a token boundary.
//
// wholeWord decides whether the query's last token must end at one too. Relaxing
// it is what makes the box feel live: a half-typed final word still finds the
// sentence, without "the" quietly matching inside "there" whenever the reader
// meant the word.
func (ix *index) phrase(q string, wholeWord bool, limit int) Result {
	res := Result{Mode: ModePhrase}
	for at := 0; at+len(q) <= len(ix.text); {
		i := strings.Index(ix.text[at:], q)
		if i < 0 {
			break
		}
		p := at + i
		end := p + len(q)
		startOK := p == 0 || ix.text[p-1] == ' '
		endOK := !wholeWord || end == len(ix.text) || ix.text[end] == ' '
		if startOK && endOK {
			res.Total++
			if len(res.Hits) < limit {
				res.Hits = append(res.Hits, Hit{CharOffset: p, CharEnd: end})
			}
		}
		at = p + 1
	}
	return res
}

// loose finds passages holding the query's words in any order, forgiving one of
// them. It is the "I know roughly what it said" pass, and it is deliberately
// only reached when the book does not contain what was typed.
func (ix *index) loose(q string, limit int) Result {
	res := Result{Mode: ModeLoose}

	terms := distinctTerms(q)
	if len(terms) < 2 {
		return res
	}

	// A term the book does not contain is the misremembered word this pass
	// exists for; it drops out of the search and out of what can be required.
	present := make([]string, 0, len(terms))
	for _, t := range terms {
		if len(ix.postings[t]) > 0 {
			present = append(present, t)
		}
	}

	// Forgive one word, never more, and never the second of two — "the door"
	// with one word missing is a search for the other word, which is not what
	// anybody typed.
	need := len(terms) - 1
	if need < 2 {
		need = 2
	}
	if len(present) < need {
		return res
	}

	// Rarest first, so the scan is driven from the terms with the least to
	// walk. The driver set is the (present - need + 1) rarest: a window that
	// holds `need` of the present terms misses at most present-need of them,
	// so by pigeonhole it must contain one of these, and scanning them finds
	// every window without scanning a stopword's postings.
	sort.SliceStable(present, func(a, b int) bool {
		return len(ix.postings[present[a]]) < len(ix.postings[present[b]])
	})
	drivers := present[:len(present)-need+1]

	width := looseWindowFloor
	if w := looseWindowPerTerm * len(terms); w > width {
		width = w
	}

	var spans []span
	scanned := 0
	for _, driver := range drivers {
		for _, at := range ix.postings[driver] {
			if scanned >= maxDriverPostings {
				break
			}
			scanned++

			lo, hi := int(at)-width, int(at)+width
			found, first, last := 0, int(at), int(at)
			for _, t := range present {
				pos, ok := nearestInRange(ix.postings[t], lo, hi, int(at))
				if !ok {
					continue
				}
				found++
				if pos < first {
					first = pos
				}
				if pos > last {
					last = pos
				}
			}
			if found >= need {
				spans = append(spans, span{first: first, last: last, terms: found})
			}
		}
	}
	if len(spans) == 0 {
		return res
	}

	// Adjacent driver positions inside one passage describe the same place
	// seen twice; merge them so a repeated word is not three results.
	sort.Slice(spans, func(a, b int) bool {
		if spans[a].first != spans[b].first {
			return spans[a].first < spans[b].first
		}
		return spans[a].last < spans[b].last
	})
	merged := spans[:1]
	for _, s := range spans[1:] {
		prev := &merged[len(merged)-1]
		if s.first <= prev.last {
			if s.last > prev.last {
				prev.last = s.last
			}
			if s.terms > prev.terms {
				prev.terms = s.terms
			}
			continue
		}
		merged = append(merged, s)
	}

	// Most of the query's words, packed into the least text: the passage that
	// says the thing rather than the chapter that happens to contain the words.
	sort.SliceStable(merged, func(a, b int) bool {
		if merged[a].terms != merged[b].terms {
			return merged[a].terms > merged[b].terms
		}
		wa, wb := merged[a].last-merged[a].first, merged[b].last-merged[b].first
		if wa != wb {
			return wa < wb
		}
		return merged[a].first < merged[b].first
	})

	res.Total = len(merged)
	if len(merged) > limit {
		merged = merged[:limit]
	}
	for _, s := range merged {
		res.Hits = append(res.Hits, Hit{
			CharOffset: int(ix.starts[s.first]),
			CharEnd:    int(ix.ends[s.last]),
			Score:      float64(s.terms)*1000 - float64(s.last-s.first),
		})
	}
	return res
}

// span is one candidate passage, in token positions.
type span struct {
	first, last int
	terms       int
}

// distinctTerms is the query's tokens, deduplicated, first occurrence order
// kept so the scan is deterministic.
func distinctTerms(q string) []string {
	fields := strings.Fields(q)
	if len(fields) > maxQueryTerms {
		fields = fields[:maxQueryTerms]
	}
	seen := make(map[string]bool, len(fields))
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		if !seen[f] {
			seen[f] = true
			out = append(out, f)
		}
	}
	return out
}

// nearestInRange returns the posting inside [lo,hi] closest to target, which
// keeps a term that recurs in the window from stretching the span across the
// whole of it.
func nearestInRange(postings []int32, lo, hi, target int) (int, bool) {
	i := sort.Search(len(postings), func(i int) bool { return int(postings[i]) >= lo })
	best, found := 0, false
	for ; i < len(postings) && int(postings[i]) <= hi; i++ {
		p := int(postings[i])
		if !found || abs(p-target) < abs(best-target) {
			best, found = p, true
		}
	}
	return best, found
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// lru is a tiny fixed-capacity map with least-recently-used eviction. It is a
// copy of the passage matcher's for the same reason that one exists: the need
// is one map of four entries, and it is guarded by the Searcher's own mutex.
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
	if l.cap <= 0 {
		return
	}
	if l.order.Len() >= l.cap {
		if oldest := l.order.Back(); oldest != nil {
			l.order.Remove(oldest)
			delete(l.entries, oldest.Value.(*lruEntry).key)
		}
	}
	l.entries[key] = l.order.PushFront(&lruEntry{key, idx})
}
