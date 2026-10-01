package http

import (
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/collinpendleton/backhog/api/booktext"
	"github.com/collinpendleton/backhog/api/internal/books"
)

// The name index (MAD-670): the classic back-of-the-book index, served
// under the same spoiler clamp every read path speaks. Two questions:
// "which names have I met?" (/names) and "where has this one come up?"
// (/mentions) — both answered with canonical offsets, chapters and peek
// links, both clamped so that a name first appearing past the reading
// position simply does not exist yet.
//
// The default clamp is the position for *every* caller — cookie sessions
// too, the library search's rule: nothing here moves a position, and the
// whole point of an index you consult mid-book is "names seen so far".
// `until=none` is the loud opt-in that lifts it, as everywhere else.

// nameFirstSeen cites a name's first mention: where it is, which chapter,
// and the peek link that lands a reader on it.
type nameFirstSeen struct {
	CharStart int          `json:"char_start"`
	Chapter   *chapterView `json:"chapter"`
	Percent   float64      `json:"percent"`
	DeepLink  string       `json:"deep_link"`
}

// nameIndexEntry is one name in the index: how many times it has come up
// in what the caller may read, where it first did, and whether this
// reader has hidden it as a false positive.
type nameIndexEntry struct {
	Name      string        `json:"name"`
	Mentions  int           `json:"mentions"`
	FirstSeen nameFirstSeen `json:"first_seen"`
	Hidden    bool          `json:"hidden"`
}

type namesResponse struct {
	// Names is alphabetical, the way a book's own index prints.
	Names []nameIndexEntry `json:"names"`
	Bound boundView        `json:"bound"`
}

// handleBookNames serves the index: GET /api/books/{entryID}/names?until=.
//
// Clamping is the feature: occurrences at or past the bound are dropped
// before the list is built, and a name whose every occurrence was dropped
// is absent entirely — not "mentioned later", not counted, absent, the
// same way the chapter index locks the future away.
func (s *Server) handleBookNames(w http.ResponseWriter, r *http.Request) {
	userID, entryID, bookID, ok := s.bookEntry(w, r)
	if !ok {
		return
	}
	if s.epubs == nil {
		fail(w, errorf(http.StatusServiceUnavailable, "canonical text storage unavailable"))
		return
	}
	et, ok := s.ensureBookText(w, r)
	if !ok {
		return
	}

	// The default clamp is the position for every caller; an explicit
	// `until` of any of the three shapes wins, and a malformed one is a
	// 400, never a guess.
	bound, ok := s.resolveReadBoundDefault(w, r, userID, entryID, bookID, et.CharCount, untilPosition)
	if !ok {
		return
	}

	occ, err := s.store.ListNameOccurrences(r.Context(), et.MediaFileID)
	if err != nil {
		fail(w, err)
		return
	}
	hidden, err := s.store.HiddenBookNames(r.Context(), userID, bookID)
	if err != nil {
		fail(w, err)
		return
	}

	// Group under the clamp: mentions counts occurrences inside the bound,
	// and a name with none of them does not exist yet.
	type entry struct {
		display string
		count   int
		first   int
	}
	entries := make(map[string]*entry, len(occ))
	order := make([]string, 0, len(occ))
	for _, o := range occ {
		if o.CharStart >= bound.offset {
			continue
		}
		e, seen := entries[o.Name]
		if !seen {
			e = &entry{display: o.Display, first: o.CharStart}
			entries[o.Name] = e
			order = append(order, o.Name)
		}
		e.count++
	}
	if len(order) == 0 {
		writeJSON(w, http.StatusOK, namesResponse{Names: []nameIndexEntry{}, Bound: bound.view})
		return
	}

	views, err := s.searchViewsFor(r.Context(), userID, entryID, bookID)
	if err != nil {
		fail(w, err)
		return
	}
	out := namesResponse{Names: make([]nameIndexEntry, 0, len(order)), Bound: bound.view}
	for _, name := range order {
		e := entries[name]
		out.Names = append(out.Names, nameIndexEntry{
			Name:     e.display,
			Mentions: e.count,
			FirstSeen: nameFirstSeen{
				CharStart: e.first,
				Chapter:   chapterAt(views.chapters, e.first),
				Percent:   percentAtOffset(e.first, views),
				DeepLink:  deepLink(entryID, e.first),
			},
			Hidden: hidden[name],
		})
	}
	sort.Slice(out.Names, func(i, j int) bool {
		return strings.ToLower(out.Names[i].Name) < strings.ToLower(out.Names[j].Name)
	})
	writeJSON(w, http.StatusOK, out)
}

// mentionHit is one occurrence of a name. The shape is pinned: the MCP
// server's find_mentions decodes it field for field.
type mentionHit struct {
	CharOffset int           `json:"char_offset"`
	CharEnd    int           `json:"char_end"`
	Snippet    books.Snippet `json:"snippet"`
	Chapter    *chapterView  `json:"chapter"`
	DeepLink   string        `json:"deep_link"`
}

