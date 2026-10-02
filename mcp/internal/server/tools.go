package server

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/collinpendleton/backhog/mcp/internal/backhog"
)

// linker absolutizes the API's relative peek links against the origin the
// human opens, so a citation is clickable wherever the model's answer ends
// up. API calls may go to an internal address while links stay public.
type linker struct{ base string }

func newLinker(opts Options) *linker {
	base := strings.TrimRight(opts.PublicURL, "/")
	if base == "" && opts.Client != nil {
		base = opts.Client.BaseURL()
	}
	return &linker{base: base}
}

// peek builds the absolute peek deep link for an offset: a jump into the
// reader that never moves the saved position.
func (l *linker) peek(entryID string, offset int) string {
	return l.base + "/books/" + url.PathEscape(entryID) + "/read?offset=" + strconv.Itoa(offset) + "&peek=1"
}

// relative keeps whatever the API already returned when it is absolute, and
// absolutizes the relative form the read paths use.
func (l *linker) relative(link string) string {
	if link == "" || strings.HasPrefix(link, "http://") || strings.HasPrefix(link, "https://") {
		return link
	}
	return l.base + link
}

// addTools registers the backhog tool surface. Every description carries
// the two rules the whole bridge exists to enforce: cite with the deep
// links, and let "the book doesn't say (so far)" be an answer.
func addTools(srv *mcp.Server, c *backhog.Client, link *linker) {
	addListBooks(srv, c, link)
	addReadingPosition(srv, c, link)
	addListChapters(srv, c, link)
	addReadText(srv, c, link)
	addSearchBook(srv, c, link)
	addSearchLibrary(srv, c, link)
	addGetPassage(srv, c, link)
	addFindMentions(srv, c, link)
	addListNames(srv, c, link)
	addListSeries(srv, c, link)
	addGetSeries(srv, c, link)
	addRecordQuizResult(srv, c)
}

// The spoiler opt-in every read tool shares: the parameter's jsonschema
// description carries the loud warning (struct tags are literals, so it is
// spelled out per field).

// --- list_books ---------------------------------------------------------

type listBooksIn struct {
	Status string `json:"status,omitempty" jsonschema:"optional filter: one of backlog, playing (reading), played (finished), dropped, ignored, wishlist"`
	Limit  int    `json:"limit,omitempty" jsonschema:"optional page size, default 50, max 200"`
	Offset int    `json:"offset,omitempty" jsonschema:"optional page offset for paging past the limit"`
}

type bookSummary struct {
	EntryID    string   `json:"entry_id"`
	Title      string   `json:"title"`
	Authors    []string `json:"authors,omitempty"`
	Status     string   `json:"status"`
	Percent    *float64 `json:"progress_percent,omitempty"`
	LastReadAt string   `json:"last_read_at,omitempty"`
}

type listBooksOut struct {
	Books []bookSummary `json:"books"`
	Total int           `json:"total"`
	Note  string        `json:"note,omitempty"`
}

func addListBooks(srv *mcp.Server, c *backhog.Client, link *linker) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "list_books",
		Annotations: readOnly,
		Description: "List the books in the user's backhog library with reading status and how far " +
			"they have gotten (progress percent). Use this first to discover book entry IDs for the " +
			"other tools. 'playing' means currently reading; 'played' means finished.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in listBooksIn) (*mcp.CallToolResult, listBooksOut, error) {
		limit := in.Limit
		if limit <= 0 {
			limit = 50
		}
		if limit > 200 {
			limit = 200
		}
		entries, total, err := c.ListBooks(ctx, in.Status, limit, in.Offset)
		if err != nil {
			return nil, listBooksOut{}, fmt.Errorf("listing library: %w", err)
		}
		out := listBooksOut{Total: total, Books: make([]bookSummary, 0, len(entries))}
		for _, e := range entries {
			b := bookSummary{
				EntryID: e.ID, Title: e.Book.Title, Authors: e.Book.Authors,
				Status: e.Status, Percent: e.ProgressPercent,
			}
			if e.LastReadAt != nil {
				b.LastReadAt = e.LastReadAt.Format("2006-01-02")
			}
			out.Books = append(out.Books, b)
		}
		return nil, out, nil
	})
}

// --- get_reading_position ----------------------------------------------

type positionIn struct {
	Book string `json:"book" jsonschema:"the book's backhog entry ID (from list_books)"`
}

type readingPositionOut struct {
	backhog.Position
	DeepLink string `json:"deep_link"`
}

