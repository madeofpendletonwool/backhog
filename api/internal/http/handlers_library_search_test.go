package http

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
)

// The library-wide search's acceptance bar (MAD-470), on the leak fixture's
// pattern: a shelf of two books, a phrase query that must come back with
// working peek deep links, and the proof — asserted as absence, over the
// whole body — that nothing past a reading position ever rides along. The
// shelf also carries the permission proof: another account's unshared book
// does not answer, and a shared one does.

// libraryShelfEntry is the second book on the shelf: a festival story the
// first book never mentions, so a query can name exactly one book.
const shelfSecondEntry = "e2"

// secondShelfEpub builds that book: two chapters about a lantern festival.
func secondShelfEpub(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	entries := [][2]string{
		{"META-INF/container.xml", `<?xml version="1.0"?>
<container version="1.0" xmlns="urn:oasis:names:tc:opendocument:xmlns:container">
  <rootfiles><rootfile full-path="OEBPS/content.opf" media-type="application/oebps-package+xml"/></rootfiles>
</container>`},
		{"OEBPS/content.opf", `<?xml version="1.0"?>
<package xmlns="http://www.idpf.org/2007/opf" version="2.0">
  <manifest>
    <item id="ncx" href="toc.ncx" media-type="application/x-dtbncx+xml"/>
    <item id="c1" href="c1.xhtml" media-type="application/xhtml+xml"/>
    <item id="c2" href="c2.xhtml" media-type="application/xhtml+xml"/>
  </manifest>
  <spine toc="ncx"><itemref idref="c1"/><itemref idref="c2"/></spine>
</package>`},
		{"OEBPS/toc.ncx", `<?xml version="1.0"?>
<ncx xmlns="http://www.daisy.org/z3986/2005/ncx/">
  <navMap>
    <navPoint><navLabel><text>Arrival</text></navLabel><content src="c1.xhtml"/></navPoint>
    <navPoint><navLabel><text>Procession</text></navLabel><content src="c2.xhtml"/></navPoint>
  </navMap>
</ncx>`},
		{"OEBPS/c1.xhtml", `<html><body>
		  <h1>Arrival</h1>
		  <p>The lantern festival began at the harbour before dawn.</p></body></html>`},
		{"OEBPS/c2.xhtml", `<html><body>
		  <h1>Procession</h1>
		  <p>By nightfall the whole festival floated up the river, lantern by lantern.</p></body></html>`},
	}
	for _, e := range entries {
		w, err := zw.Create(e[0])
		if err != nil {
			t.Fatalf("zip entry %s: %v", e[0], err)
		}
		if _, err := io.WriteString(w, e[1]); err != nil {
			t.Fatalf("zip write %s: %v", e[0], err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
	return buf.Bytes()
}

// libraryShelf is the leak fixture plus the festival book, both parsed and
// indexed through the real read path — the only way the index ever fills.
type libraryShelf struct {
	*leakApp
}

func newLibraryShelf(t *testing.T) *libraryShelf {
	t.Helper()
	app := newLeakApp(t)

	// The leak fixture's file is ownerless as attached; the permission
	// proof below needs it to belong to somebody.
	if _, err := app.store.DB().Exec(`
		UPDATE media_files SET attached_by = ? WHERE book_id = 'OL1W'`, app.userID); err != nil {
		t.Fatalf("own the fixture file: %v", err)
	}

	// The second book, its own entry, its owner-attached file.
	data := secondShelfEpub(t)
	p := app.root + "/festival.epub"
	if err := os.MkdirAll(app.root, 0o755); err != nil {
		t.Fatalf("mkdir root: %v", err)
	}
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatalf("write festival fixture: %v", err)
	}
	if _, err := app.store.DB().Exec(`
		INSERT INTO books (id, title) VALUES ('OL2W', 'The Lantern Festival')`); err != nil {
		t.Fatalf("seed second book: %v", err)
	}
	if _, err := app.store.DB().Exec(`
		INSERT INTO library_entries (id, user_id, media_type, book_id, status)
		VALUES (?, ?, 'book', 'OL2W', 'backlog')`, shelfSecondEntry, app.userID); err != nil {
		t.Fatalf("seed second entry: %v", err)
	}
	if _, err := app.store.DB().Exec(`
		INSERT INTO media_files (root, path, kind, size_bytes, mtime, book_id, scanned_at, attached_by, is_primary_text)
		VALUES (?, 'festival.epub', 'epub', ?, ?, 'OL2W', ?, ?, 1)`,
		app.root, len(data), time.Now().UnixNano(), time.Now().UTC(), app.userID); err != nil {
		t.Fatalf("seed festival file: %v", err)
	}

	// Both books go through the text endpoint once: parsing on demand is
	// how the arena fills a canonical text, and the index rides along.
	for _, entry := range []string{epubFixtureEntry, shelfSecondEntry} {
		if status, body := app.get(t, "/api/books/"+entry+"/text"); status != http.StatusOK {
			t.Fatalf("parse %s: status %d: %v", entry, status, body)
		}
	}
	return &libraryShelf{leakApp: app}
}

// librarySearch runs one query over the cookie session.
func (s *libraryShelf) librarySearch(t *testing.T, query string) (int, map[string]any) {
	t.Helper()
	return s.get(t, "/api/books/search/text?q="+url.QueryEscape(query))
}

func libraryHits(t *testing.T, body map[string]any) []map[string]any {
	t.Helper()
	raw, ok := body["results"].([]any)
	if !ok {
		t.Fatalf("results = %#v", body["results"])
	}
	out := make([]map[string]any, 0, len(raw))
	for _, r := range raw {
		out = append(out, r.(map[string]any))
	}
	return out
}

func libraryBeyond(t *testing.T, body map[string]any) []map[string]any {
	t.Helper()
	raw, _ := body["matched_beyond"].([]any)
	out := make([]map[string]any, 0, len(raw))
	for _, r := range raw {
		out = append(out, r.(map[string]any))
	}
	return out
}

// assertNoLibraryLeak fails if the reveal — or any text past the fixture's
// cut — appears anywhere in the answer: hits, snippets, titles, the bound,
// the lot. The whole body, marshalled, is the haystack, minus the query
// echo: the client typed those words, so their presence is not a leak.
func assertNoLibraryLeak(t *testing.T, what string, body map[string]any) {
	t.Helper()
	stripped := make(map[string]any, len(body))
	for k, v := range body {
		if k == "query" {
			continue
		}
		stripped[k] = v
	}
	raw, err := json.Marshal(stripped)
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	assertNoLeak(t, what, string(raw))
}

func TestLibrarySearchFindsPhrasesAcrossTheShelf(t *testing.T) {
	shelf := newLibraryShelf(t)

	// Both books are unread, so hits arrive through the unread opt-in —
	// the flow a reader reaches for with "which of my books says this?".
	status, body := shelf.get(t,
		"/api/books/search/text?q="+url.QueryEscape("lantern festival")+"&unread=hits")
	if status != http.StatusOK {
		t.Fatalf("status = %d: %v", status, body)
	}
	// The mystery never mentions lanterns, and only the festival book's
	// first chapter holds the phrase itself.
	hits := libraryHits(t, body)
	if len(hits) != 1 {
		t.Fatalf("hits = %d, want 1: %v", len(hits), body)
	}
	hit := hits[0]
	if hit["book_id"] != shelfSecondEntry {
		t.Errorf("hit from %v, want the festival book", hit["book_id"])
	}
	ch, _ := hit["chapter"].(map[string]any)
	if ch == nil || ch["title"] != "Arrival" {
		t.Errorf("hit chapter = %v", ch)
	}
	start := int(hit["char_start"].(float64))
	if hit["deep_link"] != deepLink(shelfSecondEntry, start) {
		t.Errorf("deep link = %v", hit["deep_link"])
	}
	snippet, _ := hit["snippet"].(map[string]any)
	if snippet == nil || !strings.Contains(
		strings.ToLower(snippet["passage"].(string)), "festival") {
		t.Errorf("hit snippet = %v", snippet)
	}
	if hit["percent"].(float64) <= 0 {
		t.Errorf("hit percent = %v", hit["percent"])
	}
	if beyond := libraryBeyond(t, body); len(beyond) != 0 {
		t.Errorf("beyond = %v, want none (the festival book answered with hits)", beyond)
	}
	if body["mode"] != "phrase" {
		t.Errorf("mode = %v, want phrase", body["mode"])
	}

	// The single word reaches both chapters, still only the festival book.
	status, both := shelf.get(t,
		"/api/books/search/text?q="+url.QueryEscape("festival")+"&unread=hits")
	if status != http.StatusOK {
		t.Fatalf("festival status = %d", status)
	}
	hits = libraryHits(t, both)
	if len(hits) != 2 {
		t.Fatalf("festival hits = %d, want 2: %v", len(hits), both)
	}
	for _, hit := range hits {
		if hit["book_id"] != shelfSecondEntry {
			t.Errorf("festival hit from %v", hit["book_id"])
		}
	}
}

func TestLibrarySearchUnreadBooksAreTitlesOnly(t *testing.T) {
	shelf := newLibraryShelf(t) // the festival book was never opened

	// Default: the book says it matches and nothing else.
	_, body := shelf.librarySearch(t, "harbour before dawn")
	if hits := libraryHits(t, body); len(hits) != 0 {
		t.Errorf("unread book served hits: %v", hits)
	}
	beyond := libraryBeyond(t, body)
	if len(beyond) != 1 || beyond[0]["book_id"] != shelfSecondEntry ||
		beyond[0]["title"] != "The Lantern Festival" {
		t.Fatalf("beyond = %v, want the festival book titled", beyond)
	}
	for _, banned := range []string{"char_start", "char_end", "chapter", "snippet", "deep_link"} {
		if _, has := beyond[0][banned]; has {
			t.Errorf("beyond row carries %q — a title is the whole answer", banned)
		}
	}

	// unread=hits opts in, per book, without touching anything else.
	status, opened := shelf.get(t, "/api/books/search/text?q="+url.QueryEscape("harbour before dawn")+"&unread=hits")
	if status != http.StatusOK {
		t.Fatalf("unread=hits status = %d: %v", status, opened)
	}
	hits := libraryHits(t, opened)
	if len(hits) != 1 || hits[0]["book_id"] != shelfSecondEntry {
		t.Fatalf("unread=hits hits = %v", hits)
	}
	if snippet, _ := hits[0]["snippet"].(map[string]any); snippet == nil ||
		!strings.Contains(strings.ToLower(snippet["passage"].(string)), "harbour") {
		t.Errorf("unread=hit snippet = %v", snippet)
	}

	// until=none is the loud opt-in and subsumes the narrow one.
	status, noneBody := shelf.get(t, "/api/books/search/text?q="+url.QueryEscape("harbour before dawn")+"&until=none")
	if status != http.StatusOK {
		t.Fatalf("until=none status = %d", status)
	}
	if hits := libraryHits(t, noneBody); len(hits) != 1 {
		t.Errorf("until=none hits = %v", hits)
	}
}

func TestLibrarySearchLeakBar(t *testing.T) {
	shelf := newLibraryShelf(t)
	geo := leakGeometryOf(t, shelf.leakApp)
	shelf.setLeakPosition(t, geo.beta) // read exactly through Alpha

	// The reveal phrase exists on the shelf — the unclamped search finds
	// it — but the default clamps to the position and the mystery says so
	// as a title and nothing more.
	if _, cookie := shelf.get(t, "/api/books/search/text?q="+url.QueryEscape(leakReveal)+"&until=none"); cookie["total"] == float64(0) {
		t.Fatalf("fixture sanity: unclamped search found no %q", leakReveal)
	}
	_, body := shelf.librarySearch(t, leakReveal)
	assertNoLibraryLeak(t, "clamped library search", body)
	if hits := libraryHits(t, body); len(hits) != 0 {
		t.Errorf("clamped search served reveal hits: %v", hits)
	}
	beyond := libraryBeyond(t, body)
	if len(beyond) != 1 || beyond[0]["book_id"] != epubFixtureEntry {
		t.Fatalf("beyond = %v, want the mystery titled", beyond)
	}
	if body["total"] != float64(0) {
		t.Errorf("clamped total = %v, want 0", body["total"])
	}
	if bound, _ := body["bound"].(map[string]any); bound["until"] != untilPosition {
		t.Errorf("bound = %v, want position", bound)
	}

	// A read-territory phrase answers with its hit — and a context that
	// stops at the cut.
	_, village := shelf.librarySearch(t, "quiet village")
	hits := libraryHits(t, village)
	if len(hits) != 1 || hits[0]["book_id"] != epubFixtureEntry {
		t.Fatalf("quiet village hits = %v", village)
	}
	snippet, _ := hits[0]["snippet"].(map[string]any)
	if snippet == nil || !strings.Contains(strings.ToLower(snippet["passage"].(string)), "village") {
		t.Errorf("hit snippet = %v", snippet)
	}
	assertNoLibraryLeak(t, "read-territory hit", village)

	// A phrase past the cut but inside the straddling paragraph is dropped
	// whole, exactly as the in-book search drops it.
	_, candle := shelf.librarySearch(t, leakPastCut)
	assertNoLibraryLeak(t, "cut word search", candle)

	// The same client, saying the quiet part out loud, gets everything:
	// the festival book included, because until=none subsumes unread.
	status, none := shelf.get(t, "/api/books/search/text?q="+url.QueryEscape(leakReveal)+"&until=none")
	if status != http.StatusOK {
		t.Fatalf("until=none status = %d", status)
	}
	if hits := libraryHits(t, none); len(hits) != 1 ||
		!strings.Contains(strings.ToLower(hits[0]["snippet"].(map[string]any)["passage"].(string)), "butler") {
		t.Errorf("until=none withheld the reveal: %v", none)
	}

	// A token-authenticated client gets the identical default — the clamp
	// here is a property of the endpoint, not of the credential.
	resp, tokenBody := asLeakToken(t, shelf.ts.URL, shelf.secret, http.MethodGet,
		"/api/books/search/text?q="+url.QueryEscape(leakReveal), nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("token status = %d: %v", resp.StatusCode, tokenBody)
	}
	assertNoLibraryLeak(t, "token library search", tokenBody)

	// Refusals keep their shapes: too short is a 422, a bogus until is a
	// 400, a numeric until is refused as meaningless shelf-wide.
	for _, bad := range []string{
		"/api/books/search/text?q=ab",
		"/api/books/search/text?q=words&until=banana",
		"/api/books/search/text?q=words&until=300",
	} {
		status, _ := shelf.get(t, bad)
		if status != http.StatusUnprocessableEntity && status != http.StatusBadRequest {
			t.Errorf("%s: status = %d", bad, status)
		}
	}
}

func TestLibrarySearchScopeIsTheUsers(t *testing.T) {
	shelf := newLibraryShelf(t)

	// A second account with an entry for the mystery but no share: the
	// files belong to the shelf's owner, so the book must not answer.
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("jar: %v", err)
	}
	stranger := &http.Client{Jar: jar, Timeout: 10 * time.Second}
	register(t, shelf.ts.URL, stranger, "stranger@example.com", "stranger", "hogwash123")
	me := struct {
		ID string `json:"id"`
	}{}
	resp, err := stranger.Get(shelf.ts.URL + "/api/auth/me")
	if err != nil {
		t.Fatalf("me: %v", err)
	}
	if err := json.NewDecoder(resp.Body).Decode(&me); err != nil {
		t.Fatalf("decode me: %v", err)
	}
	resp.Body.Close()
	if _, err := shelf.store.DB().Exec(`
		INSERT INTO library_entries (id, user_id, media_type, book_id, status)
		VALUES ('stranger-entry', ?, 'book', 'OL1W', 'backlog')`, me.ID); err != nil {
		t.Fatalf("seed stranger entry: %v", err)
	}

	strangerSearch := func(query string) map[string]any {
		resp, err := stranger.Get(shelf.ts.URL + "/api/books/search/text?q=" + url.QueryEscape(query))
		if err != nil {
			t.Fatalf("stranger search: %v", err)
		}
		defer resp.Body.Close()
		body := map[string]any{}
		_ = json.NewDecoder(resp.Body).Decode(&body)
		return body
	}

	unshared := strangerSearch("quiet village")
	if hits := libraryHits(t, unshared); len(hits) != 0 {
		t.Errorf("unshared book answered a stranger: %v", hits)
	}
	if beyond := libraryBeyond(t, unshared); len(beyond) != 0 {
		t.Errorf("unshared book titled itself to a stranger: %v", beyond)
	}

	// The owner shares; the same query answers, through the stranger's
	// own entry, with the stranger's own clamp (they have read nothing).
	share, _ := json.Marshal(map[string]string{"user_id": me.ID})
	req, err := http.NewRequest(http.MethodPost,
		shelf.ts.URL+"/api/books/"+epubFixtureEntry+"/shares", bytes.NewReader(share))
	if err != nil {
		t.Fatalf("share request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	sresp, err := shelf.client.Do(req)
	if err != nil {
		t.Fatalf("share: %v", err)
	}
	sresp.Body.Close()
	if sresp.StatusCode != http.StatusCreated && sresp.StatusCode != http.StatusOK {
		t.Fatalf("share status = %d", sresp.StatusCode)
	}

	shared := strangerSearch("quiet village")
	if hits := libraryHits(t, shared); len(hits) != 0 {
		t.Errorf("a stranger who read nothing was served hits: %v", hits)
	}
	if beyond := libraryBeyond(t, shared); len(beyond) != 1 || beyond[0]["book_id"] != "stranger-entry" {
		t.Errorf("shared beyond = %v, want the stranger's own entry titled", beyond)
	}
	// Saying the quiet part out loud works for them too.
	resp2, err := stranger.Get(shelf.ts.URL + "/api/books/search/text?q=" + url.QueryEscape("quiet village") + "&until=none")
	if err != nil {
		t.Fatalf("stranger until=none: %v", err)
	}
	defer resp2.Body.Close()
	noneBody := map[string]any{}
	_ = json.NewDecoder(resp2.Body).Decode(&noneBody)
	if hits := libraryHits(t, noneBody); len(hits) != 1 || hits[0]["book_id"] != "stranger-entry" {
		t.Errorf("stranger until=none hits = %v", hits)
	}
}
