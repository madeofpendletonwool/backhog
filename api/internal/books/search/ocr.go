package search

import (
	"context"
	"sort"
	"sync"

	"github.com/collinpendleton/backhog/api/booktext"
)

// The second corpus: OCR lettering search for image-native books.
//
// A comic or picture book has no canonical text — that is the paged position
// model's whole premise — but the optional OCR worker reads its drawn
// lettering into per-page rows, and this is the searcher over those rows. It
// is deliberately NOT the canonical searcher with a page join bolted on:
//
//   - a hit's address is a PAGE INDEX, the position axis a paged book
//     actually has, never a char offset — nothing here ever produces or
//     consumes one;
//   - each page is searched as its own text, because that is what the
//     lettering is: independent balloons and captions, not a scroll;
//   - the same two tiers (phrase verbatim, loose forgiving one word) with
//     the same folded space, so typing works exactly as it does in prose
//     and one client renderer serves both.
//
// The corpus is search-only by construction: it lives in ocr_pages, no
// epub_texts row can point at it, and the canonical pipeline (alignment,
// passage matching, the knowledge layer) never loads it — the load-site
// tests pin that, not a convention.

// PageText is one page of a lettering corpus: its 1-based page number and
// the worker's raw reading. The searcher folds the text itself.
type PageText struct {
	Number int
	Text   string
}

// PagedHit is one match, addressed the only way a paged book can be: the
// 0-based page index (the position axis), plus the span inside that page's
// normalized text for snippet highlighting. Raw carries the page's raw
// reading so the caller can build the snippet without a second corpus load.
type PagedHit struct {
	Page  int
	Start int
	End   int
	// Raw is the page's lettering as the worker read it.
	Raw string
	// Score orders the loose pass, meaningless for phrase hits.
	Score float64
}

// PagedResult is one search over a lettering corpus.
type PagedResult struct {
	Mode  Mode
	Total int
	Hits  []PagedHit
}

// PagedSearcher searches lettering corpora. Safe for concurrent use; load
// returns a file's pages, and revision — supplied by the caller on every
// Search, from whatever it cheaply probes (a count-plus-timestamp digest)
// — is part of the cache key, so a re-OCR'd file can never be searched
// through yesterday's index.
type PagedSearcher struct {
	load  func(ctx context.Context, mediaFileID int64) ([]PageText, error)
	mu    sync.Mutex
	cache *pagedLRU
}

// NewPaged builds a searcher over a lettering-corpus loader.
func NewPaged(load func(ctx context.Context, mediaFileID int64) ([]PageText, error)) *PagedSearcher {
	return &PagedSearcher{load: load, cache: newPagedLRU(cacheCapacity)}
}

// Search finds query inside a file's lettering, page by page, returning at
// most limit hits in page order (phrase) or score order (loose).
//
// revision identifies the exact corpus behind mediaFileID — whatever the
// caller cheaply probes (a count-plus-timestamp digest is enough). It gates
// the cache because a re-OCR keeps the same file id, and an index built over
// yesterday's lettering would hand back pages into a book that changed.
func (s *PagedSearcher) Search(ctx context.Context, mediaFileID int64, revision, query string, limit int) (PagedResult, error) {
	complete := query != trimRightSpaces(query)

	q := booktext.Normalize(query)
	if len(q) < minQueryBytes {
		return PagedResult{}, ErrTooShort
	}
	if limit <= 0 {
		limit = 20
	}

	pages, err := s.pages(ctx, mediaFileID, revision)
	if err != nil {
		return PagedResult{}, err
	}

	// Whole-word first, the prose searcher's rule exactly: only when no
	// page contains the phrase as typed does a half-typed last word get to
	// match as a prefix.
	if res := pages.phrase(q, true, limit); res.Total > 0 {
		return res, nil
	}
	if !complete {
		if res := pages.phrase(q, false, limit); res.Total > 0 {
			return res, nil
		}
	}
	return pages.loose(q, limit), nil
}

// pages returns a file's built page indexes, constructing them if the LRU
// does not hold this revision of them. The lock is held across the build so
// two searches racing on the same cold comic do the work once.
func (s *PagedSearcher) pages(ctx context.Context, mediaFileID int64, revision string) (*pageCorpus, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if pages, ok := s.cache.get(mediaFileID, revision); ok {
		return pages, nil
	}
	texts, err := s.load(ctx, mediaFileID)
	if err != nil {
		return nil, err
	}
	pages := buildPageCorpus(texts)
	s.cache.put(mediaFileID, revision, pages)
	return pages, nil
}

