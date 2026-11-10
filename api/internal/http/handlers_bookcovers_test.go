package http

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
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

// TestBookCoverPicker walks the picker: list every cover a work has, pin one,
// and check the pin survives the metadata refresh that would otherwise put
// Open Library's promoted cover back.
func TestBookCoverPicker(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/works/OL1W.json":
			writeFixture(t, w, map[string]any{"title": "Dune", "covers": []int64{111, 333, -1}})
		case "/works/OL1W/editions.json":
			if r.URL.Query().Get("limit") == "" {
				t.Errorf("editions fetched without a limit")
			}
			writeFixture(t, w, map[string]any{"entries": []map[string]any{
				{"key": "/books/OL9M", "publishers": []string{"Ace"}, "publish_date": "1990",
					"physical_format": "Paperback", "covers": []int64{222, 111}},
			}})
		case "/search.json":
			writeFixture(t, w, map[string]any{"docs": []map[string]any{}})
		case "/b/id/111-L.jpg", "/b/id/222-L.jpg":
			w.Header().Set("Content-Type", "image/png")
			w.Write(pngBytes)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(upstream.Close)

	database, err := db.Open(filepath.Join(t.TempDir(), "covers.db"))
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
	st := store.New(database)
	provider := metadata.NewOpenLibraryAt(upstream.URL).WithCoverBase(upstream.URL)
	ts := httptest.NewServer(NewServer(config.Config{}, st, nil, provider, covers, nil, &backfill.Runner{}, nil).Routes())
	t.Cleanup(ts.Close)

	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar, Timeout: 10 * time.Second}
	reg, _ := json.Marshal(map[string]string{"email": "c@example.com", "username": "coverpicker", "password": "hogwash123"})
	resp, err := client.Post(ts.URL+"/api/auth/register", "application/json", bytes.NewReader(reg))
	if err != nil || resp.StatusCode != http.StatusCreated {
		t.Fatalf("register: %v %v", err, resp)
	}
	resp.Body.Close()

	ctx := context.Background()
	promoted := provider.CoverURL(111)
	if err := st.UpsertBook(ctx, metadata.Book{ID: "OL1W", Title: "Dune", CoverURL: promoted}, ""); err != nil {
		t.Fatalf("seed book: %v", err)
	}
	// The promoted cover is already cached, as it would be after a browse.
	if _, err := covers.Fetch(ctx, "OL1W", promoted); err != nil {
		t.Fatalf("seed cover: %v", err)
	}
	before, _ := os.ReadFile(covers.Path("OL1W"))

	resp, err = client.Get(ts.URL + "/api/books/OL1W/covers")
	if err != nil {
		t.Fatalf("list covers: %v", err)
	}
	var list bookCoversResponse
	json.NewDecoder(resp.Body).Decode(&list)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list covers: status %d", resp.StatusCode)
	}
	if list.CurrentID != 111 {
		t.Errorf("current_id = %d, want 111", list.CurrentID)
	}
	var ids []int64
	for _, c := range list.Covers {
		ids = append(ids, c.ID)
	}
	if len(ids) != 3 || ids[0] != 222 || ids[1] != 111 || ids[2] != 333 {
		t.Fatalf("cover ids = %v, want edition covers first then the work's, deduped: [222 111 333]", ids)
	}
	if list.Covers[0].Publisher != "Ace" || list.Covers[0].Year == nil || *list.Covers[0].Year != 1990 {
		t.Errorf("edition cover lost its printing: %+v", list.Covers[0])
	}

	put := func(body string) *http.Response {
		req, _ := http.NewRequest(http.MethodPut, ts.URL+"/api/books/OL1W/cover", bytes.NewReader([]byte(body)))
		req.Header.Set("Content-Type", "application/json")
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("set cover: %v", err)
		}
		return resp
	}

	// A cover the upstream cannot serve leaves the old one alone.
	resp = put(`{"cover_id": 999}`)
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadGateway {
		t.Errorf("dead cover: status %d, want 502", resp.StatusCode)
	}
	if got, _ := st.CoverURLForBook(ctx, "OL1W"); got != promoted {
		t.Errorf("dead cover changed cover_url to %q", got)
	}

	resp = put(`{"cover_id": 222}`)
	var book models.Book
	json.NewDecoder(resp.Body).Decode(&book)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("set cover: status %d", resp.StatusCode)
	}
	if book.CoverURL != provider.CoverURL(222) {
		t.Errorf("cover_url = %q, want the chosen cover", book.CoverURL)
	}
	if after, _ := os.ReadFile(covers.Path("OL1W")); len(after) == 0 || len(before) == 0 {
		t.Errorf("cached cover missing after replace")
	}

	// A metadata refresh carrying the promoted cover must not undo the pick.
	if err := st.UpsertBook(ctx, metadata.Book{ID: "OL1W", Title: "Dune", CoverURL: promoted}, ""); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if got, _ := st.CoverURLForBook(ctx, "OL1W"); got != provider.CoverURL(222) {
		t.Errorf("refresh overwrote the pinned cover: %q", got)
	}

	// A versioned cover URL is cached hard; the bare one revalidates.
	resp, _ = client.Get(ts.URL + "/api/covers/book/OL1W")
	resp.Body.Close()
	if cc := resp.Header.Get("Cache-Control"); cc != "public, no-cache" {
		t.Errorf("bare cover Cache-Control = %q", cc)
	}
	resp, _ = client.Get(ts.URL + "/api/covers/book/OL1W?v=222")
	resp.Body.Close()
	if cc := resp.Header.Get("Cache-Control"); cc != "public, max-age=31536000, immutable" {
		t.Errorf("versioned cover Cache-Control = %q", cc)
	}
}