func addReadingPosition(srv *mcp.Server, c *backhog.Client, link *linker) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "get_reading_position",
		Annotations: readOnly,
		Description: "Where the user currently is in a book: chapter, percent, and character offset. " +
			"Every other tool serves text only up to this point, so this is the 'as of' any answer " +
			"about the book stands on. Cite it when saying where the user is.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in positionIn) (*mcp.CallToolResult, readingPositionOut, error) {
		pos, err := c.ReadingPosition(ctx, in.Book)
		if err != nil {
			return nil, readingPositionOut{}, fmt.Errorf("reading position: %w", err)
		}
		return nil, readingPositionOut{Position: *pos, DeepLink: link.peek(in.Book, pos.CharOffset)}, nil
	})
}

// --- list_chapters -------------------------------------------------------

type listChaptersIn struct {
	Book            string `json:"book" jsonschema:"the book's backhog entry ID (from list_books)"`
	IncludeSpoilers bool   `json:"include_spoilers,omitempty" jsonschema:"default false. SPOILERS: true reads past the user's saved reading position (the whole book). Only set it when the user has explicitly asked to see beyond where they have read; false bounds results to what they have actually read."`
}

type chapterOut struct {
	Number    int    `json:"number"`
	Title     string `json:"title"`
	Locked    bool   `json:"locked"`
	CharStart int    `json:"char_start"`
	CharEnd   int    `json:"char_end"`
}

type listChaptersOut struct {
	Chapters  []chapterOut  `json:"chapters"`
	CharCount int           `json:"char_count"`
	Bound     backhog.Bound `json:"bound"`
	Note      string        `json:"note,omitempty"`
}

func addListChapters(srv *mcp.Server, c *backhog.Client, link *linker) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "list_chapters",
		Annotations: readOnly,
		Description: "List a book's chapters. Chapters at or past the user's reading position are " +
			"locked: only their number and title survive (a title can itself spoil, so treat locked " +
			"titles cautiously when quoting them back). Bound says where the reading has reached.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in listChaptersIn) (*mcp.CallToolResult, listChaptersOut, error) {
		chapters, charCount, bound, err := c.Chapters(ctx, in.Book, in.IncludeSpoilers)
		if err != nil {
			return nil, listChaptersOut{}, fmt.Errorf("listing chapters: %w", err)
		}
		out := listChaptersOut{CharCount: charCount, Bound: bound, Chapters: make([]chapterOut, 0, len(chapters))}
		for _, ch := range chapters {
			out.Chapters = append(out.Chapters, chapterOut{
				Number: ch.Number, Title: ch.Title, Locked: ch.Locked,
				CharStart: ch.CharStart, CharEnd: ch.CharEnd,
			})
		}
		if !in.IncludeSpoilers {
			out.Note = "Chapters marked locked are past the reading position; their content is withheld."
		}
		return nil, out, nil
	})
}

// --- read_text -----------------------------------------------------------

// Pagination caps: enough prose for a real answer, never enough to blow a
// context window. The model pages forward with next_from.
const (
	readDefaultChars = 12000
	readMinChars     = 1000
	readMaxChars     = 24000
)

type readTextIn struct {
	Book            string `json:"book" jsonschema:"the book's backhog entry ID (from list_books)"`
	From            int    `json:"from,omitempty" jsonschema:"optional character offset to start at (from a previous result's next_from); default the beginning"`
	To              int    `json:"to,omitempty" jsonschema:"optional character offset to stop before; default the end of the readable window"`
	Chapter         int    `json:"chapter,omitempty" jsonschema:"optional 1-based chapter number (from list_chapters) to read instead of a from/to range"`
	MaxChars        int    `json:"max_chars,omitempty" jsonschema:"optional cap on characters returned this call, 1000-24000, default 12000; page with next_from"`
	IncludeSpoilers bool   `json:"include_spoilers,omitempty" jsonschema:"default false. SPOILERS: true reads past the user's saved reading position (the whole book). Only set it when the user has explicitly asked to see beyond where they have read; false bounds results to what they have actually read."`
}

type readTextOut struct {
	// Text is the book's prose (the display text, capitals and punctuation
	// intact), not the folded canonical form offsets address.
	Text     string              `json:"text"`
	From     int                 `json:"from"`
	To       int                 `json:"to"`
	NextFrom *int                `json:"next_from,omitempty"`
	Chapter  *backhog.ChapterRef `json:"chapter,omitempty"`
	Bound    backhog.Bound       `json:"bound"`
	DeepLink string              `json:"deep_link"`
	Note     string              `json:"note,omitempty"`
}

