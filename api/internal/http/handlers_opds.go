package http

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/collinpendleton/backhog/api/internal/auth"
	"github.com/collinpendleton/backhog/api/internal/ereader"
	"github.com/collinpendleton/backhog/api/internal/models"
	"github.com/collinpendleton/backhog/api/internal/store"
)

// The OPDS 1.2 catalog (MAD-830): the library as an e-reader browses it.
//
// Designed for the smallest client first. CrossPoint parses a feed on an
// ESP32 with a few hundred kilobytes of RAM and keeps at most 62 entries a
// page, 160 bytes of a title, 120 of an author and 768 of an href; it finds
// search only through an inline {searchTerms} link, and it ignores covers
// and summaries. KOReader and Thorium read the same feeds and use more of
// them (covers, OpenSearch). So: small pages, absolute links, budgeted
// strings, an inline search link on every feed, and covers that cost the
// small client nothing but a skipped element.

const (
	opdsPageSize = 25

	opdsNavType = "application/atom+xml;profile=opds-catalog;kind=navigation"
	opdsAcqType = "application/atom+xml;profile=opds-catalog;kind=acquisition"

	relAcquisition = "http://opds-spec.org/acquisition"
	relImage       = "http://opds-spec.org/image"
	relThumbnail   = "http://opds-spec.org/image/thumbnail"
)

type opdsFeed struct {
	XMLName xml.Name    `xml:"feed"`
	Xmlns   string      `xml:"xmlns,attr"`
	Opds    string      `xml:"xmlns:opds,attr"`
	ID      string      `xml:"id"`
	Title   string      `xml:"title"`
	Updated string      `xml:"updated"`
	Author  opdsAuthor  `xml:"author"`
	Links   []opdsLink  `xml:"link"`
	Entries []opdsEntry `xml:"entry"`
}

type opdsAuthor struct {
	Name string `xml:"name"`
}

type opdsLink struct {
	Rel    string `xml:"rel,attr,omitempty"`
	Href   string `xml:"href,attr"`
	Type   string `xml:"type,attr,omitempty"`
	Title  string `xml:"title,attr,omitempty"`
	Length int64  `xml:"length,attr,omitempty"`
}

type opdsEntry struct {
	Title   string       `xml:"title"`
	ID      string       `xml:"id"`
	Updated string       `xml:"updated"`
	Authors []opdsAuthor `xml:"author,omitempty"`
	Content *opdsContent `xml:"content,omitempty"`
	Links   []opdsLink   `xml:"link"`
}

type opdsContent struct {
	Type string `xml:"type,attr"`
	Text string `xml:",chardata"`
}

// opdsBook is one book as the catalog shows it, whatever surface it came
// from (an entry, a reading-now card, a series row).
type opdsBook struct {
	EntryID string
	BookID  string
	Title   string
	Authors []string
	Updated time.Time
}

