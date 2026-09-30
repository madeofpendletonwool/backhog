package http

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/collinpendleton/backhog/api/internal/auth"
	"github.com/collinpendleton/backhog/api/internal/books"
	"github.com/collinpendleton/backhog/api/internal/books/search"
	"github.com/collinpendleton/backhog/api/internal/models"
	"github.com/collinpendleton/backhog/api/internal/store"
)

// Search inside one book. The passage endpoint next door answers "where is
// this page I am holding"; this answers "where is that line I remember", which
// is the same machinery pointed at a much shorter query.
//
// What makes it worth having is not the finding — it is that a hit is a
// canonical character offset, the one position this arena stores. So every
// result comes back already translated: the chapter it sits in, the page of the
// reader's own printing (with its error bar), and the second of the audiobook.
// One search, three coordinates, and a jump into either the reader or the
// player from any row.

const (
	// searchDefaultLimit is how many hits are rendered when the client does
	// not say. Twenty is a screenful of snippets; the total says how many
	// there really were.
	searchDefaultLimit = 20

	// searchMaxLimit caps what a client may ask for. Each rendered hit costs
	// a paragraph of display text and two interpolations.
	searchMaxLimit = 50

	// searchViewsTTL is how long a book's derivation inputs (chapters, audio
	// timeline, anchor maps) are reused across searches.
	//
	// Loading them means reading up to a few thousand alignment anchors, and
	// the database runs on a single connection: paying that on every keystroke
	// would put the whole app in line behind a search box. Anchors change only
	// when an alignment publishes or a page is scanned, both rare, and half a
	// minute of staleness in a *search result's* page estimate costs nothing —
	// the stored position endpoints are not cached and stay exact.
	searchViewsTTL = 30 * time.Second
)

// searchHit is one match, in every space the arena can express it.
type searchHit struct {
	CharOffset int     `json:"char_offset"`
	CharEnd    int     `json:"char_end"`
	Percent    float64 `json:"percent"`
	// Context is the match as the book prints it, split for highlighting —
	// the same shape the passage endpoint returns, so one client renderer
	// serves both. Under a clamp the context stops at the bound: nothing
	// the reader has not reached rides along in the skirt of a paragraph.
	Context books.Snippet `json:"context"`
	Chapter *chapterView  `json:"chapter"`
	// Audio and Page are null when the book has no alignment or no page map.
	// A search result never invents a coordinate it cannot derive.
	Audio *audioView `json:"audio"`
	Page  *pageView  `json:"page"`
	// BookID and DeepLink are the hit's provenance: which book it came from
	// and the peek jump that lands a reader on it without moving their
	// saved position — the citation an answer grounded in this hit carries.
	BookID   string `json:"book_id"`
	DeepLink string `json:"deep_link"`
}

// pageSearchHit is one match in a paged book's lettering: addressed by the
// page index — the axis the book actually has — with the OCR reading the
// match landed in. No offsets anywhere: a comic has no text axis to address.
type pageSearchHit struct {
	PageIndex int     `json:"page_index"`
	Percent   float64 `json:"percent"`
	// Context is the lettering as the OCR read it, split for highlighting.
	Context books.Snippet `json:"context"`
}

// pageSearchResponse is the paged twin of searchResponse: the same two
// tiers and honesty fields, a different axis, plus the corpus grade the
// results stand on — stylized lettering is best-effort and the answer says
// so rather than passing for text search.
type pageSearchResponse struct {
	Query string `json:"query"`
	// Axis is always "page" here: hits address page indexes, and the
	// client must not mistake them for offsets.
	Axis string     `json:"axis"`
	Mode search.Mode `json:"mode"`
	Total int        `json:"total"`
	// Truncated is set when Total exceeds the hits returned.
	Truncated bool            `json:"truncated"`
	Results   []pageSearchHit `json:"results"`
	// Bound is the clamp this answer was served under, on the page axis:
	// char_offset carries the page index it stopped at.
	Bound boundView `json:"bound"`
	// Corpus grades the lettering corpus every hit came from: its state,
	// coverage and mean confidence. Null never happens on this path — no
	// usable corpus is a 422, not an empty answer pretending to be one.
	Corpus *models.OCRCorpus `json:"corpus"`
}

