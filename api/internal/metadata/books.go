package metadata

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"

	"golang.org/x/time/rate"
)

const (
	openLibraryBaseURL  = "https://openlibrary.org"
	openLibraryCoverURL = "https://covers.openlibrary.org/b/id/%d-L.jpg"
	// Open Library asks API consumers to identify themselves with a
	// descriptive User-Agent so they can throttle offenders specifically
	// instead of blocking the whole app.
	openLibraryUserAgent = "Backhog/1.0 (https://github.com/madeofpendletonwool/backhog)"
)

// ErrBookNotFound is returned when Open Library answers 404 for a work,
// edition or search — a real "no such book", not a transport failure.
var ErrBookNotFound = errors.New("book not found upstream")

// ErrQueryNotAllowed is returned when Open Library answers 422 for a search —
// it rejects stopword-only queries ("the", "a") outright. That is an empty
// result set, not a failure the caller should surface as an error.
var ErrQueryNotAllowed = errors.New("query not allowed upstream")

// Book is a provider-agnostic, work-level metadata record. ID is the
// provider's work key (Open Library "OL12345W"). Edition is set only when the
// record came from an edition-oriented lookup such as ISBN.
type Book struct {
	ID               string
	Title            string
	Authors          []string
	Description      string
	CoverURL         string
	FirstPublishYear *int
	Subjects         []string
	Edition          *BookEdition
	Raw              []byte
}

// BookEdition is one printing of a work (Open Library edition key, e.g.
// "OL7440402M"). Page counts and physical page maps belong here, not on the
// work.
type BookEdition struct {
	ID            string
	BookID        string
	ISBN10        string
	ISBN13        string
	Publisher     string
	PublishedYear *int
	PageCount     *int
	Binding       string
	Language      string
	CoverURL      string
	Raw           []byte
}

// BookProvider fetches book metadata from an upstream catalogue. The interface
// is deliberately narrow so a second source can be added without touching the
// handlers, mirroring Provider for games.
type BookProvider interface {
	Search(ctx context.Context, query string, limit int) ([]Book, error)
	GetByWorkKey(ctx context.Context, key string) (Book, error)
	GetByISBN(ctx context.Context, isbn string) (Book, error)
	// GetEditions lists the printings of a work — what the add dialog offers
	// as the edition picker.
	GetEditions(ctx context.Context, key string) ([]BookEdition, error)
}

// isbnPattern accepts ISBN-10 (last digit may be X) and ISBN-13 after
// normalisation has stripped separators.
var isbnPattern = regexp.MustCompile(`^(\d{9}[\dX]|\d{13})$`)

// NormalizeISBN strips the separators people paste and upper-cases the ISBN-10
// check digit.
func NormalizeISBN(isbn string) string {
	isbn = strings.Map(func(r rune) rune {
		switch r {
		case '-', ' ':
			return -1
		}
		return r
	}, strings.TrimSpace(isbn))
	return strings.ToUpper(isbn)
}

// ValidISBN reports whether s (already normalized) is a well-formed ISBN-10
// or ISBN-13.
func ValidISBN(s string) bool { return isbnPattern.MatchString(s) }

// OpenLibrary is a BookProvider backed by Open Library. No credentials are
// needed — the trade for IGDB's Twitch dance — so it works out of the box.
// Requests are rate-limited and identified by User-Agent per Open Library's
// fair-use policy.
type OpenLibrary struct {
	base    string
	http    *http.Client
	limiter *rate.Limiter
}

// NewOpenLibrary constructs the production client against openlibrary.org.
func NewOpenLibrary() *OpenLibrary {
	return newOpenLibrary(openLibraryBaseURL)
}

// NewOpenLibraryAt constructs a client against a custom base URL, for
// integration tests that serve fake Open Library responses.
func NewOpenLibraryAt(base string) *OpenLibrary {
	return newOpenLibrary(base)
}

func newOpenLibrary(base string) *OpenLibrary {
	return &OpenLibrary{
		base: base,
		http: &http.Client{Timeout: 15 * time.Second},
		// Open Library's guidance is roughly one request per second
		// sustained; a small burst keeps multi-request lookups (ISBN)
		// from feeling sluggish without abusing the endpoint.
		limiter: rate.NewLimiter(rate.Limit(1), 2),
	}
}

