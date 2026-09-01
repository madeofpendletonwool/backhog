package http

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/collinpendleton/backhog/api/internal/backfill"
	"github.com/collinpendleton/backhog/api/internal/config"
	"github.com/collinpendleton/backhog/api/internal/db"
	"github.com/collinpendleton/backhog/api/internal/metadata"
	"github.com/collinpendleton/backhog/api/internal/models"
	"github.com/collinpendleton/backhog/api/internal/store"
)

// pngBytes is a 1x1 PNG — decodable by the accent sampler.
var pngBytes = []byte{
	0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0x00, 0x00, 0x00, 0x0d,
	0x49, 0x48, 0x44, 0x52, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
	0x08, 0x06, 0x00, 0x00, 0x00, 0x1f, 0x15, 0xc4, 0x89, 0x00, 0x00, 0x00,
	0x0d, 0x49, 0x44, 0x41, 0x54, 0x78, 0x9c, 0x62, 0x00, 0x01, 0x00, 0x00,
	0x05, 0x00, 0x01, 0x0d, 0x0a, 0x2d, 0xb4, 0x00, 0x00, 0x00, 0x00, 0x49,
	0x45, 0x4e, 0x44, 0xae, 0x42, 0x60, 0x82,
}

// booksTestApp boots the full router over a migrated database with the real
// OpenLibrary provider pointed at a fake upstream, plus one logged-in user —
// the full search → cache → serve path without touching the live API.
type booksTestApp struct {
	ts       *httptest.Server
	upstream *httptest.Server
	client   *http.Client
	store    *store.Store
}

func newBooksTestApp(t *testing.T) *booksTestApp {
	t.Helper()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/search.json":
			writeFixture(t, w, map[string]any{"docs": []map[string]any{{
				"key":                "/works/OL1168083W",
				"title":              "The Hobbit",
				"author_name":        []string{"J. R. R. Tolkien"},
				"first_publish_year": 1937,
			}}})
		case r.URL.Path == "/works/OL1168083W.json":
			writeFixture(t, w, map[string]any{
				"title":              "The Hobbit",
				"description":        "In a hole in the ground there lived a hobbit.",
				"subjects":           []string{"Fantasy"},
				"first_publish_date": "1937",
			})
		case r.URL.Path == "/isbn/9780261102217.json":
			writeFixture(t, w, map[string]any{
				"key":             "/books/OL7440402M",
				"title":           "The Hobbit",
				"isbn_13":         []string{"9780261102217"},
				"publishers":      []string{"HarperCollins"},
				"publish_date":    "1997",
				"number_of_pages": 366,
				"physical_format": "Paperback",
				"works":           []map[string]string{{"key": "/works/OL1168083W"}},
			})
		case r.URL.Path == "/cover.jpg":
			w.Header().Set("Content-Type", "image/png")
			w.Write(pngBytes)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(upstream.Close)

	database, err := db.Open(filepath.Join(t.TempDir(), "books_e2e.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	if err := db.Migrate(database); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	covers, err := metadata.NewCoverCache(filepath.Join(t.TempDir(), "covers"))
	if err != nil {
		t.Fatalf("cover cache: %v", err)
	}

	ston := store.New(database)
	srv := NewServer(config.Config{}, ston, nil,
		metadata.NewOpenLibraryAt(upstream.URL), covers, nil, &backfill.Runner{}, nil)
	ts := httptest.NewServer(srv.Routes())
	t.Cleanup(ts.Close)

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookie jar: %v", err)
	}
	client := &http.Client{Jar: jar, Timeout: 10 * time.Second}

	body, _ := json.Marshal(map[string]string{
		"email":    "reader@example.com",
		"username": "reader",
		"password": "hogwash123",
	})
	resp, err := client.Post(ts.URL+"/api/auth/register", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("register: status %d", resp.StatusCode)
	}

	return &booksTestApp{ts: ts, upstream: upstream, client: client, store: ston}
}

func writeFixture(t *testing.T, w http.ResponseWriter, v any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		t.Fatalf("encode fixture: %v", err)
	}
}

func (a *booksTestApp) get(t *testing.T, path string) (int, []byte) {
	t.Helper()
	resp, err := a.client.Get(a.ts.URL + path)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	defer resp.Body.Close()
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(resp.Body); err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return resp.StatusCode, buf.Bytes()
}

