package passage

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"strings"
	"testing"
)

// synthNovel builds a deterministic pseudo-novel: paragraphs of palette
// words joined by single spaces (the canonical shape), each opening with
// a unique tag token, with a 45-word epigraph repeated at both ends — the
// classic recurring passage. Palette words include diacritics and
// "rn"-words so the noise tests exercise real OCR failure modes.
func synthNovel(paragraphs int) (text, epigraph string) {
	palette := []string{
		"the", "a", "of", "and", "in", "was", "he", "she", "they", "it",
		"night", "morning", "corridor", "clock", "government", "burning",
		"lantern", "harbor", "winter", "mountain", "river", "garden",
		"café", "señor", "naïve", "über", "crème", "séance",
	}
	rng := rand.New(rand.NewSource(42))
	para := func(tag string, words int) string {
		out := make([]string, 0, words+1)
		out = append(out, tag)
		for i := 0; i < words; i++ {
			out = append(out, palette[rng.Intn(len(palette))])
		}
		return strings.Join(out, " ")
	}

	epigraph = para("epigraph", 44)
	parts := []string{epigraph}
	for i := 0; i < paragraphs; i++ {
		parts = append(parts, para(fmt.Sprintf("p%04d", i), 44))
	}
	parts = append(parts, epigraph)
	return strings.Join(parts, " "), epigraph
}

// midWindow takes n consecutive tokens from the middle of the text and
// reports where the window starts in bytes.
func midWindow(text string, n int) (window string, at int) {
	fields := strings.Fields(text)
	k := len(fields) / 2
	window = strings.Join(fields[k:k+n], " ")
	return window, strings.Index(text, window)
}

// diacriticsFold is the classic OCR dropout: accents gone, letters kept.
var diacriticsFold = map[rune]rune{
	'é': 'e', 'è': 'e', 'ê': 'e', 'á': 'a', 'à': 'a', 'â': 'a',
	'í': 'i', 'ó': 'o', 'ú': 'u', 'ü': 'u', 'ö': 'o', 'ä': 'a',
	'ï': 'i', 'ñ': 'n', 'ç': 'c', 'ß': 's', 'ù': 'u', 'û': 'u',
}

// mangleOCR roughs a clean passage up the way a camera scan does:
// diacritics dropped, "rn" read as "m", and the occasional pair of
// adjacent characters swapped. Word count never changes — the failure
// modes are all inside words.
func mangleOCR(s string, seed int64) string {
	rng := rand.New(rand.NewSource(seed))
	words := strings.Split(s, " ")
	for i := range words {
		w := []rune(words[i])
		for j, r := range w {
			if sub, ok := diacriticsFold[r]; ok {
				w[j] = sub
			}
		}
		words[i] = strings.ReplaceAll(string(w), "rn", "m")
		w = []rune(words[i])
		if len(w) > 4 && rng.Intn(6) == 0 {
			j := 1 + rng.Intn(len(w)-3)
			w[j], w[j+1] = w[j+1], w[j]
		}
		words[i] = string(w)
	}
	return strings.Join(words, " ")
}

