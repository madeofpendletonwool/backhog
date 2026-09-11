package search

import (
	"context"
	"errors"
	"testing"

	"github.com/collinpendleton/backhog/api/booktext"
)

// pages is a lettering corpus the way the worker writes one: raw
// tesseract-style readings, punctuation and case intact, one entry per
// page. A comic: independent balloons, no scroll.
var pages = []PageText{
	{Number: 1, Text: "WE WERE LEAN AND HUNGRY!\nTHERE'S NOTHING WRONG WITH THAT!"},
	{Number: 2, Text: "the door slammed behind them."},
	{Number: 3, Text: "\"You're not going anywhere,\" he said—finally."},
	{Number: 4, Text: ""},
}

func newPaged(t *testing.T, texts []PageText, revisions map[string]string) (*PagedSearcher, *[]PageText) {
	t.Helper()
	loaded := append([]PageText(nil), texts...)
	calls := 0
	s := NewPaged(func(_ context.Context, mediaFileID int64) ([]PageText, error) {
		calls++
		if mediaFileID != 7 {
			return nil, errors.New("no such file")
		}
		return loaded, nil
	})
	t.Cleanup(func() { _ = calls })
	return s, &loaded
}

func pagedSearch(t *testing.T, s *PagedSearcher, revision, q string, limit int) PagedResult {
	t.Helper()
	res, err := s.Search(context.Background(), 7, revision, q, limit)
	if err != nil {
		t.Fatalf("Search(%q): %v", q, err)
	}
	return res
}

func TestPagedPhraseMatchesAcrossFolding(t *testing.T) {
	s, _ := newPaged(t, pages, nil)

	// The remembered query as a reader types it — no capitals, no
	// punctuation, curly quotes and em-dashes folded away.
	res := pagedSearch(t, s, "r1", "youre not going anywhere he said finally", 20)
	if res.Mode != ModePhrase || res.Total != 1 {
		t.Fatalf("res = %s total %d, want phrase/1", res.Mode, res.Total)
	}
	hit := res.Hits[0]
	if hit.Page != 2 { // 0-based page index: page_number 3
		t.Fatalf("hit page = %d, want 2", hit.Page)
	}
	// The span sits inside the page's folded text and names the words.
	norm := booktext.Normalize(pages[2].Text)
	if got := norm[hit.Start:hit.End]; got != "youre not going anywhere he said finally" {
		t.Fatalf("hit span = %q", got)
	}
	// The raw reading travels with the hit for snippet building.
	if hit.Raw != pages[2].Text {
		t.Fatalf("hit raw = %q", hit.Raw)
	}
}

func TestPagedPhraseHitsEveryPageInOrder(t *testing.T) {
	s, _ := newPaged(t, []PageText{
		{Number: 1, Text: "the door"},
		{Number: 2, Text: "nothing"},
		{Number: 3, Text: "the door again"},
	}, nil)

	res := pagedSearch(t, s, "r1", "the door", 20)
	if res.Total != 2 || len(res.Hits) != 2 {
		t.Fatalf("total %d hits %d, want 2/2", res.Total, len(res.Hits))
	}
	if res.Hits[0].Page != 0 || res.Hits[1].Page != 2 {
		t.Fatalf("pages = %d,%d, want 0,2 in page order", res.Hits[0].Page, res.Hits[1].Page)
	}
}

func TestPagedPrefixOnlyForIncompleteLastWord(t *testing.T) {
	s, _ := newPaged(t, []PageText{
		{Number: 1, Text: "the theatre of the absurd"},
		{Number: 2, Text: "the door is closed"},
	}, nil)

	// A whole-word query must not bury "the door" under "theatre".
	res := pagedSearch(t, s, "r1", "the door", 20)
	if res.Mode != ModePhrase || res.Total != 1 || res.Hits[0].Page != 1 {
		t.Fatalf("whole-word: %s total %d, want phrase/1 on page 1", res.Mode, res.Total)
	}
	// A still-typed last word (no trailing space yet) matches as a
	// prefix — the same rule that makes the prose box feel live.
	res = pagedSearch(t, s, "r1", "the door is clos", 20)
	if res.Mode != ModePhrase || res.Total != 1 {
		t.Fatalf("prefix: %s total %d, want phrase/1", res.Mode, res.Total)
	}
}

func TestPagedLooseForgivesOneWord(t *testing.T) {
	s, _ := newPaged(t, []PageText{
		{Number: 1, Text: "lean and hungry, that was us"},
		{Number: 2, Text: "a completely unrelated caption about boats"},
	}, nil)

	// "lean hungry misremembered" — one word absent from the corpus, the
	// other two present on page 1 in any order.
	res := pagedSearch(t, s, "r1", "lean hungry zorknid", 20)
	if res.Mode != ModeLoose {
		t.Fatalf("mode = %s, want loose", res.Mode)
	}
	if res.Total != 1 || res.Hits[0].Page != 0 {
		t.Fatalf("total %d first page %d, want 1/0", res.Total, res.Hits[0].Page)
	}
}

func TestPagedEmptyPagesStayOnTheAxis(t *testing.T) {
	s, texts := newPaged(t, pages, nil)
	res := pagedSearch(t, s, "r1", "slammed", 20)
	if res.Total != 1 || res.Hits[0].Page != 1 {
		t.Fatalf("total %d page %d, want 1/1", res.Total, res.Hits[0].Page)
	}
	// An unread page (4, empty) owns no bytes and never matches...
	res = pagedSearch(t, s, "r1", "anything at all zzz", 20)
	for _, h := range res.Hits {
		if h.Page == 3 {
			t.Fatalf("empty page matched: %#v", h)
		}
	}
	_ = texts
}

func TestPagedTooShortRefused(t *testing.T) {
	s, _ := newPaged(t, pages, nil)
	if _, err := s.Search(context.Background(), 7, "r1", "ab", 20); err != ErrTooShort {
		t.Fatalf("err = %v, want ErrTooShort", err)
	}
}

func TestPagedRevisionGatesTheCache(t *testing.T) {
	s, loaded := newPaged(t, []PageText{{Number: 1, Text: "original lettering"}}, nil)

	if res := pagedSearch(t, s, "r1", "original lettering", 20); res.Total != 1 {
		t.Fatalf("r1 total = %d, want 1", res.Total)
	}
	// The corpus is re-read with different lettering. Same file id, new
	// revision: the cached index built over the old text must not answer.
	*loaded = []PageText{{Number: 1, Text: "replacement lettering"}}
	if res := pagedSearch(t, s, "r2", "replacement lettering", 20); res.Total != 1 {
		t.Fatalf("r2 new phrase total = %d, want 1 (cache served stale index)", res.Total)
	}
	if res := pagedSearch(t, s, "r2", "original lettering", 20); res.Mode == ModePhrase {
		t.Fatal("stale phrase still matches after a revision bump")
	}
}

func TestPagedLimitAndTotal(t *testing.T) {
	s, _ := newPaged(t, []PageText{
		{Number: 1, Text: "door"},
		{Number: 2, Text: "door"},
		{Number: 3, Text: "door"},
	}, nil)

	res := pagedSearch(t, s, "r1", "door", 2)
	if res.Total != 3 || len(res.Hits) != 2 {
		t.Fatalf("total %d hits %d, want 3/2", res.Total, len(res.Hits))
	}
}
