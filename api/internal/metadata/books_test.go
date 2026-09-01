package metadata

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

// fakeOpenLibrary serves canned Open Library responses and records the
// requests made against it, so provider tests never touch the live API.
type fakeOpenLibrary struct {
	ts      *httptest.Server
	mux     *http.ServeMux
	agents  []string
	request []string
}

func newFakeOpenLibrary(t *testing.T) (*fakeOpenLibrary, *OpenLibrary) {
	t.Helper()
	f := &fakeOpenLibrary{mux: http.NewServeMux()}
	var inner http.Handler = f.mux
	f.ts = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.agents = append(f.agents, r.Header.Get("User-Agent"))
		f.request = append(f.request, r.URL.Path+"?"+r.URL.RawQuery)
		inner.ServeHTTP(w, r)
	}))
	t.Cleanup(f.ts.Close)
	return f, newOpenLibrary(f.ts.URL)
}

func writeFixture(t *testing.T, w http.ResponseWriter, v any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		t.Fatalf("encode fixture: %v", err)
	}
}

func TestOpenLibrarySearch(t *testing.T) {
	year := 1937
	f, client := newFakeOpenLibrary(t)
	f.mux.HandleFunc("/search.json", func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("q"); got != "hobbit" {
			t.Errorf("q = %q, want %q", got, "hobbit")
		}
		if got := r.URL.Query().Get("fields"); got == "" || !containsString(got, "author_name") {
			t.Errorf("fields = %q, want author_name requested", got)
		}
		writeFixture(t, w, map[string]any{
			"numFound": 1,
			"docs": []map[string]any{{
				"key":                "/works/OL1168083W",
				"title":              "The Hobbit",
				"author_name":        []string{"J. R. R. Tolkien"},
				"first_publish_year": year,
				"cover_i":            8225269,
			}},
		})
	})

	books, err := client.Search(context.Background(), " hobbit ", 5)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(books) != 1 {
		t.Fatalf("got %d books, want 1", len(books))
	}
	b := books[0]
	if b.ID != "OL1168083W" {
		t.Errorf("ID = %q, want work key OL1168083W", b.ID)
	}
	if b.Title != "The Hobbit" {
		t.Errorf("Title = %q", b.Title)
	}
	if len(b.Authors) != 1 || b.Authors[0] != "J. R. R. Tolkien" {
		t.Errorf("Authors = %v", b.Authors)
	}
	if b.FirstPublishYear == nil || *b.FirstPublishYear != year {
		t.Errorf("FirstPublishYear = %v, want %d", b.FirstPublishYear, year)
	}
	want := "https://covers.openlibrary.org/b/id/8225269-L.jpg"
	if b.CoverURL != want {
		t.Errorf("CoverURL = %q, want %q", b.CoverURL, want)
	}
	if len(b.Raw) == 0 {
		t.Error("Raw not captured")
	}
	if len(f.agents) == 0 || f.agents[0] != openLibraryUserAgent {
		t.Errorf("User-Agent = %v, want the descriptive agent Open Library asks for", f.agents)
	}
}

func TestOpenLibrarySearchEmptyQuery(t *testing.T) {
	_, client := newFakeOpenLibrary(t)
	books, err := client.Search(context.Background(), "   ", 10)
	if err != nil || books != nil {
		t.Errorf("empty query: books = %v, err = %v; want nil, nil", books, err)
	}
}

func TestOpenLibraryGetByWorkKey(t *testing.T) {
	f, client := newFakeOpenLibrary(t)
	// The work record: description in Open Library's object form.
	f.mux.HandleFunc("/works/OL1168083W.json", func(w http.ResponseWriter, r *http.Request) {
		writeFixture(t, w, map[string]any{
			"title":              "The Hobbit",
			"description":        map[string]string{"type": "/type/text", "value": "In a hole in the ground there lived a hobbit."},
			"subjects":           []string{"Fantasy", "Middle Earth"},
			"covers":             []int64{-1, 8225269},
			"first_publish_date": "September 21, 1937",
		})
	})
	// The keyed search that resolves author names.
	f.mux.HandleFunc("/search.json", func(w http.ResponseWriter, r *http.Request) {
		writeFixture(t, w, map[string]any{
			"docs": []map[string]any{{
				"key":         "/works/OL1168083W",
				"author_name": []string{"J. R. R. Tolkien"},
			}},
		})
	})

	b, err := client.GetByWorkKey(context.Background(), "/works/OL1168083W")
	if err != nil {
		t.Fatalf("GetByWorkKey: %v", err)
	}
	if b.ID != "OL1168083W" {
		t.Errorf("ID = %q, want OL1168083W", b.ID)
	}
	if b.Description != "In a hole in the ground there lived a hobbit." {
		t.Errorf("Description = %q", b.Description)
	}
	if len(b.Subjects) != 2 || b.Subjects[0] != "Fantasy" {
		t.Errorf("Subjects = %v", b.Subjects)
	}
	if len(b.Authors) != 1 || b.Authors[0] != "J. R. R. Tolkien" {
		t.Errorf("Authors = %v", b.Authors)
	}
	// The -1 cover id must be skipped in favour of the real one.
	if b.CoverURL != "https://covers.openlibrary.org/b/id/8225269-L.jpg" {
		t.Errorf("CoverURL = %q", b.CoverURL)
	}
	// Author search gave no year; the work's freeform date must supply 1937.
	if b.FirstPublishYear == nil || *b.FirstPublishYear != 1937 {
		t.Errorf("FirstPublishYear = %v, want 1937", b.FirstPublishYear)
	}
}