func testMatcher(text string, loads *int) *Matcher {
	return New(func(_ context.Context, _ string) (string, error) {
		if loads != nil {
			*loads++
		}
		return text, nil
	})
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// A clean 40-word passage from the middle of a novel resolves to exactly
// where it sits.
func TestFindCleanPassage(t *testing.T) {
	text, _ := synthNovel(120)
	m := testMatcher(text, nil)
	window, at := midWindow(text, 40)

	res, err := m.Find(context.Background(), "t1", window)
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	if abs(res.Match.CharOffset-at) > 2 {
		t.Errorf("char offset = %d, want %d (±2)", res.Match.CharOffset, at)
	}
	if res.Match.Confidence < 0.99 {
		t.Errorf("confidence = %v, want ~1 for a clean passage", res.Match.Confidence)
	}
	if len(res.Alternatives) != 0 {
		t.Errorf("clean passage reported %d alternatives", len(res.Alternatives))
	}
	if got := text[res.Match.CharOffset:res.Match.CharEnd]; got != window {
		t.Errorf("matched span = %q, want the window itself", got)
	}
}

// OCR noise — swapped characters, dropped diacritics, rn read as m —
// still lands on the right place, with a confidence that reflects the
// garbage it tolerated.
func TestFindNoisyOCRPassage(t *testing.T) {
	text, _ := synthNovel(120)
	m := testMatcher(text, nil)
	window, at := midWindow(text, 40)

	noisy := mangleOCR(window, 7)
	if noisy == window {
		t.Fatal("mangling produced no noise; the fixture is too clean")
	}
	res, err := m.Find(context.Background(), "t1", noisy)
	if err != nil {
		t.Fatalf("find noisy: %v", err)
	}
	if abs(res.Match.CharOffset-at) > 10 {
		t.Errorf("char offset = %d, want %d (±10) through noise", res.Match.CharOffset, at)
	}
	if res.Match.Confidence < 0.75 || res.Match.Confidence >= 1 {
		t.Errorf("confidence = %v, want it damped but strong", res.Match.Confidence)
	}
	if len(res.Alternatives) != 0 {
		t.Errorf("noisy unique passage reported %d alternatives", len(res.Alternatives))
	}
}

// A query too short to be anyone's but the book's own words is refused
// rather than guessed at.
func TestFindRefusesTooShort(t *testing.T) {
	text, epigraph := synthNovel(10)
	m := testMatcher(text, nil)

	if _, err := m.Find(context.Background(), "t1", "the night corridor was very quiet"); !errors.Is(err, ErrTooShort) {
		t.Errorf("six-word query err = %v, want ErrTooShort", err)
	}
	if _, err := m.Find(context.Background(), "t1", "   "); !errors.Is(err, ErrTooShort) {
		t.Errorf("blank query err = %v, want ErrTooShort", err)
	}
	// At the floor it is a real query, not an error — and ten words that
	// really are in the book match.
	head := strings.Fields(epigraph)[:minQueryTokens]
	if _, err := m.Find(context.Background(), "t1", strings.Join(head, " ")); err != nil {
		t.Errorf("ten-word query from the text err = %v, want accepted", err)
	}
}

// A passage that genuinely recurs — the epigraph at both ends — comes
// back with alternatives instead of a silent guess.
func TestFindRepeatedPassageReturnsAlternatives(t *testing.T) {
	text, epigraph := synthNovel(80)
	m := testMatcher(text, nil)

	first := strings.Index(text, epigraph)
	last := strings.LastIndex(text, epigraph)
	if first == last {
		t.Fatal("fixture epigraph does not repeat")
	}

	res, err := m.Find(context.Background(), "t1", epigraph)
	if err != nil {
		t.Fatalf("find recurring: %v", err)
	}
	if res.Match.CharOffset != first {
		t.Errorf("top match offset = %d, want the first occurrence %d", res.Match.CharOffset, first)
	}
	if len(res.Alternatives) == 0 {
		t.Fatal("a recurring passage returned no alternatives")
	}
	found := false
	for _, alt := range res.Alternatives {
		if alt.CharOffset == last {
			found = true
		}
	}
	if !found {
		t.Errorf("alternatives = %v, want the second occurrence at %d", res.Alternatives, last)
	}
}

// Gibberish is a no-match, not a low-confidence guess.
func TestFindNoMatch(t *testing.T) {
	text, _ := synthNovel(10)
	m := testMatcher(text, nil)

	junk := make([]string, 30)
	for i := range junk {
		junk[i] = fmt.Sprintf("zq%dwjkb", i)
	}
	if _, err := m.Find(context.Background(), "t1", strings.Join(junk, " ")); !errors.Is(err, ErrNoMatch) {
		t.Errorf("gibberish err = %v, want ErrNoMatch", err)
	}
}

// A passage typed back the way a person would type it — capitals,
// curly quotes, punctuation — normalizes to the same folded space the
// canonical text lives in and matches exactly.
func TestFindNormalizesTheQuery(t *testing.T) {
	text, _ := synthNovel(120)
	m := testMatcher(text, nil)
	window, at := midWindow(text, 40)

	dressed := "“" + strings.ToUpper(window) + "!”"
	res, err := m.Find(context.Background(), "t1", dressed)
	if err != nil {
		t.Fatalf("find dressed query: %v", err)
	}
	if res.Match.CharOffset != at {
		t.Errorf("char offset = %d, want %d", res.Match.CharOffset, at)
	}
	if res.Match.Confidence < 0.99 {
		t.Errorf("confidence = %v, want ~1", res.Match.Confidence)
	}
}

// The index is cached per text id: repeated scans of one book load and
// build once.
func TestIndexCachedPerText(t *testing.T) {
	text, epigraph := synthNovel(20)
	loads := 0
	m := testMatcher(text, &loads)

	for i := 0; i < 3; i++ {
		if _, err := m.Find(context.Background(), "t1", epigraph); err != nil {
			t.Fatalf("find %d: %v", i, err)
		}
	}
	if loads != 1 {
		t.Errorf("loader ran %d times for one text, want 1", loads)
	}
}