// opdsBase is the absolute origin every catalog link starts with. A client
// resolves relative hrefs against the feed's URL in theory; CrossPoint's
// parser is young and a firmware bug there would strand the device, so the
// catalog never asks it to. BACKHOG_PUBLIC_URL wins when set; otherwise the
// request says where it was addressed, through the proxy headers when there
// is a proxy.
func (s *Server) opdsBase(r *http.Request) string {
	if s.cfg.PublicURL != "" {
		return strings.TrimRight(s.cfg.PublicURL, "/")
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if p := r.Header.Get("X-Forwarded-Proto"); p == "https" || p == "http" {
		scheme = p
	}
	host := r.Host
	if h := r.Header.Get("X-Forwarded-Host"); h != "" {
		host = strings.TrimSpace(strings.Split(h, ",")[0])
	}
	return scheme + "://" + host
}

// handleOPDSRoot is the catalog's front page: the shelves an e-reader can
// browse, in the order a reader reaches for them.
func (s *Server) handleOPDSRoot(w http.ResponseWriter, r *http.Request) {
	base := s.opdsBase(r)
	nav := func(path, title, about string) opdsEntry {
		return opdsEntry{
			Title: title, ID: "urn:backhog:opds:" + strings.TrimPrefix(path, "/"),
			Updated: opdsTime(time.Now()),
			Content: &opdsContent{Type: "text", Text: about},
			Links:   []opdsLink{{Rel: "subsection", Href: base + "/api/opds" + path, Type: opdsAcqType}},
		}
	}
	entries := []opdsEntry{
		nav("/reading", "Reading now", "The books you are in the middle of."),
		nav("/queue", "Queue", "Up next, in your order."),
		nav("/recent", "Recently added", "Newest on the shelf first."),
		nav("/all", "All books", "Every book you can read here, A to Z."),
	}
	lists := nav("/lists", "Lists", "Your lists.")
	lists.Links[0].Type = opdsNavType
	series := nav("/series", "Series", "Every series on your shelf, in reading order.")
	series.Links[0].Type = opdsNavType
	entries = append(entries, lists, series)

	s.writeOPDS(w, r, base, opdsNavType, "", "Backhog", entries, nil)
}

// handleOPDSShelf serves one of the fixed acquisition shelves.
func (s *Server) handleOPDSShelf(shelf string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID, err := auth.MustUserID(r.Context())
		if err != nil {
			fail(w, errUnauthorized)
			return
		}
		var books []opdsBook
		var title string
		switch shelf {
		case "reading":
			title = "Reading now"
			now, err := s.store.ReadingNow(r.Context(), userID)
			if err != nil {
				fail(w, err)
				return
			}
			for _, b := range now.Books {
				books = append(books, bookFromEntry(b.Entry))
			}
		case "queue":
			title = "Queue"
			entries, err := s.store.Queue(r.Context(), userID)
			if err != nil {
				fail(w, err)
				return
			}
			books = booksFromEntries(entries)
		case "recent", "all":
			sort := "added"
			title = "Recently added"
			if shelf == "all" {
				title, sort = "All books", "title"
			}
			entries, err := s.store.ListEntries(r.Context(), userID,
				store.LibraryFilter{MediaType: models.MediaBook, Sort: sort})
			if err != nil {
				fail(w, err)
				return
			}
			books = booksFromEntries(entries)
		}
		s.serveOPDSBooks(w, r, userID, "/"+shelf, title, books)
	}
}

// handleOPDSSearch answers the {searchTerms} template: title and author,
// the same match the library's own search box makes, then series and ISBN.
func (s *Server) handleOPDSSearch(w http.ResponseWriter, r *http.Request) {
	userID, err := auth.MustUserID(r.Context())
	if err != nil {
		fail(w, errUnauthorized)
		return
	}
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	var books []opdsBook
	if q != "" {
		entries, err := s.store.ListEntries(r.Context(), userID,
			store.LibraryFilter{MediaType: models.MediaBook, Query: q, Sort: "title"})
		if err != nil {
			fail(w, err)
			return
		}
		books = booksFromEntries(entries)
		// Then series and ISBN, which the library's own search box does not
		// cover but a reader at a device keyboard reaches for ("Discworld",
		// or the ISBN on the back of a paperback). Duplicates of title hits
		// are dropped when the page is built.
		more, err := s.store.SearchBooksBySeriesOrISBN(r.Context(), userID, q)
		if err != nil {
			fail(w, err)
			return
		}
		for _, m := range more {
			var authors []string
			_ = json.Unmarshal([]byte(m.AuthorsJSON), &authors)
			books = append(books, opdsBook{EntryID: m.EntryID, BookID: m.BookID, Title: m.Title, Authors: authors})
		}
	}
	s.serveOPDSBooks(w, r, userID, "/search?q="+url.QueryEscape(q), "Search: "+q, books)
}

