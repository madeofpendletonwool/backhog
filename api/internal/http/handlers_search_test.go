package http

import (
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"testing"

	"github.com/collinpendleton/backhog/api/internal/books/position"
)

// Search inside a book, end to end over a real parsed EPUB. The point of the
// endpoint is not the finding — it is that every hit comes back already
// translated into the audiobook and the printed page — so most of what is
// asserted here is that those views appear when their maps exist and stay null
// when they do not.

// searchPath builds a query against the fixture entry.
func searchPath(entry, q string) string {
	return "/api/books/" + entry + "/search?q=" + url.QueryEscape(q)
}

// results narrows the response's results array, failing the test if it is the
// wrong shape.
func results(t *testing.T, body map[string]any) []map[string]any {
	t.Helper()
	raw, ok := body["results"].([]any)
	if !ok {
		t.Fatalf("results = %#v", body["results"])
	}
	out := make([]map[string]any, 0, len(raw))
	for _, r := range raw {
		m, ok := r.(map[string]any)
		if !ok {
			t.Fatalf("result = %#v", r)
		}
		out = append(out, m)
	}
	return out
}

func snippetOf(t *testing.T, hit map[string]any) (before, passage, after string) {
	t.Helper()
	ctx, ok := hit["context"].(map[string]any)
	if !ok {
		t.Fatalf("context = %#v", hit["context"])
	}
	s := func(k string) string {
		v, ok := ctx[k].(string)
		if !ok {
			t.Fatalf("context.%s = %#v", k, ctx[k])
		}
		return v
	}
	return s("before"), s("passage"), s("after")
}

func TestSearchInBookFindsAPhraseThroughTheFold(t *testing.T) {
	app := newPositionTestApp(t, nil)
	app.charCount(t)

	// The fixture paragraph is `“It’s ﬁne,” he said—truly.`: curly quotes, an
	// apostrophe, an fi ligature and an em dash, none of which the reader
	// types. The canonical fold makes them all the same query.
	for _, q := range []string{
		"its fine he said truly",
		"It's fine, he said truly",
		"ITS FINE HE SAID",
	} {
		status, body := app.api(t, http.MethodGet, searchPath(positionEntry, q), nil)
		if status != http.StatusOK {
			t.Fatalf("%q: status = %d: %v", q, status, body)
		}
		if body["mode"] != "phrase" {
			t.Errorf("%q: mode = %v, want phrase", q, body["mode"])
		}
		hits := results(t, body)
		if len(hits) != 1 {
			t.Fatalf("%q: %d hits, want 1: %v", q, len(hits), hits)
		}
		// The snippet is the book's own characters, not the folded text.
		_, passage, _ := snippetOf(t, hits[0])
		if !strings.Contains(passage, "ﬁne") || !strings.Contains(passage, "It’s") {
			t.Errorf("%q: passage = %q, want the book's own punctuation", q, passage)
		}
	}
}

