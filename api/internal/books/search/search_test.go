package search

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/collinpendleton/backhog/api/booktext"
)

// text is a canonical text the way the ingester writes one: folded, single
// spaces, nothing else. Written as prose and normalized so the fixture stays
// readable.
const prose = `It was a bright cold day in April, and the clocks were striking thirteen.
Winston Smith, his chin nuzzled into his breast in an effort to escape the vile
wind, slipped quickly through the glass doors of Victory Mansions, though not
quickly enough to prevent a swirl of gritty dust from entering along with him.
The hallway smelt of boiled cabbage and old rag mats. At one end of it a
coloured poster, too large for indoor display, had been tacked to the wall.
The door closed behind him, and the lamp guttered. Later the door closed again,
and he started up the stairs. A start is not a door.`

func newTestSearcher(t *testing.T, body string) *Searcher {
	t.Helper()
	canonical := booktext.Normalize(body)
	return New(func(ctx context.Context, textID string) (string, error) {
		if textID != "text-1" {
			return "", errors.New("no such text")
		}
		return canonical, nil
	})
}

// spans renders a result as the canonical substrings it points at, which is
// what the assertions are actually about.
func spans(t *testing.T, body string, res Result) []string {
	t.Helper()
	canonical := booktext.Normalize(body)
	out := make([]string, 0, len(res.Hits))
	for _, h := range res.Hits {
		if h.CharOffset < 0 || h.CharEnd > len(canonical) || h.CharEnd <= h.CharOffset {
			t.Fatalf("hit [%d,%d) is outside the text of %d bytes", h.CharOffset, h.CharEnd, len(canonical))
		}
		out = append(out, canonical[h.CharOffset:h.CharEnd])
	}
	return out
}

func search(t *testing.T, s *Searcher, q string, limit int) Result {
	t.Helper()
	res, err := s.Search(context.Background(), "text-1", "sha", q, limit)
	if err != nil {
		t.Fatalf("Search(%q): %v", q, err)
	}
	return res
}

func TestPhraseIgnoresPunctuationAndCase(t *testing.T) {
	s := newTestSearcher(t, prose)

	// All four say the same thing once the fold has been applied.
	for _, q := range []string{
		"The door closed behind",
		"the door closed behind",
		"“The door closed behind”",
		"the  door   closed  behind",
	} {
		res := search(t, s, q, 20)
		if res.Mode != ModePhrase {
			t.Errorf("%q: mode = %q, want phrase", q, res.Mode)
		}
		if got := spans(t, prose, res); len(got) != 1 || got[0] != "the door closed behind" {
			t.Errorf("%q: hits = %q", q, got)
		}
	}
}

func TestPhraseRespectsTokenBoundaries(t *testing.T) {
	s := newTestSearcher(t, prose)

	// "a start is not a door" is in the text; "art" is only inside "start".
	res := search(t, s, "art", 20)
	if res.Total != 0 {
		t.Errorf("searching %q found %d hits inside other words: %q", "art", res.Total, spans(t, prose, res))
	}

	// The word itself is found.
	if res := search(t, s, "start", 20); res.Total != 1 {
		t.Errorf("searching %q: total = %d, want 1", "start", res.Total)
	}
}

func TestPhrasePrefixWhileTyping(t *testing.T) {
	s := newTestSearcher(t, prose)

	// Mid-word: no whole-word hit exists, so the last token matches as a
	// prefix and the sentence still turns up.
	res := search(t, s, "the door clos", 20)
	if res.Mode != ModePhrase {
		t.Fatalf("mode = %q, want phrase", res.Mode)
	}
	if got := spans(t, prose, res); len(got) != 2 {
		t.Fatalf("hits = %q, want the two 'the door clos…' occurrences", got)
	}

	// A trailing space says the word is finished, so the prefix relaxation is
	// off: "clos" is not a word in this book, so there is no phrase hit and
	// the query falls through to the loose pass.
	if res := search(t, s, "the door clos ", 20); res.Mode != ModeLoose {
		t.Errorf("a completed word matched as a prefix: %q", spans(t, prose, res))
	}
}

func TestPhraseReturnsEveryOccurrenceInBookOrder(t *testing.T) {
	s := newTestSearcher(t, prose)

	res := search(t, s, "the door closed", 20)
	if res.Total != 2 {
		t.Fatalf("total = %d, want 2", res.Total)
	}
	if len(res.Hits) != 2 {
		t.Fatalf("hits = %d, want 2", len(res.Hits))
	}
	if res.Hits[0].CharOffset >= res.Hits[1].CharOffset {
		t.Errorf("hits are not in book order: %d then %d", res.Hits[0].CharOffset, res.Hits[1].CharOffset)
	}
}