// handleOPDSLists is the navigation feed of the caller's lists.
func (s *Server) handleOPDSLists(w http.ResponseWriter, r *http.Request) {
	userID, err := auth.MustUserID(r.Context())
	if err != nil {
		fail(w, errUnauthorized)
		return
	}
	lists, err := s.store.GetLists(r.Context(), userID)
	if err != nil {
		fail(w, err)
		return
	}
	base := s.opdsBase(r)
	entries := make([]opdsEntry, 0, len(lists))
	for _, l := range lists {
		// Lists carry no arena of their own (the built-in smart lists come
		// in game and book flavours under the same names), so a list earns
		// a row by what it holds right now: at least one book this device
		// can open.
		members, err := s.store.ListEntriesFor(r.Context(), userID, l.ID)
		if err != nil {
			fail(w, err)
			return
		}
		n, err := s.readableCount(r, userID, booksFromEntries(members))
		if err != nil {
			fail(w, err)
			return
		}
		if n == 0 {
			continue
		}
		e := opdsEntry{
			Title:   ereader.FeedTitle(l.Name),
			ID:      "urn:backhog:list:" + l.ID,
			Updated: opdsTime(l.CreatedAt),
			Links:   []opdsLink{{Rel: "subsection", Href: base + "/api/opds/lists/" + url.PathEscape(l.ID), Type: opdsAcqType}},
		}
		about := countBooks(n)
		if d := strings.TrimSpace(l.Description); d != "" {
			about += " · " + d
		}
		e.Content = &opdsContent{Type: "text", Text: shortText(about)}
		entries = append(entries, e)
	}
	s.writeOPDS(w, r, base, opdsNavType, "/lists", "Lists", entries, nil)
}

// handleOPDSList is one list's books, smart lists included, in list order.
func (s *Server) handleOPDSList(w http.ResponseWriter, r *http.Request) {
	userID, err := auth.MustUserID(r.Context())
	if err != nil {
		fail(w, errUnauthorized)
		return
	}
	listID := chi.URLParam(r, "listID")
	list, err := s.store.GetList(r.Context(), userID, listID)
	if errors.Is(err, store.ErrNotFound) {
		fail(w, errNotFound)
		return
	} else if err != nil {
		fail(w, err)
		return
	}
	entries, err := s.store.ListEntriesFor(r.Context(), userID, listID)
	if err != nil {
		fail(w, err)
		return
	}
	s.serveOPDSBooks(w, r, userID, "/lists/"+url.PathEscape(listID), list.Name, booksFromEntries(entries))
}

// handleOPDSSeriesIndex is the navigation feed of the caller's series.
func (s *Server) handleOPDSSeriesIndex(w http.ResponseWriter, r *http.Request) {
	userID, err := auth.MustUserID(r.Context())
	if err != nil {
		fail(w, errUnauthorized)
		return
	}
	rows, err := s.store.BookSeriesIndex(r.Context(), userID)
	if err != nil {
		fail(w, err)
		return
	}
	base := s.opdsBase(r)
	entries := make([]opdsEntry, 0, len(rows))
	for _, row := range rows {
		members, err := s.seriesBooks(r, userID, row.Name)
		if err != nil {
			fail(w, err)
			return
		}
		n, err := s.readableCount(r, userID, members)
		if err != nil {
			fail(w, err)
			return
		}
		if n == 0 {
			continue
		}
		entries = append(entries, opdsEntry{
			Title:   ereader.FeedTitle(row.Name),
			ID:      "urn:backhog:series:" + url.QueryEscape(strings.ToLower(row.Name)),
			Updated: opdsTime(time.Now()),
			Content: &opdsContent{Type: "text", Text: countBooks(n)},
			// The name rides in the query rather than the path: series
			// names carry slashes and colons often enough to matter.
			Links: []opdsLink{{Rel: "subsection", Href: base + "/api/opds/series/books?name=" + url.QueryEscape(row.Name), Type: opdsAcqType}},
		})
	}
	s.writeOPDS(w, r, base, opdsNavType, "/series", "Series", entries, nil)
}

