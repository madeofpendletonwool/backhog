package http

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/collinpendleton/backhog/api/booktext"
	"github.com/collinpendleton/backhog/api/internal/auth"
	"github.com/collinpendleton/backhog/api/internal/books"
	"github.com/collinpendleton/backhog/api/internal/books/search"
	"github.com/collinpendleton/backhog/api/internal/models"
	"github.com/collinpendleton/backhog/api/internal/store"
)

// Library-wide text search (MAD-470): the in-book search pointed at the
// whole shelf.
//
// Finding is the cheap half. SQLite FTS5 over the canonical chapter text
// (book_text_fts, written on ingest and healed by the index walk) names the
// books that contain the phrase, ranked; then each candidate is searched
// exactly the way the in-book endpoint searches one book — the same
// Searcher, the same snippets out of the display companion, the same
// chapters and percentages — so a hit here is the same object a hit there
// is, plus the book it came from.
//
// Spoiler safety is the feature. The default clamp for *every* caller is
// the reader's own positions: hits past where they are in a book they are
// reading are dropped before the answer is built, and a book whose every
// hit is past the bound — including a book never opened — says so as a
// title and nothing more. `until=none` is the explicit opt-in that lifts
// all of it, the same parameter and the same rule every read path already
// speaks.

const (
	// librarySearchCandidateBooks caps how many ranked books a query may
	// graduate to the exact-placement stage. The FTS row limit already
	// bounds the raw match work; this bounds the per-book passes behind it.
	librarySearchCandidateBooks = 40

	// librarySearchPerBook caps one book's visible hits so a phrase a
	// single book repeats cannot eat the whole result list.
	librarySearchPerBook = 5

	// librarySearchFetchPerBook is the fetch window widened for clamped
	// requests, where hits are about to be dropped and a window that
	// stopped at the visible cap could drop readable hits it never fetched.
	librarySearchFetchPerBook = 10
)

// librarySearchHit is one match in one book, in the same shape the in-book
// search returns plus the book itself: the citation an answer grounded in
// this hit carries.
type librarySearchHit struct {
	BookID    string        `json:"book_id"`
	Title     string        `json:"title"`
	Chapter   *chapterView  `json:"chapter"`
	Snippet   books.Snippet `json:"snippet"`
	CharStart int           `json:"char_start"`
	CharEnd   int           `json:"char_end"`
	Percent   float64       `json:"percent"`
	DeepLink  string        `json:"deep_link"`
}

// librarySearchBeyond is a book that matched but has nothing to show: every
// hit sits past the caller's position, or the book was never opened. The
// title is the whole answer — that the phrase is in there somewhere is a
// fact about the library, not a spoiler, and it is all the default gives.
type librarySearchBeyond struct {
	BookID string `json:"book_id"`
	Title  string `json:"title"`
}

// librarySearchResponse is one query's answer across the shelf.
type librarySearchResponse struct {
	Query string `json:"query"`
	// Mode is "phrase" when some book contains what was typed and "loose"
	// when these are the closest passages instead — decided, as the in-book
	// search decides it, by what the texts contain rather than by what
	// survived the clamp.
	Mode  search.Mode `json:"mode"`
	Total int         `json:"total"`
	// Truncated is set when more hits existed than were returned.
	Truncated bool               `json:"truncated"`
	Results   []librarySearchHit `json:"results"`
	// MatchedBeyond lists the books whose hits were all withheld, titles
	// only. Not counted in Total: they are books, not hits.
	MatchedBeyond []librarySearchBeyond `json:"matched_beyond"`
	// Bound is the clamp this answer was served under. A library has one
	// bound per book, so the offsets live in each withheld book's own
	// reads; this echo carries the mode, and nothing it claims to place.
	Bound boundView `json:"bound"`
}

