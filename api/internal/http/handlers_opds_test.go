package http

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The OPDS catalog (MAD-830), read the way a device reads it: Basic auth,
// Atom XML, absolute links followed as given.

type feedXML struct {
	Title   string     `xml:"title"`
	Links   []linkXML  `xml:"link"`
	Entries []entryXML `xml:"entry"`
}

type linkXML struct {
	Rel    string `xml:"rel,attr"`
	Href   string `xml:"href,attr"`
	Type   string `xml:"type,attr"`
	Length int64  `xml:"length,attr"`
}

type entryXML struct {
	Title  string    `xml:"title"`
	ID     string    `xml:"id"`
	Author []string  `xml:"author>name"`
	Links  []linkXML `xml:"link"`
}

func (f feedXML) link(rel string) (linkXML, bool) {
	for _, l := range f.Links {
		if l.Rel == rel {
			return l, true
		}
	}
	return linkXML{}, false
}

func (e entryXML) link(rel string) (linkXML, bool) {
	for _, l := range e.Links {
		if l.Rel == rel {
			return l, true
		}
	}
	return linkXML{}, false
}

// feed fetches one catalog URL (absolute, as a device follows it, or a
// path) with Basic auth and parses it.
func (a *epubTestApp) feed(t *testing.T, device, href string) feedXML {
	t.Helper()
	path := strings.TrimPrefix(href, a.ts.URL)
	resp, body := a.deviceRequest(t, http.MethodGet, path, basic("reader", device), nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s = %d (%s)", path, resp.StatusCode, body)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/atom+xml;profile=opds-catalog") {
		t.Errorf("%s: Content-Type %q", path, ct)
	}
	if resp.ContentLength != int64(len(body)) {
		t.Errorf("%s: Content-Length %d for %d bytes", path, resp.ContentLength, len(body))
	}
	// CrossPoint parses every byte on an ESP32.
	if len(body) > 32<<10 {
		t.Errorf("%s: %d bytes, over the 32 KiB page budget", path, len(body))
	}
	var f feedXML
	if err := xml.Unmarshal(body, &f); err != nil {
		t.Fatalf("%s: not XML: %v", path, err)
	}
	// Every feed carries the inline search template CrossPoint looks for.
	if s, ok := f.link("search"); !ok || !strings.Contains(s.Href, "{searchTerms}") || !strings.HasPrefix(s.Href, "http") {
		t.Errorf("%s: search link %+v", path, s)
	}
	return f
}