func TestOpenLibraryGetByWorkKeyNotFound(t *testing.T) {
	f, client := newFakeOpenLibrary(t)
	f.mux.HandleFunc("/works/OL1W.json", func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	})
	if _, err := client.GetByWorkKey(context.Background(), "OL1W"); !errors.Is(err, ErrBookNotFound) {
		t.Errorf("err = %v, want ErrBookNotFound", err)
	}
}

func TestOpenLibraryGetEditions(t *testing.T) {
	pages := 366
	f, client := newFakeOpenLibrary(t)
	f.mux.HandleFunc("/works/OL1168083W/editions.json", func(w http.ResponseWriter, r *http.Request) {
		writeFixture(t, w, map[string]any{
			"entries": []map[string]any{
				{
					"key":             "/books/OL7440402M",
					"title":           "The Hobbit",
					"isbn_13":         []string{"9780261102217"},
					"publishers":      []string{"HarperCollins"},
					"publish_date":    "1997",
					"number_of_pages": pages,
					"physical_format": "Paperback",
					"languages":       []map[string]string{{"key": "/languages/eng"}},
				},
				{
					"key":          "/books/OL26411848M",
					"title":        "The Hobbit",
					"publish_date": "1937",
					"publishers":   []string{"Allen & Unwin"},
				},
			},
		})
	})

	eds, err := client.GetEditions(context.Background(), "/works/OL1168083W")
	if err != nil {
		t.Fatalf("GetEditions: %v", err)
	}
	if len(eds) != 2 {
		t.Fatalf("got %d editions, want 2", len(eds))
	}
	first := eds[0]
	if first.ID != "OL7440402M" {
		t.Errorf("edition ID = %q, want /books/ prefix stripped", first.ID)
	}
	if first.BookID != "OL1168083W" {
		t.Errorf("edition BookID = %q, want the asked-about work", first.BookID)
	}
	if first.ISBN13 != "9780261102217" || first.PageCount == nil || *first.PageCount != pages {
		t.Errorf("edition = %+v", first)
	}
	if first.Language != "eng" {
		t.Errorf("Language = %q, want prefix stripped", first.Language)
	}
	// A sparse entry survives with empty defaults rather than failing the list.
	if eds[1].ID != "OL26411848M" || eds[1].ISBN13 != "" || eds[1].PageCount != nil {
		t.Errorf("sparse edition = %+v", eds[1])
	}
}

func TestOpenLibraryGetEditionsNotFound(t *testing.T) {
	f, client := newFakeOpenLibrary(t)
	f.mux.HandleFunc("/works/OL1W/editions.json", func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	})
	if _, err := client.GetEditions(context.Background(), "OL1W"); !errors.Is(err, ErrBookNotFound) {
		t.Errorf("err = %v, want ErrBookNotFound", err)
	}
}

