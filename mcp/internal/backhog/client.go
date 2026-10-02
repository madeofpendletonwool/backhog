// Package backhog is the MCP server's HTTP client for the backhog API.
//
// It speaks only the public API a personal token (MAD-465) can reach, and
// it issues exactly two writes: recording a quiz result (MAD-471, the one
// POST the quiz:write scope names) and importing extracted claims
// (MAD-466, the one POST the claims:write scope names). Every read path it
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
	"bytes"
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

// post writes one JSON body to an API path and decodes the answer. It
// exists for exactly the two writes the bridge carries — RecordQuizResult
// and ImportClaims — and stays that narrow on purpose: the bridge's whole
// spoiler-safety story rests on its writes being countable on one hand.
func (c *Client) post(ctx context.Context, path string, body any, out any) error {
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}
	u := *c.base
	u.Path = strings.TrimSuffix(u.Path, "/") + path
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("reaching backhog at %s: %w", c.base, err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		msg := strings.TrimSpace(string(data))
		var envelope struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(data, &envelope) == nil && envelope.Error != "" {
			msg = envelope.Error
		}
		return &APIError{Status: resp.StatusCode, Body: msg}
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(data, out)
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

// LibraryBeyondBook is a book the library search matched but would not
// show: every hit sits past the user's reading position, or the book was
// never opened. The title is the whole answer — where in the book, and
// what it says there, is exactly what the clamp holds back.
type LibraryBeyondBook struct {
	BookID string `json:"book_id"`
	Title  string `json:"title"`
}

type librarySearchResponse struct {
	Query   string             `json:"query"`
	Total   int                `json:"total"`
	Results []LibrarySearchHit `json:"results"`
	// MatchedBeyond is present on backhogs with the endpoint; absent
	// (nil) on ones that predate it.
	MatchedBeyond []LibraryBeyondBook `json:"matched_beyond"`
	Bound         Bound               `json:"bound"`
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

// NameIndexEntry is one name of a book's index (MAD-670): how often it has
// come up in what the caller may read, and the citation for where it first
// did. A name first appearing past the reading position is absent.
type NameIndexEntry struct {
	Name      string        `json:"name"`
	Mentions  int           `json:"mentions"`
	FirstSeen NameFirstSeen `json:"first_seen"`
	Hidden    bool          `json:"hidden"`
}

// NameFirstSeen cites a name's introduction: the offset, the chapter it
// sits in, and the peek link that lands a reader on it.
type NameFirstSeen struct {
	CharStart int         `json:"char_start"`
	Chapter   *ChapterRef `json:"chapter"`
	Percent   float64     `json:"percent"`
	DeepLink  string      `json:"deep_link"`
}

type namesResponse struct {
	Names []NameIndexEntry `json:"names"`
	Bound Bound            `json:"bound"`
}

// Names lists a book's name index, bounded to the caller's reading
// position by default. On a backhog that predates the endpoint it returns
// an APIError with status 404.
func (c *Client) Names(ctx context.Context, entryID string, spoilers bool) (*namesResponse, error) {
	var out namesResponse
	if err := c.get(ctx, "/api/books/"+url.PathEscape(entryID)+"/names", untilQuery(spoilers), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// SeriesSummary is one row of the series index (MAD-469): a series the
// caller's shelf holds members of, and how those members stand.
type SeriesSummary struct {
	Name     string `json:"name"`
	Books    int    `json:"books"`
	Finished int    `json:"finished"`
	Reading  int    `json:"reading"`
}

type seriesIndexResponse struct {
	Series []SeriesSummary `json:"series"`
}

// SeriesList names every series the caller's shelf holds a member of.
// On a backhog that predates the endpoint it returns an APIError with
// status 404.
func (c *Client) SeriesList(ctx context.Context) ([]SeriesSummary, error) {
	var out seriesIndexResponse
	if err := c.get(ctx, "/api/books/series", nil, &out); err != nil {
		return nil, err
	}
	return out.Series, nil
}

// SeriesPosition is the stored reading position of one series member: the
// facts a "story so far" scopes against.
type SeriesPosition struct {
	Mode       string  `json:"position_mode"`
	CharOffset int     `json:"char_offset"`
	Percent    float64 `json:"percent"`
}

// SeriesBook is one member of a series, in reading order. Finished says
// the whole book is fair game — a finished book's bound is its own end —
// while a mid-read member's position is exactly where every tool stops.
type SeriesBook struct {
	EntryID          string         `json:"entry_id"`
	BookID           string         `json:"book_id"`
	Title            string         `json:"title"`
	Authors          []string       `json:"authors"`
	Status           string         `json:"status"`
	Finished         bool           `json:"finished"`
	SeriesNumber     *float64       `json:"series_number,omitempty"`
	FirstPublishYear *int           `json:"first_publish_year,omitempty"`
	Position         SeriesPosition `json:"position"`
	DeepLink         string         `json:"deep_link"`
}

type seriesDetailResponse struct {
	Name  string       `json:"name"`
	Books []SeriesBook `json:"books"`
}

// Series lists the caller's books of one series in reading order with
// per-book status and position. On a backhog without series it returns an
// APIError with status 404.
//
// The name goes in raw: get() assigns the path to url.URL.Path, which
// renders it escaped exactly once — pre-escaping here would double the
// escape and name a series that does not exist. A series whose name
// contains a slash cannot be addressed through a path at all, which is
// the API's own limit, not this client's.
func (c *Client) Series(ctx context.Context, name string) (*seriesDetailResponse, error) {
	var out seriesDetailResponse
	if err := c.get(ctx, "/api/books/series/"+name, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// QuizResultReport is the self-reported outcome a quizzing client sends
// after grading (MAD-471): the questions it asked, the answers the reader
// got right, and the chapter span the quiz covered. Backhog stores and
// counts; it never grades or generates.
type QuizResultReport struct {
	Questions    int               `json:"questions"`
	Correct      int               `json:"correct"`
	ChapterRange *QuizChapterRange `json:"chapter_range,omitempty"`
	Source       string            `json:"source"`
}

// QuizChapterRange is the inclusive 1-based chapter span a quiz covered.
type QuizChapterRange struct {
	From int `json:"from"`
	To   int `json:"to"`
}

// QuizResultRecorded is backhog's answer to a recorded result: the stored
// row and any achievements the count tipped, for the toast.
type QuizResultRecorded struct {
	QuizResult struct {
		ID           string    `json:"id"`
		Questions    int       `json:"questions"`
		Correct      int       `json:"correct"`
		ChapterStart *int      `json:"chapter_start,omitempty"`
		ChapterEnd   *int      `json:"chapter_end,omitempty"`
		Source       string    `json:"source"`
		CreatedAt    time.Time `json:"created_at"`
	} `json:"quiz_result"`
	Achievements []QuizAchievement `json:"achievements"`
}

// QuizAchievement is one comprehension achievement the result unlocked.
type QuizAchievement struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Tier  string `json:"tier"`
}

// RecordQuizResult reports a quiz outcome over one of the two writes this
// client carries (MAD-471). Needs a token with the quiz:write scope; a
// read-only token gets the standard 403 the API hands every write,
// returned here as an APIError so the tool can say what to fix.
func (c *Client) RecordQuizResult(ctx context.Context, entryID string, report QuizResultReport) (*QuizResultRecorded, error) {
	var out QuizResultRecorded
	if err := c.post(ctx, "/api/books/"+url.PathEscape(entryID)+"/quiz-results", report, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ClaimEntityRef is the cast member a claim's triple names, resolved
// through the shelf's own entity table (MAD-466).
type ClaimEntityRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Kind string `json:"kind,omitempty"`
}

// Claim is one stored truth as the claims read serves it (MAD-466): the
// statement that holds at the request's bound, the quote that grounds it
// with its offsets and peek link, and where its truth was revealed. A
// claim absent from the answer does not exist yet — its evidence or its
// reveal sits past the reading position.
type Claim struct {
	ID            string          `json:"id"`
	Statement     string          `json:"statement"`
	Subject       *string         `json:"subject"`
	Predicate     *string         `json:"predicate"`
	Object        *string         `json:"object"`
	SubjectEntity *ClaimEntityRef `json:"subject_entity"`
	ObjectEntity  *ClaimEntityRef `json:"object_entity"`
	Quote         string          `json:"quote"`
	Provenance    Provenance      `json:"provenance"`
	RevealOffset  int             `json:"reveal_offset"`
	// Superseded says the served statement is a later version's truth,
	// not the claim's original words.
	Superseded bool   `json:"superseded"`
	Source     string `json:"source"`
	// Versions is how many later truths exist in total, revealed or not.
	Versions int `json:"versions"`
}

type claimsResponse struct {
	Claims      []Claim `json:"claims"`
	Total       int     `json:"total"`
	StaleHidden int     `json:"stale_hidden"`
	Bound       Bound   `json:"bound"`
}

// Claims lists a book's stored truths as they stand at the caller's
// position (MAD-466). Entity optionally narrows to one cast member by
// name or alias. On a backhog that predates the claims store it returns
// an APIError with status 404.
func (c *Client) Claims(ctx context.Context, entryID, entity string, spoilers bool) (*claimsResponse, error) {
	q := untilQuery(spoilers)
	if entity != "" {
		q.Set("entity", entity)
	}
	var out claimsResponse
	if err := c.get(ctx, "/api/books/"+url.PathEscape(entryID)+"/claims", q, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// EntityFirstSeen cites an entity's introduction: the earliest evidence
// among the claims that name it.
type EntityFirstSeen struct {
	CharStart int         `json:"char_start"`
	Chapter   *ChapterRef `json:"chapter"`
	Percent   float64     `json:"percent"`
	DeepLink  string      `json:"deep_link"`
}

// Entity is one member of the cast a book's claims speak about (MAD-466):
// how many readable truths name it and where its story with the reader
// began.
type Entity struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Kind      string          `json:"kind,omitempty"`
	Aliases   []string        `json:"aliases"`
	Claims    int             `json:"claims"`
	FirstSeen EntityFirstSeen `json:"first_seen"`
}

type entitiesResponse struct {
	Entities []Entity `json:"entities"`
	Bound    Bound    `json:"bound"`
}

// Entities lists the cast a book's readable claims speak about (MAD-466).
// Name optionally resolves one entity through its aliases. An entity
// whose every mention sits past the position does not exist yet.
func (c *Client) Entities(ctx context.Context, entryID, name string, spoilers bool) (*entitiesResponse, error) {
	q := untilQuery(spoilers)
	if name != "" {
		q.Set("name", name)
	}
	var out entitiesResponse
	if err := c.get(ctx, "/api/books/"+url.PathEscape(entryID)+"/entities", q, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ClaimVersionImport is one later truth of a claim: its own evidence and
// the offset where it is revealed.
type ClaimVersionImport struct {
	Statement     string `json:"statement"`
	CharStart     int    `json:"char_start"`
	CharEnd       int    `json:"char_end"`
	Quote         string `json:"quote"`
	RevealOffset  *int   `json:"reveal_offset,omitempty"`
	Source        string `json:"source,omitempty"`
	SourceVersion string `json:"source_version,omitempty"`
}

// ClaimImport is one claim of an extraction run: a statement, its optional
// triple, and the evidence that grounds it — the quote must fold to the
// canonical text at exactly [CharStart, CharEnd), or the item is rejected.
type ClaimImport struct {
	Statement     string               `json:"statement"`
	Subject       *string              `json:"subject,omitempty"`
	Predicate     *string              `json:"predicate,omitempty"`
	Object        *string              `json:"object,omitempty"`
	CharStart     int                  `json:"char_start"`
	CharEnd       int                  `json:"char_end"`
	Quote         string               `json:"quote"`
	Source        string               `json:"source,omitempty"`
	SourceVersion string               `json:"source_version,omitempty"`
	Versions      []ClaimVersionImport `json:"versions,omitempty"`
}

// EntityImport registers a person, place or thing the claims are about,
// with the aliases it answers to.
type EntityImport struct {
	Name    string   `json:"name"`
	Kind    string   `json:"kind,omitempty"`
	Aliases []string `json:"aliases,omitempty"`
}

// ClaimsBatch is one extraction run's report.
type ClaimsBatch struct {
	Source        string         `json:"source"`
	SourceVersion string         `json:"source_version,omitempty"`
	Claims        []ClaimImport  `json:"claims"`
	Entities      []EntityImport `json:"entities,omitempty"`
}

// ClaimImportResult is one item's verdict: stored (with its id) or
// rejected with the reason — a rejection is the cite-or-drop door saying
// the quote did not check out.
type ClaimImportResult struct {
	Index   int    `json:"index"`
	Status  string `json:"status"`
	ClaimID string `json:"claim_id,omitempty"`
	Error   string `json:"error,omitempty"`
}

// ClaimsImported is the import's answer: what landed, what was rejected,
// and why per item.
type ClaimsImported struct {
	Stored   int                 `json:"stored"`
	Entities int                 `json:"entities"`
	Rejected int                 `json:"rejected"`
	Results  []ClaimImportResult `json:"results"`
}

// ImportClaims writes one extraction run through the claims import
// (MAD-466) — the second and last write this client carries. Every item
// passes the API's deterministic cite-or-drop check; needs a token with
// the claims:write scope, and a read-only token gets the 403 returned
// here as an APIError.
func (c *Client) ImportClaims(ctx context.Context, entryID string, batch ClaimsBatch) (*ClaimsImported, error) {
	var out ClaimsImported
	if err := c.post(ctx, "/api/books/"+url.PathEscape(entryID)+"/claims", batch, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