type mentionsResponse struct {
	Name    string       `json:"name"`
	Total   int          `json:"total"`
	Results []mentionHit `json:"results"`
	Bound   boundView    `json:"bound"`
}

// handleBookMentions lists where a name has come up so far:
// GET /api/books/{entryID}/mentions?name=…&limit=20&until=.
//
// Occurrences past the bound are dropped in the query itself, so Total is
// an honest "mentions so far" and the snippets stop at the bound the way
// every hit's context does. The name is matched on its folded form, so
// however the caller capitalizes it is fine.
func (s *Server) handleBookMentions(w http.ResponseWriter, r *http.Request) {
	userID, entryID, bookID, ok := s.bookEntry(w, r)
	if !ok {
		return
	}
	if s.epubs == nil {
		fail(w, errorf(http.StatusServiceUnavailable, "canonical text storage unavailable"))
		return
	}
	query := r.URL.Query().Get("name")
	key := booktext.Normalize(strings.TrimSpace(query))
	if key == "" {
		fail(w, errorf(http.StatusBadRequest, "name is required"))
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

	et, ok := s.ensureBookText(w, r)
	if !ok {
		return
	}

	bound, ok := s.resolveReadBoundDefault(w, r, userID, entryID, bookID, et.CharCount, untilPosition)
	if !ok {
		return
	}
	censoring := bound.offset < et.CharCount

	occ, err := s.store.NameMentions(r.Context(), et.MediaFileID, key, bound.offset)
	if err != nil {
		fail(w, err)
		return
	}
	out := mentionsResponse{Total: len(occ), Results: []mentionHit{}, Bound: bound.view}
	if len(occ) == 0 {
		// No name to echo but the caller's own spelling.
		out.Name = query
		writeJSON(w, http.StatusOK, out)
		return
	}
	out.Name = occ[0].Display
	if len(occ) > limit {
		occ = occ[:limit]
	}

	views, err := s.searchViewsFor(r.Context(), userID, entryID, bookID)
	if err != nil {
		fail(w, err)
		return
	}
	snippets, err := s.epubs.Snippets(r.Context(), et)
	if err != nil {
		fail(w, errorf(http.StatusInternalServerError, "could not read this ebook's text"))
		return
	}
	for _, o := range occ {
		var context books.Snippet
		if censoring {
			context, ok = snippets.AtUntil(o.CharStart, o.CharEnd, bound.offset)
		} else {
			context, ok = snippets.At(o.CharStart, o.CharEnd)
		}
		if !ok {
			// The sidecar and the display file disagree about this
			// offset; one mention fewer beats a mis-highlighted one.
			continue
		}
		out.Results = append(out.Results, mentionHit{
			CharOffset: o.CharStart,
			CharEnd:    o.CharEnd,
			Snippet:    context,
			Chapter:    chapterAt(views.chapters, o.CharStart),
			DeepLink:   deepLink(entryID, o.CharStart),
		})
	}
	writeJSON(w, http.StatusOK, out)
}

// nameHideRequest is the body of both hide and unhide: the name as the
// index printed it. The server folds it — the stored key is the folded
// form, so "Elizabeth" and "elizabeth" hide the same entry.
type nameHideRequest struct {
	Name string `json:"name"`
}

// handleHideBookName records a reader's verdict that a name is a false
// positive: POST /api/books/{entryID}/names/hide {name}.
//
// Hidden is stored, not regenerated — the extractor's next pass must not
// resurrect what a person has judged wrong. The POST shape is
// cookie-only by the token write gate, which is right: the index is the
// reader's own surface, and hiding is a reader's action.
func (s *Server) handleHideBookName(w http.ResponseWriter, r *http.Request) {
	s.hideBookName(w, r, true)
}

// handleUnhideBookName takes a name back: POST /api/books/{entryID}/names/unhide.
func (s *Server) handleUnhideBookName(w http.ResponseWriter, r *http.Request) {
	s.hideBookName(w, r, false)
}

func (s *Server) hideBookName(w http.ResponseWriter, r *http.Request, hide bool) {
	userID, _, bookID, ok := s.bookEntry(w, r)
	if !ok {
		return
	}
	var body nameHideRequest
	if err := decode(r, &body); err != nil {
		fail(w, err)
		return
	}
	key := booktext.Normalize(strings.TrimSpace(body.Name))
	if key == "" {
		fail(w, errorf(http.StatusBadRequest, "name is required"))
		return
	}
	var err error
	if hide {
		err = s.store.HideBookName(r.Context(), userID, bookID, key)
	} else {
		err = s.store.UnhideBookName(r.Context(), userID, bookID, key)
	}
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