// olSearchDoc is one hit from /search.json.
type olSearchDoc struct {
	Key              string   `json:"key"`
	Title            string   `json:"title"`
	AuthorName       []string `json:"author_name"`
	FirstPublishYear *int     `json:"first_publish_year"`
	CoverID          int64    `json:"cover_i"`
	// Editions is the printing (or printings) of this work that actually
	// matched the query — Open Library's own answer to "which edition made
	// this a hit". It is what rescues a work whose promoted title is in a
	// language nobody searched in; see preferMatchedEdition.
	Editions struct {
		Docs []olSearchEdition `json:"docs"`
	} `json:"editions"`
}

// olSearchEdition is one matched printing inside a search hit.
type olSearchEdition struct {
	Title   string `json:"title"`
	CoverID int64  `json:"cover_i"`
}

// olText accepts Open Library's dual encoding where a text field is either a
// plain string or {"type": "...", "value": "..."}.
type olText string

func (t *olText) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err == nil {
		*t = olText(s)
		return nil
	}
	var obj struct {
		Value string `json:"value"`
	}
	if err := json.Unmarshal(b, &obj); err == nil {
		*t = olText(obj.Value)
		return nil
	}
	return errors.New("openlibrary: unrecognized text field")
}

// olWork is the payload of /works/{key}.json.
type olWork struct {
	Title            string   `json:"title"`
	Description      olText   `json:"description"`
	Subjects         []string `json:"subjects"`
	Covers           []int64  `json:"covers"`
	FirstPublishDate string   `json:"first_publish_date"`
}

// olEdition is the payload of /isbn/{isbn}.json.
type olEdition struct {
	Key            string   `json:"key"`
	Title          string   `json:"title"`
	ISBN10         []string `json:"isbn_10"`
	ISBN13         []string `json:"isbn_13"`
	Publishers     []string `json:"publishers"`
	PublishDate    string   `json:"publish_date"`
	NumberOfPages  *int     `json:"number_of_pages"`
	PhysicalFormat string   `json:"physical_format"`
	Languages      []struct {
		Key string `json:"key"`
	} `json:"languages"`
	Covers []int64 `json:"covers"`
	Works  []struct {
		Key string `json:"key"`
	} `json:"works"`
}

// Search returns works matching a free-text query. Search hits are lean: the
// rich fields (description, subjects) arrive only on a work detail lookup,
// exactly like IGDB search vs. gameFieldsFull.
func (c *OpenLibrary) Search(ctx context.Context, query string, limit int) ([]Book, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, nil
	}
	if limit <= 0 || limit > 50 {
		limit = 20
	}

	var payload struct {
		Docs []olSearchDoc `json:"docs"`
	}
	// The editions block must be asked for by name, alongside the work
	// fields — requesting only its subfields comes back empty.
	path := fmt.Sprintf("/search.json?q=%s&limit=%d"+
		"&fields=key,title,author_name,first_publish_year,cover_i"+
		",editions,editions.title,editions.cover_i",
		url.QueryEscape(query), limit)
	if err := c.getJSON(ctx, path, &payload); err != nil {
		if errors.Is(err, ErrQueryNotAllowed) {
			return []Book{}, nil
		}
		return nil, err
	}

	books := make([]Book, 0, len(payload.Docs))
	for _, doc := range payload.Docs {
		b := Book{
			ID:               workKey(doc.Key),
			Title:            doc.Title,
			Authors:          doc.AuthorName,
			FirstPublishYear: doc.FirstPublishYear,
		}
		if doc.CoverID != 0 {
			b.CoverURL = fmt.Sprintf(openLibraryCoverURL, doc.CoverID)
		}
		preferMatchedEdition(&b, doc, query)
		if encoded, err := json.Marshal(doc); err == nil {
			b.Raw = encoded
		}
		books = append(books, b)
	}
	return books, nil
}

