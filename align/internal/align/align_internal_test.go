package align

import "testing"

func TestFoldedNumerals(t *testing.T) {
	// The book prints "23" and the narrator says "twenty-three", which
	// normalizes to "twenty three". Both sides have to arrive at the same
	// tokens or every anchor around a number is wrong.
	tests := []struct {
		in   string
		want []string
	}{
		{"23", []string{"twenty", "three"}},
		{"7", []string{"seven"}},
		{"15", []string{"fifteen"}},
		{"100", []string{"one", "hundred"}},
		{"342", []string{"three", "hundred", "forty", "two"}},
		{"1904", []string{"one", "thousand", "nine", "hundred", "four"}},
		{"1st", []string{"first"}},
		{"2nd", []string{"second"}},
		{"3rd", []string{"third"}},
		{"12th", []string{"twelfth"}},
		{"21st", []string{"twenty", "first"}},
		{"40th", []string{"fortieth"}},
		// Not numbers: left exactly as they are.
		{"east", []string{"east"}},
		{"august", []string{"august"}},
		{"1980s", []string{"1980s"}},
		{"1234567890123", []string{"1234567890123"}},
	}
	for _, tt := range tests {
		got := appendFolded(nil, tt.in)
		if !equalWords(got, tt.want) {
			t.Errorf("appendFolded(%q) = %v, want %v", tt.in, got, tt.want)
		}
	}
}

func TestFoldedNumeralsAgreeAcrossSpellings(t *testing.T) {
	// The point of the fold, stated as the property it has to have: the
	// printed form and the spoken form must land on the same tokens.
	pairs := [][2][]string{
		{{"23"}, {"twenty", "three"}},
		{{"1st"}, {"first"}},
		{{"342"}, {"three", "hundred", "forty", "two"}},
	}
	for _, p := range pairs {
		var printed, spoken []string
		for _, w := range p[0] {
			printed = appendFolded(printed, w)
		}
		for _, w := range p[1] {
			spoken = appendFolded(spoken, w)
		}
		if !equalWords(printed, spoken) {
			t.Errorf("printed %v folded to %v; spoken %v folded to %v", p[0], printed, p[1], spoken)
		}
	}
}

func TestFoldedAbbreviations(t *testing.T) {
	pairs := [][2]string{
		{"mr", "mister"},
		{"mrs", "missus"},
		{"dr", "doctor"},
		{"st", "saint"},
		{"st", "street"},
		{"mt", "mount"},
		{"capt", "captain"},
		{"vs", "versus"},
	}
	for _, p := range pairs {
		printed := appendFolded(nil, p[0])
		spoken := appendFolded(nil, p[1])
		if !equalWords(printed, spoken) {
			t.Errorf("%q folded to %v but %q folded to %v", p[0], printed, p[1], spoken)
		}
	}
	// Ordinary words that merely start like an abbreviation are untouched.
	for _, w := range []string{"saintly", "streetlight", "doctors", "mister"} {
		got := appendFolded(nil, w)
		if len(got) != 1 {
			t.Fatalf("appendFolded(%q) = %v, want one token", w, got)
		}
		if w != "mister" && got[0] != w {
			t.Errorf("appendFolded(%q) = %q, want it left alone", w, got[0])
		}
	}
}

func TestTokenizeCanonicalKeepsByteOffsets(t *testing.T) {
	// Offsets are byte offsets, not rune counts: a canonical text with a
	// non-ASCII letter in it must not shift everything after it.
	text := "the café was quiet"
	tokens := tokenizeCanonical(text)
	want := []canonToken{
		{Start: 0, Word: "the"},
		{Start: 4, Word: "café"},
		{Start: 10, Word: "was"},
		{Start: 14, Word: "quiet"},
	}
	if len(tokens) != len(want) {
		t.Fatalf("got %d tokens, want %d: %+v", len(tokens), len(want), tokens)
	}
	for i, w := range want {
		if tokens[i] != w {
			t.Errorf("token %d = %+v, want %+v", i, tokens[i], w)
		}
		if text[tokens[i].Start:tokens[i].Start+len(tokens[i].Word)] != w.Word {
			t.Errorf("token %d does not slice back out of the text", i)
		}
	}
}

