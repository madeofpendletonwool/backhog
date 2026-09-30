package server_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/collinpendleton/backhog/mcp/internal/backhog"
	"github.com/collinpendleton/backhog/mcp/internal/server"
)

// The integration bar from MAD-468: every tool, driven through a real MCP
// client session, returns position-bounded data with working deep links,
// and a read-only token cannot mutate anything. The fixture backhog here
// mirrors the API's own leak-test fixture (handlers_leak_test.go): a
// three-chapter mystery whose reveal ("The butler did it.") sits in the
// second chapter, and a reader stopped partway through the first. What is
// withheld is asserted as absent, not merely unchecked.

const (
	fxEntry  = "entry-1"
	fxToken  = "bh_test_secret"
	fxReveal = "the butler did it"
	fxPast   = "candlestick"
	fxAfter  = "after the trial"
)

// fxChapter is one chapter of the fixture: its display blocks (prose) and,
// derived from them, the folded canonical text and each block's canonical
// start. The stub owns its own folding so the geometry is exact.
type fxChapter struct {
	title  string
	blocks []string
	start  int // canonical start, filled by the fixture builder
	folded []string
}

// fold is the shape of booktext.Normalize that suffices for this fixture's
// ASCII prose: lowercase, drop punctuation, collapse spaces.
func fold(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == ' ':
			b.WriteRune(r)
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

type fixture struct {
	chapters  []fxChapter
	canonical string
	charCount int
	// position is the reader's char offset: the bound every clamped read
	// stops at.
	position int
}

func newFixture() *fixture {
	f := &fixture{chapters: []fxChapter{
		{title: "Alpha", blocks: []string{
			"The quiet village slept under the hill.",
			"Nobody suspected anything at first.",
		}},
		{title: "Beta", blocks: []string{
			"The detective searched the manor and found the silver candlestick hidden in the pantry.",
			"The butler did it. He confessed by morning.",
		}},
		{title: "Gamma", blocks: []string{
			"After the trial the village changed forever.",
		}},
	}}
	var folds []string
	off := 0
	for i := range f.chapters {
		f.chapters[i].start = off
		for _, b := range f.chapters[i].blocks {
			f.chapters[i].folded = append(f.chapters[i].folded, fold(b))
			off += len(fold(b)) + 1 // the joining space
		}
		folds = append(folds, f.chapters[i].folded...)
	}
	f.canonical = strings.Join(folds, " ")
	f.charCount = len(f.canonical)
	// The reading position: partway through Alpha's second paragraph, so
	// the straddling display block gets cut mid-paragraph and everything
	// from the candlestick on is past the bound.
	f.position = strings.Index(f.canonical, "nobody suspected") + 14
	return f
}

// blockStarts maps spine index → canonical start of each display block.
func (f *fixture) blockStarts(ch int) []int {
	var starts []int
	off := f.chapters[ch].start
	for _, folded := range f.chapters[ch].folded {
		starts = append(starts, off)
		off += len(folded) + 1 // the joining space
	}
	return starts
}

// chapterAt finds the chapter owning a canonical offset.
func (f *fixture) chapterAt(off int) *fxChapter {
	for i := range f.chapters {
		ch := &f.chapters[i]
		end := f.charCount
		if i+1 < len(f.chapters) {
			end = f.chapters[i+1].start
		}
		if off >= ch.start && off < end {
			return ch
		}
	}
	return nil
}

// request is one recorded API hit.
type request struct {
	Method string
	Path   string
	Until  string
}

// stubBackhog is the fixture backhog: the read-path subset of the real API
// with the same JSON shapes and the same clamp semantics (absent until on a
// token request clamps to the position; until=none serves everything).
type stubBackhog struct {
	*fixture
	ts *httptest.Server

	// mentionsMissing emulates a backhog that predates the name index,
	// for the degrade path's test.
	mentionsMissing bool

	mu       sync.Mutex
	recorded []request
}

func newStubBackhog(t *testing.T) *stubBackhog {
	t.Helper()
	stub := &stubBackhog{fixture: newFixture()}
	stub.ts = httptest.NewServer(http.HandlerFunc(stub.serve))
	t.Cleanup(stub.ts.Close)
	return stub
}

func (s *stubBackhog) record(r *http.Request) {
	s.mu.Lock()
	s.recorded = append(s.recorded, request{Method: r.Method, Path: r.URL.Path, Until: r.URL.Query().Get("until")})
	s.mu.Unlock()
}

func (s *stubBackhog) requests() []request {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]request(nil), s.recorded...)
}

