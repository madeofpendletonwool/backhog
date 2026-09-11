package http

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/collinpendleton/backhog/api/internal/config"
	"github.com/collinpendleton/backhog/api/internal/models"
)

// The OCR lettering queue's HTTP surface, end to end over the position
// harness: the comic entry becomes searchable through the same internal
// endpoints a real worker drives, the ledger makes a re-run idempotent,
// and — the load-site guarantee — the corpus stays invisible to every
// text-mode code path even while it answers search.

// ocrApp is the position harness with the OCR worker token set, so the
// /internal half of the queue answers.
func ocrApp(t *testing.T) *positionTestApp {
	t.Helper()
	return newPositionTestApp(t, nil, func(c *config.Config) {
		c.OCRWorkerToken = "test-ocr-token"
	})
}

// internalOCR is one authenticated call into the internal worker API.
func internalOCR(t *testing.T, app *positionTestApp, method, path string, body any) (int, map[string]any, []byte) {
	t.Helper()
	var payload []byte
	if body != nil {
		var err error
		if payload, err = json.Marshal(body); err != nil {
			t.Fatalf("marshal: %v", err)
		}
	}
	req, err := http.NewRequest(method, app.ts.URL+path, strings.NewReader(string(payload)))
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Authorization", "Bearer test-ocr-token")
	resp, err := app.client.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var out map[string]any
	if strings.Contains(resp.Header.Get("Content-Type"), "json") {
		_ = json.Unmarshal(raw, &out)
	}
	return resp.StatusCode, out, raw
}

// runOCRPass drives one whole worker run over the internal API: claim,
// fetch every page, upload lettering for each, complete. It returns the
// page image hashes it saw, keyed by page number, so tests can pin the
// ledger.
func runOCRPass(t *testing.T, app *positionTestApp, entry string, lettering map[int]string) map[int]string {
	t.Helper()

	status, body := app.api(t, http.MethodPost, "/api/books/"+entry+"/ocr", nil)
	if status != http.StatusCreated {
		t.Fatalf("enqueue: %d %v", status, body)
	}

	_, claim, _ := internalOCR(t, app, http.MethodPost, "/internal/ocr/claim", map[string]any{"worker": "w1"})
	jobID, _ := claim["job"].(map[string]any)["id"].(string)
	if jobID == "" {
		t.Fatalf("claim returned no job: %v", claim)
	}
	pageCount := int(claim["page_count"].(float64))

	hashes := map[int]string{}
	var pages []map[string]any
	for page := 1; page <= pageCount; page++ {
		req, _ := http.NewRequest(http.MethodGet,
			app.ts.URL+fmt.Sprintf("/internal/ocr/%s/page/%d?worker=w1", jobID, page), nil)
		req.Header.Set("Authorization", "Bearer test-ocr-token")
		resp, err := app.client.Do(req)
		if err != nil {
			t.Fatalf("page %d: %v", page, err)
		}
		raw, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode == http.StatusUnprocessableEntity {
			continue // vector art or an unservable codec: no lettering
		}
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("page %d: status %d", page, resp.StatusCode)
		}
		hash := resp.Header.Get("X-Backhog-Page-Sha256")
		if hash == "" {
			t.Fatalf("page %d served without its image hash", page)
		}
		hashes[page] = hash
		_ = raw
		pages = append(pages, map[string]any{
			"page_number":     page,
			"image_sha256":    hash,
			"text":            lettering[page],
			"mean_confidence": 0.9,
		})
	}

	if len(pages) > 0 {
		if code, body, _ := internalOCR(t, app, http.MethodPost,
			fmt.Sprintf("/internal/ocr/%s/pages", jobID),
			map[string]any{"worker": "w1", "ocr_version": "1", "pages": pages}); code != http.StatusOK {
			t.Fatalf("pages upload: %d %v", code, body)
		}
	}
	code, body, _ := internalOCR(t, app, http.MethodPost,
		fmt.Sprintf("/internal/ocr/%s/complete", jobID),
		map[string]any{"worker": "w1", "model": "tesseract 5.3.0 (eng)"})
	if code != http.StatusOK {
		t.Fatalf("complete: %d %v", code, body)
	}
	return hashes
}