func addReadText(srv *mcp.Server, c *backhog.Client, link *linker) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "read_text",
		Annotations: readOnly,
		Description: "Read a book's text as prose, bounded to what the user has actually read: one " +
			"call returns up to about one chapter (capped by max_chars); page forward with next_from. " +
			"Reads are always cut at the user's reading position unless include_spoilers is set. " +
			"Cite passages using the deep_link and character offsets in the result. If the text stops " +
			"short (next_from absent before the book's end), the rest is past the reading position: " +
			"say the book doesn't say yet rather than filling in from outside knowledge.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in readTextIn) (*mcp.CallToolResult, readTextOut, error) {
		return readText(ctx, c, link, in)
	})
}

func readText(ctx context.Context, c *backhog.Client, link *linker, in readTextIn) (*mcp.CallToolResult, readTextOut, error) {
	chapters, charCount, bound, err := c.Chapters(ctx, in.Book, in.IncludeSpoilers)
	if err != nil {
		return nil, readTextOut{}, fmt.Errorf("reading chapter index: %w", err)
	}
	if len(chapters) == 0 {
		return nil, readTextOut{}, fmt.Errorf("this book has no parsed text to read")
	}

	maxChars := in.MaxChars
	if maxChars <= 0 {
		maxChars = readDefaultChars
	}
	if maxChars < readMinChars {
		maxChars = readMinChars
	}
	if maxChars > readMaxChars {
		maxChars = readMaxChars
	}

	// Resolve the window: a chapter number names its own range; otherwise
	// from/to, defaulting to the whole (readable) book.
	start := in.From
	end := 0
	if in.Chapter > 0 {
		var ch *backhog.Chapter
		for i := range chapters {
			if chapters[i].Number == in.Chapter {
				ch = &chapters[i]
				break
			}
		}
		if ch == nil {
			return nil, readTextOut{}, fmt.Errorf("no chapter number %d — use list_chapters to see this book's chapters", in.Chapter)
		}
		start, end = ch.CharStart, ch.CharEnd
		if in.From > start {
			start = in.From
		}
		if in.To > 0 && in.To < end {
			end = in.To
		}
	} else if in.To > 0 {
		end = in.To
	}
	if end <= 0 || end > charCount {
		end = charCount
	}
	if start < 0 {
		start = 0
	}

	// The readable window never passes the echoed bound (which equals
	// charCount on an unclamped read).
	readableEnd := bound.CharOffset
	if readableEnd <= 0 || readableEnd > charCount {
		readableEnd = charCount
	}

	out := readTextOut{Bound: bound, From: start, DeepLink: link.peek(in.Book, start)}
	if start >= end {
		return nil, out, fmt.Errorf("the requested range [%d,%d) is empty or inverted — character offsets run 0 to %d", start, end, charCount)
	}
	if start >= readableEnd {
		out.Note = "Nothing to read: this range starts at or past the user's reading position " +
			"(bound.char_offset). The book does not say anything here yet."
		return nil, out, nil
	}
	if end > readableEnd {
		end = readableEnd
	}

	// Walk the chapters the window overlaps, emitting the display blocks
	// whose canonical starts fall inside it, until the cap or the window
	// runs out. A multi-document chapter exposes canonical starts only for
	// its first document: its unanchored tail is skipped (and said so)
	// rather than cited to offsets nobody can verify.
	var text []string
	emitted, stopAt, note := 0, -1, ""
	for i := range chapters {
		ch := &chapters[i]
		if ch.Locked || ch.CharEnd <= start {
			continue
		}
		if ch.CharStart >= end {
			break
		}
		doc, err := c.Display(ctx, in.Book, ch.SpineIndex, in.IncludeSpoilers)
		if err != nil {
			return nil, readTextOut{}, fmt.Errorf("reading chapter %d: %w", ch.Number, err)
		}
		if out.Chapter == nil && ch.Number > 0 {
			ref := backhog.ChapterRef{SpineIndex: ch.SpineIndex, Title: ch.Title,
				Number: ch.Number, CharStart: ch.CharStart, CharEnd: ch.CharEnd}
			out.Chapter = &ref
		}
		starts := ch.Blocks
		for b, block := range doc.Blocks {
			if b >= len(starts) {
				note = "A chapter spanning multiple documents had its unanchored tail skipped to keep citations exact."
				if i+1 < len(chapters) && !chapters[i+1].Locked {
					stopAt = chapters[i+1].CharStart
				}
				break
			}
			s := starts[b]
			if s < start {
				continue
			}
			if s >= end {
				break
			}
			if emitted > 0 && emitted+len(block) > maxChars {
				stopAt = s
				break
			}
			text = append(text, block)
			emitted += len(block)
		}
		if stopAt >= 0 {
			break
		}
	}

	out.Text = strings.Join(text, "\n\n")
	if stopAt >= 0 && stopAt < readableEnd {
		out.NextFrom = &stopAt
	}
	// To is where this result stops on the canonical axis: the next
	// chapter/block boundary, or the window's end. Prose runs a little
	// longer than its canonical form, so To is a boundary, not a byte
	// count of Text.
	if out.NextFrom != nil {
		out.To = *out.NextFrom
	} else {
		out.To = min(end, readableEnd)
	}
	out.Note = note
	if out.NextFrom == nil && note == "" && end < charCount && end == readableEnd {
		out.Note = "End of the readable window: the rest of the book is past the user's reading position."
	}
	return nil, out, nil
}

