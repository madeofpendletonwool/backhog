package http

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/collinpendleton/backhog/api/internal/backfill"
	"github.com/collinpendleton/backhog/api/internal/config"
	"github.com/collinpendleton/backhog/api/internal/db"
	"github.com/collinpendleton/backhog/api/internal/fixtures"
	"github.com/collinpendleton/backhog/api/internal/media"
	"github.com/collinpendleton/backhog/api/internal/metadata"
	"github.com/collinpendleton/backhog/api/internal/store"
)

// attachTestApp is a booted router whose media runner points at a real
// fixture NAS tree, with the EPUB text dir configured so attaching an EPUB
// actually parses it.
type attachTestApp struct {
	ts     *httptest.Server
	client *http.Client
	store  *store.Store
	root   string
}

func newAttachTestApp(t *testing.T) *attachTestApp {
	t.Helper()

	root := t.TempDir()
	write := func(rel, body string) {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", rel, err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}
	writeBytes := func(rel string, body []byte) {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", rel, err)
		}
		if err := os.WriteFile(p, body, 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}
	// A directory-per-book audiobook, a single-file audiobook, a real EPUB,
	// a DRM-free Kindle file, an Audible file the scanner must skip and
	// explain — and the pdf text side: a text-native book, an image-only
	// comic the scanner inventories but the parse refuses, and an encrypted
	// file the scanner refuses.
	write("Neal Stephenson/Anathem/01 - Erasmas.m4b", "fake audio 1")
	write("Neal Stephenson/Anathem/02 - Apert.m4b", "fake audio 2")
	write("Andy Weir/Project Hail Mary.m4b", "fake single audio")
	write("books/Dune.epub", string(apiEpubFixture(t)))
	writeBytes("kindle/Synthetic PalmDOC Book.mobi", fixtures.MOBI6Palmdoc)
	write("Audible/locked.aax", "audible DRM bytes")
	writeBytes("pdf/The Synthetic Book.pdf", fixtures.BuildProsePDF())
	writeBytes("pictures/Pictures.pdf", fixtures.BuildImageOnlyPDF())
	lockedPDF, err := fixtures.BuildEncryptedPDF(true)
	if err != nil {
		t.Fatalf("build encrypted pdf: %v", err)
	}
	writeBytes("pdf/locked.pdf", lockedPDF)

	database, err := db.Open(filepath.Join(t.TempDir(), "attach.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	if err := db.Migrate(database); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	st := store.New(database)

	cfg := config.Config{EpubTextDir: filepath.Join(t.TempDir(), "epub_text")}
	srv := NewServer(cfg, st, nil, nil, nil, nil, &backfill.Runner{}, media.NewRunner(st, []string{root}))
	ts := httptest.NewServer(srv.Routes())
	t.Cleanup(ts.Close)

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookie jar: %v", err)
	}
	client := &http.Client{Jar: jar, Timeout: 10 * time.Second}
	register(t, ts.URL, client, "attach@example.com", "attacher", "hogwash123")

	// The metadata cache holds the works the fixtures imitate, so matching
	// and adding to the library both work offline.
	for _, b := range []metadata.Book{
		{ID: "OL1W", Title: "Anathem", Authors: []string{"Neal Stephenson"}},
		{ID: "OL2W", Title: "Project Hail Mary", Authors: []string{"Andy Weir"}},
		{ID: "OL3W", Title: "Dune", Authors: []string{"Frank Herbert"}},
		{ID: "OL4W", Title: "The Synthetic Book", Authors: []string{"Fixture Author"}},
		{ID: "OL5W", Title: "Pictures", Authors: []string{"Fixture Artist"}},
	} {
		if err := st.UpsertBook(t.Context(), b, ""); err != nil {
			t.Fatalf("seed book %s: %v", b.ID, err)
		}
	}

	return &attachTestApp{ts: ts, client: client, store: st, root: root}
}

func (a *attachTestApp) req(t *testing.T, method, path string, body any) (int, map[string]any) {
	t.Helper()
	var reader *bytes.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		reader = bytes.NewReader(raw)
	} else {
		reader = bytes.NewReader(nil)
	}
	req, err := http.NewRequest(method, a.ts.URL+path, reader)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := a.client.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	var decoded map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		t.Fatalf("%s %s: decode: %v", method, path, err)
	}
	return resp.StatusCode, decoded
}

