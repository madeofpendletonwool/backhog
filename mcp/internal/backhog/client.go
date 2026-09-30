// Package backhog is the MCP server's HTTP client for the backhog API.
//
// It speaks only the public API a personal token (MAD-465) can reach and
// only ever issues GETs: the token is read-only by construction, so the
// bridge cannot mutate a library even if a tool tried. Every read path it
// touches clamps itself to the caller's reading position under the token
// default `until=position` (MAD-467), which is where the spoiler safety of
// every tool result comes from — this client adds `until=none` only when a
// caller explicitly asks for spoilers.
//
// The JSON field names mirror the API's response bodies exactly; nothing is
// renamed on the way through, so a shape change in the API shows up here as
// a decode failure rather than a silent blank.
package backhog

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Client is a configured connection to one backhog instance. It is safe for
// concurrent use.
type Client struct {
	base  *url.URL // origin only, no path — API calls append /api/...
	token string
	http  *http.Client
}

// New builds a client for the backhog at rawBase (the origin a browser
// opens — nginx serves the SPA there and proxies /api to the API), holding
// the given personal API token. A trailing slash and an accidental /api
// suffix are tolerated so both "http://backhog:8080" and
// "http://backhog:8080/api/" mean the same thing.
func New(rawBase, token string) (*Client, error) {
	if strings.TrimSpace(rawBase) == "" {
		return nil, fmt.Errorf("BACKHOG_URL is not set — point it at your backhog instance, e.g. http://backhog:8080")
	}
	if strings.TrimSpace(token) == "" {
		return nil, fmt.Errorf("BACKHOG_TOKEN is not set — mint a personal token in Settings → API tokens and pass it as BACKHOG_TOKEN")
	}
	u, err := url.Parse(strings.TrimRight(strings.TrimSpace(rawBase), "/"))
	if err != nil || u.Scheme == "" || u.Host == "" {
		return nil, fmt.Errorf("BACKHOG_URL %q is not a valid URL", rawBase)
	}
	u.Path = strings.TrimSuffix(u.Path, "/api")
	u.RawQuery, u.Fragment = "", ""
	return &Client{base: u, token: token, http: &http.Client{Timeout: 30 * time.Second}}, nil
}

// BaseURL returns the origin this client talks to, without a trailing
// slash — the default base for deep links when no public URL is set.
func (c *Client) BaseURL() string { return c.base.String() }

// APIError carries the status a backhog answer came back with, so tools can
// tell "your token expired" from "this backhog predates that endpoint".
type APIError struct {
	Status int
	Body   string
}

func (e *APIError) Error() string {
	msg := e.Body
	if msg == "" {
		msg = http.StatusText(e.Status)
	}
	return fmt.Sprintf("backhog answered %d: %s", e.Status, msg)
}