func (s *stubBackhog) bound(until string) map[string]any {
	switch until {
	case "none":
		return map[string]any{"until": "none", "char_offset": s.charCount, "chapter": nil, "percent": 100.0}
	default:
		ch := s.chapterAt(s.position)
		var view any
		if ch != nil {
			view = map[string]any{"spine_index": 0, "title": ch.title, "title_source": "toc",
				"number": indexOfChapter(s.chapters, ch), "char_start": ch.start, "char_end": s.charCount}
		}
		return map[string]any{"until": "position", "char_offset": s.position, "chapter": view,
			"percent": float64(s.position) / float64(s.charCount) * 100}
	}
}

func indexOfChapter(chapters []fxChapter, ch *fxChapter) int {
	for i := range chapters {
		if &chapters[i] == ch {
			return i + 1
		}
	}
	return 0
}

// clamp is the effective offset a request stops at.
func (s *stubBackhog) clamp(until string) int {
	if until == "none" {
		return s.charCount
	}
	return s.position
}

func (s *stubBackhog) serve(w http.ResponseWriter, r *http.Request) {
	s.record(r)
	if r.Header.Get("Authorization") != "Bearer "+fxToken {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "not authenticated"})
		return
	}
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "this token is read-only"})
		return
	}
	until := r.URL.Query().Get("until")

	switch {
	case r.URL.Path == "/api/library":
		writeJSON(w, http.StatusOK, map[string]any{
			"entries": []map[string]any{{
				"id": fxEntry, "media_type": "book", "status": "playing",
				"book": map[string]any{"id": "OL1W", "title": "The Village Mystery",
					"authors": []string{"A. Author"}},
				"progress_percent": float64(s.position) / float64(s.charCount) * 100,
			}},
			"total": 1,
		})

	case r.URL.Path == "/api/books/"+fxEntry+"/position":
		ch := s.chapterAt(s.position)
		writeJSON(w, http.StatusOK, map[string]any{
			"position_mode": "text", "char_offset": s.position, "source": "read",
			"percent":    float64(s.position) / float64(s.charCount) * 100,
			"char_count": s.charCount,
			"chapter": map[string]any{"spine_index": 0, "title": ch.title, "number": 1,
				"char_start": ch.start, "char_end": s.charCount},
		})

	case r.URL.Path == "/api/books/"+fxEntry+"/text/chapters":
		clamped := until != "none"
		limit := s.clamp(until)
		var out []map[string]any
		for i, ch := range s.chapters {
			end := s.charCount
			if i+1 < len(s.chapters) {
				end = s.chapters[i+1].start
			}
			row := map[string]any{"spine_index": i, "title": ch.title, "title_source": "toc",
				"number": i + 1, "char_start": ch.start, "char_end": end,
				"blocks": s.blockStarts(i), "locked": false}
			if clamped && ch.start >= limit {
				row["locked"] = true
				row["blocks"] = nil
			}
			out = append(out, row)
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"char_count": s.charCount, "chapters": out, "bound": s.bound(until),
		})

	case r.URL.Path == "/api/books/"+fxEntry+"/text":
		from, to := 0, s.charCount
		fmt.Sscanf(r.URL.Query().Get("from"), "%d", &from)
		fmt.Sscanf(r.URL.Query().Get("to"), "%d", &to)
		if from < 0 {
			from = 0
		}
		if to <= from || to > s.charCount {
			to = s.charCount
		}
		limit := s.clamp(until)
		effTo := min(to, limit)
		text := ""
		if from < effTo {
			text = s.canonical[from:effTo]
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"from": from, "to": effTo, "char_count": s.charCount, "text": text,
			"bound": s.bound(until),
			"provenance": map[string]any{"book_id": fxEntry, "chapter": nil,
				"char_start": from, "char_end": effTo, "deep_link": ""},
		})

	case r.URL.Path == "/api/books/"+fxEntry+"/text/display":
		spine := 0
		fmt.Sscanf(r.URL.Query().Get("spine"), "%d", &spine)
		limit := s.clamp(until)
		starts := s.blockStarts(spine)
		var blocks []string
		for b, block := range s.chapters[spine].blocks {
			start := starts[b]
			if start >= limit {
				break
			}
			end := start + len(s.chapters[spine].folded[b])
			if end <= limit {
				blocks = append(blocks, block)
				continue
			}
			// The straddling block: keep the display prefix the reading
			// reached (a proportional cut stands in for the API's exact
			// fold-back).
			keep := float64(limit-start) / float64(len(s.chapters[spine].folded[b]))
			cut := int(float64(len(block)) * keep)
			blocks = append(blocks, block[:cut])
			break
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"spine_index": spine, "blocks": blocks, "bound": s.bound(until),
		})

	case r.URL.Path == "/api/books/"+fxEntry+"/search":
		q := fold(r.URL.Query().Get("q"))
		limit := s.clamp(until)
		var hits []map[string]any
		searchable := s.canonical[:limit]
		for off := 0; ; {
			i := strings.Index(searchable[off:], q)
			if i < 0 {
				break
			}
			at := off + i
			// Render the hit's display block as context.
			block, blockIdx, chIdx := s.displayBlockAt(at)
			folded := s.chapters[chIdx].folded[blockIdx]
			rel := at - s.blockStarts(chIdx)[blockIdx]
			bi := displayIndexOf(block, folded, rel, len(q))
			bi = min(bi, len(block))
			end := min(bi+len(q), len(block))
			hits = append(hits, map[string]any{
				"char_offset": at, "char_end": at + len(q),
				"percent": float64(at) / float64(s.charCount) * 100,
				"context": map[string]any{"before": block[:bi], "passage": block[bi:end],
					"after": block[end:]},
				"chapter": map[string]any{"spine_index": chIdx, "title": s.chapters[chIdx].title,
					"number": chIdx + 1, "char_start": s.chapters[chIdx].start, "char_end": s.charCount},
				"book_id": fxEntry, "deep_link": fmt.Sprintf("/books/%s/read?offset=%d&peek=1", fxEntry, at),
			})
			off = at + len(q)
		}
		mode := "phrase"
		if len(hits) == 0 {
			mode = "loose"
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"query": q, "axis": "text", "mode": mode, "total": len(hits),
			"truncated": false, "results": hits, "bound": s.bound(until),
		})

	case r.URL.Path == "/api/books/"+fxEntry+"/mentions":
		// The name index (MAD-670). A stub that wants to emulate a backhog
		// predating it flips mentionsMissing and answers 404.
		if s.mentionsMissing {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
			return
		}
		name := fold(r.URL.Query().Get("name"))
		limit := s.clamp(until)
		var hits []map[string]any
		for off := 0; ; {
			i := strings.Index(s.canonical[off:], name)
			if i < 0 {
				break
			}
			at := off + i
			if at >= limit {
				break
			}
			block, blockIdx, chIdx := s.displayBlockAt(at)
			folded := s.chapters[chIdx].folded[blockIdx]
			rel := at - s.blockStarts(chIdx)[blockIdx]
			bi := displayIndexOf(block, folded, rel, len(name))
			end := min(bi+len(name), len(block))
			hits = append(hits, map[string]any{
				"char_offset": at, "char_end": at + len(name),
				"snippet": map[string]any{"before": block[:bi], "passage": block[bi:end],
					"after": block[end:]},
				"chapter": map[string]any{"spine_index": chIdx, "title": s.chapters[chIdx].title,
					"title_source": "toc", "number": chIdx + 1,
					"char_start": s.chapters[chIdx].start, "char_end": s.charCount},
				"deep_link": fmt.Sprintf("/books/%s/read?offset=%d&peek=1", fxEntry, at),
			})
			off = at + len(name)
		}
		display := r.URL.Query().Get("name")
		if display == "" {
			display = name
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"name": display, "total": len(hits), "results": hits, "bound": s.bound(until),
		})

	case r.URL.Path == "/api/books/search/text":
		// MAD-470 has not landed on this backhog.
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})

	default:
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
	}
}

