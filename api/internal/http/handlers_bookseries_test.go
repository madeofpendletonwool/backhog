package http

import (
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"testing"
	"time"
)

// seedSeriesLibrary plants a three-book "Mistborn" shelf: book one finished,
// book two mid-read at a known offset, book three waiting — plus a lone
// book outside any series. Returns the ids the assertions talk about.
func seedSeriesLibrary(t *testing.T, app *epubTestApp) {
	t.Helper()
	seed := []string{
		`INSERT INTO books (id, title, authors_json, first_publish_year, series_name, series_number)
			VALUES ('OLS1', 'The Final Empire', '["B. Sanderson"]', 2006, 'Mistborn', 1.0)`,
		`INSERT INTO books (id, title, authors_json, first_publish_year, series_name, series_number)
			VALUES ('OLS2', 'The Well of Ascension', '["B. Sanderson"]', 2007, 'Mistborn', 2.5)`,
		`INSERT INTO books (id, title, authors_json, first_publish_year, series_name)
			VALUES ('OLS3', 'The Hero of Ages', '["B. Sanderson"]', 2008, 'Mistborn')`,
		`INSERT INTO books (id, title, authors_json)
			VALUES ('OLLONE', 'Elantris', '["B. Sanderson"]')`,
		`INSERT INTO library_entries (id, user_id, media_type, book_id, status)
			VALUES ('se1', '` + app.userID + `', 'book', 'OLS1', 'played')`,
		`INSERT INTO library_entries (id, user_id, media_type, book_id, status)
			VALUES ('se2', '` + app.userID + `', 'book', 'OLS2', 'playing')`,
		`INSERT INTO library_entries (id, user_id, media_type, book_id, status)
			VALUES ('se3', '` + app.userID + `', 'book', 'OLS3', 'backlog')`,
		`INSERT INTO library_entries (id, user_id, media_type, book_id, status)
			VALUES ('se4', '` + app.userID + `', 'book', 'OLLONE', 'backlog')`,
		`INSERT INTO book_progress (entry_id, char_offset, char_offset_source,
			percent_complete, position_mode)
			VALUES ('se2', 4200, 'read', 31.5, 'text')`,
	}
	for _, q := range seed {
		if _, err := app.store.DB().Exec(q); err != nil {
			t.Fatalf("seed %q: %v", q, err)
		}
	}
}

func TestBookSeriesIndex(t *testing.T) {
	app := newEpubTestApp(t)
	seedSeriesLibrary(t, app)

	status, body := app.get(t, "/api/books/series")
	if status != http.StatusOK {
		t.Fatalf("index status = %d: %v", status, body)
	}
	series, _ := body["series"].([]any)
	if len(series) != 1 {
		t.Fatalf("series = %v, want exactly Mistborn", body)
	}
	row, _ := series[0].(map[string]any)
	if row["name"] != "Mistborn" || row["books"] != float64(3) ||
		row["finished"] != float64(1) || row["reading"] != float64(1) {
		t.Fatalf("row = %v", row)
	}
}

func TestBookSeriesDetailOrderAndBounds(t *testing.T) {
	app := newEpubTestApp(t)
	seedSeriesLibrary(t, app)

	status, body := app.get(t, "/api/books/series/Mistborn")
	if status != http.StatusOK {
		t.Fatalf("detail status = %d: %v", status, body)
	}
	if body["name"] != "Mistborn" {
		t.Fatalf("name = %v", body["name"])
	}
	books, _ := body["books"].([]any)
	if len(books) != 3 {
		t.Fatalf("books = %v", books)
	}

	// Reading order: rank first, unknown ranks last.
	first, _ := books[0].(map[string]any)
	second, _ := books[1].(map[string]any)
	third, _ := books[2].(map[string]any)
	if first["entry_id"] != "se1" || second["entry_id"] != "se2" || third["entry_id"] != "se3" {
		t.Fatalf("order = %v, %v, %v", first["entry_id"], second["entry_id"], third["entry_id"])
	}
	if first["series_number"] != float64(1) || second["series_number"] != 2.5 {
		t.Fatalf("ranks = %v, %v", first["series_number"], second["series_number"])
	}
	if _, present := third["series_number"]; present {
		t.Fatalf("unknown rank came back as %v", third["series_number"])
	}

	// Per-book scope facts: the finished book is finished, the mid-read
	// book carries its position and a peek link that never moves it.
	if first["finished"] != true || first["status"] != "played" {
		t.Fatalf("first = %v", first)
	}
	pos, _ := second["position"].(map[string]any)
	if pos["char_offset"] != float64(4200) || pos["percent"] != 31.5 || pos["position_mode"] != "text" {
		t.Fatalf("second position = %v", pos)
	}
	if link, _ := second["deep_link"].(string); link != "/books/se2/read?offset=4200&peek=1" {
		t.Fatalf("second deep_link = %q", link)
	}
	if link, _ := first["deep_link"].(string); link != "/books/se1/read" {
		t.Fatalf("finished book deep_link = %q, want the plain reader", link)
	}

	// The name matches case-insensitively — identity is the name.
	if status, _ := app.get(t, "/api/books/series/mistborn"); status != http.StatusOK {
		t.Fatalf("lowercased name status = %d", status)
	}
}

