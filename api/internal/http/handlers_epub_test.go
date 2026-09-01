package http

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/collinpendleton/backhog/api/internal/backfill"
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
		{"OEBPS/c1.xhtml", `<html><head><script>nope()</script></head><body>
		  <h1>Alpha</h1><p>“It’s ﬁne,” he said—truly.</p><p>More text.</p></body></html>`},
		{"OEBPS/c2.xhtml", `<html><body><p>The end.</p></body></html>`},
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
	if body["parser_version"] != "1" {
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