// displayBlockAt finds the display block holding a canonical offset.
func (s *stubBackhog) displayBlockAt(off int) (string, int, int) {
	for ci := range s.chapters {
		starts := s.blockStarts(ci)
		for bi, folded := range s.chapters[ci].folded {
			if off >= starts[bi] && off < starts[bi]+len(folded) {
				return s.chapters[ci].blocks[bi], bi, ci
			}
		}
	}
	return "", 0, 0
}

// displayIndexOf approximates where a canonical span sits in its display
// block: proportional fold-back, exact for this fixture's short blocks.
func displayIndexOf(block string, folded string, rel, qlen int) int {
	if len(folded) == 0 {
		return 0
	}
	return int(float64(len(block)) * float64(rel) / float64(len(folded)))
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

// --- the MCP harness ------------------------------------------------------

// harness wires a real MCP client session to the server under test, which
// itself talks HTTP to the fixture backhog.
type harness struct {
	stub    *stubBackhog
	session *mcp.ClientSession
	ctx     context.Context
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	stub := newStubBackhog(t)
	client, err := backhog.New(stub.ts.URL, fxToken)
	if err != nil {
		t.Fatalf("client: %v", err)
	}
	srv := server.New(server.Options{Client: client})
	ctx := context.Background()
	ctrans, strans := mcp.NewInMemoryTransports()
	if _, err := srv.Connect(ctx, strans, nil); err != nil {
		t.Fatalf("server connect: %v", err)
	}
	cl := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "0"}, nil)
	session, err := cl.Connect(ctx, ctrans, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return &harness{stub: stub, session: session, ctx: ctx}
}

