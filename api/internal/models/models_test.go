package models

import "testing"

func TestLabelContinuedChapters(t *testing.T) {
	ch := func(title, source string, start, end int) EpubChapter {
		return EpubChapter{Title: title, TitleSource: source, CharStart: start, CharEnd: end}
	}
	chapters := []EpubChapter{
		ch("Contents", "toc", 0, 100),
		ch("Begin Reading", "toc", 100, 150_000),
		ch("", "none", 150_000, 160_000),
		ch("", "none", 160_000, 160_000), // no text: not a part
		ch("", "none", 160_000, 170_000),
		ch("About the Author", "toc", 170_000, 171_000),
		// A guessed heading is not a TOC entry; what follows stays unnamed.
		ch("Chapter 1", "text", 171_000, 330_000),
		ch("", "none", 330_000, 340_000),
	}
	LabelContinuedChapters(chapters)

	want := []string{
		"Contents|toc", "Begin Reading|toc",
		"Begin Reading, part 2|continued", "|none", "Begin Reading, part 3|continued",
		"About the Author|toc", "Chapter 1|text", "|none",
	}
	for i, c := range chapters {
		if got := c.Title + "|" + c.TitleSource; got != want[i] {
			t.Errorf("chapter %d = %q, want %q", i, got, want[i])
		}
	}
}