// preferMatchedEdition relabels a hit with the title of the edition that
// actually matched, when the work's own title does not answer the query and
// an edition's does.
//
// A work title is whatever Open Library has promoted onto the work record,
// and that is not always the name the book is known by. Dark Tower III is
// catalogued as "A Torre Negra" — the title of a Portuguese edition — with
// "The Waste Lands" sitting on the English printings underneath it. Searching
// for The Waste Lands returns the right work under a name the reader cannot
// recognise, and every title comparison downstream scores it at zero, so the
// attach matcher proposes an omnibus instead of the book.
//
// The response already carries the answer: Open Library returns, per work,
// the edition its query matched. Swapping only on a strictly better match
// keeps this inert for the ordinary hit, where the work title is the reason
// the work was found at all.
func preferMatchedEdition(b *Book, doc olSearchDoc, query string) {
	want := searchTokens(query)
	if len(want) == 0 {
		return
	}
	best := queryCoverage(want, b.Title)
	for _, ed := range doc.Editions.Docs {
		if ed.Title == "" {
			continue
		}
		covered := queryCoverage(want, ed.Title)
		if covered <= best {
			continue
		}
		best = covered
		b.Title = ed.Title
		// The cover follows the title: a Portuguese jacket over an English
		// title is the same defect wearing different clothes. A matched
		// edition with no cover of its own keeps the work's.
		if ed.CoverID != 0 {
			b.CoverURL = fmt.Sprintf(openLibraryCoverURL, ed.CoverID)
		}
	}
}

// searchStopwords are the words too common to tell two titles apart. They
// mirror the attach matcher's list, for the same reason: counting "the" makes
// every English title resemble every other.
var searchStopwords = map[string]bool{
	"the": true, "a": true, "an": true, "of": true, "and": true, "or": true,
	"in": true, "on": true, "to": true, "for": true, "with": true, "from": true,
	"at": true, "by": true,
}

// searchTokens folds a string to its distinguishing lowercase words.
func searchTokens(s string) []string {
	fields := strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	out := fields[:0]
	for _, f := range fields {
		if !searchStopwords[f] {
			out = append(out, f)
		}
	}
	return out
}

// queryCoverage counts how many distinct query words a title accounts for.
// It is a count, not a ratio: the question is only which of two titles
// answers more of what was asked, and a longer title is not thereby a worse
// one — "The Waste Lands" and "A Torre Negra" are 3 and 0 for the same query.
func queryCoverage(want []string, title string) int {
	have := map[string]bool{}
	for _, t := range searchTokens(title) {
		have[t] = true
	}
	seen := map[string]bool{}
	n := 0
	for _, t := range want {
		if seen[t] || !have[t] {
			continue
		}
		seen[t] = true
		n++
	}
	return n
}

// GetByWorkKey returns the full work record: description, subjects and cover,
// plus author names. Authors live on separate author records, not the work;
// rather than walking them one request each, one keyed search against the
// search index resolves the whole list — two polite requests total.
func (c *OpenLibrary) GetByWorkKey(ctx context.Context, key string) (Book, error) {
	key = workKey(key)
	if key == "" {
		return Book{}, fmt.Errorf("openlibrary: empty work key")
	}

	var work olWork
	if err := c.getJSON(ctx, "/works/"+url.PathEscape(key)+".json", &work); err != nil {
		return Book{}, err
	}

	var search struct {
		Docs []olSearchDoc `json:"docs"`
	}
	if err := c.getJSON(ctx, "/search.json?q=key:/works/"+url.PathEscape(key)+
		"&fields=key,author_name,first_publish_year&limit=1", &search); err != nil {
		// Author names are a nice-to-have on top of a good work record;
		// a search-index hiccup must not fail the whole lookup.
		search.Docs = nil
	}

	b := Book{
		ID:          key,
		Title:       work.Title,
		Description: string(work.Description),
		Subjects:    work.Subjects,
	}
	for _, id := range work.Covers {
		if id > 0 {
			b.CoverURL = fmt.Sprintf(openLibraryCoverURL, id)
			break
		}
	}
	if len(search.Docs) > 0 {
		b.Authors = search.Docs[0].AuthorName
		if b.FirstPublishYear == nil {
			b.FirstPublishYear = search.Docs[0].FirstPublishYear
		}
	}
	if b.FirstPublishYear == nil {
		b.FirstPublishYear = yearFrom(work.FirstPublishDate)
	}
	if encoded, err := json.Marshal(work); err == nil {
		b.Raw = encoded
	}
	return b, nil
}