// call invokes one tool and decodes its JSON output into out.
func (h *harness) call(t *testing.T, tool string, args map[string]any, out any) {
	t.Helper()
	res, err := h.session.CallTool(h.ctx, &mcp.CallToolParams{Name: tool, Arguments: args})
	if err != nil {
		t.Fatalf("call %s: %v", tool, err)
	}
	if res.IsError {
		t.Fatalf("call %s returned a tool error: %v", tool, textOf(res))
	}
	raw := textOf(res)
	if err := json.Unmarshal([]byte(raw), out); err != nil {
		t.Fatalf("call %s: decode output: %v\n%s", tool, err, raw)
	}
}

// callErr invokes one tool expecting it to fail, returning the error text.
func (h *harness) callErr(t *testing.T, tool string, args map[string]any) string {
	t.Helper()
	res, err := h.session.CallTool(h.ctx, &mcp.CallToolParams{Name: tool, Arguments: args})
	if err != nil {
		t.Fatalf("call %s: %v", tool, err)
	}
	if !res.IsError {
		t.Fatalf("call %s unexpectedly succeeded: %v", tool, textOf(res))
	}
	return textOf(res)
}

func textOf(res *mcp.CallToolResult) string {
	var sb strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			sb.WriteString(tc.Text)
		}
	}
	return sb.String()
}

// assertNoLeak fails if any text carries the reveal, the past-the-bound
// word, or the unread final chapter — folded to lower case, because a leak
// is a leak in either the canonical or the display form.
func assertNoLeak(t *testing.T, what string, texts ...string) {
	t.Helper()
	for _, s := range texts {
		low := strings.ToLower(s)
		for _, banned := range []string{fxReveal, fxPast, fxAfter} {
			if strings.Contains(low, banned) {
				t.Fatalf("%s leaked %q:\n%s", what, banned, s)
			}
		}
	}
}

// --- the tests -------------------------------------------------------------

func TestToolSurface(t *testing.T) {
	h := newHarness(t)
	res, err := h.session.ListTools(h.ctx, nil)
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}
	want := map[string]bool{
		"list_books": false, "get_reading_position": false, "list_chapters": false,
		"read_text": false, "search_book": false, "search_library": false,
		"get_passage": false, "find_mentions": false,
	}
	for _, tool := range res.Tools {
		if _, ok := want[tool.Name]; !ok {
			t.Fatalf("unexpected tool %q", tool.Name)
		}
		want[tool.Name] = true
		if tool.Annotations == nil || !tool.Annotations.ReadOnlyHint {
			t.Fatalf("tool %q is not marked read-only", tool.Name)
		}
		if tool.Description == "" {
			t.Fatalf("tool %q has no description", tool.Name)
		}
	}
	for name, seen := range want {
		if !seen {
			t.Fatalf("tool %q missing from the surface", name)
		}
	}
}