// searchResponse is one query's answer.
type searchResponse struct {
	Query string `json:"query"`
	// Axis names the position space the results address: "text" (char
	// offsets) or "page" (page indexes of a paged book). The two answers
	// never share a shape, and the field says which one arrived.
	Axis string `json:"axis"`
	// Mode is "phrase" when the book contains what was typed and "loose"
	// when these are the closest passages instead. The client says which,
	// rather than letting a fallback pass for an exact answer.
	Mode  search.Mode `json:"mode"`
	Total int         `json:"total"`
	// Truncated is set when Total exceeds the hits returned.
	Truncated bool        `json:"truncated"`
	Results   []searchHit `json:"results"`
	// Bound is the clamp this answer was served under — the "as of" a
	// client cites its results by.
	Bound boundView `json:"bound"`
	// Alignment grades the audio map every timestamp below came from, or is
	// null when the book has none.
	Alignment *alignmentView `json:"alignment"`
}

// handleSearchInBook finds a phrase in a book: GET /api/books/{entryID}/search?q=…&limit=20&until=.
//
// A text-mode book is searched in its canonical text; a paged book
// (image-native PDF primary) is searched in its OCR lettering corpus. The
// dispatch is the classification, never the extension — and the two answers
// never share a shape: text hits carry offsets, page hits carry pages.
//
// A clamped request drops every hit past its bound before the answer is
// built, so neither the hit nor its surrounding context can be text the
// reader has not reached. Total counts what survived the clamp — "matches
// in what you've read", never a census of the whole book.
func (s *Server) handleSearchInBook(w http.ResponseWriter, r *http.Request) {
	userID, entryID, bookID, ok := s.bookEntry(w, r)
	if !ok {
		return
	}
	if s.epubs == nil || s.search == nil {
		fail(w, errorf(http.StatusServiceUnavailable, "canonical text storage unavailable"))
		return
	}

	limit := searchDefaultLimit
	if v := r.URL.Query().Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > searchMaxLimit {
			fail(w, errorf(http.StatusBadRequest,
				"limit must be between 1 and "+strconv.Itoa(searchMaxLimit)))
			return
		}
		limit = n
	}
	query := r.URL.Query().Get("q")

	// The paged dispatch: an image-native PDF primary answers from the
	// lettering corpus or is refused honestly when no worker has read it.
	if f, ok, err := s.store.PrimaryPagedMediaFile(r.Context(), userID, entryID); err == nil && ok {
		s.searchPagedBook(w, r, f, query, limit)
		return
	} else if err != nil && !errors.Is(err, store.ErrNotFound) {
		fail(w, err)
		return
	}

	et, ok := s.ensureBookText(w, r)
	if !ok {
		return
	}

	bound := readBound{until: untilNone, offset: et.CharCount, view: boundView{
		Until: untilNone, CharOffset: et.CharCount, Percent: 100,
	}}
	censoring := false
	if requested, valid := needsBound(r); !valid {
		fail(w, errorf(http.StatusBadRequest,
			"until must be \"position\", \"none\" or a non-negative character offset"))
		return
	} else if requested {
		if bound, ok = s.resolveReadBound(w, r, userID, entryID, bookID, et.CharCount); !ok {
			return
		}
		// A bound at the very end withholds nothing; only a bound short of
		// the end changes what the answer may contain or count.
		censoring = bound.offset < et.CharCount
	}

	// Under a clamp the fetch window widens to the maximum: hits past the
	// bound are about to be dropped, and a window that stopped at `limit`
	// could drop in-bound hits it never fetched. Phrase hits come back in
	// book order, so everything readable sits ahead of everything dropped
	// and the surviving count is exact; loose hits come back ranked, where
	// the wider window is the honest best effort and Truncated says so.
	fetchLimit := limit
	if censoring {
		fetchLimit = searchMaxLimit
	}

	res, err := s.search.Search(r.Context(), et.ID, et.NormalizedSHA256, query, fetchLimit)
	switch {
	case errors.Is(err, search.ErrTooShort):
		fail(w, errorf(http.StatusUnprocessableEntity,
			"type a few more characters to search this book"))
		return
	case err != nil:
		slog.ErrorContext(r.Context(), "book search", "entry", entryID, "error", err)
		fail(w, errorf(http.StatusInternalServerError, "could not search this book"))
		return
	}

	dropped := 0
	if censoring {
		kept := res.Hits[:0]
		for _, hit := range res.Hits {
			if hit.CharOffset >= bound.offset {
				dropped++
				continue
			}
			kept = append(kept, hit)
		}
		res.Hits = kept
	}

	out := searchResponse{
		Query:   query,
		Axis:    "text",
		Mode:    res.Mode,
		Results: []searchHit{},
		Bound:   bound.view,
	}
	if censoring {
		// The clamp redefines the census: Total counts matches inside what
		// the caller may read, never the whole book's.
		out.Total = len(res.Hits)
		out.Truncated = len(res.Hits) > limit
		// A loose-pass window that both dropped hits and filled up may be
		// hiding readable ones behind the dropped; phrase order makes that
		// impossible, so only loose says maybe.
		if res.Mode == search.ModeLoose && dropped > 0 && len(res.Hits)+dropped >= fetchLimit {
			out.Truncated = true
		}
	} else {
		out.Total = res.Total
		out.Truncated = res.Total > len(res.Hits)
	}
	if len(res.Hits) > limit {
		res.Hits = res.Hits[:limit]
	}
	if len(res.Hits) == 0 {
		writeJSON(w, http.StatusOK, out)
		return
	}

	views, err := s.searchViewsFor(r.Context(), userID, entryID, bookID)
	if err != nil {
		fail(w, err)
		return
	}
	snippets, err := s.epubs.Snippets(r.Context(), et)
	if err != nil {
		slog.ErrorContext(r.Context(), "book search snippets", "entry", entryID, "error", err)
		fail(w, errorf(http.StatusInternalServerError, "could not read this ebook's text"))
		return
	}
	out.Alignment = s.alignmentSummary(r.Context(), entryID)

	for _, hit := range res.Hits {
		var context books.Snippet
		var ok bool
		if censoring {
			context, ok = snippets.AtUntil(hit.CharOffset, hit.CharEnd, bound.offset)
		} else {
			context, ok = snippets.At(hit.CharOffset, hit.CharEnd)
		}
		if !ok {
			// The sidecar and the display file disagree about this offset,
			// which means an older parser wrote one of them. Showing the
			// wrong paragraph under a right offset is worse than showing
			// one hit fewer.
			continue
		}
		out.Results = append(out.Results, searchHit{
			CharOffset: hit.CharOffset,
			CharEnd:    hit.CharEnd,
			Percent:    percentAtOffset(hit.CharOffset, views),
			Context:    context,
			Chapter:    chapterAt(views.chapters, hit.CharOffset),
			Audio:      audioViewFor(hit.CharOffset, views),
			Page:       pageViewFor(hit.CharOffset, views),
			BookID:     entryID,
			DeepLink:   deepLink(entryID, hit.CharOffset),
		})
	}
	writeJSON(w, http.StatusOK, out)
}

