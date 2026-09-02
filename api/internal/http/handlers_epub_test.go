package http

import (
	"archive/zip"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/collinpendleton/backhog/api/internal/backfill"
	booktext "github.com/collinpendleton/backhog/api/internal/books"
	"github.com/collinpendleton/backhog/api/internal/config"
	"github.com/collinpendleton/backhog/api/internal/db"
	"github.com/collinpendleton/backhog/api/internal/metadata"
	"github.com/collinpendleton/backhog/api/internal/store"
)

// epubTestApp is the text-endpoint harness: full router over a migrated
// database, one registered user, an EPUB fixture on disk and inventoried.
type epubTestApp struct {
	ts     *httptest.Server
	client *http.Client
	store  *store.Store
	root   string
	userID string
}

const epubFixtureEntry = "e1"

func newEpubTestApp(t *testing.T) *epubTestApp {
	t.Helper()

	database, err := db.Open(filepath.Join(t.TempDir(), "epub_api.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	if err := db.Migrate(database); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	st := store.New(database)

	covers, err := metadata.NewCoverCache(filepath.Join(t.TempDir(), "covers"))
	if err != nil {
		t.Fatalf("cover cache: %v", err)
	}
	cfg := config.Config{EpubTextDir: filepath.Join(t.TempDir(), "epub_text")}
	srv := NewServer(cfg, st, nil, nil, covers, nil, &backfill.Runner{}, nil)
	ts := httptest.NewServer(srv.Routes())
	t.Cleanup(ts.Close)

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookie jar: %v", err)
	}
	client := &http.Client{Jar: jar, Timeout: 10 * time.Second}

	register(t, ts.URL, client, "reader@example.com", "reader", "hogwash123")

	// The seeded book entry must belong to the registered user.
	me := struct {
		ID string `json:"id"`
	}{}
	resp, err := client.Get(ts.URL + "/api/auth/me")
	if err != nil {
		t.Fatalf("me: %v", err)
	}
	defer resp.Body.Close()
	if err := json.NewDecoder(resp.Body).Decode(&me); err != nil {
		t.Fatalf("me: decode: %v", err)
	}

	return &epubTestApp{ts: ts, client: client, store: st,
		root: filepath.Join(t.TempDir(), "books"), userID: me.ID}
}

func register(t *testing.T, base string, client *http.Client, email, username, password string) {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"email": email, "username": username, "password": password})
	resp, err := client.Post(base+"/api/auth/register", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("register %s: %v", email, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("register %s: status %d", email, resp.StatusCode)
	}
}

// attachEpub writes a fixture to disk, inventories and attaches it to the
// seeded book entry. Passing nil for data attaches nothing.
func (a *epubTestApp) attachEpub(t *testing.T, data []byte) {
	t.Helper()
	seed := []string{
		`INSERT INTO books (id, title) VALUES ('OL1W', 'Fixture Book')
			ON CONFLICT(id) DO NOTHING`,
		`INSERT INTO library_entries (id, user_id, media_type, book_id, status)
			VALUES ('` + epubFixtureEntry + `', '` + a.userID + `', 'book', 'OL1W', 'playing')
			ON CONFLICT(id) DO NOTHING`,
	}
	for _, q := range seed {
		if _, err := a.store.DB().Exec(q); err != nil {
			t.Fatalf("seed %q: %v", q, err)
		}
	}
	if data == nil {
		return
	}
	p := filepath.Join(a.root, "book.epub")
	if err := os.MkdirAll(a.root, 0o755); err != nil {
		t.Fatalf("mkdir root: %v", err)
	}
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	if _, err := a.store.DB().Exec(`
		INSERT INTO media_files (root, path, kind, size_bytes, mtime, book_id, scanned_at)
		VALUES (?, 'book.epub', 'epub', ?, ?, 'OL1W', ?)`,
		a.root, len(data), time.Now().UnixNano(), time.Now().UTC()); err != nil {
		t.Fatalf("insert media file: %v", err)
	}
}

func (a *epubTestApp) get(t *testing.T, path string) (int, map[string]any) {
	t.Helper()
	resp, err := a.client.Get(a.ts.URL + path)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	defer resp.Body.Close()
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("GET %s: decode: %v", path, err)
	}
	return resp.StatusCode, body
}

