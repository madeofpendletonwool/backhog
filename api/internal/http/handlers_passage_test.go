package http

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/http/cookiejar"
	"strings"
	"testing"
	"time"
)

// The passage and physical-copy endpoints, exercised end to end over a
// real parsed EPUB: match page text into the canonical text, register a
// printing, pin pages as anchors, and see the position endpoints derive
// a page view through them.

// passageEpubFixture is a book long enough to match against: one chapter
// of many tagged paragraphs with a repeated epigraph at start and end.
func passageEpubFixture(t *testing.T) []byte {
	t.Helper()
	rng := rand.New(rand.NewSource(7))
	palette := []string{
		"night", "corridor", "was", "quiet", "and", "the", "clock", "ticked",
		"slowly", "government", "burning", "lantern", "harbour", "café",
		"séance", "crème", "naïve", "winter", "mountain", "river", "garden",
		"morning", "she", "said", "nothing", "at", "all",
	}
	para := func(tag string, n int) string {
		words := make([]string, 0, n+1)
		words = append(words, tag)
		for i := 0; i < n; i++ {
			words = append(words, palette[rng.Intn(len(palette))])
		}
		return strings.Join(words, " ") + "."
	}

	epigraph := para("Epigraph", 44)
	paras := []string{epigraph}
	for i := 0; i < 60; i++ {
		paras = append(paras, para(fmt.Sprintf("Marker%04d", i), 44))
	}
	paras = append(paras, epigraph)
	body := "<html><body><h1>Only Chapter</h1><p>" + strings.Join(paras, "</p><p>") + "</p></body></html>"

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, e := range [][2]string{
		{"META-INF/container.xml", `<?xml version="1.0"?>
<container version="1.0" xmlns="urn:oasis:names:tc:opendocument:xmlns:container">
  <rootfiles><rootfile full-path="OEBPS/content.opf" media-type="application/oebps-package+xml"/></rootfiles>
</container>`},
		{"OEBPS/content.opf", `<?xml version="1.0"?>
<package xmlns="http://www.idpf.org/2007/opf" version="2.0">
  <manifest>
    <item id="ncx" href="toc.ncx" media-type="application/x-dtbncx+xml"/>
    <item id="c1" href="c1.xhtml" media-type="application/xhtml+xml"/>
  </manifest>
  <spine toc="ncx"><itemref idref="c1"/></spine>
</package>`},
		{"OEBPS/toc.ncx", `<?xml version="1.0"?>
<ncx xmlns="http://www.daisy.org/z3986/2005/ncx/">
  <navMap><navPoint><navLabel><text>Only Chapter</text></navLabel><content src="c1.xhtml"/></navPoint></navMap>
</ncx>`},
		{"OEBPS/c1.xhtml", body},
	} {
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

// send is get()'s write-shaped sibling.
func (a *epubTestApp) send(t *testing.T, method, path string, body any) (int, map[string]any) {
	t.Helper()
	var payload []byte
	if body != nil {
		var err error
		if payload, err = json.Marshal(body); err != nil {
			t.Fatalf("marshal: %v", err)
		}
	}
	req, err := http.NewRequest(method, a.ts.URL+path, bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := a.client.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	var out map[string]any
	json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

// canonicalText forces a parse and returns the whole canonical text.
func (a *epubTestApp) canonicalText(t *testing.T) string {
	t.Helper()
	status, body := a.get(t, "/api/books/"+epubFixtureEntry+"/text")
	if status != http.StatusOK {
		t.Fatalf("text status = %d: %v", status, body)
	}
	return body["text"].(string)
}

// manglePassage is the OCR failure kit: dropped diacritics, rn read as
// m, and the occasional swapped character pair.
func manglePassage(s string, seed int64) string {
	fold := map[rune]rune{
		'é': 'e', 'è': 'e', 'ê': 'e', 'á': 'a', 'à': 'a', 'í': 'i',
		'ó': 'o', 'ú': 'u', 'ü': 'u', 'ö': 'o', 'ä': 'a', 'ï': 'i',
		'ñ': 'n', 'ç': 'c', 'ù': 'u', 'û': 'u',
	}
	rng := rand.New(rand.NewSource(seed))
	words := strings.Split(s, " ")
	for i := range words {
		w := []rune(words[i])
		for j, r := range w {
			if sub, ok := fold[r]; ok {
				w[j] = sub
			}
		}
		words[i] = strings.ReplaceAll(string(w), "rn", "m")
		w = []rune(words[i])
		if len(w) > 4 && rng.Intn(6) == 0 {
			j := 1 + rng.Intn(len(w)-3)
			w[j], w[j+1] = w[j+1], w[j]
		}
		words[i] = string(w)
	}
	return strings.Join(words, " ")
}

// TestBookPassageMatch pins a clean 40-word passage from the middle of
// the book at exactly its own offset, with context that splices back
// onto the canonical text.
func TestBookPassageMatch(t *testing.T) {
	app := newEpubTestApp(t)
	app.attachEpub(t, passageEpubFixture(t))
	text := app.canonicalText(t)

	fields := strings.Fields(text)
	window := strings.Join(fields[len(fields)/2:len(fields)/2+40], " ")
	at := strings.Index(text, window)

	status, body := app.send(t, http.MethodPost, "/api/books/"+epubFixtureEntry+"/passage",
		map[string]string{"text": window})
	if status != http.StatusOK {
		t.Fatalf("passage status = %d: %v", status, body)
	}
	match := body["match"].(map[string]any)
	if match["char_offset"].(float64) != float64(at) {
		t.Errorf("char_offset = %v, want %d", match["char_offset"], at)
	}
	if match["confidence"].(float64) < 0.99 {
		t.Errorf("confidence = %v, want ~1", match["confidence"])
	}
	if body["ambiguous"] != false {
		t.Errorf("unique passage reported ambiguous")
	}

	// The context is the canonical text around the match, spliced
	// without translation.
	ctxMap := body["context"].(map[string]any)
	before, passageText, after := ctxMap["before"].(string), ctxMap["passage"].(string), ctxMap["after"].(string)
	from, to := at-passageContextBytes, at+len(window)+passageContextBytes
	if from < 0 {
		from = 0
	}
	if to > len(text) {
		to = len(text)
	}
	if joined := before + passageText + after; joined != text[from:to] {
		t.Errorf("context does not splice onto the canonical text")
	}
}

// TestBookPassageNoiseAndRefusals covers the rest of the matcher's
// contract through the API: noisy OCR still lands, short and unmatched
// passages are refused with 422s, and a recurring passage comes back
// ambiguous.
func TestBookPassageNoiseAndRefusals(t *testing.T) {
	app := newEpubTestApp(t)
	app.attachEpub(t, passageEpubFixture(t))
	text := app.canonicalText(t)

	fields := strings.Fields(text)
	window := strings.Join(fields[len(fields)/2:len(fields)/2+40], " ")
	at := strings.Index(text, window)

	noisy := manglePassage(window, 3)
	status, body := app.send(t, http.MethodPost, "/api/books/"+epubFixtureEntry+"/passage",
		map[string]string{"text": noisy})
	if status != http.StatusOK {
		t.Fatalf("noisy passage status = %d: %v", status, body)
	}
	match := body["match"].(map[string]any)
	if match["char_offset"].(float64) != float64(at) {
		t.Errorf("noisy char_offset = %v, want %d", match["char_offset"], at)
	}

	// The repeated epigraph is genuinely ambiguous: alternatives come
	// back and the caller is told to choose.
	epigraph := strings.Join(fields[:45], " ")
	status, body = app.send(t, http.MethodPost, "/api/books/"+epubFixtureEntry+"/passage",
		map[string]string{"text": epigraph})
	if status != http.StatusOK {
		t.Fatalf("recurring passage status = %d: %v", status, body)
	}
	if body["ambiguous"] != true {
		t.Fatalf("recurring passage not flagged ambiguous: %v", body)
	}
	alts, _ := body["alternatives"].([]any)
	if len(alts) == 0 {
		t.Fatal("recurring passage returned no alternatives")
	}
	if got := body["match"].(map[string]any)["char_offset"].(float64); got != 0 {
		t.Errorf("top match = %v, want the first occurrence at 0", got)
	}

	// Too short and no match are both refusals, not guesses.
	for _, c := range []struct {
		what string
		text string
	}{
		{"too short", "the night corridor was very quiet"},
		{"no match", "zq1wjkb zq2wjkb zq3wjkb zq4wjkb zq5wjkb zq6wjkb zq7wjkb zq8wjkb zq9wjkb zq10wjkb zq11wjkb"},
	} {
		status, body = app.send(t, http.MethodPost, "/api/books/"+epubFixtureEntry+"/passage",
			map[string]string{"text": c.text})
		if status != http.StatusUnprocessableEntity {
			t.Errorf("%s: status = %d, want 422 (body %v)", c.what, status, body)
		}
	}

	// Someone else's entry is the usual indistinguishable 404.
	jar, _ := cookiejar.New(nil)
	other := &http.Client{Jar: jar, Timeout: 10 * time.Second}
	register(t, app.ts.URL, other, "snooppass@example.com", "snooppass", "hogwash123")
	resp, err := other.Post(app.ts.URL+"/api/books/"+epubFixtureEntry+"/passage",
		"application/json", strings.NewReader(`{"text":"whatever"}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("cross-user passage: status = %d, want 404", resp.StatusCode)
	}
}

// TestPhysicalCopiesAndAnchors walks the whole bridge: register a
// printing, pin pages, correct a pin, see the position endpoint derive a
// page view through the map, and drop the copy to clear it.
func TestPhysicalCopiesAndAnchors(t *testing.T) {
	app := newEpubTestApp(t)
	app.attachEpub(t, passageEpubFixture(t))
	text := app.canonicalText(t)
	mid := len(text) / 2

	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := app.store.DB().Exec(q, args...); err != nil {
			t.Fatalf("seed %q: %v", q, err)
		}
	}
	exec(`INSERT INTO book_editions (id, book_id, page_count) VALUES ('OL1M', 'OL1W', 320)`)
	exec(`UPDATE library_entries SET edition_id = 'OL1M' WHERE id = ?`, epubFixtureEntry)

	// Register the printing.
	status, body := app.send(t, http.MethodPost, "/api/books/"+epubFixtureEntry+"/copies",
		map[string]string{"edition_id": "OL1M", "notes": "paperback"})
	if status != http.StatusCreated {
		t.Fatalf("create copy status = %d: %v", status, body)
	}
	copyID := body["copy"].(map[string]any)["id"].(string)

	// Duplicate registration is a conflict; a foreign edition is a
	// client bug worth naming.
	status, body = app.send(t, http.MethodPost, "/api/books/"+epubFixtureEntry+"/copies",
		map[string]string{"edition_id": "OL1M"})
	if status != http.StatusConflict {
		t.Errorf("duplicate copy status = %d, want 409", status)
	}
	exec(`INSERT INTO books (id, title) VALUES ('OL9W', 'Other Book')`)
	exec(`INSERT INTO book_editions (id, book_id) VALUES ('OL9M', 'OL9W')`)
	status, body = app.send(t, http.MethodPost, "/api/books/"+epubFixtureEntry+"/copies",
		map[string]string{"edition_id": "OL9M"})
	if status != http.StatusBadRequest || !strings.Contains(body["error"].(string), "not a printing") {
		t.Errorf("foreign edition: status = %d body %v, want 400 naming the problem", status, body)
	}

	// Pin two pages.
	pin := func(page, offset int) (int, map[string]any) {
		t.Helper()
		return app.send(t, http.MethodPost,
			"/api/books/"+epubFixtureEntry+"/copies/"+copyID+"/pages",
			map[string]any{"printed_page": page, "char_offset": offset, "source": "ocr", "confidence": 0.9})
	}
	if status, body = pin(1, 0); status != http.StatusOK {
		t.Fatalf("pin page 1: status = %d: %v", status, body)
	}
	if status, body = pin(240, mid); status != http.StatusOK {
		t.Fatalf("pin page 240: status = %d: %v", status, body)
	}

	// Re-scanning page 240 corrects it rather than adding a second
	// anchor for the page.
	if status, body = pin(240, mid+50); status != http.StatusOK {
		t.Fatalf("re-pin page 240: status = %d: %v", status, body)
	}
	status, body = app.send(t, http.MethodGet, "/api/books/"+epubFixtureEntry+"/copies/"+copyID+"/pages", nil)
	if status != http.StatusOK {
		t.Fatalf("list pages: status = %d: %v", status, body)
	}
	anchors := body["anchors"].([]any)
	if len(anchors) != 2 {
		t.Fatalf("anchors = %d, want 2 after the correction", len(anchors))
	}
	corrected := anchors[1].(map[string]any)
	if corrected["char_offset"].(float64) != float64(mid+50) {
		t.Errorf("page 240 offset = %v, want the corrected %d", corrected["char_offset"], mid+50)
	}

	// Bad pins are refused before they can poison the map.
	for _, bad := range []struct {
		what string
		body map[string]any
	}{
		{"page zero", map[string]any{"printed_page": 0, "char_offset": 5}},
		{"offset past the text", map[string]any{"printed_page": 2, "char_offset": len(text) + 1}},
		{"bad source", map[string]any{"printed_page": 2, "char_offset": 5, "source": "divination"}},
		{"confidence over 1", map[string]any{"printed_page": 2, "char_offset": 5, "source": "ocr", "confidence": 1.5}},
	} {
		status, body = app.send(t, http.MethodPost,
			"/api/books/"+epubFixtureEntry+"/copies/"+copyID+"/pages", bad.body)
		if status != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400", bad.what, status)
		}
	}
	status, _ = app.send(t, http.MethodPost,
		"/api/books/"+epubFixtureEntry+"/copies/nosuchcopy/pages",
		map[string]any{"printed_page": 2, "char_offset": 5})
	if status != http.StatusNotFound {
		t.Errorf("unknown copy: status = %d, want 404", status)
	}

	// The position endpoint now derives a page view through the map: an
	// exact anchor hit returns the page as pinned.
	status, body = app.send(t, http.MethodGet,
		"/api/books/"+epubFixtureEntry+"/position?char="+fmt.Sprint(mid+50), nil)
	if status != http.StatusOK {
		t.Fatalf("translate status = %d: %v", status, body)
	}
	page, ok := body["page"].(map[string]any)
	if !ok {
		t.Fatalf("no page view with anchors recorded: %v", body)
	}
	if page["page"].(float64) != 240 || page["derived"] != true || page["confidence"].(float64) != 0.9 {
		t.Errorf("page view = %v, want page 240 derived at 0.9", page)
	}

	// Notes are editable; the edition is not.
	status, body = app.send(t, http.MethodPatch, "/api/books/"+epubFixtureEntry+"/copies/"+copyID,
		map[string]any{"notes": "signed copy"})
	if status != http.StatusOK || body["copy"].(map[string]any)["notes"] != "signed copy" {
		t.Errorf("patch notes: status = %d body %v", status, body)
	}
	status, body = app.send(t, http.MethodGet, "/api/books/"+epubFixtureEntry+"/copies", nil)
	if status != http.StatusOK {
		t.Fatalf("list copies: status = %d", status)
	}
	copies := body["copies"].([]any)
	if len(copies) != 1 || copies[0].(map[string]any)["anchor_count"].(float64) != 2 {
		t.Errorf("copies = %v, want one copy with 2 anchors", copies)
	}

	// Dropping the copy clears the map with it.
	status, _ = app.send(t, http.MethodDelete, "/api/books/"+epubFixtureEntry+"/copies/"+copyID, nil)
	if status != http.StatusNoContent {
		t.Errorf("delete copy: status = %d, want 204", status)
	}
	status, body = app.send(t, http.MethodGet,
		"/api/books/"+epubFixtureEntry+"/position?char="+fmt.Sprint(mid+50), nil)
	if status != http.StatusOK {
		t.Fatalf("translate after delete: status = %d", status)
	}
	if page, still := body["page"]; still && page != nil {
		t.Errorf("page view survived its copy: %v", page)
	}
}

// TestBorrowedCopyFlow walks the library loan over HTTP: the register
// flow asks owned or borrowed, a checkout carries its due date, return
// states the card without losing the map, a re-checkout reopens the same
// row with a new deadline, and buying the book clears the loan state.
func TestBorrowedCopyFlow(t *testing.T) {
	app := newEpubTestApp(t)
	app.attachEpub(t, passageEpubFixture(t))
	text := app.canonicalText(t)
	mid := len(text) / 2

	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := app.store.DB().Exec(q, args...); err != nil {
			t.Fatalf("seed %q: %v", q, err)
		}
	}
	exec(`INSERT INTO book_editions (id, book_id, page_count) VALUES ('OL1M', 'OL1W', 320)`)

	base := "/api/books/" + epubFixtureEntry + "/copies"

	// A due date belongs to a borrowing; the register flow says so.
	status, body := app.send(t, http.MethodPost, base, map[string]any{
		"edition_id": "OL1M", "acquisition": "owned", "due_at": "2026-09-12"})
	if status != http.StatusBadRequest {
		t.Fatalf("due date on an owned copy: status = %d, want 400: %v", status, body)
	}

	// Check the printing out of the library.
	status, body = app.send(t, http.MethodPost, base, map[string]any{
		"edition_id": "OL1M", "acquisition": "borrowed", "due_at": "2026-09-12"})
	if status != http.StatusCreated {
		t.Fatalf("create borrowed copy: status = %d: %v", status, body)
	}
	copyID := body["copy"].(map[string]any)["id"].(string)
	c := body["copy"].(map[string]any)
	if c["acquisition"] != "borrowed" || c["due_at"] == nil || c["returned_at"] != nil {
		t.Errorf("fresh checkout = %v, want borrowed, due, in hand", c)
	}

	// One scanned page on the map before the loan's adventures start.
	status, body = app.send(t, http.MethodPost, base+"/"+copyID+"/pages",
		map[string]any{"printed_page": 240, "char_offset": mid, "source": "manual"})
	if status != http.StatusOK {
		t.Fatalf("pin page: status = %d: %v", status, body)
	}

	// Give it back: returned, due date kept for the record, map kept.
	status, body = app.send(t, http.MethodPost, base+"/"+copyID+"/return", nil)
	if status != http.StatusOK {
		t.Fatalf("return copy: status = %d: %v", status, body)
	}
	c = body["copy"].(map[string]any)
	if c["returned_at"] == nil || c["due_at"] == nil || c["acquisition"] != "borrowed" {
		t.Errorf("returned copy = %v, want borrowed, due, returned", c)
	}

	status, body = app.send(t, http.MethodGet, base, nil)
	if status != http.StatusOK {
		t.Fatalf("list copies: status = %d", status)
	}
	listed := body["copies"].([]any)[0].(map[string]any)
	if listed["returned_at"] == nil || listed["anchor_count"].(float64) != 1 {
		t.Errorf("listed returned copy = %v, want its state and its one anchor", listed)
	}

	// The impossible is refused in words: returning twice.
	status, body = app.send(t, http.MethodPost, base+"/"+copyID+"/return", nil)
	if status != http.StatusBadRequest || !strings.Contains(body["error"].(string), "already returned") {
		t.Errorf("double return: status = %d %v, want 400 saying so", status, body)
	}

	// Check the same printing out again: same row, new deadline, map kept.
	status, body = app.send(t, http.MethodPost, base+"/"+copyID+"/reopen",
		map[string]any{"due_at": "2026-10-20"})
	if status != http.StatusOK {
		t.Fatalf("reopen copy: status = %d: %v", status, body)
	}
	c = body["copy"].(map[string]any)
	if c["returned_at"] != nil || c["due_at"] == nil ||
		!strings.Contains(c["due_at"].(string), "2026-10-20") {
		t.Errorf("reopened copy = %v, want in hand with the new due date", c)
	}
	status, body = app.send(t, http.MethodGet, base+"/"+copyID+"/pages", nil)
	if status != http.StatusOK || len(body["anchors"].([]any)) != 1 {
		t.Errorf("pages after reopen = %v, want the map kept", body)
	}

	// The PATCH nudges a live loan's deadline and leaves it alone otherwise.
	status, body = app.send(t, http.MethodPatch, base+"/"+copyID,
		map[string]any{"notes": "renewed", "due_at": nil})
	if status != http.StatusOK || body["copy"].(map[string]any)["due_at"] != nil {
		t.Errorf("patch due date to none: status = %d %v", status, body)
	}

	// Buy the book: owned, no return state, map kept.
	status, body = app.send(t, http.MethodPost, base+"/"+copyID+"/own", nil)
	if status != http.StatusOK {
		t.Fatalf("own copy: status = %d: %v", status, body)
	}
	c = body["copy"].(map[string]any)
	if c["acquisition"] != "owned" || c["due_at"] != nil || c["returned_at"] != nil {
		t.Errorf("bought copy = %v, want owned with the loan state cleared", c)
	}
	status, body = app.send(t, http.MethodGet, base+"/"+copyID+"/pages", nil)
	if status != http.StatusOK || len(body["anchors"].([]any)) != 1 {
		t.Errorf("pages after buying = %v, want the map kept", body)
	}
	if status, body = app.send(t, http.MethodPost, base+"/"+copyID+"/reopen",
		map[string]any{}); status != http.StatusBadRequest {
		t.Errorf("reopening an owned copy: status = %d, want 400", status)
	}
}

// TestPageMapSeedAndErrorBar covers the honesty half of the page bridge:
// a registered printing is usable before anybody scans anything, every
// derived page carries an error bar, scanning tightens it, and a map
// that cannot bound its own error says so with a null bar instead of
// inventing one.
func TestPageMapSeedAndErrorBar(t *testing.T) {
	app := newEpubTestApp(t)
	app.attachEpub(t, passageEpubFixture(t))
	text := app.canonicalText(t)
	mid := len(text) / 2

	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := app.store.DB().Exec(q, args...); err != nil {
			t.Fatalf("seed %q: %v", q, err)
		}
	}
	exec(`INSERT INTO book_editions (id, book_id, page_count) VALUES ('OL1M', 'OL1W', 320)`)
	exec(`UPDATE library_entries SET edition_id = 'OL1M' WHERE id = ?`, epubFixtureEntry)

	pageAt := func(t *testing.T, offset int) map[string]any {
		t.Helper()
		status, body := app.send(t, http.MethodGet,
			"/api/books/"+epubFixtureEntry+"/position?char="+fmt.Sprint(offset), nil)
		if status != http.StatusOK {
			t.Fatalf("translate status = %d: %v", status, body)
		}
		page, _ := body["page"].(map[string]any)
		return page
	}

	// Nothing is registered yet, so there is no printing whose pages
	// these would be.
	if page := pageAt(t, mid); page != nil {
		t.Fatalf("page view for a book with no physical copy: %v", page)
	}

	status, body := app.send(t, http.MethodPost, "/api/books/"+epubFixtureEntry+"/copies",
		map[string]string{"edition_id": "OL1M"})
	if status != http.StatusCreated {
		t.Fatalf("create copy status = %d: %v", status, body)
	}
	copyID := body["copy"].(map[string]any)["id"].(string)

	// Registering the copy is enough: the printing's own page count
	// stretched across the text answers immediately, in the middle of a
	// 320-page book, with a bar wide enough to admit it is a guess.
	seeded := pageAt(t, mid)
	if seeded == nil {
		t.Fatal("a registered printing with a page count produced no page view")
	}
	if got := seeded["page"].(float64); got < 120 || got > 200 {
		t.Errorf("seeded page = %v, want roughly halfway through a 320-page printing", got)
	}
	seededMargin, ok := seeded["margin"].(float64)
	if !ok {
		t.Fatalf("seeded page view carries no error bar: %v", seeded)
	}
	if seededMargin < 10 {
		t.Errorf("seeded bar = ±%v, want a wide one — nobody has looked at the paper", seededMargin)
	}

	// Scanning pages tightens it. Three anchors around the midpoint turn
	// a whole-book guess into a local interpolation.
	pin := func(page, offset int) {
		t.Helper()
		status, body := app.send(t, http.MethodPost,
			"/api/books/"+epubFixtureEntry+"/copies/"+copyID+"/pages",
			map[string]any{"printed_page": page, "char_offset": offset, "source": "ocr", "confidence": 0.95})
		if status != http.StatusOK {
			t.Fatalf("pin page %d: status = %d: %v", page, status, body)
		}
	}
	pin(120, mid-len(text)/8)
	pin(200, mid+len(text)/8)

	scanned := pageAt(t, mid)
	if scanned == nil {
		t.Fatal("page view disappeared once anchors existed")
	}
	scannedMargin, ok := scanned["margin"].(float64)
	if !ok {
		t.Fatalf("scanned page view carries no error bar: %v", scanned)
	}
	if scannedMargin >= seededMargin {
		t.Errorf("bar did not tighten with scans: ±%v after, ±%v before", scannedMargin, seededMargin)
	}
	if got := scanned["confidence"].(float64); got != 0.95 {
		t.Errorf("confidence = %v, want the scanned anchors' 0.95", got)
	}

	// A printing whose page count nobody recorded, with a single scan on
	// it, knows where one page is and nothing about how fast pages go by.
	// That answer goes out with a null bar rather than a made-up one.
	exec(`UPDATE book_editions SET page_count = NULL WHERE id = 'OL1M'`)
	for _, page := range []int{120, 200} {
		exec(`DELETE FROM page_anchors WHERE physical_copy_id = ? AND printed_page = ?`, copyID, page)
	}
	pin(150, mid)

	lone := pageAt(t, mid+2000)
	if lone == nil {
		t.Fatal("a single anchor produced no page view")
	}
	if lone["page"].(float64) != 150 {
		t.Errorf("single-anchor page = %v, want the only anchor's 150", lone["page"])
	}
	if lone["margin"] != nil {
		t.Errorf("single-anchor bar = %v, want null — the map has no scale", lone["margin"])
	}
}