// --- search_book ---------------------------------------------------------

type searchBookIn struct {
	Book            string `json:"book" jsonschema:"the book's backhog entry ID (from list_books)"`
	Query           string `json:"query" jsonschema:"the phrase or words to find, as the book prints them"`
	Limit           int    `json:"limit,omitempty" jsonschema:"optional max hits, default 10, max 50"`
	IncludeSpoilers bool   `json:"include_spoilers,omitempty" jsonschema:"default false. SPOILERS: true reads past the user's saved reading position (the whole book). Only set it when the user has explicitly asked to see beyond where they have read; false bounds results to what they have actually read."`
}

type searchHitOut struct {
	CharOffset int                 `json:"char_offset"`
	CharEnd    int                 `json:"char_end"`
	Chapter    *backhog.ChapterRef `json:"chapter,omitempty"`
	Context    backhog.Snippet     `json:"context"`
	Percent    float64             `json:"percent"`
	DeepLink   string              `json:"deep_link"`
}

type searchBookOut struct {
	Query     string         `json:"query"`
	Mode      string         `json:"mode"`
	Total     int            `json:"total"`
	Truncated bool           `json:"truncated"`
	Hits      []searchHitOut `json:"hits"`
	Bound     backhog.Bound  `json:"bound"`
	Note      string         `json:"note,omitempty"`
}

func addSearchBook(srv *mcp.Server, c *backhog.Client, link *linker) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "search_book",
		Annotations: readOnly,
		Description: "Find a phrase inside one book, getting back where it appears with an exact " +
			"quote (context) and a deep link for citation. Mode 'phrase' means the book contains the " +
			"exact words; 'loose' means these are the closest passages. Hits past the user's reading " +
			"position are dropped unless include_spoilers is set — an empty result means the book " +
			"does not say it (so far), not that it never does.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in searchBookIn) (*mcp.CallToolResult, searchBookOut, error) {
		limit := in.Limit
		if limit <= 0 {
			limit = 10
		}
		if limit > 50 {
			limit = 50
		}
		res, err := c.SearchBook(ctx, in.Book, in.Query, limit, in.IncludeSpoilers)
		if err != nil {
			return nil, searchBookOut{}, fmt.Errorf("searching book: %w", err)
		}
		out := searchBookOut{
			Query: res.Query, Mode: res.Mode, Total: res.Total,
			Truncated: res.Truncated, Bound: res.Bound,
			Hits: make([]searchHitOut, 0, len(res.Results)),
		}
		for _, h := range res.Results {
			out.Hits = append(out.Hits, searchHitOut{
				CharOffset: h.CharOffset, CharEnd: h.CharEnd, Chapter: h.Chapter,
				Context: h.Context, Percent: h.Percent,
				DeepLink: link.relative(h.DeepLink),
			})
		}
		if !in.IncludeSpoilers {
			out.Note = "Hits are bounded to the user's reading position (see bound)."
		}
		return nil, out, nil
	})
}

// --- search_library ------------------------------------------------------

type searchLibraryIn struct {
	Query           string `json:"query" jsonschema:"the phrase to find across every book the user can read"`
	Limit           int    `json:"limit,omitempty" jsonschema:"optional max hits, default 20"`
	IncludeSpoilers bool   `json:"include_spoilers,omitempty" jsonschema:"default false. SPOILERS: true reads past the user's saved reading position (the whole book). Only set it when the user has explicitly asked to see beyond where they have read; false bounds results to what they have actually read."`
}

