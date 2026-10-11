package ereader

import (
	"bytes"
	"strings"
	"testing"
	"unicode/utf8"
)

// The goldens below were produced by CrossPoint's own code — its
// KOReaderDocumentId.cpp, OpdsFilename.cpp and StringUtils.cpp compiled on
// the host at commit e54c008 — not by the functions under test. CrossPoint's
// document id exists to interoperate with KOReader's partialMD5, so it
// stands in for both clients until a device confirms it end to end.

// patterned returns n deterministic, non-repeating-at-1KiB bytes: the same
// recipe the goldens were generated from.
func patterned(n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte((i*31 + 7) % 251)
	}
	return b
}

func TestPartialMD5MatchesCrossPoint(t *testing.T) {
	cases := []struct {
		size int
		want string
	}{
		{0, "d41d8cd98f00b204e9800998ecf8427e"},
		{500, "20d7e49d5efbe12563021059740872ae"},
		{1024, "5121b74d11d0ad611a246b4137993844"},
		{1025, "35b02546e34868e7b75dab8c43d47c36"},
		{4097, "612eca124074682ad0645d03a3d03589"},
		{300000, "3d87fe2dd28ddc7895ae5e290bfa539e"},
		{5000000, "26776a0cc9a8a34badc67cca03828cd4"},
	}
	for _, c := range cases {
		data := patterned(c.size)
		got, err := PartialMD5(bytes.NewReader(data), int64(len(data)))
		if err != nil {
			t.Fatalf("size %d: %v", c.size, err)
		}
		if got != c.want {
			t.Errorf("size %d: PartialMD5 = %s, CrossPoint says %s", c.size, got, c.want)
		}
	}
}

func TestBookFilenameMatchesCrossPoint(t *testing.T) {
	cases := []struct {
		format        FilenameFormat
		author, title string
		name, md5     string
	}{
		{FormatAuthorTitle, "J. Doe", "My Book", "J. Doe - My Book.epub", "405acafb5db777a1aebc25fa24a5680a"},
		{FormatTitleAuthor, "J. Doe", "My Book", "My Book - J. Doe.epub", "304b9820bc256ab67f9b219e5a4e65d9"},
		{FormatTitleOnly, "J. Doe", "My Book", "My Book.epub", "9a671b9d07b5404d84ce60af27058b04"},
		{FormatAuthorTitle, "", "My Book", "My Book.epub", "9a671b9d07b5404d84ce60af27058b04"},
		{FormatAuthorTitle, "A/B", "C:D*E?", "A_B - C_D_E_.epub", "3e865877a38eb08a075278f5bf5e21a2"},
		{FormatAuthorTitle, "", "", "book.epub", "03053ffc045564439ff7f2cabb3b58c5"},
		{FormatAuthorTitle, "Gabriel García Márquez", "Cien años de soledad",
			"Gabriel García Márquez - Cien años de soledad.epub", "9c588279aa0cccaf20ba79f806512c67"},
		{FormatTitleAuthor, "  .Lead Dots", "Trailing dots...",
			"Trailing dots... -   .Lead Dots.epub", "b8e537a148e9755a39875899bcbe33b7"},
		{FormatAuthorTitle, "Author", `Title with "quotes" <and> pipes | here`,
			"Author - Title with _quotes_ _and_ pipes _ here.epub", "e8db4c11a8dc54648cbdaf745ec8a125"},
		{FormatAuthorTitle, "Brandon Sanderson",
			"The Way of Kings: Book One of the Stormlight Archive, a very long subtitle that goes on and on past the budget",
			"Brandon Sanderson - The Way of Kings_ Book One of the Stormlight Archive, a very long subtitle that.epub",
			"e424ab203ad9df0b668b49e5030be5b5"},
		{FormatAuthorTitle, "村上 春樹", "ノルウェイの森 🌲", "村上 春樹 - ノルウェイの森 🌲.epub", "e7c5217d5fe6940df2adb7f5333535fc"},
		{FormatTitleOnly, "", strings.Repeat("漢", 40), strings.Repeat("漢", 33) + ".epub", "25a29886fc6297e211a89a189176ea11"},
		{FormatAuthorTitle, "Tab\tAuthor", "Line\nBreak", "TabAuthor - LineBreak.epub", "599f26823187691f45a44b2f3cae927d"},
	}
	for _, c := range cases {
		name := BookFilename(c.author, c.title, c.format)
		if name != c.name {
			t.Errorf("BookFilename(%q, %q, %s) = %q, CrossPoint says %q", c.author, c.title, c.format, name, c.name)
			continue
		}
		if got := FilenameMD5(name); got != c.md5 {
			t.Errorf("FilenameMD5(%q) = %s, CrossPoint says %s", name, got, c.md5)
		}
	}
}

func TestFeedTextFitsCrossPointBudgets(t *testing.T) {
	long := strings.Repeat("Stormlight ", 30)
	title := FeedTitle(long)
	if len(title) > maxTitleBytes || !strings.HasSuffix(title, "…") || !utf8.ValidString(title) {
		t.Errorf("FeedTitle(long) = %q (%d bytes)", title, len(title))
	}
	cjk := FeedTitle(strings.Repeat("森", 100))
	if len(cjk) > maxTitleBytes || !utf8.ValidString(cjk) {
		t.Errorf("FeedTitle(cjk) cut a code point: %d bytes, valid=%v", len(cjk), utf8.ValidString(cjk))
	}
	if got := FeedTitle("  The   Hobbit \n"); got != "The Hobbit" {
		t.Errorf("FeedTitle collapses whitespace: got %q", got)
	}
	if got := FeedTitle(""); got != "Untitled" {
		t.Errorf("FeedTitle(\"\") = %q", got)
	}
	if got := FeedAuthor([]string{"Terry Pratchett", " ", "Neil  Gaiman"}); got != "Terry Pratchett, Neil Gaiman" {
		t.Errorf("FeedAuthor = %q", got)
	}
	if got := FeedAuthor(nil); got != "" {
		t.Errorf("FeedAuthor(nil) = %q", got)
	}
	many := make([]string, 20)
	for i := range many {
		many[i] = "Contributor Number"
	}
	if got := FeedAuthor(many); len(got) > maxAuthorBytes || !strings.HasSuffix(got, "…") {
		t.Errorf("FeedAuthor(many) = %q (%d bytes)", got, len(got))
	}
}