func TestSearchInBookPlacesEveryHitInThreeSpaces(t *testing.T) {
	app := newPositionTestApp(t, fixedAnchors{
		audio: []position.Anchor{
			{CharOffset: 0, Value: 0, Confidence: 0.9},
			{CharOffset: 40, Value: 60, Confidence: 0.9},
		},
		pages: []position.Anchor{
			{CharOffset: 0, Value: 1, Confidence: 1},
			{CharOffset: 40, Value: 12, Confidence: 1},
		},
	})
	app.charCount(t)

	status, body := app.api(t, http.MethodGet, searchPath(positionEntry, "he said"), nil)
	if status != http.StatusOK {
		t.Fatalf("status = %d: %v", status, body)
	}
	hits := results(t, body)
	if len(hits) == 0 {
		t.Fatal("no hits")
	}
	hit := hits[0]

	chapter, ok := hit["chapter"].(map[string]any)
	if !ok || chapter["title"] != "Alpha" {
		t.Errorf("chapter = %#v, want Alpha", hit["chapter"])
	}
	if p, ok := hit["percent"].(float64); !ok || p <= 0 || p > 100 {
		t.Errorf("percent = %#v", hit["percent"])
	}

	audio, ok := hit["audio"].(map[string]any)
	if !ok {
		t.Fatalf("audio = %#v, want a derived timestamp", hit["audio"])
	}
	if audio["derived"] != true {
		t.Errorf("audio.derived = %v, want true", audio["derived"])
	}
	if seconds, ok := audio["seconds"].(float64); !ok || seconds <= 0 {
		t.Errorf("audio.seconds = %#v, want a real position on the tape", audio["seconds"])
	}
	if audio["track_number"] == nil {
		t.Error("audio view did not resolve a track")
	}

	page, ok := hit["page"].(map[string]any)
	if !ok {
		t.Fatalf("page = %#v, want a derived page", hit["page"])
	}
	if p, ok := page["page"].(float64); !ok || p < 1 {
		t.Errorf("page.page = %#v", page["page"])
	}
	// Two anchors bound the map, so the answer carries a real error bar
	// rather than the null that means "no idea how fast pages go by".
	if _, ok := page["margin"].(float64); !ok {
		t.Errorf("page.margin = %#v, want a bar from the two anchors", page["margin"])
	}
}

func TestSearchInBookInventsNoCoordinatesItCannotDerive(t *testing.T) {
	// An EPUB and an audiobook, but no alignment and no page map.
	app := newPositionTestApp(t, nil)
	app.charCount(t)

	status, body := app.api(t, http.MethodGet, searchPath(positionEntry, "he said"), nil)
	if status != http.StatusOK {
		t.Fatalf("status = %d: %v", status, body)
	}
	if body["alignment"] != nil {
		t.Errorf("alignment = %v, want null with no alignment", body["alignment"])
	}
	for _, hit := range results(t, body) {
		if hit["audio"] != nil {
			t.Errorf("audio = %v, want null with no alignment", hit["audio"])
		}
		if hit["page"] != nil {
			t.Errorf("page = %v, want null with no page map", hit["page"])
		}
		if hit["chapter"] == nil {
			t.Error("chapter is null, but the text was parsed")
		}
	}
}

func TestSearchInBookFallsBackToLoose(t *testing.T) {
	app := newPositionTestApp(t, nil)
	app.charCount(t)

	// "he said plainly" is not in the book; "he said" is. Nothing matches the
	// phrase, so the loose pass answers and says so.
	status, body := app.api(t, http.MethodGet, searchPath(positionEntry, "he said plainly"), nil)
	if status != http.StatusOK {
		t.Fatalf("status = %d: %v", status, body)
	}
	if body["mode"] != "loose" {
		t.Fatalf("mode = %v, want loose", body["mode"])
	}
	hits := results(t, body)
	if len(hits) == 0 {
		t.Fatal("loose pass returned nothing")
	}
	if _, passage, _ := snippetOf(t, hits[0]); !strings.Contains(passage, "said") {
		t.Errorf("loose passage = %q, want the 'he said' passage", passage)
	}
}

func TestSearchInBookNoMatchIsAnEmptyList(t *testing.T) {
	app := newPositionTestApp(t, nil)
	app.charCount(t)

	status, body := app.api(t, http.MethodGet,
		searchPath(positionEntry, "quantum chromodynamics"), nil)
	if status != http.StatusOK {
		t.Fatalf("status = %d: %v", status, body)
	}
	if body["total"] != 0.0 {
		t.Errorf("total = %v, want 0", body["total"])
	}
	if hits := results(t, body); len(hits) != 0 {
		t.Errorf("results = %v, want an empty list rather than null", hits)
	}
}