type libraryHitOut struct {
	BookID    string              `json:"book_id"`
	Title     string              `json:"title,omitempty"`
	Chapter   *backhog.ChapterRef `json:"chapter,omitempty"`
	Snippet   backhog.Snippet     `json:"snippet"`
	CharStart int                 `json:"char_start"`
	CharEnd   int                 `json:"char_end"`
	DeepLink  string              `json:"deep_link"`
}

type searchLibraryOut struct {
	Query string          `json:"query"`
	Total int             `json:"total"`
	Hits  []libraryHitOut `json:"hits"`
	// Books that matched with every hit withheld by the spoiler clamp —
	// titles only. Tell the user these books contain the phrase but say
	// nothing about where; that is the honest "the library knows, you
	// haven't read that far yet".
	MatchedBeyond []backhog.LibraryBeyondBook `json:"matched_beyond,omitempty"`
	Bound         backhog.Bound               `json:"bound"`
}

func addSearchLibrary(srv *mcp.Server, c *backhog.Client, link *linker) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "search_library",
		Annotations: readOnly,
		Description: "Search every book in the user's library at once and get ranked hits with exact " +
			"quotes and deep links. Hits are suppressed past the user's reading position in each book " +
			"unless include_spoilers is set. Needs a backhog with library-wide search; if this backhog " +
			"predates it, use search_book per book instead.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in searchLibraryIn) (*mcp.CallToolResult, searchLibraryOut, error) {
		limit := in.Limit
		if limit <= 0 {
			limit = 20
		}
		res, err := c.SearchLibrary(ctx, in.Query, limit, in.IncludeSpoilers)
		var apiErr *backhog.APIError
		if err != nil {
			if asAPIErr(err, &apiErr) && apiErr.Status == 404 {
				return nil, searchLibraryOut{}, fmt.Errorf("this backhog does not have library-wide search yet " +
					"(it lands with backhog's library-search update); search one book at a time with search_book instead")
			}
			return nil, searchLibraryOut{}, fmt.Errorf("searching library: %w", err)
		}
		out := searchLibraryOut{Query: res.Query, Total: res.Total, Bound: res.Bound,
			Hits: make([]libraryHitOut, 0, len(res.Results))}
		for _, h := range res.Results {
			out.Hits = append(out.Hits, libraryHitOut{
				BookID: h.BookID, Title: h.Title, Chapter: h.Chapter, Snippet: h.Snippet,
				CharStart: h.CharStart, CharEnd: h.CharEnd, DeepLink: link.relative(h.DeepLink),
			})
		}
		out.MatchedBeyond = res.MatchedBeyond
		return nil, out, nil
	})
}

// --- get_passage ---------------------------------------------------------

type getPassageIn struct {
	Book            string `json:"book" jsonschema:"the book's backhog entry ID (from list_books)" jsonschema:"the book's backhog entry ID (from list_books)"`
	CharStart       int    `json:"char_start" jsonschema:"character offset the passage starts at (inclusive)"`
	CharEnd         int    `json:"char_end" jsonschema:"character offset the passage ends at (exclusive)"`
	IncludeSpoilers bool   `json:"include_spoilers,omitempty" jsonschema:"default false. SPOILERS: true reads past the user's saved reading position (the whole book). Only set it when the user has explicitly asked to see beyond where they have read; false bounds results to what they have actually read."`
}

type getPassageOut struct {
	// Text is the canonical form at exactly [char_start, char_end): the
	// normalized text (lowercased, punctuation folded) every offset in
	// backhog addresses. It is exact and verifiable, not the book's prose;
	// a search hit's context carries the prose rendering.
	Text      string              `json:"text"`
	CharStart int                 `json:"char_start"`
	CharEnd   int                 `json:"char_end"`
	CharCount int                 `json:"char_count"`
	Chapter   *backhog.ChapterRef `json:"chapter,omitempty"`
	Bound     backhog.Bound       `json:"bound"`
	DeepLink  string              `json:"deep_link"`
	Note      string              `json:"note,omitempty"`
}

