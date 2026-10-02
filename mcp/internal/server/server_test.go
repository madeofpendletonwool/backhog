package server_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
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
	// fxSeries is the series the fixture's two books belong to.
	fxSeries = "Village Mystery"
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
	// Body carries the JSON of a write, when the method has one.
	Body string
}

// stubBackhog is the fixture backhog: the read-path subset of the real API
// with the same JSON shapes and the same clamp semantics (absent until on a
// token request clamps to the position; until=none serves everything),
// plus the one write the bridge carries — the quiz-results POST, which the
// stub gates behind quizWrite the way the real one gates behind the
// quiz:write scope.
type stubBackhog struct {
	*fixture
	ts *httptest.Server

	// mentionsMissing emulates a backhog that predates the name index,
	// for the degrade path's test.
	mentionsMissing bool
	// seriesMissing emulates a backhog that predates book series.
	seriesMissing bool
	// quizWrite emulates a token carrying the quiz:write scope: the
	// quiz-results POST is accepted when set, and answered with the same
	// 403 every write gets when not.
	quizWrite bool

	mu       sync.Mutex
	recorded []request
	// quizYearCorrect is the running correct-answer count the stub's
	// comprehension ladder reads, so achievements cross like the real one.
	quizResults int
	quizCorrect int
}

func newStubBackhog(t *testing.T) *stubBackhog {
	t.Helper()
	stub := &stubBackhog{fixture: newFixture()}
	stub.ts = httptest.NewServer(http.HandlerFunc(stub.serve))
	t.Cleanup(stub.ts.Close)
	return stub
}

func (s *stubBackhog) record(r *http.Request, body string) {
	s.mu.Lock()
	s.recorded = append(s.recorded, request{Method: r.Method, Path: r.URL.Path, Until: r.URL.Query().Get("until"), Body: body})
	s.mu.Unlock()
}