// TestOCROffIsOff pins the invariant-8 bar from the token's side: an
// empty OCR_WORKER_TOKEN disables the whole internal half — 503s, not
// open endpoints — and nothing about the public surface changes for
// books that never opted in.
func TestOCROffIsOff(t *testing.T) {
	app := newPositionTestApp(t, nil) // no token

	status, body := app.api(t, http.MethodPost, "/internal/ocr/claim", map[string]any{"worker": "w1"})
	if status != http.StatusServiceUnavailable {
		t.Fatalf("claim with no token = %d %v, want 503", status, body)
	}

	// The public status still answers, honestly saying no worker exists.
	status, body = app.api(t, http.MethodGet, "/api/books/"+positionComic+"/ocr", nil)
	if status != http.StatusOK {
		t.Fatalf("status = %d %v", status, body)
	}
	if body["worker_enabled"] != false {
		t.Errorf("worker_enabled = %v, want false", body["worker_enabled"])
	}
	if body["job"] != nil || body["corpus"] != nil {
		t.Errorf("status = %v, want no job and no corpus", body)
	}

	// A user may still queue a pass — the align contract: the job waits,
	// the UI says so, everything else works.
	status, _ = app.api(t, http.MethodPost, "/api/books/"+positionComic+"/ocr", nil)
	if status != http.StatusCreated {
		t.Errorf("enqueue with no worker = %d, want 201 (it waits)", status)
	}
	// ...and the internal half still refuses to hand it out.
	status, _ = app.api(t, http.MethodPost, "/internal/ocr/claim", map[string]any{"worker": "w1"})
	if status != http.StatusServiceUnavailable {
		t.Errorf("claim after enqueue = %d, want 503", status)
	}
}