// pageCorpus is one file's per-page normalized texts and token indexes.
type pageCorpus struct {
	pages []*pageIndex
}

// pageIndex is one page's folded text and the postings the loose pass needs.
type pageIndex struct {
	// Page is the 0-based page index — the position axis.
	Page int
	raw  string
	idx  *index
}

// buildPageCorpus folds and indexes every page. Empty results (a page the
// worker read nothing off) still occupy their slot so the page axis stays
// dense, they just own no searchable bytes.
func buildPageCorpus(texts []PageText) *pageCorpus {
	out := &pageCorpus{pages: make([]*pageIndex, 0, len(texts))}
	for _, p := range texts {
		if p.Number < 1 {
			continue
		}
		norm := booktext.Normalize(p.Text)
		out.pages = append(out.pages, &pageIndex{
			Page: p.Number - 1,
			raw:  p.Text,
			idx:  buildIndex(norm),
		})
	}
	return out
}

// phrase finds every occurrence of the folded query at a token boundary on
// any page, hits in page order.
func (c *pageCorpus) phrase(q string, wholeWord bool, limit int) PagedResult {
	res := PagedResult{Mode: ModePhrase}
	for _, p := range c.pages {
		page := p.idx.phrase(q, wholeWord, limit)
		res.Total += page.Total
		if len(res.Hits) >= limit {
			continue
		}
		for _, h := range page.Hits {
			if len(res.Hits) >= limit {
				break
			}
			res.Hits = append(res.Hits, PagedHit{Page: p.Page, Start: h.CharOffset, End: h.CharEnd, Raw: p.raw})
		}
	}
	return res
}

// loose finds passages holding the query's words in any order, forgiving one
// of them — per page, because lettering does not run across a page turn.
func (c *pageCorpus) loose(q string, limit int) PagedResult {
	res := PagedResult{Mode: ModeLoose}
	for _, p := range c.pages {
		page := p.idx.loose(q, limit)
		for _, h := range page.Hits {
			res.Hits = append(res.Hits, PagedHit{
				Page:  p.Page,
				Start: h.CharOffset,
				End:   h.CharEnd,
				Raw:   p.raw,
				Score: h.Score,
			})
		}
	}
	if len(res.Hits) == 0 {
		return res
	}
	sort.SliceStable(res.Hits, func(a, b int) bool {
		if res.Hits[a].Score != res.Hits[b].Score {
			return res.Hits[a].Score > res.Hits[b].Score
		}
		return res.Hits[a].Page < res.Hits[b].Page
	})
	res.Total = len(res.Hits)
	if len(res.Hits) > limit {
		res.Hits = res.Hits[:limit]
	}
	return res
}

// trimRightSpaces is strings.TrimRight(query, " \t\n\r") without making the
// hot path import strings for one call the caller already makes.
func trimRightSpaces(s string) string {
	for len(s) > 0 {
		switch s[len(s)-1] {
		case ' ', '\t', '\n', '\r':
			s = s[:len(s)-1]
		default:
			return s
		}
	}
	return s
}

// pagedLRU is the searcher LRU with a second key component: the revision
// the loader reported. A get only hits when the revision still matches, so
// a re-OCR'd corpus is a miss and rebuilds even though the file id is
// unchanged.
type pagedLRU struct {
	cap     int
	entries map[int64]*pagedEntry
	order   []*pagedEntry
}

type pagedEntry struct {
	fileID   int64
	revision string
	pages    *pageCorpus
}

func newPagedLRU(capacity int) *pagedLRU {
	return &pagedLRU{cap: capacity, entries: make(map[int64]*pagedEntry, capacity)}
}

func (l *pagedLRU) get(fileID int64, revision string) (*pageCorpus, bool) {
	if e, ok := l.entries[fileID]; ok && e.revision == revision {
		l.touch(e)
		return e.pages, true
	}
	return nil, false
}

func (l *pagedLRU) put(fileID int64, revision string, pages *pageCorpus) {
	if e, ok := l.entries[fileID]; ok {
		e.revision = revision
		e.pages = pages
		l.touch(e)
		return
	}
	if l.cap <= 0 {
		return
	}
	if len(l.order) >= l.cap {
		oldest := l.order[0]
		l.order = l.order[1:]
		delete(l.entries, oldest.fileID)
	}
	e := &pagedEntry{fileID: fileID, revision: revision, pages: pages}
	l.entries[fileID] = e
	l.order = append(l.order, e)
}

func (l *pagedLRU) touch(e *pagedEntry) {
	for i, cand := range l.order {
		if cand == e {
			l.order = append(append(l.order[:i], l.order[i+1:]...), e)
			return
		}
	}
}