func TestListBooks(t *testing.T) {
	h := newHarness(t)
	var out struct {
		Books []struct {
			EntryID string   `json:"entry_id"`
			Title   string   `json:"title"`
			Authors []string `json:"authors"`
			Status  string   `json:"status"`
			Percent *float64 `json:"progress_percent"`
		} `json:"books"`
		Total int `json:"total"`
	}
	h.call(t, "list_books", map[string]any{}, &out)
	if out.Total != 1 || len(out.Books) != 1 {
		t.Fatalf("library = %+v", out)
	}
	b := out.Books[0]
	if b.EntryID != fxEntry || b.Title != "The Village Mystery" || b.Status != "playing" {
		t.Fatalf("book = %+v", b)
	}
	if b.Percent == nil || *b.Percent <= 0 || *b.Percent >= 100 {
		t.Fatalf("percent = %v", b.Percent)
	}
}

func TestReadingPosition(t *testing.T) {
	h := newHarness(t)
	var out struct {
		CharOffset int     `json:"char_offset"`
		Percent    float64 `json:"percent"`
		Chapter    *struct {
			Number int    `json:"number"`
			Title  string `json:"title"`
		} `json:"chapter"`
		DeepLink string `json:"deep_link"`
	}
	h.call(t, "get_reading_position", map[string]any{"book": fxEntry}, &out)
	if out.CharOffset != h.stub.position {
		t.Fatalf("char_offset = %d, want %d", out.CharOffset, h.stub.position)
	}
	if out.Chapter == nil || out.Chapter.Number != 1 || out.Chapter.Title != "Alpha" {
		t.Fatalf("chapter = %+v", out.Chapter)
	}
	want := h.stub.ts.URL + fmt.Sprintf("/books/%s/read?offset=%d&peek=1", fxEntry, h.stub.position)
	if out.DeepLink != want {
		t.Fatalf("deep link = %q, want %q", out.DeepLink, want)
	}
}

func TestListChaptersBounded(t *testing.T) {
	h := newHarness(t)
	var out struct {
		Chapters []struct {
			Number int    `json:"number"`
			Title  string `json:"title"`
			Locked bool   `json:"locked"`
		} `json:"chapters"`
		Bound struct {
			Until      string `json:"until"`
			CharOffset int    `json:"char_offset"`
		} `json:"bound"`
	}
	h.call(t, "list_chapters", map[string]any{"book": fxEntry}, &out)
	if len(out.Chapters) != 3 {
		t.Fatalf("chapters = %+v", out.Chapters)
	}
	if out.Chapters[0].Locked {
		t.Fatal("Alpha is read territory; it must not be locked")
	}
	for _, ch := range out.Chapters[1:] {
		if !ch.Locked {
			t.Fatalf("chapter %d past the position must be locked", ch.Number)
		}
	}
	if out.Bound.Until != "position" || out.Bound.CharOffset != h.stub.position {
		t.Fatalf("bound = %+v", out.Bound)
	}
	assertNoLeak(t, "list_chapters", fmt.Sprint(out.Chapters))
}

func TestListChaptersSpoilers(t *testing.T) {
	h := newHarness(t)
	var out struct {
		Chapters []struct {
			Number    int  `json:"number"`
			CharStart int  `json:"char_start"`
			Locked    bool `json:"locked"`
		} `json:"chapters"`
	}
	h.call(t, "list_chapters", map[string]any{"book": fxEntry, "include_spoilers": true}, &out)
	for _, ch := range out.Chapters {
		if ch.Locked {
			t.Fatalf("chapter %d locked under until=none", ch.Number)
		}
	}
	// The opt-in must have traveled as until=none.
	sawNone := false
	for _, r := range h.stub.requests() {
		if r.Path == "/api/books/"+fxEntry+"/text/chapters" && r.Until == "none" {
			sawNone = true
		}
	}
	if !sawNone {
		t.Fatal("include_spoilers did not send until=none")
	}
}