// get fetches one API path (already starting with /api/) and decodes its
// JSON body into out.
func (c *Client) get(ctx context.Context, path string, query url.Values, out any) error {
	u := *c.base
	u.Path = strings.TrimSuffix(u.Path, "/") + path
	u.RawQuery = query.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("reaching backhog at %s: %w", c.base, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		body := strings.TrimSpace(string(raw))
		var envelope struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(raw, &envelope) == nil && envelope.Error != "" {
			body = envelope.Error
		}
		return &APIError{Status: resp.StatusCode, Body: body}
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(raw, out)
}

// untilQuery renders the spoiler opt-in the read paths understand: an empty
// value inherits the token default (the caller's reading position), "none"
// asks for the whole book out loud.
func untilQuery(spoilers bool) url.Values {
	q := url.Values{}
	if spoilers {
		q.Set("until", "none")
	}
	return q
}

// Bound is the effective clamp a backhog read echoed back: where it stopped
// serving text, as the "as of" a cited answer stands on. Chapter is null
// when nothing was cut or the book has no parsed spine on that path.
type Bound struct {
	Until      string      `json:"until"`
	CharOffset int         `json:"char_offset"`
	Chapter    *ChapterRef `json:"chapter"`
	Percent    float64     `json:"percent"`
}

// ChapterRef locates a point in a book's spine: which chapter, what it is
// called, and the canonical range it occupies.
type ChapterRef struct {
	SpineIndex  int    `json:"spine_index"`
	Title       string `json:"title"`
	TitleSource string `json:"title_source"`
	Number      int    `json:"number"`
	Href        string `json:"href"`
	CharStart   int    `json:"char_start"`
	CharEnd     int    `json:"char_end"`
}

// LibraryEntry is one row of list_books: a book in the caller's library
// with its reading status and stored progress.
type LibraryEntry struct {
	ID   string `json:"id"`
	Book struct {
		ID        string   `json:"id"`
		Title     string   `json:"title"`
		Authors   []string `json:"authors"`
		FirstYear *int     `json:"first_publish_year"`
	} `json:"book"`
	Status          string     `json:"status"`
	ProgressPercent *float64   `json:"progress_percent"`
	LastReadAt      *time.Time `json:"last_read_at"`
}

type libraryResponse struct {
	Entries []LibraryEntry `json:"entries"`
	Total   int            `json:"total"`
}

// ListBooks returns the caller's book library, optionally filtered by
// status (backlog/playing/played/dropped/ignored/wishlist).
func (c *Client) ListBooks(ctx context.Context, status string, limit, offset int) ([]LibraryEntry, int, error) {
	q := url.Values{}
	q.Set("media", "book")
	if status != "" {
		q.Set("status", status)
	}
	if limit > 0 {
		q.Set("limit", strconv.Itoa(limit))
	}
	if offset > 0 {
		q.Set("offset", strconv.Itoa(offset))
	}
	var out libraryResponse
	if err := c.get(ctx, "/api/library", q, &out); err != nil {
		return nil, 0, err
	}
	return out.Entries, out.Total, nil
}

// Position is one book's reading position in every space the API can
// express. CharOffset is the stored truth the spoiler clamp resolves to.
type Position struct {
	PositionMode string      `json:"position_mode"`
	CharOffset   int         `json:"char_offset"`
	Source       string      `json:"source"`
	Percent      float64     `json:"percent"`
	CharCount    int         `json:"char_count"`
	Chapter      *ChapterRef `json:"chapter"`
	UpdatedAt    *time.Time  `json:"updated_at"`
}

// ReadingPosition returns where the caller stands in one library entry.
func (c *Client) ReadingPosition(ctx context.Context, entryID string) (*Position, error) {
	var out Position
	if err := c.get(ctx, "/api/books/"+url.PathEscape(entryID)+"/position", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Chapter is one row of the chapters payload. A locked chapter starts at or
// past the request's bound: it exists and can be counted, but only its
// title (and number) survive.
type Chapter struct {
	SpineIndex int    `json:"spine_index"`
	Title      string `json:"title"`
	Number     int    `json:"number"`
	CharStart  int    `json:"char_start"`
	CharEnd    int    `json:"char_end"`
	// Blocks holds each display block's canonical start offset, in display
	// order — the map from prose back to the offset space. Nil on a locked
	// chapter.
	Blocks []int `json:"blocks"`
	Locked bool  `json:"locked"`
}

type chaptersResponse struct {
	CharCount int       `json:"char_count"`
	Chapters  []Chapter `json:"chapters"`
	Bound     Bound     `json:"bound"`
}

// Chapters returns a book's spine index. Without spoilers the request
// inherits the position clamp: chapters past the bound come back locked.
func (c *Client) Chapters(ctx context.Context, entryID string, spoilers bool) ([]Chapter, int, Bound, error) {
	var out chaptersResponse
	if err := c.get(ctx, "/api/books/"+url.PathEscape(entryID)+"/text/chapters", untilQuery(spoilers), &out); err != nil {
		return nil, 0, Bound{}, err
	}
	return out.Chapters, out.CharCount, out.Bound, nil
}

// Provenance is the citation a text slice answers with: the chapter its
// start sits in and the peek link to it.
type Provenance struct {
	BookID    string      `json:"book_id"`
	Chapter   *ChapterRef `json:"chapter"`
	CharStart int         `json:"char_start"`
	CharEnd   int         `json:"char_end"`
	DeepLink  string      `json:"deep_link"`
}

// TextSlice is a ranged read of the canonical text: the exact bytes at
// [From, To) of the folded, offset-addressable text every citation and
// search hit refers to.
type TextSlice struct {
	From       int        `json:"from"`
	To         int        `json:"to"`
	CharCount  int        `json:"char_count"`
	Text       string     `json:"text"`
	Bound      Bound      `json:"bound"`
	Provenance Provenance `json:"provenance"`
}

// Text reads the canonical text between two offsets. Without spoilers the
// slice is truncated at the caller's position; a range starting past it
// comes back empty.
func (c *Client) Text(ctx context.Context, entryID string, from, to int, spoilers bool) (*TextSlice, error) {
	q := untilQuery(spoilers)
	if from > 0 {
		q.Set("from", strconv.Itoa(from))
	}
	if to > 0 {
		q.Set("to", strconv.Itoa(to))
	}
	var out TextSlice
	if err := c.get(ctx, "/api/books/"+url.PathEscape(entryID)+"/text", q, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DisplayDoc is one spine document as prose: the book's own characters, in
// blocks whose canonical start offsets the chapters payload carries. A
// clamped request stops the prose at the bound, cutting a straddling block
// where the reading stopped.
type DisplayDoc struct {
	SpineIndex int      `json:"spine_index"`
	Blocks     []string `json:"blocks"`
	Bound      Bound    `json:"bound"`
}

// Display reads one spine document's prose.
func (c *Client) Display(ctx context.Context, entryID string, spine int, spoilers bool) (*DisplayDoc, error) {
	q := untilQuery(spoilers)
	q.Set("spine", strconv.Itoa(spine))
	var out DisplayDoc
	if err := c.get(ctx, "/api/books/"+url.PathEscape(entryID)+"/text/display", q, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Snippet is a match as the book prints it, split for highlighting.
type Snippet struct {
	Before  string `json:"before"`
	Passage string `json:"passage"`
	After   string `json:"after"`
}

// SearchHit is one match in a book's text, already placed: the chapter it
// sits in, the printed page, the second of the audiobook, and the peek link
// that jumps a reader to it.
type SearchHit struct {
	CharOffset int         `json:"char_offset"`
	CharEnd    int         `json:"char_end"`
	Percent    float64     `json:"percent"`
	Context    Snippet     `json:"context"`
	Chapter    *ChapterRef `json:"chapter"`
	BookID     string      `json:"book_id"`
	DeepLink   string      `json:"deep_link"`
}

type searchResponse struct {
	Query     string      `json:"query"`
	Axis      string      `json:"axis"`
	Mode      string      `json:"mode"`
	Total     int         `json:"total"`
	Truncated bool        `json:"truncated"`
	Results   []SearchHit `json:"results"`
	Bound     Bound       `json:"bound"`
}

// SearchBook finds a phrase inside one book. Without spoilers every hit
// past the caller's position is dropped before the answer is built.
func (c *Client) SearchBook(ctx context.Context, entryID, query string, limit int, spoilers bool) (*searchResponse, error) {
	q := untilQuery(spoilers)
	q.Set("q", query)
	if limit > 0 {
		q.Set("limit", strconv.Itoa(limit))
	}
	var out searchResponse
	if err := c.get(ctx, "/api/books/"+url.PathEscape(entryID)+"/search", q, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// LibrarySearchHit is the shape the library-wide search (MAD-470) answers
// with: a ranked hit placed in its book.
type LibrarySearchHit struct {
	BookID    string      `json:"book_id"`
	Title     string      `json:"title"`
	Chapter   *ChapterRef `json:"chapter"`
	Snippet   Snippet     `json:"snippet"`
	CharStart int         `json:"char_start"`
	CharEnd   int         `json:"char_end"`
	DeepLink  string      `json:"deep_link"`
}

type librarySearchResponse struct {
	Query   string             `json:"query"`
	Total   int                `json:"total"`
	Results []LibrarySearchHit `json:"results"`
	Bound   Bound              `json:"bound"`
}

// SearchLibrary searches every book the caller can read (MAD-470). On a
// backhog that predates the endpoint it returns an APIError with status
// 404; the tool turns that into an actionable message.
func (c *Client) SearchLibrary(ctx context.Context, query string, limit int, spoilers bool) (*librarySearchResponse, error) {
	q := untilQuery(spoilers)
	q.Set("q", query)
	if limit > 0 {
		q.Set("limit", strconv.Itoa(limit))
	}
	var out librarySearchResponse
	if err := c.get(ctx, "/api/books/search/text", q, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// MentionHit is one occurrence of a name (MAD-670): where it appears, with
// a snippet and the peek link to it.
type MentionHit struct {
	CharOffset int         `json:"char_offset"`
	CharEnd    int         `json:"char_end"`
	Snippet    Snippet     `json:"snippet"`
	Chapter    *ChapterRef `json:"chapter"`
	DeepLink   string      `json:"deep_link"`
}

type mentionsResponse struct {
	Name    string       `json:"name"`
	Total   int          `json:"total"`
	Results []MentionHit `json:"results"`
	Bound   Bound        `json:"bound"`
}

// FindMentions lists where a name has come up so far (MAD-670). On a
// backhog that predates the endpoint it returns an APIError with status
// 404; the tool turns that into an actionable message.
func (c *Client) FindMentions(ctx context.Context, entryID, name string, spoilers bool) (*mentionsResponse, error) {
	q := untilQuery(spoilers)
	q.Set("name", name)
	var out mentionsResponse
	if err := c.get(ctx, "/api/books/"+url.PathEscape(entryID)+"/mentions", q, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
