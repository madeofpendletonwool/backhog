package http

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/collinpendleton/backhog/api/internal/books"
	"github.com/collinpendleton/backhog/api/internal/books/search"
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
	// serves both.
	Context books.Snippet `json:"context"`
	Chapter *chapterView  `json:"chapter"`
	// Audio and Page are null when the book has no alignment or no page map.
	// A search result never invents a coordinate it cannot derive.
	Audio *audioView `json:"audio"`
	Page  *pageView  `json:"page"`
}

// searchResponse is one query's answer.
type searchResponse struct {
	Query string `json:"query"`
	// Mode is "phrase" when the book contains what was typed and "loose"
	// when these are the closest passages instead. The client says which,
	// rather than letting a fallback pass for an exact answer.
	Mode  search.Mode `json:"mode"`
	Total int         `json:"total"`
	// Truncated is set when Total exceeds the hits returned.
	Truncated bool        `json:"truncated"`
	Results   []searchHit `json:"results"`
	// Alignment grades the audio map every timestamp below came from, or is
	// null when the book has none.
	Alignment *alignmentView `json:"alignment"`
}

// handleSearchInBook finds a phrase in a book's canonical text:
// GET /api/books/{entryID}/search?q=…&limit=20.
func (s *Server) handleSearchInBook(w http.ResponseWriter, r *http.Request) {
	userID, entryID, bookID, ok := s.bookEntry(w, r)
	if !ok {
		return
	}
	if s.epubs == nil || s.search == nil {
		fail(w, errorf(http.StatusServiceUnavailable, "canonical text storage unavailable"))
		return
	}
	et, ok := s.ensureBookText(w, r)
	if !ok {
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
	res, err := s.search.Search(r.Context(), et.ID, et.NormalizedSHA256, query, limit)
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

	out := searchResponse{
		Query:     query,
		Mode:      res.Mode,
		Total:     res.Total,
		Truncated: res.Total > len(res.Hits),
		Results:   []searchHit{},
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
		context, ok := snippets.At(hit.CharOffset, hit.CharEnd)
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
		})
	}
	writeJSON(w, http.StatusOK, out)
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
	v, err := s.loadBookViews(ctx, userID, entryID, bookID)
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
