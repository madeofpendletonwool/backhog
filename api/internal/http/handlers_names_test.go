package http

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"strconv"
	"strings"
	"testing"
	"time"
)

// The acceptance bar for the name index (MAD-670): a character introduced
// in chapter five is absent from the index at a chapter-three position,
// and present with a correct first mention once the reader reaches chapter
// six. The fixture is the leak fixture's shape stretched to six chapters:
// Tobias runs through the whole book, Mirabel arrives in chapter five.
//
// The clamp is the default for cookie callers here (the library search's
// rule — nothing about an index moves a position), so the leak assertions
// run against the ordinary request, not a token stand-in.

const (
	namesNewcomer = "mirabel"
	namesLater    = "umbrella" // chapter six's prop, unread until it is
)

// namesEpubFixture builds the six-chapter book.
func namesEpubFixture(t *testing.T) []byte {
	t.Helper()
	chapters := [][3]string{
		{"c1", "One", `<h1>One</h1><p>Old Tobias opened the shop, and Tobias swept the floor.</p>`},
		{"c2", "Two", `<h1>Two</h1><p>The rain kept everyone away, but Tobias stayed by the stove.</p>`},
		{"c3", "Three", `<h1>Three</h1><p>A letter arrived for Tobias that morning.</p>`},
		{"c4", "Four", `<h1>Four</h1><p>Tobias counted the till twice and found it short.</p>`},
		{"c5", "Five", `<h1>Five</h1><p>The doorbell rang and Mirabel stepped in, shaking rain from her coat.</p>`},
		{"c6", "Six", `<h1>Six</h1><p>Mirabel bought the blue umbrella and promised to return. Tobias waved as Mirabel left.</p>`},
	}
	manifest := new(strings.Builder)
	spine := new(strings.Builder)
	nav := new(strings.Builder)
	for _, c := range chapters {
		manifest.WriteString(`<item id="` + c[0] + `" href="` + c[0] +
			`.xhtml" media-type="application/xhtml+xml"/>`)
		spine.WriteString(`<itemref idref="` + c[0] + `"/>`)
		nav.WriteString(`<navPoint><navLabel><text>` + c[1] +
			`</text></navLabel><content src="` + c[0] + `.xhtml"/></navPoint>`)
	}

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	entries := [][2]string{
		{"META-INF/container.xml", `<?xml version="1.0"?>
<container version="1.0" xmlns="urn:oasis:names:tc:opendocument:xmlns:container">
  <rootfiles><rootfile full-path="OEBPS/content.opf" media-type="application/oebps-package+xml"/></rootfiles>
</container>`},
		{"OEBPS/content.opf", `<?xml version="1.0"?>
<package xmlns="http://www.idpf.org/2007/opf" version="2.0">
  <manifest><item id="ncx" href="toc.ncx" media-type="application/x-dtbncx+xml"/>` +
			manifest.String() + `</manifest>
  <spine toc="ncx">` + spine.String() + `</spine>
</package>`},
		{"OEBPS/toc.ncx", `<?xml version="1.0"?>
<ncx xmlns="http://www.daisy.org/z3986/2005/ncx/"><navMap>` + nav.String() + `</navMap></ncx>`},
	}
	for _, c := range chapters {
		entries = append(entries, [2]string{"OEBPS/" + c[0] + ".xhtml",
			`<html><body>` + c[2] + `</body></html>`})
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

// namesApp is the six-chapter fixture attached to an entry, with a token
// minted for its owner.
type namesApp struct {
	*epubTestApp
	secret string
}

func newNamesApp(t *testing.T) *namesApp {
	t.Helper()
	app := newEpubTestApp(t)
	app.attachEpub(t, namesEpubFixture(t))
	return &namesApp{epubTestApp: app, secret: mintToken(t, app.ts.URL, app.client)}
}

func (a *namesApp) setPosition(t *testing.T, offset int) {
	t.Helper()
	payload, _ := json.Marshal(map[string]any{"char_offset": offset})
	req, err := http.NewRequest(http.MethodPut,
		a.ts.URL+"/api/books/"+epubFixtureEntry+"/position", bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("put position request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := a.client.Do(req)
	if err != nil {
		t.Fatalf("put position: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("put position %d: status %d: %s", offset, resp.StatusCode, raw)
	}
}

// post sends one JSON POST over the cookie session.
func (a *namesApp) post(t *testing.T, path string, body any) (int, map[string]any) {
	t.Helper()
	payload, _ := json.Marshal(body)
	req, err := http.NewRequest(http.MethodPost, a.ts.URL+path, bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("POST %s: %v", path, err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := a.client.Do(req)
	if err != nil {
		t.Fatalf("POST %s: %v", path, err)
	}
	defer resp.Body.Close()
	out := map[string]any{}
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

// namesGeometry anchors the test's geometry in the canonical text: where
// chapters three, five and six start, and where the newcomer first appears.
type namesGeometry struct {
	ch3, ch5, ch6    int
	newcomerFirst    int
	newcomerMentions int
	charCount        int
}

func namesGeometryOf(t *testing.T, a *namesApp) namesGeometry {
	t.Helper()
	status, body := a.get(t, "/api/books/"+epubFixtureEntry+"/text/chapters")
	if status != http.StatusOK {
		t.Fatalf("chapters status = %d: %v", status, body)
	}
	chapters := chaptersOf(body)
	if len(chapters) != 6 {
		t.Fatalf("fixture has %d chapters, want 6", len(chapters))
	}
	status, textBody := a.get(t, "/api/books/"+epubFixtureEntry+"/text")
	if status != http.StatusOK {
		t.Fatalf("text status = %d", status)
	}
	text, _ := textBody["text"].(string)
	first := strings.Index(text, namesNewcomer)
	if first < 0 {
		t.Fatalf("fixture text lacks %q", namesNewcomer)
	}
	return namesGeometry{
		ch3:              int(chapters[2]["char_start"].(float64)),
		ch5:              int(chapters[4]["char_start"].(float64)),
		ch6:              int(chapters[5]["char_start"].(float64)),
		newcomerFirst:    first,
		newcomerMentions: strings.Count(text, namesNewcomer),
		charCount:        int(textBody["char_count"].(float64)),
	}
}

func namesListOf(t *testing.T, body map[string]any) []map[string]any {
	t.Helper()
	raw, _ := body["names"].([]any)
	out := make([]map[string]any, 0, len(raw))
	for _, n := range raw {
		out = append(out, n.(map[string]any))
	}
	return out
}

func findName(list []map[string]any, display string) map[string]any {
	for _, n := range list {
		if n["name"] == display {
			return n
		}
	}
	return nil
}

// TestNameIndexClampsToThePosition is the acceptance test: a character
// introduced in chapter five is absent at a chapter-three position and
// present with a correct first mention at a chapter-six one.
func TestNameIndexClampsToThePosition(t *testing.T) {
	app := newNamesApp(t)
	geo := namesGeometryOf(t, app)

	// Chapter three: the newcomer does not exist yet. The default clamp is
	// the position — for this cookie caller too, because the index is a
	// surface about the book *so far*.
	app.setPosition(t, geo.ch3)
	_, body := app.get(t, "/api/books/"+epubFixtureEntry+"/names")
	list := namesListOf(t, body)
	if findName(list, "Mirabel") != nil {
		t.Errorf("index at chapter 3 already lists Mirabel: %v", list)
	}
	if findName(list, "Tobias") == nil {
		t.Errorf("index at chapter 3 lost the ever-present Tobias: %v", list)
	}

	// A name asked about directly answers nothing and counts nothing.
	_, mentions := app.get(t, "/api/books/"+epubFixtureEntry+"/mentions?name=Mirabel")
	if mentions["total"] != float64(0) || len(mentions["results"].([]any)) != 0 {
		t.Errorf("clamped mentions for Mirabel = %v, want nothing", mentions)
	}

	// Chapter six: she exists, and her first mention is the chapter-five
	// introduction — not the first one the clamp allowed the reader to
	// approach from.
	app.setPosition(t, geo.ch6)
	_, body2 := app.get(t, "/api/books/"+epubFixtureEntry+"/names")
	newcomer := findName(namesListOf(t, body2), "Mirabel")
	if newcomer == nil {
		t.Fatalf("index at chapter 6 lacks Mirabel: %v", body2)
	}
	if got := newcomer["mentions"]; got != float64(1) {
		t.Errorf("Mirabel mentions at chapter 6 = %v, want the 1 so-far one", got)
	}
	first, _ := newcomer["first_seen"].(map[string]any)
	if first == nil || int(first["char_start"].(float64)) != geo.newcomerFirst {
		t.Fatalf("Mirabel first_seen = %v, want char_start %d (chapter 5)", first, geo.newcomerFirst)
	}
	chapter, _ := first["chapter"].(map[string]any)
	if chapter == nil || chapter["title"] != "Five" {
		t.Errorf("Mirabel first_seen chapter = %v, want Five", chapter)
	}
	if first["deep_link"] != deepLink(epubFixtureEntry, geo.newcomerFirst) {
		t.Errorf("Mirabel first_seen deep_link = %v", first["deep_link"])
	}

	// The loud opt-in lifts the whole index: every mention, counted.
	_, full := app.get(t, "/api/books/"+epubFixtureEntry+"/names?until=none")
	if e := findName(namesListOf(t, full), "Mirabel"); e == nil || e["mentions"] != float64(geo.newcomerMentions) {
		t.Errorf("until=none Mirabel = %v, want %d mentions", e, geo.newcomerMentions)
	}

	// Malformed bounds are refused, never guessed.
	for _, bad := range []string{"until=banana", "until=-5", "until=" + strconv.Itoa(geo.charCount+1)} {
		status, _ := app.get(t, "/api/books/"+epubFixtureEntry+"/names?"+bad)
		if status != http.StatusBadRequest {
			t.Errorf("names?%s: status = %d, want 400", bad, status)
		}
	}
}

// TestNameMentionsCarryProvenance checks the mentions endpoint's contract:
// canonical offsets, a chapter, a snippet from the book's own prose, and
// the peek deep link — the shape the MCP server's find_mentions decodes.
func TestNameMentionsCarryProvenance(t *testing.T) {
	app := newNamesApp(t)
	geo := namesGeometryOf(t, app)
	app.setPosition(t, geo.ch6)

	_, body := app.get(t, "/api/books/"+epubFixtureEntry+"/mentions?name=Mirabel")
	if body["total"] != float64(1) {
		t.Fatalf("mentions total = %v, want 1", body["total"])
	}
	rows := results(t, body)
	if len(rows) != 1 {
		t.Fatalf("mentions rows = %d, want 1: %v", len(rows), body)
	}
	hit := rows[0]
	if int(hit["char_offset"].(float64)) != geo.newcomerFirst {
		t.Errorf("mention offset = %v, want %d", hit["char_offset"], geo.newcomerFirst)
	}
	if hit["deep_link"] != deepLink(epubFixtureEntry, geo.newcomerFirst) {
		t.Errorf("mention deep_link = %v", hit["deep_link"])
	}
	snippet, _ := hit["snippet"].(map[string]any)
	passage, _ := snippet["passage"].(string)
	if !strings.Contains(passage, "Mirabel") {
		t.Errorf("mention passage = %q, want the book's own Mirabel", passage)
	}
	// The case-fold is forgiving: however the caller spells the name.
	_, folded := app.get(t, "/api/books/"+epubFixtureEntry+"/mentions?name=mirabel")
	if folded["total"] != float64(1) {
		t.Errorf("lowercase query total = %v, want 1", folded["total"])
	}
}

// TestNameIndexUnreadClampsToNothing: a book never opened has read
// nothing, so it has met nobody. The index is empty rather than absent.
func TestNameIndexUnreadClampsToNothing(t *testing.T) {
	app := newNamesApp(t) // no position ever written

	_, body := app.get(t, "/api/books/"+epubFixtureEntry+"/names")
	if list := namesListOf(t, body); len(list) != 0 {
		t.Errorf("unread book index = %v, want empty", list)
	}
	if bound, _ := body["bound"].(map[string]any); int(bound["char_offset"].(float64)) != 0 {
		t.Errorf("unread bound = %v, want offset 0", bound)
	}
}

// TestNameIndexIsScopedToTheCaller: the file-backed door. An anonymous
// caller is refused; a stranger's entry is not theirs to index.
func TestNameIndexIsScopedToTheCaller(t *testing.T) {
	app := newNamesApp(t)

	resp, err := http.Get(app.ts.URL + "/api/books/" + epubFixtureEntry + "/names")
	if err != nil {
		t.Fatalf("anon names: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("anon names status = %d, want 401", resp.StatusCode)
	}

	jar, _ := cookiejar.New(nil)
	stranger := &http.Client{Jar: jar, Timeout: 10 * time.Second}
	register(t, app.ts.URL, stranger, "stranger@example.com", "stranger", "hogwash123")
	req, _ := http.NewRequest(http.MethodGet, app.ts.URL+"/api/books/"+epubFixtureEntry+"/names", nil)
	sresp, err := stranger.Do(req)
	if err != nil {
		t.Fatalf("stranger names: %v", err)
	}
	sresp.Body.Close()
	if sresp.StatusCode != http.StatusNotFound {
		t.Errorf("stranger names status = %d, want 404", sresp.StatusCode)
	}
}

// TestHideNameIsTheReadersVerdict: hiding a false positive stores it, the
// index marks it instead of dropping it (a wrong call is reversible), and
// the extractor's next pass cannot resurrect it because nothing
// regenerates hidden rows. A token cannot hide: the write gate holds.
func TestHideNameIsTheReadersVerdict(t *testing.T) {
	app := newNamesApp(t)
	geo := namesGeometryOf(t, app)
	app.setPosition(t, geo.ch6)

	status, _ := app.post(t, "/api/books/"+epubFixtureEntry+"/names/hide", map[string]any{"name": "Tobias"})
	if status != http.StatusOK {
		t.Fatalf("hide status = %d", status)
	}
	_, body := app.get(t, "/api/books/"+epubFixtureEntry+"/names")
	entry := findName(namesListOf(t, body), "Tobias")
	if entry == nil || entry["hidden"] != true {
		t.Fatalf("hidden Tobias = %v, want present and flagged", entry)
	}
	// Hiding is idempotent and reversible.
	app.post(t, "/api/books/"+epubFixtureEntry+"/names/hide", map[string]any{"name": "Tobias"})
	status, _ = app.post(t, "/api/books/"+epubFixtureEntry+"/names/unhide", map[string]any{"name": "Tobias"})
	if status != http.StatusOK {
		t.Fatalf("unhide status = %d", status)
	}
	_, body2 := app.get(t, "/api/books/"+epubFixtureEntry+"/names")
	if entry = findName(namesListOf(t, body2), "Tobias"); entry == nil || entry["hidden"] != false {
		t.Fatalf("unhidden Tobias = %v", entry)
	}

	// The token is read-only: hiding is the reader's own action.
	resp, _ := asLeakToken(t, app.ts.URL, app.secret, http.MethodPost,
		"/api/books/"+epubFixtureEntry+"/names/hide", map[string]any{"name": "Tobias"})
	resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		t.Error("a token hid a name — the write gate must hold")
	}

	// The hide is scoped to its reader: the same book, another account,
	// still sees the name. (The stranger has no file access here; the
	// owner's own second entry is overkill — the row is user-keyed, which
	// the schema test pins.)
}