// handleOPDSSeries is one series in reading order.
func (s *Server) handleOPDSSeries(w http.ResponseWriter, r *http.Request) {
	userID, err := auth.MustUserID(r.Context())
	if err != nil {
		fail(w, errUnauthorized)
		return
	}
	name := strings.TrimSpace(r.URL.Query().Get("name"))
	if name == "" {
		fail(w, errNotFound)
		return
	}
	books, err := s.seriesBooks(r, userID, name)
	if err != nil {
		fail(w, err)
		return
	}
	s.serveOPDSBooks(w, r, userID, "/series/books?name="+url.QueryEscape(name), name, books)
}

// seriesBooks is one series in reading order, as catalog books.
func (s *Server) seriesBooks(r *http.Request, userID, name string) ([]opdsBook, error) {
	rows, err := s.store.BookSeriesDetail(r.Context(), userID, name)
	if err != nil {
		return nil, err
	}
	books := make([]opdsBook, 0, len(rows))
	for _, row := range rows {
		var authors []string
		_ = json.Unmarshal([]byte(row.AuthorsJSON), &authors)
		books = append(books, opdsBook{EntryID: row.EntryID, BookID: row.BookID,
			Title: row.Title, Authors: authors})
	}
	return books, nil
}

// readableCount is how many of books a device could open.
func (s *Server) readableCount(r *http.Request, userID string, books []opdsBook) (int, error) {
	if len(books) == 0 {
		return 0, nil
	}
	ids := make([]string, 0, len(books))
	for _, b := range books {
		ids = append(ids, b.EntryID)
	}
	readable, err := s.store.ReadableEpubs(r.Context(), userID, ids)
	return len(readable), err
}

func countBooks(n int) string {
	if n == 1 {
		return "1 book"
	}
	return strconv.Itoa(n) + " books"
}

// serveOPDSBooks renders an acquisition feed: the readable subset of books,
// in the order given, one page at a time.
func (s *Server) serveOPDSBooks(w http.ResponseWriter, r *http.Request, userID, path, title string, books []opdsBook) {
	ids := make([]string, 0, len(books))
	for _, b := range books {
		ids = append(ids, b.EntryID)
	}
	readable, err := s.store.ReadableEpubs(r.Context(), userID, ids)
	if err != nil {
		fail(w, err)
		return
	}
	kept := books[:0:0]
	seen := map[string]bool{}
	for _, b := range books {
		if _, ok := readable[b.EntryID]; ok && !seen[b.EntryID] {
			seen[b.EntryID] = true
			kept = append(kept, b)
		}
	}

	page := 1
	if p, err := strconv.Atoi(r.URL.Query().Get("page")); err == nil && p > 1 {
		page = p
	}
	start := min((page-1)*opdsPageSize, len(kept))
	end := min(start+opdsPageSize, len(kept))

	base := s.opdsBase(r)
	entries := make([]opdsEntry, 0, end-start)
	for _, b := range kept[start:end] {
		entries = append(entries, opdsBookEntry(base, b, readable[b.EntryID]))
	}

	pageURL := func(n int) string {
		sep := "?"
		if strings.Contains(path, "?") {
			sep = "&"
		}
		return base + "/api/opds" + path + sep + "page=" + strconv.Itoa(n)
	}
	var paging []opdsLink
	if page > 1 {
		paging = append(paging, opdsLink{Rel: "previous", Href: pageURL(page - 1), Type: opdsAcqType})
	}
	if end < len(kept) {
		paging = append(paging, opdsLink{Rel: "next", Href: pageURL(page + 1), Type: opdsAcqType})
	}
	s.writeOPDS(w, r, base, opdsAcqType, path, title, entries, paging)
}