// serveQuizResult emulates POST /api/books/{entry}/quiz-results: validate
// the report's arithmetic, store it, and answer with the achievements the
// comprehension ladder crossed — first result tips Book Report, the 25th
// correct answer of the year tips Gold Star.
func (s *stubBackhog) serveQuizResult(w http.ResponseWriter, body string) {
	var report struct {
		Questions    int `json:"questions"`
		Correct      int `json:"correct"`
		ChapterRange *struct {
			From int `json:"from"`
			To   int `json:"to"`
		} `json:"chapter_range"`
		Source string `json:"source"`
	}
	if err := json.NewDecoder(strings.NewReader(body)).Decode(&report); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	if report.Questions < 1 || report.Correct < 0 || report.Correct > report.Questions {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "implausible quiz"})
		return
	}

	s.mu.Lock()
	s.quizResults++
	before := s.quizCorrect
	s.quizCorrect += report.Correct
	s.mu.Unlock()

	var achievements []map[string]any
	if s.quizResults == 1 {
		achievements = append(achievements, map[string]any{"id": "book_report", "title": "Book Report", "tier": "bronze"})
	}
	if before < 25 && s.quizCorrect >= 25 {
		achievements = append(achievements, map[string]any{"id": "gold_star", "title": "Gold Star", "tier": "silver"})
	}
	result := map[string]any{
		"id": "q-stub", "entry_id": fxEntry,
		"questions": report.Questions, "correct": report.Correct,
		"source": report.Source, "created_at": "2026-10-01T00:00:00Z",
	}
	if report.ChapterRange != nil {
		result["chapter_start"] = report.ChapterRange.From
		result["chapter_end"] = report.ChapterRange.To
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"quiz_result": result, "achievements": achievements,
	})
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
	body := ""
	if r.Method != http.MethodGet && r.Body != nil {
		raw, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		if err == nil {
			body = string(raw)
		}
	}
	s.record(r, body)
	if r.Header.Get("Authorization") != "Bearer "+fxToken {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "not authenticated"})
		return
	}
	// The one write the bridge carries: the quiz report. Gated behind
	// quizWrite like the real one is gated behind the quiz:write scope;
	// every other POST, PUT, PATCH and DELETE stays behind the 403.
	if r.Method != http.MethodGet {
		if r.Method == http.MethodPost && r.URL.Path == "/api/books/"+fxEntry+"/quiz-results" && s.quizWrite {
			s.serveQuizResult(w, body)
			return
		}
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

	case r.URL.Path == "/api/books/"+fxEntry+"/names":
		// The name index (MAD-670), same clamp as everywhere: a name
		// whose only occurrences sit past the bound does not exist yet.
		limit := s.clamp(until)
		var names []map[string]any
		for _, display := range []string{"village", "hill", "detective", "butler"} {
			key := display
			count, first := 0, -1
			for off := 0; ; {
				i := strings.Index(s.canonical[off:], key)
				if i < 0 {
					break
				}
				at := off + i
				if at >= limit {
					break
				}
				if first < 0 {
					first = at
				}
				count++
				off = at + len(key)
			}
			if count == 0 {
				continue
			}
			ch := s.chapterAt(first)
			chIdx := indexOfChapter(s.chapters, ch) - 1
			names = append(names, map[string]any{
				"name": display, "mentions": count, "hidden": false,
				"first_seen": map[string]any{
					"char_start": first, "percent": float64(first) / float64(s.charCount) * 100,
					"chapter": map[string]any{"spine_index": chIdx, "title": ch.title,
						"title_source": "toc", "number": chIdx + 1,
						"char_start": ch.start, "char_end": s.charCount},
					"deep_link": fmt.Sprintf("/books/%s/read?offset=%d&peek=1", fxEntry, first),
				},
			})
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"names": names, "bound": s.bound(until),
		})

	case r.URL.Path == "/api/books/series":
		// Book series (MAD-469). A stub that wants to emulate a backhog
		// predating them flips seriesMissing and answers 404.
		if s.seriesMissing {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"series": []map[string]any{{
				"name": fxSeries, "books": 2, "finished": 1, "reading": 1,
			}},
		})

	case r.URL.Path == "/api/books/series/"+fxSeries:
		if s.seriesMissing {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"name": fxSeries,
			// Ordered the way the real API orders them: by rank in the
			// series, so the finished book one leads.
			"books": []map[string]any{{
				"entry_id": "entry-2", "book_id": "OL0W", "title": "The Earlier Mystery",
				"authors": []string{"A. Author"},
				"status":  "played", "finished": true, "series_number": 1.0,
				"first_publish_year": 1996,
				"position": map[string]any{"position_mode": "text",
					"char_offset": 0, "percent": 100.0},
				"deep_link": "/books/entry-2/read",
			}, {
				"entry_id": fxEntry, "book_id": "OL1W", "title": "The Village Mystery",
				"authors": []string{"A. Author"},
				"status":  "playing", "finished": false, "series_number": 2.0,
				"first_publish_year": 1998,
				"position": map[string]any{"position_mode": "text",
					"char_offset": s.position,
					"percent":     float64(s.position) / float64(s.charCount) * 100},
				"deep_link": fmt.Sprintf("/books/%s/read?offset=%d&peek=1", fxEntry, s.position),
			}},
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
		"get_passage": false, "find_mentions": false, "list_names": false,
		"list_series": false, "get_series": false, "record_quiz_result": false,
	}
	for _, tool := range res.Tools {
		if _, ok := want[tool.Name]; !ok {
			t.Fatalf("unexpected tool %q", tool.Name)
		}
		want[tool.Name] = true
		if tool.Description == "" {
			t.Fatalf("tool %q has no description", tool.Name)
		}
		// The read tools are read-only by design and by token;
		// record_quiz_result is the one deliberate write and must not
		// wear the read-only hint.
		if tool.Name == "record_quiz_result" {
			if tool.Annotations != nil && tool.Annotations.ReadOnlyHint {
				t.Fatalf("record_quiz_result is marked read-only — it is the bridge's one write")
			}
			continue
		}
		if tool.Annotations == nil || !tool.Annotations.ReadOnlyHint {
			t.Fatalf("tool %q is not marked read-only", tool.Name)
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
// record_quiz_result is the exception by name — its own tests below pin
// that it writes only the one quiz POST, and only when the token may.
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

// --- the series and names surfaces (MAD-469) -------------------------------

func TestListNamesBounded(t *testing.T) {
	h := newHarness(t)
	var out struct {
		Names []struct {
			Name      string `json:"name"`
			Mentions  int    `json:"mentions"`
			FirstSeen struct {
				CharStart int    `json:"char_start"`
				DeepLink  string `json:"deep_link"`
			} `json:"first_seen"`
		} `json:"names"`
		Bound struct {
			CharOffset int `json:"char_offset"`
		} `json:"bound"`
		Note string `json:"note"`
	}
	h.call(t, "list_names", map[string]any{"book": fxEntry}, &out)

	// Only the names the reader has met: hill and village from Alpha.
	// The detective and the butler are in Beta, past the position, so
	// they do not exist yet.
	got := map[string]int{}
	for _, n := range out.Names {
		got[n.Name] = n.Mentions
	}
	if got["village"] != 1 || got["hill"] != 1 {
		t.Fatalf("names = %v, want village and hill", got)
	}
	if _, ok := got["detective"]; ok {
		t.Fatalf("detective exists before being met: %v", got)
	}
	if _, ok := got["butler"]; ok {
		t.Fatalf("butler exists before the reveal: %v", got)
	}
	if out.Bound.CharOffset != h.stub.position {
		t.Fatalf("bound = %d, want the position", out.Bound.CharOffset)
	}
	for _, n := range out.Names {
		if !strings.HasPrefix(n.FirstSeen.DeepLink, h.stub.ts.URL+"/books/") {
			t.Fatalf("first_seen link not absolutized: %q", n.FirstSeen.DeepLink)
		}
	}

	// The spoiler opt-in lifts the clamp, as everywhere.
	h.call(t, "list_names", map[string]any{"book": fxEntry, "include_spoilers": true}, &out)
	if len(out.Names) != 4 {
		t.Fatalf("spoiled names = %d, want the full cast of 4", len(out.Names))
	}
}

func TestSeriesTools(t *testing.T) {
	h := newHarness(t)
	var index struct {
		Series []struct {
			Name     string `json:"name"`
			Books    int    `json:"books"`
			Finished int    `json:"finished"`
			Reading  int    `json:"reading"`
		} `json:"series"`
	}
	h.call(t, "list_series", map[string]any{}, &index)
	if len(index.Series) != 1 || index.Series[0].Name != fxSeries ||
		index.Series[0].Books != 2 || index.Series[0].Finished != 1 || index.Series[0].Reading != 1 {
		t.Fatalf("series index = %+v", index.Series)
	}

	var detail struct {
		Name  string `json:"name"`
		Books []struct {
			EntryID  string `json:"entry_id"`
			Title    string `json:"title"`
			Status   string `json:"status"`
			Finished bool   `json:"finished"`
			Position struct {
				CharOffset int     `json:"char_offset"`
				Percent    float64 `json:"percent"`
			} `json:"position"`
			DeepLink string `json:"deep_link"`
		} `json:"books"`
	}
	h.call(t, "get_series", map[string]any{"series": fxSeries}, &detail)
	if detail.Name != fxSeries || len(detail.Books) != 2 {
		t.Fatalf("series detail = %+v", detail)
	}
	// Reading order: the finished book one, the underway book two.
	if detail.Books[0].EntryID != "entry-2" || !detail.Books[0].Finished {
		t.Fatalf("member one = %+v", detail.Books[0])
	}
	if detail.Books[1].EntryID != fxEntry || detail.Books[1].Finished ||
		detail.Books[1].Position.CharOffset != h.stub.position {
		t.Fatalf("member two = %+v", detail.Books[1])
	}
	if !strings.Contains(detail.Books[1].DeepLink, "/books/"+fxEntry+"/read?offset=") {
		t.Fatalf("underway deep link = %q", detail.Books[1].DeepLink)
	}
}

func TestSeriesToolsDegradeCleanly(t *testing.T) {
	h := newHarness(t)
	h.stub.seriesMissing = true
	msg := h.callErr(t, "list_series", map[string]any{})
	if !strings.Contains(msg, "does not have book series") {
		t.Fatalf("degrade message = %q", msg)
	}
	msg = h.callErr(t, "get_series", map[string]any{"series": fxSeries})
	if !strings.Contains(msg, "list_series") {
		t.Fatalf("get_series degrade should point at list_series: %q", msg)
	}
}

// --- the prompts (MAD-469) --------------------------------------------------

func TestPromptSurface(t *testing.T) {
	h := newHarness(t)
	res, err := h.session.ListPrompts(h.ctx, nil)
	if err != nil {
		t.Fatalf("list prompts: %v", err)
	}
	// name → the arguments, in order; a "?" marks the optional ones.
	want := map[string][]string{
		"previously_on":   {"book"},
		"cast_list":       {"book"},
		"series_so_far":   {"series"},
		"quiz_me":         {"book", "chapters?"},
		"discussion_prep": {"book", "section?"},
	}
	seen := map[string]bool{}
	for _, p := range res.Prompts {
		expected, ok := want[p.Name]
		if !ok {
			t.Fatalf("unexpected prompt %q", p.Name)
		}
		seen[p.Name] = true
		if p.Description == "" {
			t.Fatalf("prompt %q has no description", p.Name)
		}
		if len(p.Arguments) != len(expected) {
			t.Fatalf("prompt %q arguments = %+v, want %v", p.Name, p.Arguments, expected)
		}
		for i, arg := range p.Arguments {
			if arg.Name != strings.TrimSuffix(expected[i], "?") {
				t.Fatalf("prompt %q argument %d = %q, want %q", p.Name, i, arg.Name, expected[i])
			}
			if arg.Required != !strings.HasSuffix(expected[i], "?") {
				t.Fatalf("prompt %q argument %q required = %v", p.Name, arg.Name, arg.Required)
			}
		}
	}
	for name := range want {
		if !seen[name] {
			t.Fatalf("prompt %q missing from the surface", name)
		}
	}
}

func TestPromptsRenderTheirPlaybooks(t *testing.T) {
	h := newHarness(t)
	cases := []struct {
		prompt string
		argKey string
		argVal string
		// needles the playbook must carry beyond the shared rules.
		extra []string
	}{
		{"previously_on", "book", "The Village Mystery", nil},
		{"cast_list", "book", "The Village Mystery", nil},
		{"series_so_far", "series", fxSeries, nil},
		{"quiz_me", "book", "The Village Mystery", []string{"record_quiz_result", "WITHOUT"}},
		{"discussion_prep", "book", "The Village Mystery", []string{"doesn't settle"}},
	}
	for _, tc := range cases {
		res, err := h.session.GetPrompt(h.ctx, &mcp.GetPromptParams{
			Name:      tc.prompt,
			Arguments: map[string]string{tc.argKey: tc.argVal},
		})
		if err != nil {
			t.Fatalf("get %s: %v", tc.prompt, err)
		}
		if len(res.Messages) != 1 || res.Messages[0].Role != "user" {
			t.Fatalf("%s messages = %+v", tc.prompt, res.Messages)
		}
		text, ok := res.Messages[0].Content.(*mcp.TextContent)
		if !ok {
			t.Fatalf("%s content = %T", tc.prompt, res.Messages[0].Content)
		}
		// The playbook carries its subject, its rules, and the tools it
		// steers the client through.
		needles := []string{tc.argVal, "include_spoilers", "deep_link", "read_text"}
		needles = append(needles, tc.extra...)
		for _, needle := range needles {
			if !strings.Contains(text.Text, needle) {
				t.Fatalf("%s playbook lacks %q", tc.prompt, needle)
			}
		}
	}
}

func TestPromptRequiresItsArgument(t *testing.T) {
	h := newHarness(t)
	if _, err := h.session.GetPrompt(h.ctx, &mcp.GetPromptParams{Name: "previously_on"}); err == nil {
		t.Fatal("a prompt without its required argument must fail")
	}
}

// TestPreviouslyOnPlaybookIsLeakFree is the acceptance bar end-to-end: the
// exact tool walk previously_on prescribes, driven through a real MCP
// session against the fixture backhog, and every byte any tool answered
// with is asserted clean of the reveal. The clamp did the withholding; the
// prompt just has to keep the client inside it.
func TestPreviouslyOnPlaybookIsLeakFree(t *testing.T) {
	h := newHarness(t)

	// Step 1: resolve the book.
	var library struct {
		Books []struct {
			EntryID string `json:"entry_id"`
			Status  string `json:"status"`
		} `json:"books"`
	}
	h.call(t, "list_books", map[string]any{}, &library)
	if len(library.Books) != 1 || library.Books[0].EntryID != fxEntry {
		t.Fatalf("library = %+v", library.Books)
	}

	// Step 2: the position the recap stands "as of".
	var pos struct {
		CharOffset int     `json:"char_offset"`
		Percent    float64 `json:"percent"`
	}
	h.call(t, "get_reading_position", map[string]any{"book": fxEntry}, &pos)

	// Step 3: the chapters, locked markers and all.
	var chapters map[string]any
	h.call(t, "list_chapters", map[string]any{"book": fxEntry}, &chapters)

	// Step 4: read what the reader has read, paging with next_from.
	var accumulated strings.Builder
	from := 0
	for {
		var page struct {
			Text     string `json:"text"`
			NextFrom *int   `json:"next_from"`
			Note     string `json:"note"`
		}
		h.call(t, "read_text", map[string]any{"book": fxEntry, "from": from}, &page)
		accumulated.WriteString(page.Text)
		accumulated.WriteString(page.Note)
		if page.NextFrom == nil {
			break
		}
		from = *page.NextFrom
	}

	// Step 5: the cast so far, and one grounding mention each.
	var names struct {
		Names []struct {
			Name string `json:"name"`
		} `json:"names"`
	}
	h.call(t, "list_names", map[string]any{"book": fxEntry}, &names)
	for _, n := range names.Names {
		var mentions map[string]any
		h.call(t, "find_mentions", map[string]any{"book": fxEntry, "name": n.Name}, &mentions)
	}

	// Everything any tool said, in one bag: nothing in it may carry the
	// reveal, the withheld word, or the unread final chapter.
	assertNoLeak(t, "the previously_on walk",
		accumulated.String(), fmt.Sprintf("%v", chapters), fmt.Sprintf("%v", names))
}

// --- the quiz surface (MAD-471) ----------------------------------------------

// TestRecordQuizResult drives the bridge's one write end to end: the tool
// grades nothing and invents nothing, it carries the client's own report
// to the endpoint, and the answer says what the count unlocked.
func TestRecordQuizResult(t *testing.T) {
	h := newHarness(t)
	h.stub.quizWrite = true

	var out struct {
		Recorded     bool `json:"recorded"`
		Questions    int  `json:"questions"`
		Correct      int  `json:"correct"`
		Achievements []struct {
			ID    string `json:"id"`
			Title string `json:"title"`
		} `json:"achievements"`
	}
	h.call(t, "record_quiz_result", map[string]any{
		"book": fxEntry, "questions": 6, "correct": 5,
		"chapter_from": 1, "chapter_to": 1,
	}, &out)
	if !out.Recorded || out.Questions != 6 || out.Correct != 5 {
		t.Fatalf("recorded result = %+v", out)
	}
	if len(out.Achievements) != 1 || out.Achievements[0].ID != "book_report" {
		t.Fatalf("first result unlocked %+v, want book_report", out.Achievements)
	}

	// Exactly one write left the bridge, shaped for the endpoint, with
	// the honest counts and the source named.
	var post *request
	for i, r := range h.stub.requests() {
		if r.Method == http.MethodPost {
			if post != nil {
				t.Fatalf("more than one write: %s %s", r.Method, r.Path)
			}
			post = &h.stub.requests()[i]
		}
	}
	if post == nil {
		t.Fatal("the bridge never issued the quiz POST")
	}
	if post.Path != "/api/books/"+fxEntry+"/quiz-results" {
		t.Fatalf("write path = %s", post.Path)
	}
	var sent struct {
		Questions    int    `json:"questions"`
		Correct      int    `json:"correct"`
		Source       string `json:"source"`
		ChapterRange *struct {
			From int `json:"from"`
			To   int `json:"to"`
		} `json:"chapter_range"`
	}
	if err := json.Unmarshal([]byte(post.Body), &sent); err != nil {
		t.Fatalf("write body %q: %v", post.Body, err)
	}
	if sent.Questions != 6 || sent.Correct != 5 || sent.Source != "mcp" {
		t.Fatalf("write body = %s", post.Body)
	}
	if sent.ChapterRange == nil || sent.ChapterRange.From != 1 || sent.ChapterRange.To != 1 {
		t.Fatalf("chapter range = %+v", sent.ChapterRange)
	}
}

// TestRecordQuizResultNeedsWriteScope pins the degrade path: a read-only
// token gets the API's 403, and the tool's answer tells the user exactly
// which scope to mint — not a stack trace.
func TestRecordQuizResultNeedsWriteScope(t *testing.T) {
	h := newHarness(t)
	msg := h.callErr(t, "record_quiz_result", map[string]any{
		"book": fxEntry, "questions": 5, "correct": 5,
	})
	if !strings.Contains(msg, "quiz:write") {
		t.Fatalf("degrade message = %q, want it to name the quiz:write scope", msg)
	}
	// The attempt reached the API and was refused — nothing was stored.
	if h.stub.quizResults != 0 {
		t.Fatalf("a refused report was stored (%d results)", h.stub.quizResults)
	}
}

// TestRecordQuizResultValidation keeps the report honest at the tool's own
// edge: the arithmetic has to hold before it ever reaches the API.
func TestRecordQuizResultValidation(t *testing.T) {
	h := newHarness(t)
	h.stub.quizWrite = true
	for name, args := range map[string]map[string]any{
		"no questions":     {"book": fxEntry, "questions": 0, "correct": 0},
		"flattering count": {"book": fxEntry, "questions": 5, "correct": 6},
		"backwards range":  {"book": fxEntry, "questions": 5, "correct": 5, "chapter_from": 3, "chapter_to": 1},
		"half a range":     {"book": fxEntry, "questions": 5, "correct": 5, "chapter_to": 2},
	} {
		if msg := h.callErr(t, "record_quiz_result", args); msg == "" {
			t.Errorf("%s was accepted", name)
		}
	}
	for _, r := range h.stub.requests() {
		if r.Method != http.MethodGet {
			t.Fatalf("an invalid report still issued %s %s", r.Method, r.Path)
		}
	}
}

// TestQuizMePlaybookIsLeakFree walks the exact tool round quiz_me
// prescribes against the fixture backhog — position, chapters, the read
// chapters' text, a passage check while grading, then the honest report —
// and asserts every byte any tool answered with stays clean of the reveal.
// The score reported is the one the walk actually produced: only Alpha's
// text is readable, so a quiz written from it cannot ask, and a grade
// cannot cite, anything past the bound.
func TestQuizMePlaybookIsLeakFree(t *testing.T) {
	h := newHarness(t)
	h.stub.quizWrite = true

	var pos struct {
		CharOffset int `json:"char_offset"`
	}
	h.call(t, "get_reading_position", map[string]any{"book": fxEntry}, &pos)

	var chapters map[string]any
	h.call(t, "list_chapters", map[string]any{"book": fxEntry}, &chapters)

	var page struct {
		Text     string `json:"text"`
		NextFrom *int   `json:"next_from"`
		Note     string `json:"note"`
	}
	h.call(t, "read_text", map[string]any{"book": fxEntry, "chapter": 1}, &page)

	var passage map[string]any
	h.call(t, "get_passage", map[string]any{"book": fxEntry, "char_start": 0, "char_end": 10}, &passage)

	// Grading from the readable window alone: everything the quiz knows
	// is in these answers.
	var grade strings.Builder
	grade.WriteString(page.Text)
	grade.WriteString(page.Note)
	grade.WriteString(fmt.Sprint(passage))
	assertNoLeak(t, "the quiz_me walk", grade.String(), fmt.Sprint(chapters))

	// The honest report: one question, one correct — the count the walk
	// can actually defend.
	var out struct {
		Recorded bool `json:"recorded"`
	}
	h.call(t, "record_quiz_result", map[string]any{
		"book": fxEntry, "questions": 1, "correct": 1,
		"chapter_from": 1, "chapter_to": 1,
	}, &out)
	if !out.Recorded {
		t.Fatal("the report was not recorded")
	}
}