// TestBooksEndpointsEndToEnd walks search → detail → ISBN → ownership, the
// flow the UI will drive, against the fake upstream.
func TestBooksEndpointsEndToEnd(t *testing.T) {
	app := newBooksTestApp(t)

	// Search caches the results and serves them back in provider order.
	status, body := app.get(t, "/api/books/search?q=hobbit")
	if status != http.StatusOK {
		t.Fatalf("search: status %d, body %s", status, body)
	}
	var search struct {
		Results []struct {
			Book      models.Book `json:"book"`
			InLibrary bool        `json:"in_library"`
		} `json:"results"`
	}
	if err := json.Unmarshal(body, &search); err != nil {
		t.Fatalf("decode search: %v", err)
	}
	if len(search.Results) != 1 {
		t.Fatalf("search results = %d, want 1", len(search.Results))
	}
	hit := search.Results[0]
	if hit.Book.ID != "OL1168083W" || hit.Book.Title != "The Hobbit" {
		t.Errorf("search hit = %+v", hit.Book)
	}
	if hit.InLibrary {
		t.Error("search hit reported in library before being added")
	}
	if len(hit.Book.Authors) != 1 || hit.Book.Authors[0] != "J. R. R. Tolkien" {
		t.Errorf("authors = %v", hit.Book.Authors)
	}

	// Detail is served from the cache the search populated.
	status, body = app.get(t, "/api/books/OL1168083W")
	if status != http.StatusOK {
		t.Fatalf("get book: status %d, body %s", status, body)
	}
	var book models.Book
	if err := json.Unmarshal(body, &book); err != nil {
		t.Fatalf("decode book: %v", err)
	}
	if book.ID != "OL1168083W" {
		t.Errorf("book ID = %q", book.ID)
	}

	// ISBN lookup returns the work with its edition attached.
	status, body = app.get(t, "/api/books/isbn/978-0261102217")
	if status != http.StatusOK {
		t.Fatalf("isbn: status %d, body %s", status, body)
	}
	book = models.Book{}
	if err := json.Unmarshal(body, &book); err != nil {
		t.Fatalf("decode isbn book: %v", err)
	}
	if book.ID != "OL1168083W" || len(book.Editions) != 1 {
		t.Fatalf("isbn book = %+v with %d editions", book, len(book.Editions))
	}
	ed := book.Editions[0]
	if ed.ID != "OL7440402M" || ed.ISBN13 != "9780261102217" || ed.PageCount == nil || *ed.PageCount != 366 {
		t.Errorf("edition = %+v", ed)
	}

	// Mark it owned; search must now report in_library.
	if _, err := app.store.DB().ExecContext(context.Background(),
		`INSERT INTO library_entries (id, user_id, media_type, book_id, status)
		 SELECT 'be1', id, 'book', 'OL1168083W', 'backlog' FROM users LIMIT 1`); err != nil {
		t.Fatalf("seed entry: %v", err)
	}
	_, body = app.get(t, "/api/books/search?q=hobbit")
	search.Results = nil
	if err := json.Unmarshal(body, &search); err != nil {
		t.Fatalf("decode second search: %v", err)
	}
	if len(search.Results) != 1 || !search.Results[0].InLibrary {
		t.Errorf("in_library after add = %+v", search.Results)
	}

	// Bad work key / ISBN: rejected, never proxied upstream.
	if status, _ := app.get(t, "/api/books/OL1168083M"); status != http.StatusBadRequest {
		t.Errorf("edition key as work id: status %d, want 400", status)
	}
	if status, _ := app.get(t, "/api/books/isbn/notanisbn"); status != http.StatusBadRequest {
		t.Errorf("malformed isbn: status %d, want 400", status)
	}
	if status, _ := app.get(t, "/api/books/isbn/9780000000002"); status != http.StatusNotFound {
		t.Errorf("unknown isbn: status %d, want 404", status)
	}

	// Book endpoints sit behind auth.
	anon := &http.Client{Timeout: 10 * time.Second}
	resp, err := anon.Get(app.ts.URL + "/api/books/search?q=hobbit")
	if err != nil {
		t.Fatalf("anon search: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("anon search: status %d, want 401", resp.StatusCode)
	}
}

// TestBookCoversEndToEnd exercises the generalized cover routes: the book
// cover downloads through the cache and serves, and the legacy game path
// redirects.
func TestBookCoversEndToEnd(t *testing.T) {
	app := newBooksTestApp(t)

	if err := app.store.UpsertBook(context.Background(), metadata.Book{
		ID: "OL27455W", Title: "Dune",
	}, ""); err != nil {
		t.Fatalf("seed dune: %v", err)
	}
	if err := app.store.UpsertBook(context.Background(), metadata.Book{
		ID: "OL1168083W", Title: "The Hobbit",
	}, ""); err != nil {
		t.Fatalf("seed hobbit: %v", err)
	}

	// The Hobbit was cached by search; point its cover at the fake upstream.
	if _, err := app.store.DB().ExecContext(context.Background(),
		`UPDATE books SET cover_url = ? WHERE id = 'OL1168083W'`,
		app.upstream.URL+"/cover.jpg"); err != nil {
		t.Fatalf("set cover url: %v", err)
	}

	status, body := app.get(t, "/api/covers/book/OL1168083W")
	if status != http.StatusOK {
		t.Fatalf("book cover: status %d, body %s", status, body)
	}
	if !bytes.Equal(body, pngBytes) {
		t.Errorf("book cover bytes = %d, want the fixture PNG", len(body))
	}
	// The accent sample and local path are recorded on the book row.
	var accent, local string
	if err := app.store.DB().QueryRowContext(context.Background(),
		`SELECT accent_hex, cover_local_path FROM books WHERE id = 'OL1168083W'`).Scan(&accent, &local); err != nil {
		t.Fatalf("reread accent: %v", err)
	}
	if local == "" {
		t.Error("cover_local_path not recorded")
	}

	// A book with no cover URL known → 404, not a crash.
	if status, _ := app.get(t, "/api/covers/book/OL27455W"); status != http.StatusNotFound {
		t.Errorf("unknown book cover: status %d, want 404", status)
	}

	// Legacy route: permanent redirect to the namespaced game path. The
	// default client would follow the redirect to a 404, so probe it raw.
	noRedirect := &http.Client{
		Timeout: 10 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp, err := noRedirect.Get(app.ts.URL + "/api/covers/5")
	if err != nil {
		t.Fatalf("legacy cover: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusMovedPermanently {
		t.Errorf("legacy cover: status %d, want 301", resp.StatusCode)
	}
	if got := resp.Header.Get("Location"); got != "/api/covers/game/5" {
		t.Errorf("legacy cover Location = %q", got)
	}

	// And the namespaced game route still serves the old numeric covers.
	resp3, err := app.client.Get(app.ts.URL + "/api/covers/game/5")
	if err != nil {
		t.Fatalf("game cover: %v", err)
	}
	resp3.Body.Close()
	if resp3.StatusCode != http.StatusNotFound {
		t.Errorf("game cover for unknown game: status %d, want 404", resp3.StatusCode)
	}
}