// searchPagedBook answers a search over a paged book's OCR lettering. The
// corpus gate is a refusal, not an empty list: a comic the worker has not
// read is not a comic with no matches, and the difference is the whole
// honesty of the feature. A clamped request drops pages past the caller's
// page position — the paged twin of the text bound, because lettering a
// reader has not turned to is as much a spoiler as prose they have not read.
func (s *Server) searchPagedBook(w http.ResponseWriter, r *http.Request, f models.MediaFile, query string, limit int) {
	corpus, err := s.store.OCRCorpusForMediaFile(r.Context(), f.ID)
	if err != nil {
		fail(w, err)
		return
	}
	if corpus.State == "" {
		fail(w, errorf(http.StatusUnprocessableEntity,
			"this book's lettering has not been read yet — run the OCR pass (it needs the optional OCR worker) to search inside it"))
		return
	}

	bound := readBound{until: untilNone, offset: corpus.PageCount, view: boundView{
		Until: untilNone, CharOffset: corpus.PageCount, Percent: 100,
	}}
	censoring := false
	if requested, valid := needsBound(r); !valid {
		fail(w, errorf(http.StatusBadRequest,
			"until must be \"position\", \"none\" or a non-negative page index"))
		return
	} else if requested {
		userID, err := auth.MustUserID(r.Context())
		if err != nil {
			fail(w, errUnauthorized)
			return
		}
		var ok bool
		if bound, ok = s.resolvePageBound(w, r, userID, chi.URLParam(r, "entryID"), corpus.PageCount); !ok {
			return
		}
		censoring = bound.offset < corpus.PageCount-1
	}

	revision, err := s.store.OCRCorpusRevision(r.Context(), f.ID)
	if err != nil {
		fail(w, err)
		return
	}
	res, err := s.pagedSearch.Search(r.Context(), f.ID, revision, query, limit)
	if errors.Is(err, search.ErrTooShort) {
		fail(w, errorf(http.StatusUnprocessableEntity,
			"type a few more characters to search this book"))
		return
	}
	if err != nil {
		slog.ErrorContext(r.Context(), "book lettering search", "file", f.ID, "error", err)
		fail(w, errorf(http.StatusInternalServerError, "could not search this book"))
		return
	}

	dropped := 0
	if censoring {
		kept := res.Hits[:0]
		for _, hit := range res.Hits {
			if hit.Page > bound.offset {
				dropped++
				continue
			}
			kept = append(kept, hit)
		}
		res.Hits = kept
	}

	out := pageSearchResponse{
		Query:   query,
		Axis:    "page",
		Mode:    res.Mode,
		Results: []pageSearchHit{},
		Corpus:  &corpus,
		Bound:   bound.view,
	}
	if censoring {
		out.Total = len(res.Hits)
		out.Truncated = len(res.Hits) > limit || (dropped > 0 && res.Mode == search.ModeLoose)
	} else {
		out.Total = res.Total
		out.Truncated = res.Total > len(res.Hits)
	}
	if len(res.Hits) > limit {
		res.Hits = res.Hits[:limit]
	}
	for _, hit := range res.Hits {
		context, ok := books.OCRSnippet(hit.Raw, hit.Start, hit.End)
		if !ok {
			// The index and the raw reading disagree, which the revision
			// gate makes all but impossible; one hit fewer beats a
			// mis-highlighted one.
			continue
		}
		out.Results = append(out.Results, pageSearchHit{
			PageIndex: hit.Page,
			Percent:   pagePercent(hit.Page, corpus.PageCount),
			Context:   context,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

// pagePercent is the paged convention: page one is 0%, the last page 100% —
// the page you are on over the reading span, like the char axis's offset.
func pagePercent(page, pageCount int) float64 {
	if pageCount > 1 {
		return float64(page) / float64(pageCount-1) * 100
	}
	return 0
}

// searchViewsFor is loadBookViews behind the TTL cache. Every other caller of
// loadBookViews reads or writes a real position and must not see stale anchors;
// only search is willing to trade half a minute of freshness for keeping the
// single database connection free while somebody types.
func (s *Server) searchViewsFor(ctx context.Context, userID, entryID, bookID string) (bookViews, error) {
	key := userID + "\x00" + entryID
	if v, ok := s.searchViews.get(key); ok {
		return v, nil
	}
	// Reached through bookEntry, so file access is already established.
	v, err := s.loadBookViews(ctx, userID, entryID, bookID, true)
	if err != nil {
		return v, err
	}
	s.searchViews.put(key, v)
	return v, nil
}

// viewsCache is a small expiring map of derivation inputs.
//
// It is bounded by expiry rather than by size: entries are per (user, entry)
// and a person searches one book at a time, so the set of live keys is tiny and
// a stale one costs a few kilobytes for thirty seconds. Sweeping on write keeps
// it from growing across a long session.
type viewsCache struct {
	ttl     time.Duration
	mu      sync.Mutex
	entries map[string]cachedViews
}

type cachedViews struct {
	views   bookViews
	expires time.Time
}

func newViewsCache(ttl time.Duration) *viewsCache {
	return &viewsCache{ttl: ttl, entries: make(map[string]cachedViews)}
}

func (c *viewsCache) get(key string) (bookViews, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[key]
	if !ok || time.Now().After(e.expires) {
		return bookViews{}, false
	}
	return e.views, true
}

func (c *viewsCache) put(key string, v bookViews) {
	now := time.Now()
	c.mu.Lock()
	defer c.mu.Unlock()
	for k, e := range c.entries {
		if now.After(e.expires) {
			delete(c.entries, k)
		}
	}
	c.entries[key] = cachedViews{views: v, expires: now.Add(c.ttl)}
}