func TestReadTextBounded(t *testing.T) {
	h := newHarness(t)
	var out struct {
		Text     string `json:"text"`
		From     int    `json:"from"`
		To       int    `json:"to"`
		NextFrom *int   `json:"next_from"`
		Chapter  *struct {
			Number int    `json:"number"`
			Title  string `json:"title"`
		} `json:"chapter"`
		Bound struct {
			CharOffset int `json:"char_offset"`
		} `json:"bound"`
		DeepLink string `json:"deep_link"`
		Note     string `json:"note"`
	}
	h.call(t, "read_text", map[string]any{"book": fxEntry}, &out)

	if !strings.Contains(out.Text, "The quiet village slept under the hill.") {
		t.Fatalf("read text lacks Alpha's first paragraph: %q", out.Text)
	}
	if !strings.Contains(out.Text, "Nobody suspect") || strings.Contains(out.Text, "at first") {
		t.Fatalf("the straddling block should be cut mid-paragraph: %q", out.Text)
	}
	assertNoLeak(t, "read_text", out.Text)
	if out.NextFrom != nil {
		t.Fatalf("next_from = %d past the bound; the readable window is exhausted", *out.NextFrom)
	}
	if out.Note == "" {
		t.Fatal("exhausting the readable window should say so")
	}
	if out.Bound.CharOffset != h.stub.position {
		t.Fatalf("bound = %+v", out.Bound)
	}
	if out.Chapter == nil || out.Chapter.Number != 1 || out.Chapter.Title != "Alpha" {
		t.Fatalf("chapter = %+v", out.Chapter)
	}
	if want := h.stub.ts.URL + fmt.Sprintf("/books/%s/read?offset=0&peek=1", fxEntry); out.DeepLink != want {
		t.Fatalf("deep link = %q, want %q", out.DeepLink, want)
	}
}

func TestReadTextPastPosition(t *testing.T) {
	h := newHarness(t)
	var out struct {
		Text string `json:"text"`
		Note string `json:"note"`
	}
	// Beta begins past the position; the read must come back empty and say
	// why, never with the chapter's text.
	h.call(t, "read_text", map[string]any{"book": fxEntry, "chapter": 2}, &out)
	if out.Text != "" {
		t.Fatalf("chapter 2 is past the position; text = %q", out.Text)
	}
	assertNoLeak(t, "read_text chapter 2", out.Text, out.Note)
	if out.Note == "" {
		t.Fatal("an empty bounded read must explain itself")
	}
}

func TestReadTextSpoilers(t *testing.T) {
	h := newHarness(t)
	var out struct {
		Text string `json:"text"`
	}
	h.call(t, "read_text", map[string]any{"book": fxEntry, "include_spoilers": true}, &out)
	for _, want := range []string{
		"The quiet village slept under the hill.",
		"The butler did it. He confessed by morning.",
		"After the trial the village changed forever.",
	} {
		if !strings.Contains(out.Text, want) {
			t.Fatalf("spoiler read lacks %q: %q", want, out.Text)
		}
	}
}

func TestReadTextPagination(t *testing.T) {
	h := newHarness(t)
	var whole string
	seen := map[string]bool{}
	from := 0
	for range 10 {
		var out struct {
			Text     string `json:"text"`
			From     int    `json:"from"`
			NextFrom *int   `json:"next_from"`
		}
		args := map[string]any{"book": fxEntry, "include_spoilers": true,
			"max_chars": 1000, "from": from}
		h.call(t, "read_text", args, &out)
		if out.From != from {
			t.Fatalf("from echoed %d, want %d", out.From, from)
		}
		if seen[out.Text] {
			t.Fatalf("pagination looped on %q", out.Text)
		}
		seen[out.Text] = true
		whole += out.Text + "\n"
		if out.NextFrom == nil {
			// The whole spoiler-bounded book, paged through.
			for _, want := range []string{
				"The quiet village slept under the hill.",
				"silver candlestick",
				"The butler did it. He confessed by morning.",
				"After the trial the village changed forever.",
			} {
				if !strings.Contains(whole, want) {
					t.Fatalf("paged read lacks %q: %q", want, whole)
				}
			}
			return
		}
		from = *out.NextFrom
	}
	t.Fatal("pagination never finished")
}