// shelveBooks seeds n more readable books (each its own EPUB file) and one
// paper-only book, beside the fixture entry.
func (a *epubTestApp) shelveBooks(t *testing.T, n int, data []byte) {
	t.Helper()
	db := a.store.DB()
	for i := 0; i < n; i++ {
		book, entry, file := fmt.Sprintf("OLX%02dW", i), fmt.Sprintf("x%02d", i), fmt.Sprintf("x%02d.epub", i)
		if err := os.WriteFile(filepath.Join(a.root, file), data, 0o644); err != nil {
			t.Fatal(err)
		}
		for _, q := range []struct {
			sql  string
			args []any
		}{
			{`INSERT INTO books (id, title, authors_json) VALUES (?, ?, '["Ann Author"]')`,
				[]any{book, fmt.Sprintf("Shelf Book %02d", i)}},
			{`INSERT INTO library_entries (id, user_id, media_type, book_id, status) VALUES (?, ?, 'book', ?, 'backlog')`,
				[]any{entry, a.userID, book}},
			{`INSERT INTO media_files (root, path, kind, size_bytes, mtime, book_id, scanned_at) VALUES (?, ?, 'epub', ?, ?, ?, ?)`,
				[]any{a.root, file, len(data), time.Now().UnixNano(), book, time.Now().UTC()}},
		} {
			if _, err := db.Exec(q.sql, q.args...); err != nil {
				t.Fatalf("seed: %v", err)
			}
		}
	}
	for _, q := range []string{
		`INSERT INTO books (id, title) VALUES ('OLPAPERW', 'Paper Only')`,
		`INSERT INTO library_entries (id, user_id, media_type, book_id, status) VALUES ('paper', '` + a.userID + `', 'book', 'OLPAPERW', 'backlog')`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
}

func TestOPDSRootAndShelves(t *testing.T) {
	app := newEpubTestApp(t)
	fixture := apiEpubFixture(t)
	app.attachEpub(t, fixture)
	device := app.mintAs(t, app.client, map[string]any{"name": "kindle", "device": true})

	// The catalog root answers with and without the trailing slash.
	for _, root := range []string{"/api/opds", "/api/opds/"} {
		f := app.feed(t, device, root)
		if len(f.Entries) != 6 {
			t.Fatalf("%s: %d entries", root, len(f.Entries))
		}
	}
	f := app.feed(t, device, "/api/opds")
	shelves := map[string]string{}
	for _, e := range f.Entries {
		l, ok := e.link("subsection")
		if !ok || !strings.HasPrefix(l.Href, app.ts.URL+"/api/opds/") || !strings.Contains(l.Type, "profile=opds-catalog") {
			t.Errorf("root entry %q: link %+v", e.Title, l)
		}
		shelves[e.Title] = l.Href
	}

	// Every acquisition shelf lists the book (it is reading, so it is on
	// Reading now as soon as it has a position; All books always).
	all := app.feed(t, device, shelves["All books"])
	if len(all.Entries) != 1 || all.Entries[0].Title != "Fixture Book" {
		t.Fatalf("all books = %+v", all.Entries)
	}
	acq, ok := all.Entries[0].link("http://opds-spec.org/acquisition")
	if !ok || acq.Type != "application/epub+zip" || !strings.Contains(acq.Href, "/epub/") ||
		!strings.HasSuffix(acq.Href, ".epub") || acq.Length != int64(len(fixture)) {
		t.Fatalf("acquisition link %+v", acq)
	}
	if _, ok := all.Entries[0].link("http://opds-spec.org/image/thumbnail"); !ok {
		t.Error("no thumbnail link for the clients that draw covers")
	}

	// The acquisition link, followed exactly as given, is the original file.
	resp, body := app.deviceRequest(t, http.MethodGet, strings.TrimPrefix(acq.Href, app.ts.URL), basic("reader", device), nil)
	if resp.StatusCode != http.StatusOK || !bytes.Equal(body, fixture) {
		t.Errorf("following the acquisition link = %d, %d bytes", resp.StatusCode, len(body))
	}

	for _, shelf := range []string{"Recently added", "Queue", "Reading now"} {
		app.feed(t, device, shelves[shelf])
	}
	if recent := app.feed(t, device, shelves["Recently added"]); len(recent.Entries) != 1 {
		t.Errorf("recently added = %d entries", len(recent.Entries))
	}
}

func TestOPDSPaginatesAndSkipsUnreadable(t *testing.T) {
	app := newEpubTestApp(t)
	fixture := apiEpubFixture(t)
	app.attachEpub(t, fixture)
	app.shelveBooks(t, 30, fixture)
	device := app.mintAs(t, app.client, map[string]any{"name": "kindle", "device": true})

	seen := map[string]bool{}
	href := "/api/opds/all"
	pages := 0
	for href != "" {
		f := app.feed(t, device, href)
		pages++
		if len(f.Entries) > 25 {
			t.Errorf("page %d holds %d entries", pages, len(f.Entries))
		}
		for _, e := range f.Entries {
			if seen[e.ID] {
				t.Errorf("%s listed twice", e.ID)
			}
			seen[e.ID] = true
			if e.Title == "Paper Only" {
				t.Error("a book with no EPUB is in the catalog")
			}
		}
		href = ""
		if next, ok := f.link("next"); ok {
			if !strings.Contains(next.Type, "atom+xml") {
				t.Errorf("next link type %q (KOReader only follows atom-typed links)", next.Type)
			}
			href = next.Href
		}
		if pages > 1 {
			if _, ok := f.link("previous"); !ok {
				t.Errorf("page %d has no previous link", pages)
			}
		}
	}
	if pages != 2 || len(seen) != 31 {
		t.Errorf("%d pages, %d books; want 2 pages of 31", pages, len(seen))
	}
}

func TestOPDSSearchListsAndSeries(t *testing.T) {
	app := newEpubTestApp(t)
	fixture := apiEpubFixture(t)
	app.attachEpub(t, fixture)
	app.shelveBooks(t, 3, fixture)
	device := app.mintAs(t, app.client, map[string]any{"name": "kindle", "device": true})
	db := app.store.DB()

	// Search: the template, filled in the way a device fills it.
	if f := app.feed(t, device, "/api/opds/search?q=shelf+book+01"); len(f.Entries) != 1 || f.Entries[0].Title != "Shelf Book 01" {
		t.Errorf("search = %+v", f.Entries)
	}
	if f := app.feed(t, device, "/api/opds/search?q=ann+author"); len(f.Entries) != 3 {
		t.Errorf("author search = %d entries", len(f.Entries))
	}
	if f := app.feed(t, device, "/api/opds/search?q="); len(f.Entries) != 0 {
		t.Errorf("empty search = %d entries", len(f.Entries))
	}
	// ISBN, typed with hyphens the way it is printed on a cover.
	if _, err := db.Exec(`INSERT INTO book_editions (id, book_id, isbn13) VALUES ('OLX02M', 'OLX02W', '9780061020698')`); err != nil {
		t.Fatal(err)
	}
	if f := app.feed(t, device, "/api/opds/search?q=978-0-06-102069-8"); len(f.Entries) != 1 || f.Entries[0].Title != "Shelf Book 02" {
		t.Errorf("ISBN search = %+v", f.Entries)
	}

	// A list: navigation feed, then its books in list order.
	for _, q := range []string{
		`INSERT INTO lists (id, user_id, name, description, kind) VALUES ('L1', '` + app.userID + `', 'Beach reads', 'Sun & sand', 'manual')`,
		`INSERT INTO list_items (list_id, entry_id, position) VALUES ('L1', 'x02', 1), ('L1', 'paper', 2), ('L1', 'x00', 3)`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("seed list: %v", err)
		}
	}
	lists := app.feed(t, device, "/api/opds/lists")
	if len(lists.Entries) != 1 || lists.Entries[0].Title != "Beach reads" {
		t.Fatalf("lists = %+v", lists.Entries)
	}
	l, _ := lists.Entries[0].link("subsection")
	list := app.feed(t, device, l.Href)
	if len(list.Entries) != 2 || list.Entries[0].Title != "Shelf Book 02" || list.Entries[1].Title != "Shelf Book 00" {
		t.Errorf("list books = %+v", list.Entries)
	}

	// A series with a slash in its name, in reading order.
	for _, q := range []string{
		`UPDATE books SET series_name = 'Light/Dark', series_number = 2 WHERE id = 'OLX00W'`,
		`UPDATE books SET series_name = 'Light/Dark', series_number = 1 WHERE id = 'OLX01W'`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	series := app.feed(t, device, "/api/opds/series")
	if len(series.Entries) != 1 || series.Entries[0].Title != "Light/Dark" {
		t.Fatalf("series = %+v", series.Entries)
	}
	sl, _ := series.Entries[0].link("subsection")
	books := app.feed(t, device, sl.Href)
	if len(books.Entries) != 2 || books.Entries[0].Title != "Shelf Book 01" {
		t.Errorf("series books = %+v", books.Entries)
	}
	// Searching the series name finds its books, in reading order.
	if f := app.feed(t, device, "/api/opds/search?q=light%2Fdark"); len(f.Entries) != 2 || f.Entries[0].Title != "Shelf Book 01" {
		t.Errorf("series search = %+v", f.Entries)
	}
}

func TestOPDSRespectsBudgetsAndOwners(t *testing.T) {
	app := newEpubTestApp(t)
	fixture := apiEpubFixture(t)
	app.attachEpub(t, fixture)
	long := strings.Repeat("A Very Long Title & <Markup> ", 12)
	authors := `["` + strings.Repeat("Author Name, ", 15) + `"]`
	if _, err := app.store.DB().Exec(`UPDATE books SET title = ?, authors_json = ? WHERE id = 'OL1W'`, long, authors); err != nil {
		t.Fatal(err)
	}
	device := app.mintAs(t, app.client, map[string]any{"name": "kindle", "device": true})

	f := app.feed(t, device, "/api/opds/all")
	if len(f.Entries) != 1 {
		t.Fatalf("entries = %d", len(f.Entries))
	}
	e := f.Entries[0]
	if len(e.Title) > 160 || !strings.HasSuffix(e.Title, "…") || !strings.Contains(e.Title, "& <Markup>") {
		t.Errorf("title %q (%d bytes)", e.Title, len(e.Title))
	}
	if len(e.Author) != 1 || len(e.Author[0]) > 120 {
		t.Errorf("author %q", e.Author)
	}
	if len(e.ID) > 128 {
		t.Errorf("id %d bytes", len(e.ID))
	}
	for _, l := range e.Links {
		if len(l.Href) > 768 {
			t.Errorf("href %d bytes", len(l.Href))
		}
	}

	// A stranger's catalog is empty, and anonymous gets the Basic challenge.
	jar, _ := cookiejar.New(nil)
	other := &http.Client{Jar: jar, Timeout: 10 * time.Second}
	register(t, app.ts.URL, other, "snoop@example.com", "snoop", "hogwash123")
	stranger := app.mintAs(t, other, map[string]any{"name": "theirs", "device": true})
	resp, body := app.deviceRequest(t, http.MethodGet, "/api/opds/all", basic("snoop", stranger), nil)
	if resp.StatusCode != http.StatusOK || bytes.Contains(body, []byte("urn:backhog:entry:")) {
		t.Errorf("stranger's catalog = %d, %s", resp.StatusCode, body)
	}
	resp, _ = app.deviceRequest(t, http.MethodGet, "/api/opds", nil, nil)
	if resp.StatusCode != http.StatusUnauthorized || !strings.HasPrefix(resp.Header.Get("WWW-Authenticate"), "Basic") {
		t.Errorf("anonymous root = %d %q", resp.StatusCode, resp.Header.Get("WWW-Authenticate"))
	}
}