// opdsBookEntry is one book: budgeted title and author (the same strings
// the filename document ids are hashed from), covers for the clients that
// draw them, and the acquisition link to the original bytes.
func opdsBookEntry(base string, b opdsBook, size int64) opdsEntry {
	author, title := feedStrings(models.Book{Title: b.Title, Authors: b.Authors})
	updated := b.Updated
	if updated.IsZero() {
		updated = time.Now()
	}
	e := opdsEntry{
		Title:   title,
		ID:      "urn:backhog:entry:" + b.EntryID,
		Updated: opdsTime(updated),
	}
	if author != "" {
		e.Authors = []opdsAuthor{{Name: author}}
	}
	cover := base + "/api/covers/book/" + url.PathEscape(b.BookID)
	e.Links = []opdsLink{
		{Rel: relImage, Href: cover, Type: "image/jpeg"},
		{Rel: relThumbnail, Href: cover, Type: "image/jpeg"},
		{Rel: relAcquisition, Type: "application/epub+zip", Length: size,
			Href: base + "/api/opds/books/" + url.PathEscape(b.EntryID) + "/epub/" + epubSlug(title) + ".epub"},
	}
	return e
}

// writeOPDS renders a feed with the links every page carries: self, start,
// up, and the inline search template. Buffered, so the response has a
// Content-Length — a microcontroller's HTTP client is happiest knowing
// how much is coming.
func (s *Server) writeOPDS(w http.ResponseWriter, r *http.Request, base, kind, path, title string, entries []opdsEntry, extra []opdsLink) {
	root := base + "/api/opds"
	feed := opdsFeed{
		Xmlns:   "http://www.w3.org/2005/Atom",
		Opds:    "http://opds-spec.org/2010/catalog",
		ID:      "urn:backhog:opds" + strings.ReplaceAll(path, "/", ":"),
		Title:   ereader.FeedTitle(title),
		Updated: opdsTime(time.Now()),
		Author:  opdsAuthor{Name: "Backhog"},
		Links: []opdsLink{
			{Rel: "self", Href: root + path, Type: kind},
			{Rel: "start", Href: root, Type: opdsNavType},
			{Rel: "search", Href: root + "/search?q={searchTerms}", Type: "application/atom+xml", Title: "Search your library"},
		},
		Entries: entries,
	}
	if path != "" {
		feed.Links = append(feed.Links, opdsLink{Rel: "up", Href: root, Type: opdsNavType})
	}
	feed.Links = append(feed.Links, extra...)

	var buf bytes.Buffer
	buf.WriteString(xml.Header)
	if err := xml.NewEncoder(&buf).Encode(feed); err != nil {
		fail(w, err)
		return
	}
	w.Header().Set("Content-Type", kind+";charset=utf-8")
	w.Header().Set("Content-Length", strconv.Itoa(buf.Len()))
	w.Header().Set("Cache-Control", "private, no-cache")
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_, _ = w.Write(buf.Bytes())
	}
}

func bookFromEntry(e models.Entry) opdsBook {
	b := opdsBook{EntryID: e.ID, Updated: e.UpdatedAt}
	if e.Book != nil {
		b.BookID, b.Title, b.Authors = e.Book.ID, e.Book.Title, e.Book.Authors
	}
	return b
}

func booksFromEntries(entries []models.Entry) []opdsBook {
	out := make([]opdsBook, 0, len(entries))
	for _, e := range entries {
		if e.MediaType == models.MediaBook && e.Book != nil {
			out = append(out, bookFromEntry(e))
		}
	}
	return out
}

// epubSlug is the decorative last segment of a download URL: ASCII, short,
// readable in a log. The entry id is what the route actually reads.
func epubSlug(title string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(title) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			dash = false
		case !dash && b.Len() > 0:
			b.WriteByte('-')
			dash = true
		}
		if b.Len() >= 48 {
			break
		}
	}
	slug := strings.Trim(b.String(), "-")
	if slug == "" {
		return "book"
	}
	return slug
}

// shortText trims a description for a feed entry: every byte is parsed on
// the smallest device, and a list row shows a line of it at most.
func shortText(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) <= 300 {
		return s
	}
	cut := 297
	for cut > 0 && (s[cut]&0xC0) == 0x80 {
		cut--
	}
	return s[:cut] + "…"
}

func opdsTime(t time.Time) string { return t.UTC().Format(time.RFC3339) }