func TestSearchBookBounded(t *testing.T) {
	h := newHarness(t)
	var readable struct {
		Mode  string `json:"mode"`
		Total int    `json:"total"`
		Hits  []struct {
			CharOffset int    `json:"char_offset"`
			DeepLink   string `json:"deep_link"`
			Context    struct {
				Passage string `json:"passage"`
			} `json:"context"`
		} `json:"hits"`
	}
	h.call(t, "search_book", map[string]any{"book": fxEntry, "query": "village"}, &readable)
	if readable.Total != 1 || len(readable.Hits) != 1 {
		t.Fatalf("readable hits = %+v", readable)
	}
	hit := readable.Hits[0]
	if want := h.stub.ts.URL + fmt.Sprintf("/books/%s/read?offset=%d&peek=1", fxEntry, hit.CharOffset); hit.DeepLink != want {
		t.Fatalf("deep link = %q, want %q", hit.DeepLink, want)
	}
	if !strings.EqualFold(hit.Context.Passage, "village") {
		t.Fatalf("passage = %q", hit.Context.Passage)
	}

	// The reveal phrase exists in the book but past the position: the
	// bounded search must answer nothing, and say it.
	var hidden struct {
		Mode  string `json:"mode"`
		Total int    `json:"total"`
		Hits  []struct {
			Context struct {
				Passage string `json:"passage"`
			} `json:"context"`
		} `json:"hits"`
	}
	h.call(t, "search_book", map[string]any{"book": fxEntry, "query": "butler did it"}, &hidden)
	if hidden.Total != 0 || len(hidden.Hits) != 0 {
		t.Fatalf("search past the position must drop every hit: %+v", hidden)
	}
	assertNoLeak(t, "search_book", fmt.Sprint(hidden.Hits))
}

func TestSearchBookSpoilers(t *testing.T) {
	h := newHarness(t)
	var out struct {
		Total int `json:"total"`
	}
	h.call(t, "search_book", map[string]any{"book": fxEntry, "query": "butler did it", "include_spoilers": true}, &out)
	if out.Total != 1 {
		t.Fatalf("spoiler search total = %d, want 1", out.Total)
	}
}

func TestGetPassage(t *testing.T) {
	h := newHarness(t)
	off := strings.Index(h.stub.canonical, "the quiet village")
	var out struct {
		Text      string `json:"text"`
		CharStart int    `json:"char_start"`
		DeepLink  string `json:"deep_link"`
	}
	h.call(t, "get_passage", map[string]any{"book": fxEntry, "char_start": off, "char_end": off + 20}, &out)
	if out.Text != h.stub.canonical[off:off+20] {
		t.Fatalf("passage = %q, want the exact canonical bytes %q", out.Text, h.stub.canonical[off:off+20])
	}
	if out.CharStart != off {
		t.Fatalf("char_start = %d, want %d", out.CharStart, off)
	}

	// A range wholly past the position: empty, with the clamp named.
	var past struct {
		Text string `json:"text"`
		Note string `json:"note"`
	}
	start := strings.Index(h.stub.canonical, "the detective")
	h.call(t, "get_passage", map[string]any{"book": fxEntry, "char_start": start, "char_end": start + 30}, &past)
	if past.Text != "" {
		t.Fatalf("passage past the position = %q", past.Text)
	}
	assertNoLeak(t, "get_passage", past.Text, past.Note)
	if past.Note == "" {
		t.Fatal("an empty clamped passage must explain the clamp")
	}
}

func TestSearchLibraryDegradesCleanly(t *testing.T) {
	h := newHarness(t)
	msg := h.callErr(t, "search_library", map[string]any{"query": "village"})
	if !strings.Contains(msg, "search_book") {
		t.Fatalf("error should point at search_book: %q", msg)
	}
	if strings.Contains(strings.ToLower(msg), "butler") {
		t.Fatalf("error leaked fixture content: %q", msg)
	}
}

func TestFindMentionsDegradesCleanly(t *testing.T) {
	h := newHarness(t)
	h.stub.mentionsMissing = true
	msg := h.callErr(t, "find_mentions", map[string]any{"book": fxEntry, "name": "the butler"})
	if !strings.Contains(msg, "search_book") {
		t.Fatalf("error should point at search_book: %q", msg)
	}
	assertNoLeak(t, "find_mentions error", msg)
}

