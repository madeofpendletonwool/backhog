package http

import (
	"bytes"
	"image/png"
	"io"
	"net/http"
	"net/http/cookiejar"
	"strings"
	"testing"
	"time"
)

// The paged reader's HTTP surface, end to end over the position harness:
// the comic entry (image-only PDF) must manifest and serve pages, and
// every other population — prose, epub, nothing attached, vector-only —
// must be refused by name. The peek rule itself lives in the reader (a
// peek never writes); here the write side it depends on is the paged
// position round trip the position tests already pin.

func TestBookPagesManifest(t *testing.T) {
	app := newPositionTestApp(t, nil)

	status, body := app.api(t, http.MethodGet, "/api/books/"+positionComic+"/pages", nil)
	if status != http.StatusOK {
		t.Fatalf("manifest status = %d: %v", status, body)
	}
	if body["page_count"] != float64(2) {
		t.Errorf("page_count = %v, want 2", body["page_count"])
	}
	pages, ok := body["pages"].([]any)
	if !ok || len(pages) != 2 {
		t.Fatalf("pages = %#v", body["pages"])
	}
	for i, p := range pages {
		page := p.(map[string]any)
		if page["index"] != float64(i) || page["has_image"] != true {
			t.Errorf("page %d = %#v", i, page)
		}
		// The fixture's plate is 8×8; the manifest tells the reader so
		// before any image bytes arrive.
		if page["width"] != float64(8) || page["height"] != float64(8) {
			t.Errorf("page %d dims = %v×%v, want 8×8", i, page["width"], page["height"])
		}
	}
}

func TestBookPageImageServes(t *testing.T) {
	app := newPositionTestApp(t, nil)

	resp, err := app.client.Get(app.ts.URL + "/api/books/" + positionComic + "/pages/1")
	if err != nil {
		t.Fatalf("get page: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "image/png" {
		t.Errorf("content type = %q, want image/png", ct)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if cfg, err := png.DecodeConfig(bytes.NewReader(data)); err != nil || cfg.Width != 8 || cfg.Height != 8 {
		t.Errorf("png = %v err = %v, want 8×8", cfg, err)
	}
	etag := resp.Header.Get("ETag")
	if etag == "" {
		t.Fatal("no etag on a page image")
	}
	if cc := resp.Header.Get("Cache-Control"); !strings.Contains(cc, "private") {
		t.Errorf("cache-control = %q, want private", cc)
	}
	if resp.Header.Get("X-Content-Type-Options") != "nosniff" {
		t.Error("page image not nosniff'd")
	}

	// The companion cache answers identically ever after, and the ETag
	// makes the re-fetch free.
	resp2, err := app.client.Get(app.ts.URL + "/api/books/" + positionComic + "/pages/1")
	if err != nil {
		t.Fatalf("re-get page: %v", err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK || resp2.Header.Get("ETag") != etag {
		t.Errorf("re-get: status=%d etag=%q, want 200 and the same etag", resp2.StatusCode, resp2.Header.Get("ETag"))
	}
	req, _ := http.NewRequest(http.MethodGet, app.ts.URL+"/api/books/"+positionComic+"/pages/1", nil)
	req.Header.Set("If-None-Match", etag)
	resp3, err := app.client.Do(req)
	if err != nil {
		t.Fatalf("conditional get: %v", err)
	}
	resp3.Body.Close()
	if resp3.StatusCode != http.StatusNotModified {
		t.Errorf("conditional get = %d, want 304", resp3.StatusCode)
	}
}

func TestBookPagesRefusals(t *testing.T) {
	app := newPositionTestApp(t, nil)

	cases := []struct {
		entry  string
		status int
		want   string
	}{
		// An EPUB primary is a text book — pages is the wrong door.
		{positionEntry, http.StatusUnprocessableEntity, "not a PDF"},
		// The dual book's designated text is the epub, so the pdf behind it
		// does not open the paged door either.
		{positionDual, http.StatusUnprocessableEntity, "not a PDF"},
		// A text-native PDF follows its classification: prose.
		{positionProse, http.StatusUnprocessableEntity, "prose"},
		// Vector art classifies image-native but carries no page images:
		// the named refusal, with the same honesty as .kfx.
		{positionVector, http.StatusUnprocessableEntity, "vector"},
		// No text file attached at all.
		{positionAudioOnly, http.StatusNotFound, "no ebook"},
	}
	for _, c := range cases {
		status, body := app.api(t, http.MethodGet, "/api/books/"+c.entry+"/pages", nil)
		if status != c.status {
			t.Errorf("%s: status = %d, want %d: %v", c.entry, status, c.status, body)
			continue
		}
		if msg, _ := body["error"].(string); !strings.Contains(msg, c.want) {
			t.Errorf("%s: error = %q, want it to mention %q", c.entry, msg, c.want)
		}
	}

	// The page image endpoint inherits every refusal.
	status, body := app.api(t, http.MethodGet, "/api/books/"+positionProse+"/pages/0", nil)
	if status != http.StatusUnprocessableEntity {
		t.Errorf("prose page 0: status = %d, want 422: %v", status, body)
	}
}

func TestBookPageImageValidation(t *testing.T) {
	app := newPositionTestApp(t, nil)

	for _, p := range []string{"2", "-1", "abc"} {
		resp, err := app.client.Get(app.ts.URL + "/api/books/" + positionComic + "/pages/" + p)
		if err != nil {
			t.Fatalf("get %s: %v", p, err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("page %q: status = %d, want 400", p, resp.StatusCode)
		}
	}

	// Somebody else's comic is a plain 404, not a 403 — the file door's
	// rule, held by the text endpoints, held here too.
	jar2, _ := cookiejar.New(nil)
	other := &http.Client{Jar: jar2, Timeout: 10 * time.Second}
	register(t, app.ts.URL, other, "snoop@example.com", "snoop", "hogwash123")
	resp, err := other.Get(app.ts.URL + "/api/books/" + positionComic + "/pages/0")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("cross-user page: status = %d, want 404", resp.StatusCode)
	}

	// And an unknown entry likewise.
	resp, err = app.client.Get(app.ts.URL + "/api/books/does-not-exist/pages")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("missing entry: status = %d, want 404", resp.StatusCode)
	}
}

// A page turn that lands writes the paged position, and the next session
// restores from it — the round trip the reader's turn and restore wire
// themselves to (the peek rule is the reader choosing not to write).
func TestBookPagesPositionRestore(t *testing.T) {
	app := newPositionTestApp(t, nil)

	// Reading page 1 of the comic is a position write...
	status, body := app.api(t, http.MethodPut, "/api/books/"+positionComic+"/position",
		map[string]any{"page_index": 1})
	if status != http.StatusOK {
		t.Fatalf("put page position: %d %v", status, body)
	}
	// ...that the manifest-then-position flow a reader runs on open reads
	// back exactly.
	status, body = app.api(t, http.MethodGet, "/api/books/"+positionComic+"/position", nil)
	if status != http.StatusOK {
		t.Fatalf("get position: %d %v", status, body)
	}
	if body["position_mode"] != "page" || body["page_index"] != float64(1) || body["page_count"] != float64(2) {
		t.Errorf("position = mode %v page %v of %v, want page 1 of 2",
			body["position_mode"], body["page_index"], body["page_count"])
	}
}