func addGetPassage(srv *mcp.Server, c *backhog.Client, link *linker) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "get_passage",
		Annotations: readOnly,
		Description: "Fetch the exact text at a character range — the verifiable quote behind a " +
			"citation. Use it to check a quote before attributing it to the book. Note the text is " +
			"backhog's canonical form (lowercased, punctuation folded); the search tools' context " +
			"snippets show the same words as printed. A range past the reading position comes back " +
			"empty unless include_spoilers is set: that is the spoiler clamp, not a missing passage.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in getPassageIn) (*mcp.CallToolResult, getPassageOut, error) {
		if in.CharStart < 0 || in.CharEnd <= in.CharStart {
			return nil, getPassageOut{}, fmt.Errorf("char_start must be >= 0 and char_end must be greater than char_start")
		}
		slice, err := c.Text(ctx, in.Book, in.CharStart, in.CharEnd, in.IncludeSpoilers)
		if err != nil {
			return nil, getPassageOut{}, fmt.Errorf("reading passage: %w", err)
		}
		out := getPassageOut{
			Text: slice.Text, CharStart: slice.From, CharEnd: slice.To,
			CharCount: slice.CharCount, Chapter: slice.Provenance.Chapter,
			Bound: slice.Bound, DeepLink: link.peek(in.Book, slice.From),
		}
		if slice.Text == "" && slice.To <= slice.From {
			out.Note = "The range starts at or past the user's reading position, so nothing was served. " +
				"The book does not say anything here yet."
		}
		return nil, out, nil
	})
}

// --- find_mentions -------------------------------------------------------

type findMentionsIn struct {
	Book            string `json:"book" jsonschema:"the book's backhog entry ID (from list_books)"`
	Name            string `json:"name" jsonschema:"the character or place name to find occurrences of"`
	IncludeSpoilers bool   `json:"include_spoilers,omitempty" jsonschema:"default false. SPOILERS: true reads past the user's saved reading position (the whole book). Only set it when the user has explicitly asked to see beyond where they have read; false bounds results to what they have actually read."`
}

type mentionOut struct {
	CharOffset int                 `json:"char_offset"`
	CharEnd    int                 `json:"char_end"`
	Chapter    *backhog.ChapterRef `json:"chapter,omitempty"`
	Snippet    backhog.Snippet     `json:"snippet"`
	DeepLink   string              `json:"deep_link"`
}

type findMentionsOut struct {
	Name  string        `json:"name"`
	Total int           `json:"total"`
	Hits  []mentionOut  `json:"hits"`
	Bound backhog.Bound `json:"bound"`
	Note  string        `json:"note,omitempty"`
}

func addFindMentions(srv *mcp.Server, c *backhog.Client, link *linker) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "find_mentions",
		Annotations: readOnly,
		Description: "Find every place a name (a character, a place) has come up in a book so far — " +
			"the back-of-the-book index, bounded to the user's reading position. Each occurrence " +
			"carries a snippet and a deep link. A name that first appears after the reading position " +
			"simply does not exist yet. Needs a backhog with the name index; if this backhog " +
			"predates it, use search_book with the name as the query instead.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in findMentionsIn) (*mcp.CallToolResult, findMentionsOut, error) {
		res, err := c.FindMentions(ctx, in.Book, in.Name, in.IncludeSpoilers)
		var apiErr *backhog.APIError
		if err != nil {
			if asAPIErr(err, &apiErr) && apiErr.Status == 404 {
				return nil, findMentionsOut{}, fmt.Errorf("this backhog does not have the name index yet " +
					"(it lands with backhog's name-index update); use search_book with the name as the query instead")
			}
			return nil, findMentionsOut{}, fmt.Errorf("finding mentions: %w", err)
		}
		out := findMentionsOut{Name: res.Name, Total: res.Total, Bound: res.Bound,
			Hits: make([]mentionOut, 0, len(res.Results))}
		for _, h := range res.Results {
			out.Hits = append(out.Hits, mentionOut{
				CharOffset: h.CharOffset, CharEnd: h.CharEnd, Chapter: h.Chapter,
				Snippet: h.Snippet, DeepLink: link.relative(h.DeepLink),
			})
		}
		return nil, out, nil
	})
}

// asAPIErr reports whether err is (or wraps) an APIError.
func asAPIErr(err error, target **backhog.APIError) bool {
	apiErr, ok := err.(*backhog.APIError)
	if ok {
		*target = apiErr
	}
	return ok
}

// --- record_quiz_result ---------------------------------------------------

type recordQuizIn struct {
	Book        string `json:"book" jsonschema:"the book's backhog entry ID (from list_books)"`
	Questions   int    `json:"questions" jsonschema:"how many questions the quiz asked"`
	Correct     int    `json:"correct" jsonschema:"how many the reader answered correctly — the count you actually graded, never a flattering one"`
	ChapterFrom int    `json:"chapter_from,omitempty" jsonschema:"optional first chapter (1-based, from list_chapters) the quiz covered"`
	ChapterTo   int    `json:"chapter_to,omitempty" jsonschema:"optional last chapter (1-based, inclusive) the quiz covered"`
}