func (a *attachTestApp) scanAndWait(t *testing.T) {
	t.Helper()
	_, kicked := a.req(t, http.MethodPost, "/api/media/scan", nil)
	if kicked["started"] != true {
		t.Fatal("scan did not start")
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		_, status := a.req(t, http.MethodGet, "/api/media/scan", nil)
		if status["running"] == false && status["last"] != nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("scan never finished: %v", status)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// candidateBy finds a candidate by its directory path.
func candidateBy(t *testing.T, cs []any, dirPath string) map[string]any {
	t.Helper()
	for _, c := range cs {
		cm := c.(map[string]any)
		if cm["dir_path"] == dirPath {
			return cm
		}
	}
	t.Fatalf("no candidate for dir %q", dirPath)
	return nil
}

func fileIDByPath(t *testing.T, files []any, path string) float64 {
	t.Helper()
	for _, f := range files {
		fm := f.(map[string]any)
		if fm["path"] == path {
			return fm["id"].(float64)
		}
	}
	t.Fatalf("no file row for path %q", path)
	return 0
}

func addBookEntry(t *testing.T, a *attachTestApp, bookID string) string {
	t.Helper()
	status, body := a.req(t, http.MethodPost, "/api/library", map[string]any{"book_id": bookID})
	if status != http.StatusCreated {
		t.Fatalf("add %s: status %d: %v", bookID, status, body)
	}
	return body["id"].(string)
}

// TestAttachFlow walks the whole review queue end to end: scan, match,
// attach in track order, EPUB parse on attach, detach leaving the file on
// disk untouched, and the skipped-file explanation.
func TestAttachFlow(t *testing.T) {
	app := newAttachTestApp(t)
	app.scanAndWait(t)

	// Anonymous candidates are rejected.
	// (covered for /media/files already; same auth group)

	entry := addBookEntry(t, app, "OL1W")

	status, body := app.req(t, http.MethodGet, "/api/media/candidates", nil)
	if status != http.StatusOK {
		t.Fatalf("candidates: status %d: %v", status, body)
	}
	candidates := body["candidates"].([]any)
	if len(candidates) != 6 {
		t.Fatalf("got %d candidates, want 6 (two audio groups, the epub, the mobi, two pdfs): %v", len(candidates), body)
	}

	// The Anathem directory: grouped, ordered, confidently matched to the
	// library copy the user owns.
	anathem := candidateBy(t, candidates, "Neal Stephenson/Anathem")
	if anathem["kind"] != "audio" || anathem["title_guess"] != "Anathem" {
		t.Fatalf("anathem candidate = %v", anathem)
	}
	if anathem["high_confidence"] != true {
		t.Errorf("anathem not high confidence: %v", anathem["suggestions"])
	}
	sugg := anathem["suggestions"].([]any)[0].(map[string]any)
	if sugg["source"] != "library" || sugg["in_library"] != true {
		t.Errorf("anathem top suggestion = %v", sugg)
	}
	files := anathem["files"].([]any)
	if len(files) != 2 || fileOrder(files, 0) != "Neal Stephenson/Anathem/01 - Erasmas.m4b" {
		t.Errorf("anathem file order = %v", files)
	}

	// The EPUB: matched from its filename, not owned yet.
	epub := candidateBy(t, candidates, "books")
	if epub["kind"] != "epub" || epub["title_guess"] != "Dune" {
		t.Fatalf("epub candidate = %v", epub)
	}

	// The Kindle file: inventoried like any text-side book, carrying what
	// its own EXTH metadata says it is.
	mobi := candidateBy(t, candidates, "kindle")
	if mobi["kind"] != "epub" || mobi["title_guess"] != "Synthetic PalmDOC Book" {
		t.Fatalf("mobi candidate = %v", mobi)
	}

	// The pdf: a normal text-side candidate too. The scanner cannot tell a
	// text-native pdf from an image-native one — that verdict belongs to
	// the parse — so both inventory, and the encrypted sibling is the only
	// one refused, with its own name for the lock.
	synthetic := candidateBy(t, candidates, "pdf")
	if synthetic["kind"] != "epub" || synthetic["title_guess"] != "The Synthetic Book" {
		t.Fatalf("pdf candidate = %v", synthetic)
	}
	if got := len(synthetic["files"].([]any)); got != 1 {
		t.Errorf("pdf candidate holds %d files, want 1 (locked.pdf is skipped, not grouped)", got)
	}

	// The skipped files are explained, not silently missing.
	skipped := body["skipped"].([]any)
	if len(skipped) != 2 {
		t.Fatalf("skipped = %v, want two rows", skipped)
	}
	skipReason := map[string]string{}
	for _, s := range skipped {
		sm := s.(map[string]any)
		skipReason[sm["path"].(string)] = sm["reason"].(string)
	}
	if skipReason["Audible/locked.aax"] != "unsupported_extension" {
		t.Errorf("locked.aax reason = %q", skipReason["Audible/locked.aax"])
	}
	if skipReason["pdf/locked.pdf"] != "drm_pdf" {
		t.Errorf("locked.pdf reason = %q, want drm_pdf", skipReason["pdf/locked.pdf"])
	}

	// Attach the audio group in the candidate's order: the array is the
	// explicit track order.
	ids := []float64{fileID(files, 1), fileID(files, 0)} // deliberately reversed
	status, body = app.req(t, http.MethodPost, "/api/books/"+entry+"/files",
		map[string]any{"file_ids": ids, "kind": "audio"})
	if status != http.StatusCreated {
		t.Fatalf("attach audio: status %d: %v", status, body)
	}
	if body["attached"] != float64(2) {
		t.Errorf("attached = %v", body["attached"])
	}

	status, body = app.req(t, http.MethodGet, "/api/books/"+entry+"/files", nil)
	if status != http.StatusOK {
		t.Fatalf("entry files: status %d: %v", status, body)
	}
	entryFiles := body["files"].([]any)
	if len(entryFiles) != 2 || entryFiles[0].(map[string]any)["path"] != "Neal Stephenson/Anathem/02 - Apert.m4b" {
		t.Errorf("attached order = %v (array order must be track order)", entryFiles)
	}

	// Attaching an EPUB triggers the canonical-text parse.
	duneEntry := addBookEntry(t, app, "OL3W")
	status, body = app.req(t, http.MethodGet, "/api/media/files?kind=epub&unattached=true", nil)
	if status != http.StatusOK {
		t.Fatalf("files: status %d: %v", status, body)
	}
	epubID := fileIDByPath(t, body["files"].([]any), "books/Dune.epub")
	status, body = app.req(t, http.MethodPost, "/api/books/"+duneEntry+"/files",
		map[string]any{"file_ids": []float64{epubID}, "kind": "epub"})
	if status != http.StatusCreated {
		t.Fatalf("attach epub: status %d: %v", status, body)
	}
	var parsed int
	if err := app.store.DB().QueryRow(
		`SELECT COUNT(*) FROM epub_texts et JOIN media_files mf ON mf.id = et.media_file_id
		 WHERE mf.path = 'books/Dune.epub'`).Scan(&parsed); err != nil {
		t.Fatalf("probe epub_texts: %v", err)
	}
	if parsed != 1 {
		t.Errorf("epub parse rows = %d, want 1 (attach must trigger the parse)", parsed)
	}

	// Attaching the Kindle file triggers the same canonical-text parse:
	// a .mobi flows through the identical machinery — one text row, its
	// chapters partitioning it, the same as the EPUB beside it.
	mobiEntry := addBookEntry(t, app, "OL2W")
	status, body = app.req(t, http.MethodGet, "/api/media/files?kind=epub&unattached=true", nil)
	if status != http.StatusOK {
		t.Fatalf("files: status %d: %v", status, body)
	}
	mobiID := fileIDByPath(t, body["files"].([]any), "kindle/Synthetic PalmDOC Book.mobi")
	status, body = app.req(t, http.MethodPost, "/api/books/"+mobiEntry+"/files",
		map[string]any{"file_ids": []float64{mobiID}, "kind": "epub"})
	if status != http.StatusCreated {
		t.Fatalf("attach mobi: status %d: %v", status, body)
	}
	var mobiParsed, mobiChapters int
	if err := app.store.DB().QueryRow(
		`SELECT COUNT(*) FROM epub_texts et JOIN media_files mf ON mf.id = et.media_file_id
		 WHERE mf.path = 'kindle/Synthetic PalmDOC Book.mobi'`).Scan(&mobiParsed); err != nil {
		t.Fatalf("probe mobi epub_texts: %v", err)
	}
	if err := app.store.DB().QueryRow(
		`SELECT COUNT(*) FROM epub_chapters ch JOIN epub_texts et ON et.id = ch.epub_text_id
		 JOIN media_files mf ON mf.id = et.media_file_id
		 WHERE mf.path = 'kindle/Synthetic PalmDOC Book.mobi'`).Scan(&mobiChapters); err != nil {
		t.Fatalf("probe mobi chapters: %v", err)
	}
	// Three chapters, not four: the fixture's NCX opens with a "Begin
	// Reading" guide anchor at the same byte as the first real chapter, so
	// it owns no text and no longer becomes a zero-length row of its own.
	if mobiParsed != 1 || mobiChapters != 3 {
		t.Errorf("mobi parse = %d rows / %d chapters; want 1 / 3", mobiParsed, mobiChapters)
	}

	// The pdf flows through the identical machinery — one text row, its
	// outline-titled chapters partitioning it, readable and searchable end
	// to end.
	pdfEntry := addBookEntry(t, app, "OL4W")
	status, body = app.req(t, http.MethodGet, "/api/media/files?kind=epub&unattached=true", nil)
	if status != http.StatusOK {
		t.Fatalf("files: status %d: %v", status, body)
	}
	pdfID := fileIDByPath(t, body["files"].([]any), "pdf/The Synthetic Book.pdf")
	status, body = app.req(t, http.MethodPost, "/api/books/"+pdfEntry+"/files",
		map[string]any{"file_ids": []float64{pdfID}, "kind": "epub"})
	if status != http.StatusCreated {
		t.Fatalf("attach pdf: status %d: %v", status, body)
	}
	var pdfParsed, pdfChapters int
	if err := app.store.DB().QueryRow(
		`SELECT COUNT(*) FROM epub_texts et JOIN media_files mf ON mf.id = et.media_file_id
		 WHERE mf.path = 'pdf/The Synthetic Book.pdf'`).Scan(&pdfParsed); err != nil {
		t.Fatalf("probe pdf epub_texts: %v", err)
	}
	if err := app.store.DB().QueryRow(
		`SELECT COUNT(*) FROM epub_chapters ch JOIN epub_texts et ON et.id = ch.epub_text_id
		 JOIN media_files mf ON mf.id = et.media_file_id
		 WHERE mf.path = 'pdf/The Synthetic Book.pdf'`).Scan(&pdfChapters); err != nil {
		t.Fatalf("probe pdf chapters: %v", err)
	}
	if pdfParsed != 1 || pdfChapters != 3 {
		t.Errorf("pdf parse = %d rows / %d chapters; want 1 / 3", pdfParsed, pdfChapters)
	}

	// Readable: the chapters payload carries the outline's titles and the
	// ranged text fetch reads the canonical bytes.
	status, body = app.req(t, http.MethodGet, "/api/books/"+pdfEntry+"/text/chapters", nil)
	if status != http.StatusOK {
		t.Fatalf("pdf chapters: status %d: %v", status, body)
	}
	if body["toc"].(map[string]any)["source"] != "outline" {
		t.Errorf("pdf toc source = %v, want the outline", body["toc"])
	}
	pdfCh := body["chapters"].([]any)
	if len(pdfCh) != 3 || pdfCh[0].(map[string]any)["title"] != "Chapter One" {
		t.Fatalf("pdf chapter titles = %v", pdfCh)
	}
	status, body = app.req(t, http.MethodGet, "/api/books/"+pdfEntry+"/text?from=0&to=7", nil)
	if status != http.StatusOK || body["text"] != "chapter" {
		t.Errorf("pdf ranged read = (%d, %v)", status, body["text"])
	}

	// Searchable: a hit carries its chapter, over the pdf-sourced canonical
	// text exactly like an epub-sourced one.
	status, body = app.req(t, http.MethodGet, searchPath(pdfEntry, "until the hyphen joins"), nil)
	if status != http.StatusOK {
		t.Fatalf("pdf search: status %d: %v", status, body)
	}
	pdfHits := results(t, body)
	if len(pdfHits) == 0 {
		t.Fatal("pdf search found nothing")
	}
	if ch := pdfHits[0]["chapter"].(map[string]any); ch["title"] != "Chapter One" {
		t.Errorf("pdf search hit chapter = %v, want Chapter One", ch)
	}

	// The image-native pdf: inventoried, attachable — and refused a
	// canonical text with the honest interim label when read. The
	// attachment itself holds; nothing was half-parsed.
	picturesEntry := addBookEntry(t, app, "OL5W")
	status, body = app.req(t, http.MethodGet, "/api/media/files?kind=epub&unattached=true", nil)
	if status != http.StatusOK {
		t.Fatalf("files: status %d: %v", status, body)
	}
	picturesID := fileIDByPath(t, body["files"].([]any), "pictures/Pictures.pdf")
	status, body = app.req(t, http.MethodPost, "/api/books/"+picturesEntry+"/files",
		map[string]any{"file_ids": []float64{picturesID}, "kind": "epub"})
	if status != http.StatusCreated {
		t.Fatalf("attach image-only pdf: status %d: %v", status, body)
	}
	var picturesParsed int
	if err := app.store.DB().QueryRow(
		`SELECT COUNT(*) FROM epub_texts et JOIN media_files mf ON mf.id = et.media_file_id
		 WHERE mf.path = 'pictures/Pictures.pdf'`).Scan(&picturesParsed); err != nil {
		t.Fatalf("probe pictures epub_texts: %v", err)
	}
	if picturesParsed != 0 {
		t.Errorf("image-only pdf wrote %d canonical-text rows; want 0, never a half-parse", picturesParsed)
	}
	status, body = app.req(t, http.MethodGet, "/api/books/"+picturesEntry+"/text/chapters", nil)
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("image-only pdf chapters: status %d, want 422: %v", status, body)
	}
	if msg, _ := body["error"].(string); !strings.Contains(msg, "no readable text layer") {
		t.Errorf("image-only refusal = %q, want the honest interim label", msg)
	}

	// Attached files leave the review queue.
	status, body = app.req(t, http.MethodGet, "/api/media/candidates", nil)
	if status != http.StatusOK {
		t.Fatalf("candidates again: %d", status)
	}
	if got := len(body["candidates"].([]any)); got != 1 {
		t.Errorf("%d candidates after attaching, want 1 (Project Hail Mary)", got)
	}

	// Detach: the row survives, the file on disk is untouched.
	onDisk := filepath.Join(app.root, "Neal Stephenson/Anathem/02 - Apert.m4b")
	before, err := os.ReadFile(onDisk)
	if err != nil {
		t.Fatalf("read before detach: %v", err)
	}
	status, body = app.req(t, http.MethodDelete,
		fmt.Sprintf("/api/books/%s/files/%d", entry, int(ids[0])), nil)
	if status != http.StatusOK {
		t.Fatalf("detach: status %d: %v", status, body)
	}
	after, err := os.ReadFile(onDisk)
	if err != nil || !bytes.Equal(before, after) {
		t.Errorf("detach touched the file on disk: %v", err)
	}
	var bookIDSQL, trackSQL *any
	if err := app.store.DB().QueryRow(
		`SELECT book_id, track_number FROM media_files WHERE id = ?`, int(ids[0])).
		Scan(&bookIDSQL, &trackSQL); err != nil {
		t.Fatalf("probe detached row: %v", err)
	}
	if bookIDSQL != nil || trackSQL != nil {
		t.Errorf("detached row = (%v, %v); want (nil, nil)", bookIDSQL, trackSQL)
	}

	// The detached file returns to the queue.
	status, body = app.req(t, http.MethodGet, "/api/media/candidates", nil)
	if status != http.StatusOK {
		t.Fatalf("candidates third: %d", status)
	}
	if got := len(body["candidates"].([]any)); got != 2 {
		t.Errorf("%d candidates after detach, want 2", got)
	}
}

func fileID(files []any, i int) float64 {
	return files[i].(map[string]any)["id"].(float64)
}

func fileOrder(files []any, i int) string {
	return files[i].(map[string]any)["path"].(string)
}

// TestAttachErrors pins the failure modes: unknown entry, foreign file,
// kind mismatch, wrong-owner entry, and the ignore round trip.
func TestAttachErrors(t *testing.T) {
	app := newAttachTestApp(t)
	app.scanAndWait(t)
	entry := addBookEntry(t, app, "OL1W")
	other := addBookEntry(t, app, "OL2W")

	status, body := app.req(t, http.MethodGet, "/api/media/files?kind=audio&unattached=true", nil)
	audioFiles := body["files"].([]any)
	anathem0 := fileIDByPath(t, audioFiles, "Neal Stephenson/Anathem/01 - Erasmas.m4b")
	anathem1 := fileIDByPath(t, audioFiles, "Neal Stephenson/Anathem/02 - Apert.m4b")

	if status, body = app.req(t, http.MethodPost, "/api/books/nope/files",
		map[string]any{"file_ids": []float64{anathem0}, "kind": "audio"}); status != http.StatusNotFound {
		t.Errorf("unknown entry status = %d: %v", status, body)
	}
	if status, body = app.req(t, http.MethodPost, "/api/books/"+entry+"/files",
		map[string]any{"file_ids": []float64{anathem0}, "kind": "epub"}); status != http.StatusBadRequest {
		t.Errorf("kind mismatch status = %d: %v", status, body)
	}
	if status, body = app.req(t, http.MethodPost, "/api/books/"+entry+"/files",
		map[string]any{"file_ids": []float64{anathem0, anathem0}, "kind": "audio"}); status != http.StatusBadRequest {
		t.Errorf("duplicate id status = %d: %v", status, body)
	}
	if status, body = app.req(t, http.MethodPost, "/api/books/"+entry+"/files",
		map[string]any{"file_ids": []float64{9999}, "kind": "audio"}); status != http.StatusNotFound {
		t.Errorf("unknown file status = %d: %v", status, body)
	}

	// Attach to one book, then attempt the same file on another book.
	if status, body = app.req(t, http.MethodPost, "/api/books/"+entry+"/files",
		map[string]any{"file_ids": []float64{anathem0, anathem1}, "kind": "audio"}); status != http.StatusCreated {
		t.Fatalf("attach: status %d: %v", status, body)
	}
	if status, body = app.req(t, http.MethodPost, "/api/books/"+other+"/files",
		map[string]any{"file_ids": []float64{anathem0}, "kind": "audio"}); status != http.StatusConflict {
		t.Errorf("cross-book attach status = %d: %v", status, body)
	}

	// Detaching through the wrong entry is a 404, not a cross-edit.
	if status, _ = app.req(t, http.MethodDelete,
		fmt.Sprintf("/api/books/%s/files/%d", other, int(anathem0)), nil); status != http.StatusNotFound {
		t.Errorf("detach through wrong entry status = %d", status)
	}

	// Ignore hides a candidate; unignore brings it back.
	status, body = app.req(t, http.MethodPost, "/api/media/ignore",
		map[string]any{"file_ids": []float64{fileIDByPath(t, audioFiles, "Andy Weir/Project Hail Mary.m4b")}})
	if status != http.StatusOK || body["ignored"] != float64(1) {
		t.Fatalf("ignore = (%d, %v)", status, body)
	}
	status, body = app.req(t, http.MethodGet, "/api/media/candidates", nil)
	if got := len(body["candidates"].([]any)); got != 4 {
		t.Fatalf("%d candidates after ignoring Project Hail Mary, want 4 (the epub, the mobi and the two pdfs): %v", got, body)
	}
	for _, c := range body["candidates"].([]any) {
		if c.(map[string]any)["dir_path"] == "Andy Weir" {
			t.Error("ignored candidate still in the queue")
		}
	}
	status, body = app.req(t, http.MethodGet, "/api/media/files?kind=audio&unattached=true", nil)
	phm := fileIDByPath(t, body["files"].([]any), "Andy Weir/Project Hail Mary.m4b")
	if status, _ = app.req(t, http.MethodDelete, fmt.Sprintf("/api/media/ignore/%d", int(phm)), nil); status != http.StatusOK {
		t.Errorf("unignore status = %d", status)
	}
	status, body = app.req(t, http.MethodGet, "/api/media/candidates", nil)
	if got := len(body["candidates"].([]any)); got != 5 {
		t.Errorf("%d candidates after unignore, want 5", got)
	}

	// Anonymous access is refused.
	resp, err := http.Get(app.ts.URL + "/api/media/candidates")
	if err != nil {
		t.Fatalf("anonymous candidates: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("anonymous candidates status = %d, want 401", resp.StatusCode)
	}
}

// TestAlternateFormatFlow walks the case a NAS full of ebook packs produces:
// the same book as .epub and .mobi in one folder, confirmed weeks apart.
//
// Three things have to hold. The pair is one question, not two. The .mobi
// arriving after the .epub was confirmed resolves to that same book by its
// filename rather than by a fresh guess, and attaching it does not write a
// second canonical text. And switching which format the book is read from is
// available, deliberate, and takes the reader's position with it.
func TestAlternateFormatFlow(t *testing.T) {
	app := newAttachTestApp(t)
	pair := filepath.Join(app.root, "pair")
	if err := os.MkdirAll(pair, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(pair, "Dune.epub"), apiEpubFixture(t), 0o644); err != nil {
		t.Fatalf("write epub: %v", err)
	}
	if err := os.WriteFile(filepath.Join(pair, "Dune.mobi"), fixtures.MOBI6Palmdoc, 0o644); err != nil {
		t.Fatalf("write mobi: %v", err)
	}
	app.scanAndWait(t)
	entry := addBookEntry(t, app, "OL3W")

	// One candidate for the pair, holding both files, the epub speaking for
	// it. Two candidates here is the bug: one book, asked about twice.
	status, body := app.req(t, http.MethodGet, "/api/media/candidates", nil)
	if status != http.StatusOK {
		t.Fatalf("candidates: status %d: %v", status, body)
	}
	c := candidateBy(t, body["candidates"].([]any), "pair")
	if files := c["files"].([]any); len(files) != 2 ||
		files[0].(map[string]any)["path"] != "pair/Dune.epub" {
		t.Fatalf("pair candidate files = %v, want both formats with the epub first", c["files"])
	}
	if c["alternate_format"] == true {
		t.Error("a pair with nothing attached is an alternate of nothing")
	}

	// Attach only the epub, the way a user confirming last month would have.
	status, body = app.req(t, http.MethodGet, "/api/media/files?kind=epub&unattached=true", nil)
	if status != http.StatusOK {
		t.Fatalf("files: status %d: %v", status, body)
	}
	inventory := body["files"].([]any)
	epubID := fileIDByPath(t, inventory, "pair/Dune.epub")
	mobiID := fileIDByPath(t, inventory, "pair/Dune.mobi")
	status, body = app.req(t, http.MethodPost, "/api/books/"+entry+"/files",
		map[string]any{"file_ids": []float64{epubID}, "kind": "epub"})
	if status != http.StatusCreated {
		t.Fatalf("attach epub: status %d: %v", status, body)
	}

	// The mobi now comes back as what it is: another format of a book
	// already attached, matched on its own filename, pointed at the entry.
	status, body = app.req(t, http.MethodGet, "/api/media/candidates", nil)
	if status != http.StatusOK {
		t.Fatalf("candidates: status %d: %v", status, body)
	}
	c = candidateBy(t, body["candidates"].([]any), "pair")
	if c["alternate_format"] != true || c["alternate_of"] != "pair/Dune.epub" {
		t.Fatalf("mobi candidate = %v, want an alternate of the attached epub", c)
	}
	top := c["suggestions"].([]any)[0].(map[string]any)
	if top["confidence"] != float64(1) || top["entry_id"] != entry ||
		top["book"].(map[string]any)["id"] != "OL3W" {
		t.Fatalf("suggestion = %v, want the sibling's own book at confidence 1", top)
	}

	// Attaching it records the format and nothing else: still one parsed
	// canonical text, still the epub.
	status, body = app.req(t, http.MethodPost, "/api/books/"+entry+"/files",
		map[string]any{"file_ids": []float64{mobiID}, "kind": "epub"})
	if status != http.StatusCreated {
		t.Fatalf("attach mobi: status %d: %v", status, body)
	}
	var texts int
	if err := app.store.DB().QueryRow(
		`SELECT COUNT(*) FROM epub_texts et JOIN media_files mf ON mf.id = et.media_file_id
		 WHERE mf.path LIKE 'pair/%'`).Scan(&texts); err != nil {
		t.Fatalf("probe epub_texts: %v", err)
	}
	if texts != 1 {
		t.Errorf("parsed texts = %d, want 1 — a second format is owned, not a second text", texts)
	}

	status, body = app.req(t, http.MethodGet, "/api/books/"+entry+"/files", nil)
	if status != http.StatusOK {
		t.Fatalf("entry files: status %d: %v", status, body)
	}
	primary := map[string]bool{}
	for _, f := range body["files"].([]any) {
		fm := f.(map[string]any)
		primary[fm["path"].(string)] = fm["primary_text"] == true
	}
	if !primary["pair/Dune.epub"] || primary["pair/Dune.mobi"] {
		t.Fatalf("primary flags = %v, want the epub reading and the mobi merely owned", primary)
	}

	// Switching parses the target first, so a format that has never been
	// read can still be promoted in one request.
	status, body = app.req(t, http.MethodPut,
		fmt.Sprintf("/api/books/%s/files/%d/primary", entry, int64(mobiID)), nil)
	if status != http.StatusOK {
		t.Fatalf("promote: status %d: %v", status, body)
	}
	if body["file"].(map[string]any)["primary_text"] != true {
		t.Fatalf("promoted file = %v, want primary_text true", body["file"])
	}

	status, body = app.req(t, http.MethodGet, "/api/books/"+entry+"/files", nil)
	if status != http.StatusOK {
		t.Fatalf("entry files: status %d: %v", status, body)
	}
	primary = map[string]bool{}
	for _, f := range body["files"].([]any) {
		fm := f.(map[string]any)
		primary[fm["path"].(string)] = fm["primary_text"] == true
	}
	if primary["pair/Dune.epub"] || !primary["pair/Dune.mobi"] {
		t.Fatalf("primary flags after the switch = %v, want the mobi reading", primary)
	}
}

// TestAudioEditionsChooseWhichRecordingPlays covers the audio half of the
// format choice: two recordings of one book, only one of them on the
// timeline, and a switch that changes which.
//
// The fixture NAS holds a directory-per-book rip and a single-file m4b, so
// attaching both to one entry produces exactly the pair a user gets when
// they own the same title read by two different people.
func TestAudioEditionsChooseWhichRecordingPlays(t *testing.T) {
	app := newAttachTestApp(t)
	app.scanAndWait(t)
	entry := addBookEntry(t, app, "OL1W")

	status, body := app.req(t, http.MethodGet, "/api/media/files?kind=audio", nil)
	if status != http.StatusOK {
		t.Fatalf("media files: status %d: %v", status, body)
	}
	files := body["files"].([]any)
	rip := []any{
		fileIDByPath(t, files, "Neal Stephenson/Anathem/01 - Erasmas.m4b"),
		fileIDByPath(t, files, "Neal Stephenson/Anathem/02 - Apert.m4b"),
	}
	lone := fileIDByPath(t, files, "Andy Weir/Project Hail Mary.m4b")

	for _, batch := range []any{rip, []any{lone}} {
		status, body = app.req(t, http.MethodPost, "/api/books/"+entry+"/files",
			map[string]any{"file_ids": batch, "kind": "audio"})
		if status != http.StatusCreated {
			t.Fatalf("attach %v: status %d: %v", batch, status, body)
		}
	}

	status, body = app.req(t, http.MethodGet, "/api/books/"+entry+"/files", nil)
	if status != http.StatusOK {
		t.Fatalf("book files: status %d: %v", status, body)
	}
	editions := body["audio_editions"].([]any)
	if len(editions) != 2 {
		t.Fatalf("audio_editions = %v, want one per recording", editions)
	}
	playing := editions[0].(map[string]any)
	spare := editions[1].(map[string]any)
	if playing["primary"] != true || spare["primary"] != false {
		t.Fatalf("designation = %v, want the first-attached recording playing", editions)
	}
	if playing["label"] != "Anathem" || playing["track_count"] != 2.0 {
		t.Errorf("playing edition = %v, want the two-track Anathem rip", playing)
	}
	if spare["label"] != "Project Hail Mary" {
		t.Errorf("spare edition = %v, want the lone file named for itself", spare)
	}

	// Only the designated recording is a timeline. Before editions, this
	// returned all three files as one interleaved tape.
	status, body = app.req(t, http.MethodGet, "/api/books/"+entry+"/audio", nil)
	if status != http.StatusOK {
		t.Fatalf("timeline: status %d: %v", status, body)
	}
	if tracks := body["tracks"].([]any); len(tracks) != 2 {
		t.Fatalf("timeline has %d tracks, want only the designated recording's 2", len(tracks))
	}

	editionID := int64(spare["id"].(float64))
	status, body = app.req(t, http.MethodPut,
		fmt.Sprintf("/api/books/%s/audio-editions/%d/primary", entry, editionID), nil)
	if status != http.StatusOK {
		t.Fatalf("switch: status %d: %v", status, body)
	}
	if switched := body["audio_edition"].(map[string]any); switched["primary"] != true {
		t.Fatalf("switch returned %v, want the recording designated", switched)
	}

	status, body = app.req(t, http.MethodGet, "/api/books/"+entry+"/audio", nil)
	if status != http.StatusOK {
		t.Fatalf("timeline after switch: status %d: %v", status, body)
	}
	tracks := body["tracks"].([]any)
	if len(tracks) != 1 {
		t.Fatalf("timeline has %d tracks after the switch, want the lone m4b", len(tracks))
	}
	if got := tracks[0].(map[string]any)["path"]; got != "Andy Weir/Project Hail Mary.m4b" {
		t.Errorf("playing %v, want the recording just switched to", got)
	}

	// An id that is not this book's is a 404, never a hint that it exists.
	status, _ = app.req(t, http.MethodPut,
		fmt.Sprintf("/api/books/%s/audio-editions/%d/primary", entry, editionID+9000), nil)
	if status != http.StatusNotFound {
		t.Errorf("unknown edition = %d, want 404", status)
	}
}