// TestFindMentionsListsSoFar is the happy path: a name's mentions within
// the caller's position, each placed and linked; spoilers lift the clamp
// and the second, future mention appears.
func TestFindMentionsListsSoFar(t *testing.T) {
	h := newHarness(t)
	var out struct {
		Name  string `json:"name"`
		Total int    `json:"total"`
		Hits  []struct {
			CharOffset int                      `json:"char_offset"`
			Chapter    struct{ Title string }   `json:"chapter"`
			Snippet    struct{ Passage string } `json:"snippet"`
			DeepLink   string                   `json:"deep_link"`
		} `json:"hits"`
		Bound struct {
			Until string `json:"until"`
		} `json:"bound"`
	}
	h.call(t, "find_mentions", map[string]any{"book": fxEntry, "name": "village"}, &out)

	// "village" is in Alpha (read) and Gamma (past the position): one
	// mention so far, no leak of the later one.
	if out.Total != 1 || len(out.Hits) != 1 {
		t.Fatalf("mentions so far = %d hits, want 1: %+v", len(out.Hits), out)
	}
	if out.Name != "village" {
		t.Errorf("name echo = %q", out.Name)
	}
	hit := out.Hits[0]
	if hit.Chapter.Title != "Alpha" || !strings.Contains(hit.Snippet.Passage, "village") {
		t.Errorf("hit = %+v, want Alpha's village", hit)
	}
	// The linker absolutizes deep links against the backhog origin; the
	// path is the contract.
	if !strings.HasSuffix(hit.DeepLink, fmt.Sprintf("/books/%s/read?offset=%d&peek=1", fxEntry, hit.CharOffset)) {
		t.Errorf("deep link = %q", hit.DeepLink)
	}
	if out.Bound.Until != "position" {
		t.Errorf("bound = %+v, want the position clamp", out.Bound)
	}

	// The explicit spoiler opt-in lifts the clamp: Gamma's village joins.
	h.call(t, "find_mentions",
		map[string]any{"book": fxEntry, "name": "village", "include_spoilers": true}, &out)
	if out.Total != 2 || len(out.Hits) != 2 {
		t.Fatalf("spoiler mentions = %d hits, want 2: %+v", len(out.Hits), out)
	}
	if out.Hits[1].Chapter.Title != "Gamma" {
		t.Errorf("second hit = %+v, want Gamma's", out.Hits[1])
	}
}

// TestReadOnlyTokenCannotMutate is the acceptance line "a read-only token
// cannot mutate anything": across the whole battery, every request the
// bridge made was a GET carrying the bearer token. The stub answers 403 to
// anything else, so a write attempt would have failed every test above.
func TestReadOnlyTokenCannotMutate(t *testing.T) {
	h := newHarness(t)
	for _, tool := range []string{"list_books"} {
		h.call(t, tool, map[string]any{}, new(map[string]any))
	}
	h.call(t, "get_reading_position", map[string]any{"book": fxEntry}, new(map[string]any))
	h.call(t, "list_chapters", map[string]any{"book": fxEntry}, new(map[string]any))
	h.call(t, "read_text", map[string]any{"book": fxEntry}, new(map[string]any))
	h.call(t, "search_book", map[string]any{"book": fxEntry, "query": "village"}, new(map[string]any))
	h.call(t, "get_passage", map[string]any{"book": fxEntry, "char_start": 0, "char_end": 10}, new(map[string]any))

	for _, r := range h.stub.requests() {
		if r.Method != http.MethodGet {
			t.Fatalf("the bridge issued %s %s — a read-only token must never mutate", r.Method, r.Path)
		}
	}
}

// TestStreamableHTTPRoundTrip proves the transport wiring: the same server
// served over streamable HTTP answers a tool call end to end.
func TestStreamableHTTPRoundTrip(t *testing.T) {
	stub := newStubBackhog(t)
	client, err := backhog.New(stub.ts.URL, fxToken)
	if err != nil {
		t.Fatalf("client: %v", err)
	}
	srv := server.New(server.Options{Client: client})
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return srv }, nil)
	ts := httptest.NewServer(handler)
	defer ts.Close()

	ctx := context.Background()
	cl := mcp.NewClient(&mcp.Implementation{Name: "http-test", Version: "0"}, nil)
	session, err := cl.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: ts.URL}, nil)
	if err != nil {
		t.Fatalf("connect over streamable http: %v", err)
	}
	defer func() { _ = session.Close() }()

	res, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "get_reading_position",
		Arguments: map[string]any{"book": fxEntry},
	})
	if err != nil {
		t.Fatalf("call over streamable http: %v", err)
	}
	if res.IsError || !strings.Contains(textOf(res), fmt.Sprintf(`"char_offset":%d`, stub.position)) {
		t.Fatalf("unexpected result: %v", textOf(res))
	}
}