func TestSearchInBookLimitAndTotal(t *testing.T) {
	app := newPositionTestApp(t, nil)
	app.charCount(t)

	// "the" occurs more than once; one hit is asked for and the real count is
	// still reported, because "showing 1 of 4" is the useful answer.
	status, body := app.api(t, http.MethodGet, searchPath(positionEntry, "the")+"&limit=1", nil)
	if status != http.StatusOK {
		t.Fatalf("status = %d: %v", status, body)
	}
	total, _ := body["total"].(float64)
	hits := results(t, body)
	if len(hits) != 1 {
		t.Errorf("results = %d, want the limit of 1", len(hits))
	}
	if total < 1 {
		t.Errorf("total = %v, want the real count", body["total"])
	}
	if body["truncated"] != (total > 1) {
		t.Errorf("truncated = %v with total %v", body["truncated"], total)
	}
}

func TestSearchInBookRejectsBadRequests(t *testing.T) {
	app := newPositionTestApp(t, nil)
	app.charCount(t)

	for _, tc := range []struct {
		name string
		path string
		want int
	}{
		{"empty query", searchPath(positionEntry, ""), http.StatusUnprocessableEntity},
		{"punctuation only", searchPath(positionEntry, "“…”"), http.StatusUnprocessableEntity},
		{"limit zero", searchPath(positionEntry, "said") + "&limit=0", http.StatusBadRequest},
		{"limit too large", searchPath(positionEntry, "said") + "&limit=500", http.StatusBadRequest},
		{"limit garbage", searchPath(positionEntry, "said") + "&limit=lots", http.StatusBadRequest},
		{"unknown entry", searchPath("nope", "said"), http.StatusNotFound},
		{"book with no ebook", searchPath(positionAudioOnly, "said"), http.StatusNotFound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if status, body := app.api(t, http.MethodGet, tc.path, nil); status != tc.want {
				t.Errorf("status = %d, want %d: %v", status, tc.want, body)
			}
		})
	}
}

func TestSearchInBookIsScopedToTheCaller(t *testing.T) {
	app := newPositionTestApp(t, nil)
	app.charCount(t)

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookie jar: %v", err)
	}
	stranger := &http.Client{Jar: jar, Timeout: app.client.Timeout}
	register(t, app.ts.URL, stranger, "other@example.com", "other", "hogwash123")

	if status, body := app.do(t, stranger, http.MethodGet, searchPath(positionEntry, "said"), nil); status != http.StatusNotFound {
		t.Errorf("another user's book = %d, want 404: %v", status, body)
	}

	anon := &http.Client{Timeout: app.client.Timeout}
	if status, _ := app.do(t, anon, http.MethodGet, searchPath(positionEntry, "said"), nil); status != http.StatusUnauthorized {
		t.Errorf("unauthenticated = %d, want 401", status)
	}
}

// TestSearchInBookViewsAreCachedNotShared guards the one thing the search
// TTL cache could get wrong: two users searching the same entry id must never
// see each other's page map.
func TestSearchInBookViewsAreCachedNotShared(t *testing.T) {
	app := newPositionTestApp(t, fixedAnchors{
		pages: []position.Anchor{
			{CharOffset: 0, Value: 1, Confidence: 1},
			{CharOffset: 40, Value: 12, Confidence: 1},
		},
	})
	app.charCount(t)

	status, body := app.api(t, http.MethodGet, searchPath(positionEntry, "he said"), nil)
	if status != http.StatusOK {
		t.Fatalf("status = %d: %v", status, body)
	}
	first := results(t, body)
	if len(first) == 0 || first[0]["page"] == nil {
		t.Fatalf("owner got no page view: %v", first)
	}

	// Same query again, this time served from the cache: identical answer.
	_, again := app.api(t, http.MethodGet, searchPath(positionEntry, "he said"), nil)
	a, _ := json.Marshal(results(t, again))
	b, _ := json.Marshal(first)
	if string(a) != string(b) {
		t.Errorf("cached search differs:\n got %s\nwant %s", a, b)
	}
}