// GetByISBN resolves an ISBN to its edition, then to the parent work so the
// shared work cache is filled the same way search fills it. A workless or
// unreachable work degrades to an edition-backed book rather than failing —
// the edition data is still worth caching.
func (c *OpenLibrary) GetByISBN(ctx context.Context, isbn string) (Book, error) {
	isbn = NormalizeISBN(isbn)
	if !ValidISBN(isbn) {
		return Book{}, fmt.Errorf("openlibrary: malformed ISBN %q", isbn)
	}

	var ed olEdition
	if err := c.getJSON(ctx, "/isbn/"+url.PathEscape(isbn)+".json", &ed); err != nil {
		return Book{}, err
	}

	edition := editionFromOL(ed)
	if len(ed.Works) == 0 {
		return Book{}, fmt.Errorf("openlibrary: ISBN %s has no parent work", isbn)
	}
	edition.BookID = workKey(ed.Works[0].Key)

	book, err := c.GetByWorkKey(ctx, edition.BookID)
	if err != nil {
		if !errors.Is(err, ErrBookNotFound) {
			return Book{}, err
		}
		// The edition knows enough to stand in until a work lookup succeeds.
		book = Book{
			ID:       edition.BookID,
			Title:    ed.Title,
			CoverURL: edition.CoverURL,
		}
	}
	book.Edition = &edition
	return book, nil
}

// GetEditions lists a work's printings from /works/{key}/editions.json. The
// records are the same edition shape /isbn/{isbn}.json serves, minus the
// works back-reference (the work is the thing being asked about).
func (c *OpenLibrary) GetEditions(ctx context.Context, key string) ([]BookEdition, error) {
	key = workKey(key)
	if key == "" {
		return nil, fmt.Errorf("openlibrary: empty work key")
	}

	var payload struct {
		Entries []olEdition `json:"entries"`
	}
	if err := c.getJSON(ctx, "/works/"+url.PathEscape(key)+"/editions.json", &payload); err != nil {
		return nil, err
	}

	eds := make([]BookEdition, 0, len(payload.Entries))
	for _, ed := range payload.Entries {
		out := editionFromOL(ed)
		out.BookID = key
		eds = append(eds, out)
	}
	return eds, nil
}

// editionFromOL maps a raw edition record onto the provider-agnostic shape.
func editionFromOL(ed olEdition) BookEdition {
	out := BookEdition{
		ID:        strings.TrimPrefix(ed.Key, "/books/"),
		ISBN10:    first(ed.ISBN10),
		ISBN13:    first(ed.ISBN13),
		Publisher: first(ed.Publishers),
		Binding:   ed.PhysicalFormat,
		PageCount: ed.NumberOfPages,
		Raw:       marshalOrEmpty(ed),
	}
	out.PublishedYear = yearFrom(ed.PublishDate)
	for _, id := range ed.Covers {
		if id > 0 {
			out.CoverURL = fmt.Sprintf(openLibraryCoverURL, id)
			break
		}
	}
	for _, lang := range ed.Languages {
		out.Language = strings.TrimPrefix(lang.Key, "/languages/")
		break
	}
	return out
}

// getJSON issues one rate-limited, User-Agent-identified GET and decodes the
// JSON body into dst.
func (c *OpenLibrary) getJSON(ctx context.Context, path string, dst any) error {
	if err := c.limiter.Wait(ctx); err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", openLibraryUserAgent)
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("openlibrary: request %s: %w", path, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return fmt.Errorf("openlibrary: read %s: %w", path, err)
	}
	if resp.StatusCode == http.StatusNotFound {
		return fmt.Errorf("%w: %s", ErrBookNotFound, path)
	}
	if resp.StatusCode == http.StatusUnprocessableEntity {
		return fmt.Errorf("%w: %s", ErrQueryNotAllowed, path)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("openlibrary: %s returned %d: %s", path, resp.StatusCode, truncate(string(body), 200))
	}
	if err := json.Unmarshal(body, dst); err != nil {
		return fmt.Errorf("openlibrary: decode %s: %w", path, err)
	}
	return nil
}

// workKey normalises any Open Library work reference ("/works/OL123W",
// "works/OL123W", "OL123W") to the bare "OL123W" used as the cache key.
func workKey(key string) string {
	key = strings.TrimSpace(key)
	key = strings.TrimPrefix(key, "/works/")
	key = strings.TrimPrefix(key, "works/")
	return key
}

// yearFrom pulls the first plausible year out of Open Library's freeform
// dates ("September 1st 1997", "1937-08-17", sometimes "unknown").
func yearFrom(s string) *int {
	for i := 0; i+4 <= len(s); i++ {
		candidate := s[i : i+4]
		year, err := strconv.Atoi(candidate)
		if err != nil {
			continue
		}
		if year >= 1000 && year <= 2100 {
			return &year
		}
	}
	return nil
}

func first(list []string) string {
	if len(list) == 0 {
		return ""
	}
	return list[0]
}

func marshalOrEmpty(v any) []byte {
	encoded, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	return encoded
}
