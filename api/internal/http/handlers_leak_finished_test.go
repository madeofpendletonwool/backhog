package http

import (
	"net/http"
	"strings"
	"testing"
)

// The clamp's other half (MAD-469): a book the reader finished is fully
// read. `until=position` resolves to the whole book for a played entry —
// whatever spot the last session stopped on — because the series memory
// spans finished books whole. A book still being read keeps every withheld
// byte the leak bar demands.
func TestFinishedBookReadsWholeUnderPositionDefault(t *testing.T) {
	a := newLeakApp(t)
	geom := leakGeometryOf(t, a)

	// Park the reader mid-Alpha, comfortably before the reveal.
	a.setLeakPosition(t, geom.alpha+50)

	// While reading: the token's defaulted read stops at the position and
	// the reveal stays out of the answer.
	resp, body := asLeakToken(t, a.ts.URL, a.secret, http.MethodGet,
		"/api/books/"+epubFixtureEntry+"/text?from=0", nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("reading text status = %d: %v", resp.StatusCode, body)
	}
	if text := body["text"].(string); strings.Contains(strings.ToLower(text), leakReveal) {
		t.Fatalf("reveal served to a mid-read book")
	}

	// The search agrees: nothing past the position is a hit.
	resp, sbody := asLeakToken(t, a.ts.URL, a.secret, http.MethodGet,
		"/api/books/"+epubFixtureEntry+"/search?q=butler", nil)
	resp.Body.Close()
	if sbody["total"] != float64(0) {
		t.Fatalf("mid-read search total = %v, want 0", sbody["total"])
	}

	// Finish the book, leaving the stored position exactly where it was.
	if _, err := a.store.DB().Exec(
		`UPDATE library_entries SET status = 'played' WHERE id = ?`, epubFixtureEntry); err != nil {
		t.Fatalf("finish entry: %v", err)
	}

	// The defaulted read now serves the whole book — the reader has been
	// through it, so the reveal is not a spoiler the clamp owes anyone.
	resp, body = asLeakToken(t, a.ts.URL, a.secret, http.MethodGet,
		"/api/books/"+epubFixtureEntry+"/text?from=0", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("finished text status = %d: %v", resp.StatusCode, body)
	}
	if text, _ := body["text"].(string); !strings.Contains(strings.ToLower(text), leakReveal) {
		t.Fatalf("finished book did not serve the whole text")
	}
	bound, _ := body["bound"].(map[string]any)
	if bound["until"] != "position" || bound["char_offset"] != float64(geom.charCount) {
		t.Fatalf("finished bound = %v, want position at the full count", bound)
	}

	// Chapters answer unlocked to the end, and search finds the reveal —
	// the same facts, on the paths that render them.
	for _, ch := range tokenChapters(t, a, "") {
		if locked, _ := ch["locked"].(bool); locked {
			t.Fatalf("finished book still locks chapters: %v", ch)
		}
	}
	resp, sbody = asLeakToken(t, a.ts.URL, a.secret, http.MethodGet,
		"/api/books/"+epubFixtureEntry+"/search?q=butler", nil)
	resp.Body.Close()
	if total, _ := sbody["total"].(float64); total < 1 {
		t.Fatalf("finished search total = %v, want the reveal findable", sbody["total"])
	}

	// A dropped book is not a finished one: the position stands, and the
	// reveal goes back to being withheld.
	if _, err := a.store.DB().Exec(
		`UPDATE library_entries SET status = 'dropped' WHERE id = ?`, epubFixtureEntry); err != nil {
		t.Fatalf("drop entry: %v", err)
	}
	resp, body = asLeakToken(t, a.ts.URL, a.secret, http.MethodGet,
		"/api/books/"+epubFixtureEntry+"/text?from=0", nil)
	resp.Body.Close()
	if text, _ := body["text"].(string); strings.Contains(strings.ToLower(text), leakReveal) {
		t.Fatalf("dropped book served the reveal")
	}
}