func TestLimitCapsHitsNotTotal(t *testing.T) {
	s := newTestSearcher(t, prose)

	res := search(t, s, "the", 2)
	if len(res.Hits) != 2 {
		t.Errorf("hits = %d, want the limit of 2", len(res.Hits))
	}
	if res.Total <= 2 {
		t.Errorf("total = %d, want the real count of every 'the'", res.Total)
	}
}

func TestLooseForgivesOneWrongWord(t *testing.T) {
	s := newTestSearcher(t, prose)

	// "guttered" is right, "lantern" is not — the lamp guttered.
	res := search(t, s, "and the lantern guttered", 20)
	if res.Mode != ModeLoose {
		t.Fatalf("mode = %q, want loose", res.Mode)
	}
	if len(res.Hits) == 0 {
		t.Fatal("loose pass found nothing")
	}
	if got := spans(t, prose, res)[0]; !strings.Contains(got, "guttered") {
		t.Errorf("best loose hit = %q, want the passage with 'guttered'", got)
	}
}

func TestLooseForgivesWordOrder(t *testing.T) {
	s := newTestSearcher(t, prose)

	res := search(t, s, "cabbage boiled mats", 20)
	if res.Mode != ModeLoose {
		t.Fatalf("mode = %q, want loose", res.Mode)
	}
	got := spans(t, prose, res)
	if len(got) == 0 || !strings.Contains(got[0], "boiled cabbage and old rag mats") {
		t.Errorf("hits = %q, want the boiled-cabbage passage", got)
	}
}

func TestLooseRefusesToDegradeIntoOneWordSearch(t *testing.T) {
	s := newTestSearcher(t, prose)

	// Neither the phrase nor two of its words are here, and answering with
	// every "the" would be worse than answering with nothing.
	res := search(t, s, "the zzzyzx", 20)
	if res.Total != 0 {
		t.Errorf("total = %d, want 0; hits = %q", res.Total, spans(t, prose, res))
	}
}

func TestNoMatchIsEmptyNotAnError(t *testing.T) {
	s := newTestSearcher(t, prose)

	res := search(t, s, "quantum chromodynamics", 20)
	if res.Total != 0 || len(res.Hits) != 0 {
		t.Errorf("total = %d, hits = %d, want an empty result", res.Total, len(res.Hits))
	}
}

func TestTooShort(t *testing.T) {
	s := newTestSearcher(t, prose)

	for _, q := range []string{"", " ", "a", "”“", "of"} {
		if _, err := s.Search(context.Background(), "text-1", "sha", q, 20); !errors.Is(err, ErrTooShort) {
			t.Errorf("Search(%q) err = %v, want ErrTooShort", q, err)
		}
	}
	// Three folded characters is the floor and is accepted.
	if _, err := s.Search(context.Background(), "text-1", "sha", "day", 20); err != nil {
		t.Errorf("Search(%q): %v", "day", err)
	}
}

func TestIndexIsCachedPerRevision(t *testing.T) {
	loads := 0
	s := New(func(ctx context.Context, textID string) (string, error) {
		loads++
		if loads == 1 {
			return booktext.Normalize("the first canonical text"), nil
		}
		return booktext.Normalize("the second canonical text"), nil
	})
	ctx := context.Background()

	if _, err := s.Search(ctx, "text-1", "sha-a", "first", 20); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Search(ctx, "text-1", "sha-a", "canonical", 20); err != nil {
		t.Fatal(err)
	}
	if loads != 1 {
		t.Errorf("loads = %d, want the index cached after the first", loads)
	}

	// A re-parse can reuse the id; the revision is what makes the old index
	// unreachable, and searching the new text must not answer from the old.
	res, err := s.Search(ctx, "text-1", "sha-b", "second", 20)
	if err != nil {
		t.Fatal(err)
	}
	if loads != 2 {
		t.Errorf("loads = %d, want a reload for the new revision", loads)
	}
	if res.Total != 1 {
		t.Errorf("total = %d, want the new text searched", res.Total)
	}
}

func TestLoadErrorSurfaces(t *testing.T) {
	want := errors.New("boom")
	s := New(func(ctx context.Context, textID string) (string, error) { return "", want })
	if _, err := s.Search(context.Background(), "text-1", "sha", "anything", 20); !errors.Is(err, want) {
		t.Errorf("err = %v, want %v", err, want)
	}
}