// apiEpubFixture is a small real-ish book: two text chapters plus a
// cover page, NCX TOC, normalization hazards in the prose.
func apiEpubFixture(t *testing.T) []byte {
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
    <item id="cov" href="cover.xhtml" media-type="application/xhtml+xml"/>
    <item id="c1" href="c1.xhtml" media-type="application/xhtml+xml"/>
    <item id="c2" href="c2.xhtml" media-type="application/xhtml+xml"/>
  </manifest>
  <spine toc="ncx"><itemref idref="cov"/><itemref idref="c1"/><itemref idref="c2"/></spine>
</package>`},
		{"OEBPS/toc.ncx", `<?xml version="1.0"?>
<ncx xmlns="http://www.daisy.org/z3986/2005/ncx/">
  <navMap>
    <navPoint><navLabel><text>Alpha</text></navLabel><content src="c1.xhtml"/></navPoint>
    <navPoint><navLabel><text>Beta</text></navLabel><content src="c2.xhtml"/></navPoint>
  </navMap>
</ncx>`},
		{"OEBPS/cover.xhtml", `<html><body><img src="cover.png"/></body></html>`},
		// c1 is the hostile chapter: a script that must never render or
		// reach the canonical text, four references that must never reach
		// the network (remote, protocol-relative, data:, and one climbing
		// out of the zip), and one real internal illustration that must.
		{"OEBPS/c1.xhtml", `<html><head><script>nope()</script></head><body>
		  <script>alert(1)</script>
		  <h1 onclick="alert(2)">Alpha</h1>
		  <img src="https://evil.example/tracker.png" alt="remote"/>
		  <img src="//evil.example/protocol-relative.gif"/>
		  <img src="data:image/png;base64,AAAA"/>
		  <img src="../../../etc/passwd"/>
		  <img src="art.png" alt="A hog"/>
		  <p>“It’s ﬁne,” he said—truly.</p><p>More text.</p></body></html>`},
		{"OEBPS/c2.xhtml", `<html><body><p>The end.</p></body></html>`},
		{"OEBPS/art.png", string(pngPixel(t))},
		{"OEBPS/cover.png", string(pngPixel(t))},
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

func TestBookTextEndpoints(t *testing.T) {
	app := newEpubTestApp(t)
	app.attachEpub(t, apiEpubFixture(t))

	// Chapters: lazy parse happens on first hit.
	status, body := app.get(t, "/api/books/"+epubFixtureEntry+"/text/chapters")
	if status != http.StatusOK {
		t.Fatalf("chapters status = %d: %v", status, body)
	}
	if body["parser_version"] != booktext.ParserVersion {
		t.Errorf("parser_version = %v", body["parser_version"])
	}
	chapters, ok := body["chapters"].([]any)
	if !ok || len(chapters) != 3 {
		t.Fatalf("chapters = %#v", body["chapters"])
	}
	first := chapters[1].(map[string]any)
	if first["title"] != "Alpha" {
		t.Errorf("chapter 1 title = %v", first["title"])
	}
	blocks, ok := first["blocks"].([]any)
	if !ok || len(blocks) != 3 {
		t.Fatalf("chapter 1 blocks = %#v (want h1 + 2 paragraphs)", first["blocks"])
	}
	// Partition holds end to end.
	prevEnd := 0.0
	var lastEnd float64
	for i, ch := range chapters {
		c := ch.(map[string]any)
		start, end := c["char_start"].(float64), c["char_end"].(float64)
		if start != prevEnd {
			t.Errorf("chapter %d starts %v, previous ended %v", i, start, prevEnd)
		}
		prevEnd = end
		lastEnd = end
	}
	if lastEnd != body["char_count"].(float64) {
		t.Errorf("last chapter end %v != char_count %v", lastEnd, body["char_count"])
	}

	// The parse is recorded.
	var parsed int
	if err := app.store.DB().QueryRow(`SELECT COUNT(*) FROM epub_texts`).Scan(&parsed); err != nil || parsed != 1 {
		t.Errorf("epub_texts rows = %d err=%v", parsed, err)
	}

	// Ranged text: byte offsets into the canonical text.
	status, body = app.get(t, "/api/books/"+epubFixtureEntry+"/text")
	if status != http.StatusOK {
		t.Fatalf("text status = %d: %v", status, body)
	}
	full := body["text"].(string)
	want := "alpha its fine he said truly more text the end"
	if full != want {
		t.Errorf("full text = %q, want %q", full, want)
	}
	status, body = app.get(t, "/api/books/"+epubFixtureEntry+"/text?from=0&to=5")
	if status != http.StatusOK || body["text"].(string) != want[:5] {
		t.Errorf("ranged read: status=%d text=%q", status, body["text"])
	}
	if body["char_count"].(float64) != float64(len(want)) {
		t.Errorf("char_count = %v", body["char_count"])
	}
}

func TestBookTextRangeValidation(t *testing.T) {
	app := newEpubTestApp(t)
	app.attachEpub(t, apiEpubFixture(t))

	_, body := app.get(t, "/api/books/"+epubFixtureEntry+"/text")
	total := int(body["char_count"].(float64))

	cases := []struct {
		query string
	}{
		{"from=5&to=3"},
		{"from=-1"},
		{"to=" + strconv.Itoa(total+1)},
		{"from=abc"},
	}
	for _, c := range cases {
		if status, _ := app.get(t, "/api/books/"+epubFixtureEntry+"/text?"+c.query); status != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400", c.query, status)
		}
	}
	// Zero-width and full-width are fine.
	if status, _ := app.get(t, "/api/books/"+epubFixtureEntry+"/text?from=3&to=3"); status != http.StatusOK {
		t.Errorf("zero-width: status = %d", status)
	}
}

func TestBookTextErrors(t *testing.T) {
	app := newEpubTestApp(t)
	app.attachEpub(t, nil) // entry exists, no epub attached

	if status, body := app.get(t, "/api/books/"+epubFixtureEntry+"/text/chapters"); status != http.StatusNotFound || !strings.Contains(body["error"].(string), "no ebook") {
		t.Errorf("no-epub: status=%d body=%v", status, body)
	}
	if status, _ := app.get(t, "/api/books/does-not-exist/text"); status != http.StatusNotFound {
		t.Errorf("missing entry: status = %d", status)
	}

	// Another user's entry is a plain 404, not a 403.
	jar2, _ := cookiejar.New(nil)
	other := &http.Client{Jar: jar2, Timeout: 10 * time.Second}
	register(t, app.ts.URL, other, "snoop@example.com", "snoop", "hogwash123")
	resp, err := other.Get(app.ts.URL + "/api/books/" + epubFixtureEntry + "/text")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("cross-user: status = %d, want 404", resp.StatusCode)
	}
}

func TestBookTextDRM(t *testing.T) {
	app := newEpubTestApp(t)
	drm := buildDRMZip(t)
	app.attachEpub(t, drm)

	if status, body := app.get(t, "/api/books/"+epubFixtureEntry+"/text/chapters"); status != http.StatusUnprocessableEntity {
		t.Errorf("drm: status = %d body = %v, want 422", status, body)
	}
}

func buildDRMZip(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, e := range [][2]string{
		{"META-INF/container.xml", "<container/>"},
		{"META-INF/encryption.xml", "<encryption/>"},
	} {
		w, err := zw.Create(e[0])
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(w, e[1]); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// pngPixel is a 1x1 PNG: enough for the asset endpoint to have real bytes
// with a real content type to serve.
func pngPixel(t *testing.T) []byte {
	t.Helper()
	const b64 = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg=="
	data, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		t.Fatalf("decode png fixture: %v", err)
	}
	return data
}

// getRaw is get() for responses that are not JSON.
func (a *epubTestApp) getRaw(t *testing.T, path string) (*http.Response, []byte) {
	t.Helper()
	resp, err := a.client.Get(a.ts.URL + path)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("GET %s: read: %v", path, err)
	}
	return resp, data
}

func assetPath(href string) string {
	return "/api/books/" + epubFixtureEntry + "/text/asset?href=" + url.QueryEscape(href)
}

// TestBookTextIsInert is the reader's safety contract, checked at the layer
// that enforces it. The reader renders from the parsed spine — canonical
// text blocks and an image list — and never from EPUB markup, so a book
// carrying a script or a tracking pixel cannot put either on the page: the
// script never becomes text, and the remote references never become an
// address anything downstream could load.
func TestBookTextIsInert(t *testing.T) {
	app := newEpubTestApp(t)
	app.attachEpub(t, apiEpubFixture(t))

	status, body := app.get(t, "/api/books/"+epubFixtureEntry+"/text/chapters")
	if status != http.StatusOK {
		t.Fatalf("chapters status = %d: %v", status, body)
	}
	payload, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"evil.example", "alert(", "data:image", "onclick", "etc/passwd"} {
		if bytes.Contains(payload, []byte(forbidden)) {
			t.Errorf("chapters payload carries %q:\n%s", forbidden, payload)
		}
	}

	chapters := body["chapters"].([]any)
	// The image-only cover document keeps its illustration...
	cover := chapters[0].(map[string]any)["images"].([]any)
	if len(cover) != 1 || cover[0].(map[string]any)["href"] != "OEBPS/cover.png" {
		t.Errorf("cover images = %#v", cover)
	}
	// ...and the hostile chapter keeps exactly the one that is really in
	// the book, anchored ahead of the first paragraph (block 1, after the
	// h1) rather than at the top.
	images := chapters[1].(map[string]any)["images"].([]any)
	if len(images) != 1 {
		t.Fatalf("chapter 1 images = %#v, want only the internal one", images)
	}
	img := images[0].(map[string]any)
	if img["href"] != "OEBPS/art.png" || img["alt"] != "A hog" {
		t.Errorf("image = %#v", img)
	}
	if img["before_block"].(float64) != 1 {
		t.Errorf("image before_block = %v, want 1", img["before_block"])
	}

	// The script body never becomes prose either.
	_, body = app.get(t, "/api/books/"+epubFixtureEntry+"/text")
	if text := body["text"].(string); strings.Contains(text, "alert") || strings.Contains(text, "nope") {
		t.Errorf("canonical text carries script source: %q", text)
	}
}

func TestBookTextAsset(t *testing.T) {
	app := newEpubTestApp(t)
	app.attachEpub(t, apiEpubFixture(t))
	// Parse first: the asset endpoint reads the EPUB directly, but the
	// hrefs a client knows about come from the chapters payload.
	if status, body := app.get(t, "/api/books/"+epubFixtureEntry+"/text/chapters"); status != http.StatusOK {
		t.Fatalf("chapters status = %d: %v", status, body)
	}

	resp, data := app.getRaw(t, assetPath("OEBPS/art.png"))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("asset status = %d", resp.StatusCode)
	}
	if got := resp.Header.Get("Content-Type"); got != "image/png" {
		t.Errorf("content type = %q", got)
	}
	if resp.Header.Get("X-Content-Type-Options") != "nosniff" {
		t.Errorf("missing nosniff header")
	}
	if !bytes.Equal(data, pngPixel(t)) {
		t.Errorf("asset bytes = %d, want the fixture pixel", len(data))
	}
}

// TestBookTextAssetRefusals: the href is a request, not an authority. Every
// refusal answers 404 so the response never says which rule was broken.
func TestBookTextAssetRefusals(t *testing.T) {
	app := newEpubTestApp(t)
	app.attachEpub(t, apiEpubFixture(t))

	for _, href := range []string{
		"",
		"https://evil.example/tracker.png",
		"//evil.example/x.png",
		"data:image/png;base64,AAAA",
		"/etc/passwd",
		"../../../etc/passwd",
		"OEBPS/../../../etc/passwd",
		"..%2F..%2Fetc%2Fpasswd",
		"OEBPS/c1.xhtml",    // in the book, but not an image
		"OEBPS/content.opf", // ditto
		"OEBPS/missing.png",
	} {
		resp, _ := app.getRaw(t, assetPath(href))
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("asset %q: status = %d, want 404", href, resp.StatusCode)
		}
	}

	// Someone else's book is the same 404, not a 403.
	jar, _ := cookiejar.New(nil)
	other := &http.Client{Jar: jar, Timeout: 10 * time.Second}
	register(t, app.ts.URL, other, "snoop2@example.com", "snoop2", "hogwash123")
	resp, err := other.Get(app.ts.URL + assetPath("OEBPS/art.png"))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("cross-user asset: status = %d, want 404", resp.StatusCode)
	}
}

// TestBookTextDisplay is the reader's rendering contract: the display blocks
// line up one-for-one with the canonical block offsets, and they carry the
// book's own capitals and punctuation rather than the folded matching text.
func TestBookTextDisplay(t *testing.T) {
	app := newEpubTestApp(t)
	app.attachEpub(t, apiEpubFixture(t))

	status, body := app.get(t, "/api/books/"+epubFixtureEntry+"/text/chapters")
	if status != http.StatusOK {
		t.Fatalf("chapters status = %d: %v", status, body)
	}
	chapter := body["chapters"].([]any)[1].(map[string]any)
	offsets := chapter["blocks"].([]any)

	status, body = app.get(t, "/api/books/"+epubFixtureEntry+"/text/display?spine=1")
	if status != http.StatusOK {
		t.Fatalf("display status = %d: %v", status, body)
	}
	blocks := body["blocks"].([]any)
	if len(blocks) != len(offsets) {
		t.Fatalf("display blocks = %d, canonical offsets = %d", len(blocks), len(offsets))
	}
	if blocks[0] != "Alpha" {
		t.Errorf("block 0 = %q, want the heading as written", blocks[0])
	}
	// The prose keeps what the canonical text folds away.
	if got := blocks[1].(string); got != "“It’s ﬁne,” he said—truly." {
		t.Errorf("block 1 = %q, want the book's own punctuation", got)
	}
	// A script's body is not prose here either.
	for _, b := range blocks {
		if strings.Contains(b.(string), "alert") {
			t.Errorf("display block carries script source: %q", b)
		}
	}

	// The image-only cover document has no prose, and says so rather than
	// erroring or borrowing its neighbour's.
	if status, body = app.get(t, "/api/books/"+epubFixtureEntry+"/text/display?spine=0"); status != http.StatusOK {
		t.Fatalf("cover display status = %d: %v", status, body)
	}
	if got := body["blocks"].([]any); len(got) != 0 {
		t.Errorf("cover blocks = %#v, want none", got)
	}

	for _, q := range []string{"spine=99", "spine=-1", "spine=abc", ""} {
		if status, _ := app.get(t, "/api/books/"+epubFixtureEntry+"/text/display?"+q); status == http.StatusOK {
			t.Errorf("display %q: status = %d, want a refusal", q, status)
		}
	}
}
