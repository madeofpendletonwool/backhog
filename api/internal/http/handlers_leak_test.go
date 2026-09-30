package http

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/collinpendleton/backhog/api/internal/books/position"
)

// The leak bar for the spoiler-safety seam: a fixture book with a mid-book
// reveal, a reading position before it, and the proof that no read path —
// text, chapters, search, passage, or export — ever hands the reveal to a
// client that has not asked for it. The pattern is the hidden-information
// one the masked achievements use: what is withheld is asserted as absent,
// not merely missing from what was checked.
//
// The fixture: three chapters. Alpha is read territory, Beta holds the
// reveal ("The butler did it.") in its second paragraph behind an innocent
// first paragraph, Gamma is the aftermath. A token-authenticated client
// stands in for every external reader MAD-465 minted a key for; its default
// clamp is the position, which is the whole feature.

const (
	leakReveal   = "the butler did it"
	leakPastCut  = "candlestick"
	leakAftercut = "after the trial"
)

// leakEpubFixture builds the three-chapter mystery.
func leakEpubFixture(t *testing.T) []byte {
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
    <item id="c3" href="c3.xhtml" media-type="application/xhtml+xml"/>
  </manifest>
  <spine toc="ncx"><itemref idref="c1"/><itemref idref="c2"/><itemref idref="c3"/></spine>
</package>`},
		{"OEBPS/toc.ncx", `<?xml version="1.0"?>
<ncx xmlns="http://www.daisy.org/z3986/2005/ncx/">
  <navMap>
    <navPoint><navLabel><text>Alpha</text></navLabel><content src="c1.xhtml"/></navPoint>
    <navPoint><navLabel><text>Beta</text></navLabel><content src="c2.xhtml"/></navPoint>
    <navPoint><navLabel><text>Gamma</text></navLabel><content src="c3.xhtml"/></navPoint>
  </navMap>
</ncx>`},
		{"OEBPS/c1.xhtml", `<html><body>
		  <h1>Alpha</h1>
		  <p>The quiet village slept under the hill.</p>
		  <p>Nobody suspected anything at first.</p></body></html>`},
		{"OEBPS/c2.xhtml", `<html><body>
		  <h1>Beta</h1>
		  <p>The detective searched the manor and found the silver candlestick hidden in the pantry.</p>
		  <p>The butler did it. He confessed by morning.</p></body></html>`},
		{"OEBPS/c3.xhtml", `<html><body>
		  <h1>Gamma</h1>
		  <p>After the trial the village changed forever.</p></body></html>`},
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

// leakApp is the mystery attached to an entry, carrying the once-shown
// secret of a token minted for its owner.
type leakApp struct {
	*epubTestApp
	secret string
}

func newLeakApp(t *testing.T) *leakApp {
	t.Helper()
	app := newEpubTestApp(t)
	app.attachEpub(t, leakEpubFixture(t))
	return &leakApp{epubTestApp: app, secret: mintToken(t, app.ts.URL, app.client)}
}

// mintToken mints a personal API token over a cookie session — the same
// walk the settings page performs — and returns the secret shown once.
func mintToken(t *testing.T, base string, client *http.Client) string {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"name": "leak test", "expires_days": 0})
	resp, err := client.Post(base+"/api/tokens", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("mint token: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("mint token: status %d: %s", resp.StatusCode, raw)
	}
	var out struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil || out.Token == "" {
		t.Fatalf("mint token: decode %v (%q)", err, out.Token)
	}
	return out.Token
}

// asLeakToken runs one request as the external client: bearer secret, no
// cookie, nothing else. The body map is nil for non-JSON answers.
func asLeakToken(t *testing.T, base, secret, method, path string, body any) (*http.Response, map[string]any) {
	t.Helper()
	var payload []byte
	if body != nil {
		var err error
		if payload, err = json.Marshal(body); err != nil {
			t.Fatalf("marshal: %v", err)
		}
	}
	req, err := http.NewRequest(method, base+path, bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	req.Header.Set("Authorization", "Bearer "+secret)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := tokenClient().Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	out := map[string]any{}
	if strings.Contains(resp.Header.Get("Content-Type"), "json") {
		raw, _ := io.ReadAll(resp.Body)
		_ = json.Unmarshal(raw, &out)
		resp.Body = io.NopCloser(bytes.NewReader(raw))
	}
	return resp, out
}

// setLeakPosition writes the owner's reading position over the cookie
// session — the reader is the only thing that moves a position.
func (a *leakApp) setLeakPosition(t *testing.T, offset int) {
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

// leakChapters reads the chapters payload over the cookie session;
// tokenChapters reads it as the external client.
func leakChapters(t *testing.T, a *leakApp) []map[string]any {
	t.Helper()
	status, body := a.get(t, "/api/books/"+epubFixtureEntry+"/text/chapters")
	if status != http.StatusOK {
		t.Fatalf("cookie chapters status = %d: %v", status, body)
	}
	return chaptersOf(body)
}

func tokenChapters(t *testing.T, a *leakApp, query string) []map[string]any {
	t.Helper()
	resp, body := asLeakToken(t, a.ts.URL, a.secret, http.MethodGet,
		"/api/books/"+epubFixtureEntry+"/text/chapters"+query, nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("token chapters%s status = %d: %v", query, resp.StatusCode, body)
	}
	return chaptersOf(body)
}

func chaptersOf(body map[string]any) []map[string]any {
	raw, _ := body["chapters"].([]any)
	out := make([]map[string]any, 0, len(raw))
	for _, c := range raw {
		out = append(out, c.(map[string]any))
	}
	return out
}

// leakGeometry anchors the test's geometry in the canonical text itself:
// where each chapter starts, where the cut word begins, how long the book is.
type leakGeometry struct {
	alpha, beta, gamma int
	candlestick        int
	charCount          int
}

func leakGeometryOf(t *testing.T, a *leakApp) leakGeometry {
	t.Helper()
	chapters := leakChapters(t, a)
	if len(chapters) != 3 {
		t.Fatalf("fixture has %d chapters, want 3", len(chapters))
	}
	status, body := a.get(t, "/api/books/"+epubFixtureEntry+"/text")
	if status != http.StatusOK {
		t.Fatalf("text status = %d", status)
	}
	text, _ := body["text"].(string)
	at := func(needle string) int {
		i := strings.Index(text, needle)
		if i < 0 {
			t.Fatalf("fixture text lacks %q", needle)
		}
		return i
	}
	return leakGeometry{
		alpha:       int(chapters[0]["char_start"].(float64)),
		beta:        int(chapters[1]["char_start"].(float64)),
		gamma:       int(chapters[2]["char_start"].(float64)),
		candlestick: at(leakPastCut),
		charCount:   int(body["char_count"].(float64)),
	}
}

// assertNoLeak fails the test if any string carries the reveal, the
// past-the-cut word, or the unread final chapter — the three grades of
// spoiler this fixture can leak. Everything is folded to lower case first:
// the canonical text has no capitals, the display text has all of them,
// and a leak is a leak in either case.
func assertNoLeak(t *testing.T, what string, texts ...string) {
	t.Helper()
	for _, s := range texts {
		low := strings.ToLower(s)
		for _, banned := range []string{leakReveal, leakPastCut, leakAftercut} {
			if strings.Contains(low, banned) {
				t.Errorf("%s leaked %q: %.120s", what, banned, s)
			}
		}
	}
}

func TestLeakTextClampsToThePosition(t *testing.T) {
	app := newLeakApp(t)
	geo := leakGeometryOf(t, app)
	app.setLeakPosition(t, geo.beta) // read exactly through Alpha

	resp, body := asLeakToken(t, app.ts.URL, app.secret, http.MethodGet,
		"/api/books/"+epubFixtureEntry+"/text", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("token text status = %d: %v", resp.StatusCode, body)
	}
	if text, _ := body["text"].(string); !strings.Contains(text, "quiet village") {
		t.Errorf("clamped text lost the read chapter: %q", body["text"])
	}
	assertNoLeak(t, "token text", body["text"].(string))
	bound, _ := body["bound"].(map[string]any)
	if bound["until"] != untilPosition || int(bound["char_offset"].(float64)) != geo.beta {
		t.Errorf("bound = %v, want position at %d", bound, geo.beta)
	}
	prov, _ := body["provenance"].(map[string]any)
	if prov == nil || prov["book_id"] != epubFixtureEntry ||
		prov["deep_link"] != deepLink(epubFixtureEntry, 0) {
		t.Errorf("text provenance = %v", prov)
	}

	// The same client, saying the quiet part out loud, gets everything.
	_, full := asLeakToken(t, app.ts.URL, app.secret, http.MethodGet,
		"/api/books/"+epubFixtureEntry+"/text?until=none", nil)
	if !strings.Contains(full["text"].(string), leakReveal) {
		t.Error("until=none still withheld the reveal")
	}

	// The cookie session is the reader itself and is unchanged.
	_, cookie := app.get(t, "/api/books/"+epubFixtureEntry+"/text")
	if !strings.Contains(cookie["text"].(string), leakReveal) {
		t.Error("cookie text was clamped without being asked")
	}
	if cb, _ := cookie["bound"].(map[string]any); cb["until"] != untilNone {
		t.Errorf("cookie bound = %v, want none", cb)
	}

	// An explicit offset is honored from either credential: stopping at
	// Gamma keeps Beta (and its reveal) but drops Gamma's aftermath.
	_, partial := asLeakToken(t, app.ts.URL, app.secret, http.MethodGet,
		"/api/books/"+epubFixtureEntry+"/text?until="+strconv.Itoa(geo.gamma), nil)
	if text := partial["text"].(string); !strings.Contains(text, leakReveal) {
		t.Errorf("until=gamma dropped Beta, which sits before it: %q", text)
	}
	if text := partial["text"].(string); strings.Contains(strings.ToLower(text), leakAftercut) {
		t.Errorf("until=gamma served Gamma's text")
	}

	// Garbage bounds are rejected, not guessed at.
	for _, bad := range []string{"until=banana", "until=-5", "until=" + strconv.Itoa(geo.charCount+1)} {
		resp, _ := asLeakToken(t, app.ts.URL, app.secret, http.MethodGet,
			"/api/books/"+epubFixtureEntry+"/text?"+bad, nil)
		resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400", bad, resp.StatusCode)
		}
	}
}

func TestLeakUnreadBookClampsToNothing(t *testing.T) {
	app := newLeakApp(t) // no position ever written

	_, body := asLeakToken(t, app.ts.URL, app.secret, http.MethodGet,
		"/api/books/"+epubFixtureEntry+"/text", nil)
	if text, _ := body["text"].(string); text != "" {
		t.Errorf("unread book served text: %q", text)
	}
	if bound, _ := body["bound"].(map[string]any); int(bound["char_offset"].(float64)) != 0 {
		t.Errorf("unread bound = %v, want offset 0", bound)
	}
}

func TestLeakChaptersLockTheFuture(t *testing.T) {
	app := newLeakApp(t)
	geo := leakGeometryOf(t, app)
	app.setLeakPosition(t, geo.beta)

	chapters := tokenChapters(t, app, "")
	if len(chapters) != 3 {
		t.Fatalf("chapters = %d, want all three listed (locked, not dropped)", len(chapters))
	}
	if chapters[0]["locked"] != false || chapters[0]["char_end"].(float64) == 0 {
		t.Errorf("the read chapter was locked or blanked: %v", chapters[0])
	}
	for _, locked := range chapters[1:] {
		if locked["locked"] != true {
			t.Errorf("future chapter not locked: %v", locked)
			continue
		}
		raw, _ := json.Marshal(locked)
		assertNoLeak(t, "locked chapter row", string(raw))
		if locked["href"] != "" || locked["char_start"].(float64) != 0 || locked["char_end"].(float64) != 0 {
			t.Errorf("locked chapter kept its addresses: %v", locked)
		}
		if locked["blocks"] != nil {
			t.Errorf("locked chapter kept its blocks: %v", locked["blocks"])
		}
		if title, _ := locked["title"].(string); title != "Beta" && title != "Gamma" {
			t.Errorf("locked title = %q, want the title-only default", locked["title"])
		}
	}

	// Titles go quiet when the caller asks — chapter names can spoil too.
	hidden := tokenChapters(t, app, "?locked_titles=hide")
	for _, locked := range hidden[1:] {
		if title, _ := locked["title"].(string); title != "" {
			t.Errorf("locked_titles=hide still showed %q", title)
		}
	}

	// The cookie reader's chapter list is untouched.
	cookie := leakChapters(t, app)
	for _, ch := range cookie {
		if ch["locked"] == true {
			t.Errorf("cookie chapter locked without a bound: %v", ch)
		}
	}
}

func TestLeakDisplayStopsAtTheBound(t *testing.T) {
	app := newLeakApp(t)
	geo := leakGeometryOf(t, app)
	app.setLeakPosition(t, geo.candlestick) // mid-Beta, before the cut word

	spine := int(leakChapters(t, app)[1]["spine_index"].(float64))
	path := "/api/books/" + epubFixtureEntry + "/text/display?spine=" + strconv.Itoa(spine)

	resp, body := asLeakToken(t, app.ts.URL, app.secret, http.MethodGet, path, nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("token display status = %d: %v", resp.StatusCode, body)
	}
	joined := blocksJoined(body["blocks"])
	if !strings.Contains(joined, "silver") {
		t.Errorf("display dropped the read half of the paragraph: %q", joined)
	}
	assertNoLeak(t, "token display", joined)

	// The cookie reader still gets every block.
	_, cookie := app.get(t, path)
	if joined := blocksJoined(cookie["blocks"]); !strings.Contains(joined, "butler did it") {
		t.Error("cookie display lost the full chapter")
	}
}

func blocksJoined(v any) string {
	blocks, _ := v.([]any)
	out := ""
	for _, b := range blocks {
		out += b.(string) + "\n"
	}
	return out
}

func TestLeakSearchDropsFutureHits(t *testing.T) {
	app := newLeakApp(t)
	geo := leakGeometryOf(t, app)
	app.setLeakPosition(t, geo.candlestick)

	// The reveal phrase exists in the book — the cookie search finds it —
	// but the clamped search reports nothing and counts nothing.
	if _, cookie := app.get(t, "/api/books/"+epubFixtureEntry+"/search?q="+url.QueryEscape(leakReveal)); cookie["total"] == float64(0) {
		t.Fatalf("fixture sanity: cookie search found no %q", leakReveal)
	}
	resp, body := asLeakToken(t, app.ts.URL, app.secret, http.MethodGet,
		"/api/books/"+epubFixtureEntry+"/search?q="+url.QueryEscape(leakReveal), nil)
	defer resp.Body.Close()
	if body["total"] != float64(0) || len(body["results"].([]any)) != 0 {
		t.Errorf("clamped search for the reveal = %v", body)
	}

	// A hit inside the read text comes back with its provenance and a
	// context that stops at the cut: the word past the bound never rides
	// along in the skirt of the paragraph.
	resp2, hits := asLeakToken(t, app.ts.URL, app.secret, http.MethodGet,
		"/api/books/"+epubFixtureEntry+"/search?q=silver", nil)
	defer resp2.Body.Close()
	rows := results(t, hits)
	if len(rows) != 1 {
		t.Fatalf("silver hits = %d, want 1: %v", len(rows), hits)
	}
	hit := rows[0]
	if hit["book_id"] != epubFixtureEntry ||
		hit["deep_link"] != deepLink(epubFixtureEntry, int(hit["char_offset"].(float64))) {
		t.Errorf("hit provenance = %v", hit)
	}
	_, passage, after := snippetOf(t, hit)
	assertNoLeak(t, "hit context", passage, after)

	// A phrase past the cut but inside the straddling paragraph is dropped
	// whole: its words are unread even though the paragraph began in reach.
	resp3, body3 := asLeakToken(t, app.ts.URL, app.secret, http.MethodGet,
		"/api/books/"+epubFixtureEntry+"/search?q="+url.QueryEscape(leakPastCut), nil)
	defer resp3.Body.Close()
	if body3["total"] != float64(0) {
		t.Errorf("search for the cut word = %v, want dropped", body3["total"])
	}
}

func TestLeakPassageRefusesTheFuture(t *testing.T) {
	app := newLeakApp(t)
	geo := leakGeometryOf(t, app)
	app.setLeakPosition(t, geo.beta)

	// A passage from ahead of the position is refused without saying where
	// it landed — and the POST itself must pass the token gate, because
	// placing a passage writes nothing.
	resp, body := asLeakToken(t, app.ts.URL, app.secret, http.MethodPost,
		"/api/books/"+epubFixtureEntry+"/passage",
		map[string]any{"text": "The butler did it. He confessed by morning."})
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusForbidden {
		t.Fatal("the read-shaped POST was stopped by the token write gate")
	}
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("future passage status = %d: %v", resp.StatusCode, body)
	}
	if msg, _ := body["error"].(string); strings.Contains(strings.ToLower(msg), "butler") {
		t.Errorf("refusal echoed the passage text: %q", msg)
	}

	// A passage from the read chapter places fine and is cited.
	resp2, placed := asLeakToken(t, app.ts.URL, app.secret, http.MethodPost,
		"/api/books/"+epubFixtureEntry+"/passage",
		map[string]any{"text": "The quiet village slept under the hill. Nobody suspected anything at first."})
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("read passage status = %d: %v", resp2.StatusCode, placed)
	}
	if prov, _ := placed["provenance"].(map[string]any); prov == nil || prov["deep_link"] == "" {
		t.Errorf("placed passage carries no provenance: %v", placed["provenance"])
	}
	if bound, _ := placed["bound"].(map[string]any); bound["until"] != untilPosition {
		t.Errorf("placed passage bound = %v", bound)
	}
}

func TestLeakExportStopsAtThePosition(t *testing.T) {
	app := newLeakApp(t)
	geo := leakGeometryOf(t, app)
	app.setLeakPosition(t, geo.candlestick) // mid-Beta, before the cut word

	md := exportAsCookie(t, app, "until=position")
	if !strings.HasPrefix(md, "# Fixture Book") {
		t.Errorf("export lacks its title heading: %.60s", md)
	}
	if !strings.Contains(md, "## 1. Alpha") || !strings.Contains(md, "## 2. Beta") {
		t.Errorf("export lost its chapter headings: %q", firstLines(md, 6))
	}
	if !strings.Contains(md, "quiet village") || !strings.Contains(md, "silver") {
		t.Errorf("export lost read text: %q", firstLines(md, 8))
	}
	if !strings.Contains(md, "[Source: jump to this spot](/books/"+epubFixtureEntry+"/read?offset=") {
		t.Error("export lacks its source anchors")
	}
	assertNoLeak(t, "export", md)

	// A token that says nothing gets its position, like every read path.
	tokenMD := exportAsToken(t, app, "")
	assertNoLeak(t, "token default export", tokenMD)

	// until=none exports the whole book, display text and all.
	whole := exportAsCookie(t, app, "until=none")
	if !strings.Contains(whole, "The butler did it.") {
		t.Error("full export dropped the reveal chapter")
	}
	if !strings.Contains(whole, "## 3. Gamma") {
		t.Error("full export dropped the last chapter")
	}

	// Nothing read yet exports an honest stub, not an empty 200.
	app.setLeakPosition(t, 0)
	empty := exportAsCookie(t, app, "until=position")
	assertNoLeak(t, "empty export", empty)
	if !strings.Contains(empty, "Nothing read yet") {
		t.Errorf("empty export = %q", firstLines(empty, 6))
	}
}

func exportAsCookie(t *testing.T, app *leakApp, query string) string {
	t.Helper()
	resp, err := app.client.Get(app.ts.URL + "/api/books/" + epubFixtureEntry + "/text/export?" + query)
	if err != nil {
		t.Fatalf("export?%s: %v", query, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("export?%s status = %d: %s", query, resp.StatusCode, raw)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "text/markdown") {
		t.Errorf("export content-type = %q", ct)
	}
	raw, _ := io.ReadAll(resp.Body)
	return string(raw)
}

func exportAsToken(t *testing.T, app *leakApp, query string) string {
	t.Helper()
	suffix := ""
	if query != "" {
		suffix = "?" + query
	}
	resp, _ := asLeakToken(t, app.ts.URL, app.secret, http.MethodGet,
		"/api/books/"+epubFixtureEntry+"/text/export"+suffix, nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("token export status = %d", resp.StatusCode)
	}
	raw, _ := io.ReadAll(resp.Body)
	return string(raw)
}

func firstLines(s string, n int) string {
	lines := strings.Split(s, "\n")
	if len(lines) > n {
		lines = lines[:n]
	}
	return strings.Join(lines, " ⏎ ")
}

// TestLeakAudioPositionClamps verifies the acceptance the whole seam hangs
// on for listeners: a position written from the player — through the
// alignment, not around it — clamps the text at exactly the offset the
// timestamp translated to.
func TestLeakAudioPositionClamps(t *testing.T) {
	app := newPositionTestApp(t, fixedAnchors{
		audio: []position.Anchor{
			{CharOffset: 0, Value: 0, Confidence: 0.9},
			{CharOffset: 40, Value: 10, Confidence: 0.8},
		},
	})
	app.charCount(t)

	status, body := app.api(t, http.MethodPut, "/api/books/"+positionEntry+"/position",
		map[string]any{"audio_seconds": 5.0, "audio_file_id": positionTrackOne})
	if status != http.StatusOK {
		t.Fatalf("put listening position: %d %v", status, body)
	}
	if got := body["position"].(map[string]any)["char_offset"]; got != float64(20) {
		t.Fatalf("fixture sanity: 5s translated to %v chars, want 20", got)
	}

	secret := mintToken(t, app.ts.URL, app.client)
	resp, text := asLeakToken(t, app.ts.URL, secret, http.MethodGet,
		"/api/books/"+positionEntry+"/text", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("token text status = %d: %v", resp.StatusCode, text)
	}
	if bound, _ := text["bound"].(map[string]any); bound["until"] != untilPosition ||
		int(bound["char_offset"].(float64)) != 20 {
		t.Errorf("audio-written bound = %v, want position/20", text["bound"])
	}
	if length := len(text["text"].(string)); length != 20 {
		t.Errorf("clamped text length = %d, want 20", length)
	}
}

// TestLeakPagedSearchClampsToThePage is the paged twin: a comic read to
// page one must not have its second page's lettering served, counted, or
// hinted at by search.
func TestLeakPagedSearchClampsToThePage(t *testing.T) {
	app := ocrApp(t)
	runOCRPass(t, app, positionComic, map[int]string{
		1: "the hero enters the silent library",
		2: "the dragon was hiding there all along",
	})

	if status, body := app.api(t, http.MethodPut, "/api/books/"+positionComic+"/position",
		map[string]any{"page_index": 0}); status != http.StatusOK {
		t.Fatalf("put page position: %d %v", status, body)
	}

	secret := mintToken(t, app.ts.URL, app.client)
	resp, body := asLeakToken(t, app.ts.URL, secret, http.MethodGet,
		"/api/books/"+positionComic+"/search?q=dragon", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("token paged search status = %d: %v", resp.StatusCode, body)
	}
	if body["total"] != float64(0) || len(body["results"].([]any)) != 0 {
		t.Errorf("clamped paged search = %v", body)
	}
	if bound, _ := body["bound"].(map[string]any); bound["until"] != untilPosition {
		t.Errorf("paged bound = %v", bound)
	}

	// The in-app search still sees both pages.
	if _, cookie := app.api(t, http.MethodGet, "/api/books/"+positionComic+"/search?q=dragon", nil); cookie["total"] != float64(1) {
		t.Errorf("cookie paged search total = %v, want 1", cookie["total"])
	}
}

// TestLeakScopeIsStillTheUsers pins that the clamp changed nothing about
// who may read what: a stranger's token gets the same 404 the stranger's
// cookie always did.
func TestLeakScopeIsStillTheUsers(t *testing.T) {
	app := newLeakApp(t)

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookie jar: %v", err)
	}
	stranger := &http.Client{Jar: jar, Timeout: 10 * time.Second}
	register(t, app.ts.URL, stranger, "nosy@example.com", "nosy", "hogwash123")
	strangerSecret := mintToken(t, app.ts.URL, stranger)

	resp, body := asLeakToken(t, app.ts.URL, strangerSecret, http.MethodGet,
		"/api/books/"+epubFixtureEntry+"/text", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("stranger token text status = %d: %v", resp.StatusCode, body)
	}
}