func TestBookSeriesScopedToTheCaller(t *testing.T) {
	app := newEpubTestApp(t)
	seedSeriesLibrary(t, app)

	// A second account holds none of the series: its index is empty and
	// the detail is a 404, indistinguishable from a series that does not
	// exist.
	stranger := newUserSession(t, app)
	if status, body := getAs(t, stranger, app.ts.URL+"/api/books/series"); status != http.StatusOK {
		t.Fatalf("stranger index status = %d: %v", status, body)
	} else if series, _ := body["series"].([]any); len(series) != 0 {
		t.Fatalf("stranger index = %v", series)
	}
	if status, _ := getAs(t, stranger, app.ts.URL+"/api/books/series/Mistborn"); status != http.StatusNotFound {
		t.Fatalf("stranger detail status = %d", status)
	}

	// Anonymous sees only the door.
	resp, err := http.Get(app.ts.URL + "/api/books/series")
	if err != nil {
		t.Fatalf("anon get: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anon status = %d", resp.StatusCode)
	}

	// Unknown series for the owner is the same 404.
	if status, _ := app.get(t, "/api/books/series/Nope"); status != http.StatusNotFound {
		t.Fatalf("unknown series status = %d", status)
	}
}

func TestBookSeriesReadableWithAToken(t *testing.T) {
	app := newEpubTestApp(t)
	seedSeriesLibrary(t, app)

	secret := mintToken(t, app.ts.URL, app.client)
	req, err := http.NewRequest(http.MethodGet, app.ts.URL+"/api/books/series/Mistborn", nil)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+secret)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("token get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("token detail status = %d", resp.StatusCode)
	}
	var out struct {
		Books []struct {
			EntryID string `json:"entry_id"`
		} `json:"books"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(out.Books) != 3 || out.Books[0].EntryID != "se1" {
		t.Fatalf("token detail = %+v", out)
	}
}

// newUserSession registers a second account and returns its cookie-bearing
// client.
func newUserSession(t *testing.T, app *epubTestApp) *http.Client {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("jar: %v", err)
	}
	client := &http.Client{Jar: jar, Timeout: 10 * time.Second}
	register(t, app.ts.URL, client, "stranger@example.com", "stranger", "hogwash123")
	return client
}

// getAs is app.get for a caller that is not the app's own session.
func getAs(t *testing.T, client *http.Client, url string) (int, map[string]any) {
	t.Helper()
	resp, err := client.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer resp.Body.Close()
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("GET %s: decode: %v", url, err)
	}
	return resp.StatusCode, body
}

// The store layer's ordering rule under a tie: books whose rank nobody
// recorded fall back to publish year, then title — and the first-evidence
// write only ever fills an unnamed book.
func TestBookSeriesOrderFallsBackToYear(t *testing.T) {
	app := newEpubTestApp(t)
	seed := []string{
		`INSERT INTO books (id, title, first_publish_year, series_name)
			VALUES ('OLT1', 'Zebra Book', 2001, 'Ties')`,
		`INSERT INTO books (id, title, first_publish_year, series_name)
			VALUES ('OLT2', 'Aardvark Book', 1999, 'Ties')`,
		`INSERT INTO books (id, title, first_publish_year)
			VALUES ('OLT3', 'Middle Book', 1999)`,
		`INSERT INTO library_entries (id, user_id, media_type, book_id, status)
			VALUES ('te1', '` + app.userID + `', 'book', 'OLT1', 'backlog')`,
		`INSERT INTO library_entries (id, user_id, media_type, book_id, status)
			VALUES ('te2', '` + app.userID + `', 'book', 'OLT2', 'backlog')`,
		`INSERT INTO library_entries (id, user_id, media_type, book_id, status)
			VALUES ('te3', '` + app.userID + `', 'book', 'OLT3', 'backlog')`,
	}
	for _, q := range seed {
		if _, err := app.store.DB().Exec(q); err != nil {
			t.Fatalf("seed %q: %v", q, err)
		}
	}

	// Ties: same (missing) rank, same year — the title decides.
	books, err := app.store.BookSeriesDetail(t.Context(), app.userID, "Ties")
	if err != nil {
		t.Fatalf("detail: %v", err)
	}
	if len(books) != 2 || books[0].EntryID != "te2" || books[1].EntryID != "te1" {
		t.Fatalf("tie order = %+v", books)
	}

	// The first-evidence write names the unnamed book and nothing else.
	wrote, err := app.store.SetBookSeriesIfEmpty(t.Context(), "OLT3", "Ties", nil)
	if err != nil || !wrote {
		t.Fatalf("set unnamed: wrote=%v err=%v", wrote, err)
	}
	if wrote, err := app.store.SetBookSeriesIfEmpty(t.Context(), "OLT2", "Other Saga", nil); err != nil || wrote {
		t.Fatalf("second evidence must not win: wrote=%v err=%v", wrote, err)
	}
	books, _ = app.store.BookSeriesDetail(t.Context(), app.userID, "Ties")
	if len(books) != 3 || books[0].EntryID != "te2" || books[1].EntryID != "te3" || books[2].EntryID != "te1" {
		t.Fatalf("order after naming = %s, %s, %s", books[0].EntryID, books[1].EntryID, books[2].EntryID)
	}
}