// TestOCRInternalTokenRequired: a token set means the endpoints exist —
// for the bearer of the token only.
func TestOCRInternalTokenRequired(t *testing.T) {
	app := ocrApp(t)

	req, _ := http.NewRequest(http.MethodPost, app.ts.URL+"/internal/ocr/claim", strings.NewReader(`{"worker":"w1"}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("claim without token = %d, want 401", resp.StatusCode)
	}

	// The alignment token is a different secret and opens nothing here.
	req, _ = http.NewRequest(http.MethodPost, app.ts.URL+"/internal/ocr/claim", strings.NewReader(`{"worker":"w1"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer align-token-pretender")
	resp, err = app.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("claim with the wrong token = %d, want 401", resp.StatusCode)
	}

	// Empty queue with the right token: 204, the polling answer.
	status, _, _ := internalOCR(t, app, http.MethodPost, "/internal/ocr/claim", map[string]any{"worker": "w1"})
	if status != http.StatusNoContent {
		t.Errorf("empty claim = %d, want 204", status)
	}
}

// TestOCRComicBecomesSearchable is the feature, end to end: a fixture
// comic's lettering is read through the worker API, and a search over the
// book answers with page-targeted hits wired to the paged peek — never an
// offset, never a position write.
func TestOCRComicBecomesSearchable(t *testing.T) {
	app := ocrApp(t)

	runOCRPass(t, app, positionComic, map[int]string{
		1: "WE WERE LEAN AND HUNGRY!",
		2: "There's NOTHING wrong with that!",
	})

	status, body := app.api(t, http.MethodGet, "/api/books/"+positionComic+"/ocr", nil)
	if status != http.StatusOK {
		t.Fatalf("status: %d %v", status, body)
	}
	corpus, _ := body["corpus"].(map[string]any)
	if corpus == nil || corpus["state"] != models.OCRReady {
		t.Fatalf("corpus = %v, want ready", body["corpus"])
	}
	if corpus["coverage"] != 1.0 || corpus["page_count"] != float64(2) {
		t.Errorf("corpus = %v, want full coverage over 2 pages", corpus)
	}

	// The search: phrase the reader remembers, hit the page it lives on.
	status, body = app.api(t, http.MethodGet,
		"/api/books/"+positionComic+"/search?q=nothing+wrong+with+that", nil)
	if status != http.StatusOK {
		t.Fatalf("search: %d %v", status, body)
	}
	if body["mode"] != "phrase" {
		t.Errorf("mode = %v, want phrase", body["mode"])
	}
	results, _ := body["results"].([]any)
	if len(results) != 1 {
		t.Fatalf("results = %v", body["results"])
	}
	hit, _ := results[0].(map[string]any)
	if hit["page_index"] != float64(1) {
		t.Errorf("hit = %v, want page_index 1", hit)
	}
	if _, ok := hit["char_offset"]; ok {
		t.Error("paged hit carries a char offset — the axis a comic cannot have")
	}
	if hit["percent"] != 100.0 { // last of two pages: page one is 0%, the last 100%
		t.Errorf("percent = %v, want 100", hit["percent"])
	}
	context, _ := hit["context"].(map[string]any)
	if !strings.Contains(context["passage"].(string), "NOTHING wrong") {
		t.Errorf("context passage = %v, wants the raw lettering", context)
	}
	if corpus2, _ := body["corpus"].(map[string]any); corpus2 == nil {
		t.Error("search response does not carry the corpus grade")
	}

	// The same query as the reader types it — folded, half-remembered.
	status, body = app.api(t, http.MethodGet,
		"/api/books/"+positionComic+"/search?q=lean+and+hungry", nil)
	if status != http.StatusOK || body["mode"] != "phrase" {
		t.Fatalf("folded search: %d %v", status, body)
	}

	// Something the comic never says: an honest empty, mode still phrase,
	// no loose guesses from two pages of lettering.
	status, body = app.api(t, http.MethodGet,
		"/api/books/"+positionComic+"/search?q=war+and+peace", nil)
	if status != http.StatusOK {
		t.Fatalf("absent search: %d %v", status, body)
	}
	if body["mode"] != "loose" || body["total"] != float64(0) {
		t.Errorf("absent search = %v", body)
	}

	// The peek rule: a search hit lands as a look, never a move. Put the
	// reader somewhere first, search after it, and the stored page stays.
	status, body = app.api(t, http.MethodPut, "/api/books/"+positionComic+"/position",
		map[string]any{"page_index": 0})
	if status != http.StatusOK {
		t.Fatalf("put position: %d %v", status, body)
	}
	status, _ = app.api(t, http.MethodGet,
		"/api/books/"+positionComic+"/search?q=second+page+target", nil)
	_ = status
	status, pos := app.api(t, http.MethodGet, "/api/books/"+positionComic+"/position", nil)
	if status != http.StatusOK {
		t.Fatalf("position after search: %d", status)
	}
	if pos["position_mode"] != "page" || pos["page_index"] != float64(0) {
		t.Errorf("position after search = %v, want still page 0 — a hit never writes", pos)
	}
}

// TestOCRLedgerSkipsUnchangedPages: a second run over the same file gets
// every page's pin in its claim — the extraction ledger that makes
// re-runs idempotent and never re-bills an unchanged page.
func TestOCRLedgerSkipsUnchangedPages(t *testing.T) {
	app := ocrApp(t)
	hashes := runOCRPass(t, app, positionComic, map[int]string{
		1: "first read",
		2: "second read",
	})

	// Re-enqueue after a finished run and claim: the ledger arrives full.
	status, _ := app.api(t, http.MethodPost, "/api/books/"+positionComic+"/ocr", nil)
	if status != http.StatusCreated && status != http.StatusOK {
		t.Fatalf("re-enqueue = %d", status)
	}
	_, claim, _ := internalOCR(t, app, http.MethodPost, "/internal/ocr/claim", map[string]any{"worker": "w1"})
	ledger, _ := claim["ledger"].(map[string]any)
	if len(ledger) != 2 {
		t.Fatalf("ledger = %v, want both pages", ledger)
	}
	for page, want := range map[string]string{"1": hashes[1], "2": hashes[2]} {
		entry, _ := ledger[page].(map[string]any)
		if entry == nil || entry["image_sha256"] != want || entry["ocr_version"] != "1" {
			t.Errorf("ledger[%s] = %v, want hash %s version 1", page, ledger[page], want)
		}
	}

	// The served page still hashes to the same pin — the worker's skip
	// rule (hash match + version match) fires for every page.
	jobID := claim["job"].(map[string]any)["id"].(string)
	for page, want := range hashes {
		req, _ := http.NewRequest(http.MethodGet,
			app.ts.URL+fmt.Sprintf("/internal/ocr/%s/page/%d?worker=w1", jobID, page), nil)
		req.Header.Set("Authorization", "Bearer test-ocr-token")
		resp, err := app.client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		got := resp.Header.Get("X-Backhog-Page-Sha256")
		resp.Body.Close()
		if got != want {
			t.Errorf("page %d hash = %s, want the ledger pin %s", page, got, want)
		}
	}

	// Completing without touching a page keeps the corpus searchable.
	code, body, _ := internalOCR(t, app, http.MethodPost,
		fmt.Sprintf("/internal/ocr/%s/complete", jobID),
		map[string]any{"worker": "w1", "model": "tesseract 5.3.0 (eng)"})
	if code != http.StatusOK {
		t.Fatalf("complete: %d %v", code, body)
	}
	status, sbody := app.api(t, http.MethodGet,
		"/api/books/"+positionComic+"/search?q=second+read", nil)
	if status != http.StatusOK || sbody["total"] != float64(1) {
		t.Fatalf("search after no-op rerun: %d %v", status, sbody)
	}
}

// TestOCREnqueueRefusals: only a paged book can be lettered, and every
// other population gets its named refusal.
func TestOCREnqueueRefusals(t *testing.T) {
	app := ocrApp(t)

	cases := []struct {
		entry  string
		status int
		want   string
	}{
		{positionEntry, http.StatusUnprocessableEntity, "not a PDF"},
		{positionProse, http.StatusUnprocessableEntity, "text layer"},
		{positionDual, http.StatusUnprocessableEntity, "not a PDF"},
		{positionAudioOnly, http.StatusNotFound, "no ebook"},
		{"does-not-exist", http.StatusNotFound, "not found"},
	}
	for _, c := range cases {
		status, body := app.api(t, http.MethodPost, "/api/books/"+c.entry+"/ocr", nil)
		if status != c.status {
			t.Errorf("%s: status = %d, want %d: %v", c.entry, status, c.status, body)
			continue
		}
		if msg, _ := body["error"].(string); !strings.Contains(msg, c.want) {
			t.Errorf("%s: error = %q, want it to mention %q", c.entry, msg, c.want)
		}
	}
}

// TestOCRPageEndpointRules: the page stream is claim-scoped and validates
// its inputs like the reader's own page endpoint.
func TestOCRPageEndpointRules(t *testing.T) {
	app := ocrApp(t)
	if _, body := app.api(t, http.MethodPost, "/api/books/"+positionComic+"/ocr", nil); body == nil {
		t.Fatal("enqueue failed")
	}
	_, claim, _ := internalOCR(t, app, http.MethodPost, "/internal/ocr/claim", map[string]any{"worker": "w1"})
	jobID := claim["job"].(map[string]any)["id"].(string)

	// Another worker's id does not open the claim's pages.
	status, _, _ := internalOCR(t, app, http.MethodGet,
		fmt.Sprintf("/internal/ocr/%s/page/1?worker=someone-else", jobID), nil)
	if status != http.StatusConflict {
		t.Errorf("wrong worker page = %d, want 409", status)
	}
	// No worker id at all.
	status, _, _ = internalOCR(t, app, http.MethodGet,
		fmt.Sprintf("/internal/ocr/%s/page/1", jobID), nil)
	if status != http.StatusBadRequest {
		t.Errorf("no worker page = %d, want 400", status)
	}
	// Out of range and nonsense pages.
	for _, p := range []string{"0", "9", "abc"} {
		status, _, _ = internalOCR(t, app, http.MethodGet,
			fmt.Sprintf("/internal/ocr/%s/page/%s?worker=w1", jobID, p), nil)
		if status != http.StatusBadRequest {
			t.Errorf("page %s = %d, want 400", p, status)
		}
	}
	// Unknown job.
	status, _, _ = internalOCR(t, app, http.MethodGet,
		"/internal/ocr/no-such-job/page/1?worker=w1", nil)
	if status != http.StatusNotFound {
		t.Errorf("unknown job page = %d, want 404", status)
	}
}

// TestOCRSearchRefusals: a comic nobody has read answers with the honest
// 422, and so does a too-short query.
func TestOCRSearchRefusals(t *testing.T) {
	app := ocrApp(t)

	// Classify the comic first (the position/pages endpoints do this
	// lazily), so the search dispatch sees a paged primary.
	if status, body := app.api(t, http.MethodGet, "/api/books/"+positionComic+"/pages", nil); status != http.StatusOK {
		t.Fatalf("classify: %d %v", status, body)
	}

	status, body := app.api(t, http.MethodGet, "/api/books/"+positionComic+"/search?q=lean+hun", nil)
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("unread comic search = %d %v, want 422", status, body)
	}
	if msg, _ := body["error"].(string); !strings.Contains(msg, "lettering") {
		t.Errorf("error = %q, wants the lettering not read yet message", msg)
	}

	runOCRPass(t, app, positionComic, map[int]string{1: "lettering", 2: "more"})

	status, _ = app.api(t, http.MethodGet, "/api/books/"+positionComic+"/search?q=zz", nil)
	if status != http.StatusUnprocessableEntity {
		t.Errorf("short query = %d, want 422", status)
	}
}

// TestOCRDeleteClearsSearch: the user-facing stop drops the corpus and
// with it the searchability.
func TestOCRDeleteClearsSearch(t *testing.T) {
	app := ocrApp(t)
	runOCRPass(t, app, positionComic, map[int]string{1: "words", 2: "more words"})

	status, _ := app.api(t, http.MethodDelete, "/api/books/"+positionComic+"/ocr", nil)
	if status != http.StatusNoContent {
		t.Fatalf("delete = %d, want 204", status)
	}
	status, body := app.api(t, http.MethodGet, "/api/books/"+positionComic+"/ocr", nil)
	if status != http.StatusOK || body["corpus"] != nil || body["job"] != nil {
		t.Fatalf("status after clear = %d %v", status, body)
	}
	status, _ = app.api(t, http.MethodGet, "/api/books/"+positionComic+"/search?q=more+words", nil)
	if status != http.StatusUnprocessableEntity {
		t.Errorf("search after clear = %d, want 422", status)
	}
}

// TestOCRSearchInvisibleToTextPaths is the load-site guarantee the whole
// corpus design hangs on: with a full searchable corpus present, every
// text-mode code path — the reader's text endpoints, the passage matcher,
// alignment eligibility — still refuses the paged book exactly as it did
// before the corpus existed. The corpus cannot leak because nothing that
// loads a canonical text can load it, and this pins that at each load
// site rather than by convention.
func TestOCRSearchInvisibleToTextPaths(t *testing.T) {
	app := ocrApp(t)
	runOCRPass(t, app, positionComic, map[int]string{
		1: "WE WERE LEAN AND HUNGRY!",
		2: "There's NOTHING wrong with that!",
	})

	// The reader's text endpoints: image-native refusal, unchanged.
	for _, path := range []string{"/text/chapters", "/text", "/text/display"} {
		status, body := app.api(t, http.MethodGet, "/api/books/"+positionComic+path, nil)
		if status != http.StatusUnprocessableEntity {
			t.Errorf("%s = %d %v, want the image-native 422", path, status, body)
			continue
		}
		if msg, _ := body["error"].(string); !strings.Contains(msg, "pages, not prose") {
			t.Errorf("%s error = %q, want the paged refusal", path, msg)
		}
	}

	// The passage matcher: same refusal, not a match against lettering.
	status, body := app.api(t, http.MethodPost, "/api/books/"+positionComic+"/passage",
		map[string]any{"text": "we were lean and hungry", "printed_page": 1})
	if status != http.StatusUnprocessableEntity {
		t.Errorf("passage = %d %v, want 422", status, body)
	}

	// Alignment eligibility: no canonical text exists to align against,
	// corpus or no corpus.
	status, body = app.api(t, http.MethodPost, "/api/books/"+positionComic+"/align", nil)
	if status != http.StatusUnprocessableEntity {
		t.Errorf("align = %d %v, want 422", status, body)
	}

	// And the canonical tables hold nothing for the file: the corpus is
	// not a text and never becomes one.
	var texts int
	if err := app.store.DB().QueryRow(
		`SELECT COUNT(*) FROM epub_texts`).Scan(&texts); err != nil {
		t.Fatal(err)
	}
	if texts != 0 {
		t.Errorf("epub_texts holds %d rows for the corpus fixture, want 0", texts)
	}
	var chapters int
	if err := app.store.DB().QueryRow(
		`SELECT COUNT(*) FROM epub_chapters`).Scan(&chapters); err != nil {
		t.Fatal(err)
	}
	if chapters != 0 {
		t.Errorf("epub_chapters holds %d rows, want 0", chapters)
	}

	// Meanwhile the same search over a text-mode book keeps answering in
	// offsets — the two corpora never share a shape.
	app2 := newPositionTestApp(t, nil)
	_ = app2.charCount(t)
	status, body = app2.api(t, http.MethodGet,
		"/api/books/"+positionEntry+"/search?q=its+fine+he+said+truly", nil)
	if status != http.StatusOK {
		t.Fatalf("prose search regression: %d %v", status, body)
	}
	results, _ := body["results"].([]any)
	if len(results) == 0 {
		t.Fatal("prose search lost its hit")
	}
	hit, _ := results[0].(map[string]any)
	if _, ok := hit["page_index"]; ok {
		t.Error("prose hit carries a page_index — the text corpus grew the wrong axis")
	}
	if hit["char_offset"] == nil {
		t.Error("prose hit lost its char offset")
	}
}