type recordQuizOut struct {
	Recorded     bool                      `json:"recorded"`
	Questions    int                       `json:"questions"`
	Correct      int                       `json:"correct"`
	Achievements []backhog.QuizAchievement `json:"achievements"`
	Note         string                    `json:"note,omitempty"`
}

// addRecordQuizResult registers the bridge's only write: the report a
// quizzing client files after grading. It exists so the achievements and
// the Reading Season card can count comprehension — and its honesty is
// entirely the caller's: backhog stores what is sent, it never grades.
func addRecordQuizResult(srv *mcp.Server, c *backhog.Client) {
	mcp.AddTool(srv, &mcp.Tool{
		Name: "record_quiz_result",
		Description: "Record a quiz's outcome after you have graded the reader's answers: how many " +
			"questions were asked and how many they got right. The only write this server carries. " +
			"Report the count you actually graded — a flattering count buys a hollow achievement. " +
			"Call it once per quiz, only with the reader's knowledge; anything it unlocks is for fun, " +
			"not proof. Needs a token with the quiz:write scope.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in recordQuizIn) (*mcp.CallToolResult, recordQuizOut, error) {
		if in.Questions < 1 {
			return nil, recordQuizOut{}, fmt.Errorf("questions must be at least 1 — a quiz with no questions did not happen")
		}
		if in.Correct < 0 || in.Correct > in.Questions {
			return nil, recordQuizOut{}, fmt.Errorf("correct must be between 0 and the questions asked")
		}
		report := backhog.QuizResultReport{
			Questions: in.Questions, Correct: in.Correct, Source: "mcp",
		}
		if in.ChapterFrom > 0 || in.ChapterTo > 0 {
			if in.ChapterFrom < 1 || in.ChapterTo < in.ChapterFrom {
				return nil, recordQuizOut{}, fmt.Errorf("the chapter range must run from a chapter to the same or a later one")
			}
			report.ChapterRange = &backhog.QuizChapterRange{From: in.ChapterFrom, To: in.ChapterTo}
		}
		recorded, err := c.RecordQuizResult(ctx, in.Book, report)
		var apiErr *backhog.APIError
		if err != nil {
			if asAPIErr(err, &apiErr) && apiErr.Status == http.StatusForbidden {
				return nil, recordQuizOut{}, fmt.Errorf("this token cannot record quiz results — mint one with the quiz:write scope in Settings → API tokens")
			}
			return nil, recordQuizOut{}, fmt.Errorf("recording quiz result: %w", err)
		}
		out := recordQuizOut{
			Recorded:     true,
			Questions:    recorded.QuizResult.Questions,
			Correct:      recorded.QuizResult.Correct,
			Achievements: recorded.Achievements,
		}
		if len(out.Achievements) == 0 {
			out.Note = "Recorded. Nothing new unlocked — the ladder climbs slowly, that is the point."
		}
		return nil, out, nil
	})
}

// --- list_names ----------------------------------------------------------

type listNamesIn struct {
	Book            string `json:"book" jsonschema:"the book's backhog entry ID (from list_books)"`
	IncludeSpoilers bool   `json:"include_spoilers,omitempty" jsonschema:"default false. SPOILERS: true reads past the user's saved reading position (the whole book). Only set it when the user has explicitly asked to see beyond where they have read; false bounds results to what they have actually read."`
}

type nameOut struct {
	Name      string                `json:"name"`
	Mentions  int                   `json:"mentions"`
	FirstSeen backhog.NameFirstSeen `json:"first_seen"`
}

type listNamesOut struct {
	Names []nameOut     `json:"names"`
	Bound backhog.Bound `json:"bound"`
	Note  string        `json:"note,omitempty"`
}