func TestMonotoneRejectsBackwardsAnchors(t *testing.T) {
	// An audiobook is read front to back, so an anchor that moves the
	// reader backwards while the tape moves forwards cannot be true. The
	// database would happily store it and the position translator would
	// interpolate straight through it.
	opts := DefaultOptions()
	in := []Anchor{
		{CharOffset: 100, AudioSeconds: 10, Confidence: 0.9},
		{CharOffset: 400, AudioSeconds: 20, Confidence: 0.9},
		{CharOffset: 250, AudioSeconds: 30, Confidence: 0.9}, // backwards
		{CharOffset: 400, AudioSeconds: 40, Confidence: 0.9}, // repeated
		{CharOffset: 900, AudioSeconds: 50, Confidence: 0.1}, // under the floor
		{CharOffset: 950, AudioSeconds: 60, Confidence: 0.9},
	}
	got := monotone(in, opts)
	want := []int{100, 400, 950}
	if len(got) != len(want) {
		t.Fatalf("kept %d anchors, want %d: %+v", len(got), len(want), got)
	}
	for i, off := range want {
		if got[i].CharOffset != off {
			t.Errorf("anchor %d at char %d, want %d", i, got[i].CharOffset, off)
		}
	}
}

func TestMonotonicPathRefusesToGoBackwards(t *testing.T) {
	// Two windows, the second of which is best supported by a position
	// earlier in the book than the first window's. The DP must reject the
	// backwards pairing even though it scores higher on votes alone.
	opts := DefaultOptions()
	windows := []window{
		{firstTok: 0, lastTok: 100},
		{firstTok: 100, lastTok: 200},
	}
	cands := [][]candidate{
		{{offset: 5000, votes: 50}},
		{{offset: 100, votes: 40}, {offset: 5100, votes: 10}},
	}
	got := monotonicPath(windows, cands, opts)
	if got[0] != 5000 {
		t.Errorf("window 0 placed at %d, want 5000", got[0])
	}
	if got[1] != 5100 {
		t.Errorf("window 1 placed at %d, want 5100 - the backwards candidate won", got[1])
	}
}

func TestBridgeGapsLeavesTheEndsAlone(t *testing.T) {
	// Interior holes are bridged; the unplaced runs at the head and tail
	// are the audiobook's intro and outro, which exist in no EPUB and
	// must never be dragged onto one.
	windows := []window{
		{firstTok: 0}, {firstTok: 100}, {firstTok: 200},
		{firstTok: 300}, {firstTok: 400}, {firstTok: 500},
	}
	offsets := []int{-1, 1000, -1, -1, 4000, -1}
	bridgeGaps(windows, offsets)
	if offsets[0] != -1 || offsets[5] != -1 {
		t.Errorf("the ends were bridged: %v", offsets)
	}
	if offsets[2] != 2000 || offsets[3] != 3000 {
		t.Errorf("interior gap = %v, want the two windows at 2000 and 3000", offsets)
	}
}

func TestFitAlignFindsTheWindowInsideItsRegion(t *testing.T) {
	reference := toks("alpha beta gamma delta epsilon zeta eta theta iota kappa")
	// The query is the middle of the reference with one word misheard and
	// one dropped, which is what a transcript of it looks like.
	query := toks("gamma delta epsilonn zeta theta")

	aligned := fitAlign(query, reference, 2, 4)
	if aligned[0] != 2 {
		t.Errorf("first query token aligned to %d, want 2", aligned[0])
	}
	if aligned[1] != 3 {
		t.Errorf("second query token aligned to %d, want 3", aligned[1])
	}
	if aligned[3] != 5 {
		t.Errorf("zeta aligned to %d, want 5", aligned[3])
	}
	if aligned[4] != 7 {
		t.Errorf("theta aligned to %d, want 7", aligned[4])
	}
	for i := 1; i < len(aligned); i++ {
		if aligned[i] >= 0 && aligned[i-1] >= 0 && aligned[i] <= aligned[i-1] {
			t.Fatalf("alignment is not monotone: %v", aligned)
		}
	}
}

func toks(s string) []matchTok {
	words := fields(s)
	out := make([]matchTok, len(words))
	for i, w := range words {
		out[i] = matchTok{Word: w, Src: int32(i)}
	}
	return out
}

func equalWords(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