// handleSearchLibraryText finds a phrase across every book the caller can
// read: GET /api/books/search/text?q=…&limit=20&until=position|none&unread=titles|hits.
//
// `until` defaults to "position" for every caller — cookie sessions too,
// because this endpoint is not the reader (nothing here moves a position)
// and spoiler safety is the point of asking the shelf rather than the book.
// `unread=hits` is the narrower opt-in that shows hits from books never
// opened, for the reader whose question is "which of my books mention this
// at all"; `until=none` remains the loud one that lifts everything.
func (s *Server) handleSearchLibraryText(w http.ResponseWriter, r *http.Request) {
	userID, err := auth.MustUserID(r.Context())
	if err != nil {
		fail(w, errUnauthorized)
		return
	}
	if s.epubs == nil || s.search == nil {
		fail(w, errorf(http.StatusServiceUnavailable, "canonical text storage unavailable"))
		return
	}

	query := r.URL.Query().Get("q")
	q := booktext.Normalize(query)
	if len(q) < 3 {
		fail(w, errorf(http.StatusUnprocessableEntity,
			"type a few more characters to search your library"))
		return
	}

	mode := untilPosition
	switch v := r.URL.Query().Get("until"); v {
	case "", untilPosition:
	case untilNone:
		mode = untilNone
	default:
		fail(w, errorf(http.StatusBadRequest, "until must be \"position\" or \"none\" here — one book's offset is not another's"))
		return
	}
	unreadHits := false
	switch v := r.URL.Query().Get("unread"); v {
	case "", "titles":
	case "hits":
		unreadHits = true
	default:
		fail(w, errorf(http.StatusBadRequest, "unread must be \"titles\" or \"hits\""))
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

	// Candidates come from the index: the phrase as typed first, and only
	// when no book on the shelf contains it, the terms in any order — the
	// in-book search's two tiers, decided one shelf-sized step earlier.
	exact := s.libraryCandidates(r.Context(), q, true)
	resMode := search.ModePhrase
	if len(exact) == 0 {
		exact = s.libraryCandidates(r.Context(), q, false)
		resMode = search.ModeLoose
	}
	if len(exact) == 0 {
		writeJSON(w, http.StatusOK, librarySearchResponse{
			Query: query, Mode: resMode, Results: []librarySearchHit{},
			MatchedBeyond: []librarySearchBeyond{}, Bound: s.libraryBoundView(mode),
		})
		return
	}

	// The permission seam: a candidate book exists for this caller only as
	// their own entry with the file-access rule satisfied. Books that fail
	// a leg simply do not answer — the denial shape every file path uses.
	ids := make([]string, 0, len(exact))
	for _, m := range exact {
		ids = append(ids, m.BookID)
	}
	if len(ids) > librarySearchCandidateBooks {
		ids = ids[:librarySearchCandidateBooks]
	}
	scope, err := s.store.LibrarySearchScope(r.Context(), userID, ids)
	if err != nil {
		fail(w, err)
		return
	}
	// The scope query answers in whatever order SQLite likes; the index's
	// ranking is the order the ids were asked in, so the books go back into
	// it here — best match first is the whole point of ranking.
	byBook := make(map[string]store.LibrarySearchBook, len(scope))
	for _, b := range scope {
		byBook[b.BookID] = b
	}
	ranked := make([]store.LibrarySearchBook, 0, len(scope))
	for _, id := range ids {
		if b, ok := byBook[id]; ok {
			ranked = append(ranked, b)
		}
	}

	censoring := mode == untilPosition
	out := librarySearchResponse{
		Query: query, Mode: resMode,
		Results:       []librarySearchHit{},
		MatchedBeyond: []librarySearchBeyond{},
		Bound:         s.libraryBoundView(mode),
	}
	census := 0
	more := false

	for _, b := range ranked {
		// The clamp, resolved per book through the same position the read
		// paths clamp by: a stored character offset, with a page-axis
		// position offering nothing and an unread book (no row at all)
		// answering titles-only unless the caller asked for its hits.
		progress, err := s.store.BookProgress(r.Context(), userID, b.EntryID)
		if err != nil {
			fail(w, err)
			return
		}
		unread := progress.UpdatedAt.IsZero()
		bound := b.CharCount
		if censoring {
			bound = progress.CharOffset
			if progress.PositionMode == models.PositionModePage {
				bound = 0
			}
			if bound > b.CharCount {
				bound = b.CharCount
			}
			if unread && unreadHits {
				bound = b.CharCount
			}
		}
		bookCensoring := censoring && bound < b.CharCount

		fetch := librarySearchPerBook
		if bookCensoring {
			fetch = librarySearchFetchPerBook
		}
		res, err := s.search.Search(r.Context(), b.TextID, b.NormalizedSHA256, query, fetch)
		if err != nil {
			slog.ErrorContext(r.Context(), "library search book", "entry", b.EntryID, "error", err)
			continue
		}

		hits := res.Hits
		if bookCensoring {
			kept := hits[:0]
			for _, hit := range hits {
				if hit.CharOffset >= bound {
					continue
				}
				kept = append(kept, hit)
			}
			hits = kept
		}
		// The honest census per book: a clamped book counts what survived
		// its clamp (its full Total includes text the caller may not read),
		// an unclamped one counts everything it contains.
		if bookCensoring {
			census += len(hits)
		} else {
			census += res.Total
		}
		if len(hits) > librarySearchPerBook {
			hits = hits[:librarySearchPerBook]
			more = true
		}
		if len(hits) == 0 {
			// The book matched — the index said so — but nothing it holds
			// is somewhere the caller has read. The title is what the
			// clamp lets through, and it is enough to be useful.
			if res.Total > 0 {
				out.MatchedBeyond = append(out.MatchedBeyond,
					librarySearchBeyond{BookID: b.EntryID, Title: b.Title})
			}
			continue
		}

		snippets, err := s.epubs.Snippets(r.Context(), epubTextOf(b))
		if err != nil {
			slog.ErrorContext(r.Context(), "library search snippets", "entry", b.EntryID, "error", err)
			continue
		}
		views, err := s.searchViewsFor(r.Context(), userID, b.EntryID, b.BookID)
		if err != nil {
			fail(w, err)
			return
		}

		for _, hit := range hits {
			var context books.Snippet
			var ok bool
			if bookCensoring {
				context, ok = snippets.AtUntil(hit.CharOffset, hit.CharEnd, bound)
			} else {
				context, ok = snippets.At(hit.CharOffset, hit.CharEnd)
			}
			if !ok {
				// The companions and the index disagree about this offset;
				// one hit fewer beats a mis-highlighted one, same as the
				// in-book search.
				continue
			}
			if len(out.Results) >= limit {
				more = true
				break
			}
			out.Results = append(out.Results, librarySearchHit{
				BookID:    b.EntryID,
				Title:     b.Title,
				Chapter:   chapterAt(views.chapters, hit.CharOffset),
				Snippet:   context,
				CharStart: hit.CharOffset,
				CharEnd:   hit.CharEnd,
				Percent:   percentAtOffset(hit.CharOffset, views),
				DeepLink:  deepLink(b.EntryID, hit.CharOffset),
			})
		}
	}

	out.Total = census
	out.Truncated = more || out.Total > len(out.Results)
	writeJSON(w, http.StatusOK, out)
}

// libraryCandidates runs one MATCH tier over the index and returns the
// books that matched, best first. phrase selects the folded query as an FTS
// phrase; otherwise the terms match in any order (FTS's implicit AND).
func (s *Server) libraryCandidates(ctx context.Context, q string, phrase bool) []store.BookTextMatch {
	expr := strings.Join(strings.Fields(store.FoldForFTS(q)), " ")
	if expr == "" {
		return nil
	}
	if phrase {
		expr = `"` + expr + `"`
	}
	matches, err := s.store.SearchBookTextIndex(ctx, expr, 500)
	if err != nil {
		slog.ErrorContext(ctx, "library search index", "error", err)
		return nil
	}
	return matches
}

// libraryBoundView echoes the clamp mode without claiming to place it: a
// library-wide bound has no single offset to report, only the rule it was
// served under.
func (s *Server) libraryBoundView(mode string) boundView {
	if mode == untilNone {
		return boundView{Until: untilNone, Percent: 100}
	}
	return boundView{Until: untilPosition}
}

// epubTextOf rebuilds the EpubText the Searcher and Snippets want from the
// scope row's facts — the pieces those two read are the id, which names the
// companion files, and the revision, which keys the cache.
func epubTextOf(b store.LibrarySearchBook) models.EpubText {
	return models.EpubText{ID: b.TextID, MediaFileID: b.MediaFileID, CharCount: b.CharCount}
}