func addListNames(srv *mcp.Server, c *backhog.Client, link *linker) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "list_names",
		Annotations: readOnly,
		Description: "The book's own index of names — every character and place that has come up " +
			"so far, with mention counts and a cited first appearance — bounded to the user's " +
			"reading position. A name that first appears past the position does not exist yet. " +
			"This is the cast list of who the user has actually met. Needs a backhog with the " +
			"name index; if this backhog predates it, read_text and search_book with the name " +
			"as the query answer instead.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in listNamesIn) (*mcp.CallToolResult, listNamesOut, error) {
		res, err := c.Names(ctx, in.Book, in.IncludeSpoilers)
		var apiErr *backhog.APIError
		if err != nil {
			if asAPIErr(err, &apiErr) && apiErr.Status == 404 {
				return nil, listNamesOut{}, fmt.Errorf("this backhog does not have the name index yet " +
					"(it lands with backhog's name-index update); read_text and search_book with a " +
					"name as the query answer instead")
			}
			return nil, listNamesOut{}, fmt.Errorf("listing names: %w", err)
		}
		out := listNamesOut{Bound: res.Bound, Names: make([]nameOut, 0, len(res.Names))}
		for _, n := range res.Names {
			if n.Hidden {
				continue
			}
			n.FirstSeen.DeepLink = link.relative(n.FirstSeen.DeepLink)
			out.Names = append(out.Names, nameOut{Name: n.Name, Mentions: n.Mentions, FirstSeen: n.FirstSeen})
		}
		if !in.IncludeSpoilers {
			out.Note = "Names are bounded to the user's reading position (see bound); a name absent here may appear later."
		}
		return nil, out, nil
	})
}

// --- list_series ---------------------------------------------------------

type listSeriesOut struct {
	Series []backhog.SeriesSummary `json:"series"`
	Note   string                  `json:"note,omitempty"`
}

func addListSeries(srv *mcp.Server, c *backhog.Client, link *linker) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "list_series",
		Annotations: readOnly,
		Description: "Every series the user's shelf holds books of, with how many they own, " +
			"have finished, and are reading. Series membership comes from the library's own " +
			"Calibre metadata. Use this to resolve a series name the user gives before " +
			"get_series.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in struct{}) (*mcp.CallToolResult, listSeriesOut, error) {
		series, err := c.SeriesList(ctx)
		var apiErr *backhog.APIError
		if err != nil {
			if asAPIErr(err, &apiErr) && apiErr.Status == 404 {
				return nil, listSeriesOut{}, fmt.Errorf("this backhog does not have book series yet " +
					"(they land with backhog's series update); the tools cannot answer about series on it")
			}
			return nil, listSeriesOut{}, fmt.Errorf("listing series: %w", err)
		}
		if series == nil {
			series = []backhog.SeriesSummary{}
		}
		return nil, listSeriesOut{Series: series}, nil
	})
}

// --- get_series ----------------------------------------------------------

type getSeriesIn struct {
	Series string `json:"series" jsonschema:"the series name, exactly as list_series reports it"`
}

type seriesBookOut struct {
	EntryID          string                 `json:"entry_id"`
	Title            string                 `json:"title"`
	Authors          []string               `json:"authors,omitempty"`
	Status           string                 `json:"status"`
	Finished         bool                   `json:"finished"`
	SeriesNumber     *float64               `json:"series_number,omitempty"`
	FirstPublishYear *int                   `json:"first_publish_year,omitempty"`
	Position         backhog.SeriesPosition `json:"position"`
	DeepLink         string                 `json:"deep_link"`
}

type getSeriesOut struct {
	Name  string          `json:"name"`
	Books []seriesBookOut `json:"books"`
	Note  string          `json:"note,omitempty"`
}

func addGetSeries(srv *mcp.Server, c *backhog.Client, link *linker) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "get_series",
		Annotations: readOnly,
		Description: "The user's books of one series, in reading order, with each book's reading " +
			"status and position. The scope of a 'story so far': a book with finished=true has " +
			"been read whole (its text tools serve all of it), a book mid-read is served exactly " +
			"to its position, and an unread book contributes nothing yet — read_text enforces " +
			"all of this, so never set include_spoilers for series work.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in getSeriesIn) (*mcp.CallToolResult, getSeriesOut, error) {
		res, err := c.Series(ctx, in.Series)
		var apiErr *backhog.APIError
		if err != nil {
			if asAPIErr(err, &apiErr) && apiErr.Status == 404 {
				return nil, getSeriesOut{}, fmt.Errorf("no series of that name on this shelf — " +
					"list_series shows the exact names, or this backhog predates book series entirely")
			}
			return nil, getSeriesOut{}, fmt.Errorf("reading series: %w", err)
		}
		out := getSeriesOut{Name: res.Name, Books: make([]seriesBookOut, 0, len(res.Books))}
		for _, b := range res.Books {
			b.DeepLink = link.relative(b.DeepLink)
			out.Books = append(out.Books, seriesBookOut{
				EntryID: b.EntryID, Title: b.Title, Authors: b.Authors, Status: b.Status,
				Finished: b.Finished, SeriesNumber: b.SeriesNumber,
				FirstPublishYear: b.FirstPublishYear, Position: b.Position, DeepLink: b.DeepLink,
			})
		}
		return nil, out, nil
	})
}