func TestOpenLibraryGetByISBN(t *testing.T) {
	pages := 366
	f, client := newFakeOpenLibrary(t)
	f.mux.HandleFunc("/isbn/9780261102217.json", func(w http.ResponseWriter, r *http.Request) {
		writeFixture(t, w, map[string]any{
			"key":             "/books/OL7440402M",
			"title":           "The Hobbit",
			"isbn_10":         []string{"0261102213"},
			"isbn_13":         []string{"9780261102217"},
			"publishers":      []string{"HarperCollins", "Allen & Unwin"},
			"publish_date":    "September 1st 1997",
			"number_of_pages": pages,
			"physical_format": "Paperback",
			"languages":       []map[string]string{{"key": "/languages/eng"}},
			"covers":          []int64{8225269},
			"works":           []map[string]string{{"key": "/works/OL1168083W"}},
		})
	})
	f.mux.HandleFunc("/works/OL1168083W.json", func(w http.ResponseWriter, r *http.Request) {
		writeFixture(t, w, map[string]any{"title": "The Hobbit"})
	})
	f.mux.HandleFunc("/search.json", func(w http.ResponseWriter, r *http.Request) {
		writeFixture(t, w, map[string]any{
			"docs": []map[string]any{{
				"key":                "/works/OL1168083W",
				"author_name":        []string{"J. R. R. Tolkien"},
				"first_publish_year": 1937,
			}},
		})
	})

	b, err := client.GetByISBN(context.Background(), "978-0261102217")
	if err != nil {
		t.Fatalf("GetByISBN: %v", err)
	}
	if b.ID != "OL1168083W" {
		t.Errorf("work ID = %q, want OL1168083W", b.ID)
	}
	if b.Edition == nil {
		t.Fatal("Edition not set")
	}
	ed := b.Edition
	if ed.ID != "OL7440402M" {
		t.Errorf("edition ID = %q", ed.ID)
	}
	if ed.BookID != "OL1168083W" {
		t.Errorf("edition BookID = %q", ed.BookID)
	}
	if ed.ISBN13 != "9780261102217" {
		t.Errorf("ISBN13 = %q", ed.ISBN13)
	}
	if ed.ISBN10 != "0261102213" {
		t.Errorf("ISBN10 = %q", ed.ISBN10)
	}
	if ed.Publisher != "HarperCollins" {
		t.Errorf("Publisher = %q, want first of the list", ed.Publisher)
	}
	if ed.PublishedYear == nil || *ed.PublishedYear != 1997 {
		t.Errorf("PublishedYear = %v, want 1997 from %q", ed.PublishedYear, "September 1st 1997")
	}
	if ed.PageCount == nil || *ed.PageCount != pages {
		t.Errorf("PageCount = %v, want %d", ed.PageCount, pages)
	}
	if ed.Binding != "Paperback" {
		t.Errorf("Binding = %q", ed.Binding)
	}
	if ed.Language != "eng" {
		t.Errorf("Language = %q, want prefix stripped", ed.Language)
	}
}

func TestOpenLibraryGetByISBNValidation(t *testing.T) {
	f, client := newFakeOpenLibrary(t)
	f.mux.HandleFunc("/isbn/9780261102217.json", func(w http.ResponseWriter, r *http.Request) {
		writeFixture(t, w, map[string]any{
			"key":   "/books/OL7440402M",
			"title": "The Hobbit",
			"works": []map[string]string{{"key": "/works/OL1168083W"}},
		})
	})
	f.mux.HandleFunc("/works/OL1168083W.json", func(w http.ResponseWriter, r *http.Request) {
		writeFixture(t, w, map[string]any{"title": "The Hobbit"})
	})
	f.mux.HandleFunc("/search.json", func(w http.ResponseWriter, r *http.Request) {
		writeFixture(t, w, map[string]any{"docs": []any{}})
	})

	if _, err := client.GetByISBN(context.Background(), "not-an-isbn"); err == nil {
		t.Error("malformed ISBN accepted")
	}
	// A bad ISBN must fail before any HTTP request is made.
	if len(f.request) != 0 {
		t.Errorf("malformed ISBN issued %d HTTP requests", len(f.request))
	}
	// Separators are normalised away, not rejected.
	b, err := client.GetByISBN(context.Background(), "978 0 2611 0221 7")
	if err != nil {
		t.Fatalf("spaced ISBN rejected: %v", err)
	}
	if b.ID != "OL1168083W" || b.Edition == nil || b.Edition.ID != "OL7440402M" {
		t.Errorf("spaced ISBN resolved to %+v (edition %+v)", b, b.Edition)
	}
}

func TestNormalizeISBN(t *testing.T) {
	cases := map[string]string{
		"978-0-261-10221-7": "9780261102217",
		"0 2611 0221 3":     "0261102213",
		"026110221x":        "026110221X",
	}
	for in, want := range cases {
		if got := NormalizeISBN(in); got != want {
			t.Errorf("NormalizeISBN(%q) = %q, want %q", in, got, want)
		}
	}
	if !ValidISBN("026110221X") || !ValidISBN("9780261102217") {
		t.Error("ValidISBN rejected a well-formed ISBN")
	}
	if ValidISBN("978026110221") || ValidISBN("abcdefghijkl") {
		t.Error("ValidISBN accepted a malformed ISBN")
	}
}

func TestYearFrom(t *testing.T) {
	for in, want := range map[string]int{
		"September 1st 1997": 1997,
		"1937-08-17":         1937,
		"1997":               1997,
		"unknown":            0,
		"published in 89":    0,
	} {
		got := yearFrom(in)
		if want == 0 {
			if got != nil {
				t.Errorf("yearFrom(%q) = %v, want nil", in, got)
			}
			continue
		}
		if got == nil || *got != want {
			t.Errorf("yearFrom(%q) = %v, want %d", in, got, want)
		}
	}
	if got := yearFrom("first printing 2050"); got == nil || *got != 2050 {
		t.Errorf("yearFrom future date = %v, want 2050", got)
	}
}

func containsString(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
