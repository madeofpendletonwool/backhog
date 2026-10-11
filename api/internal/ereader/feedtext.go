package ereader

import (
	"strings"
	"unicode/utf8"
)

// CrossPoint's OPDS parser keeps at most this many bytes of an entry's
// title and author (lib/OpdsParser: MAX_TITLE_CHARS, MAX_AUTHOR_CHARS —
// named chars, measured in bytes via strnlen). Anything longer is cut
// mid-character, so the catalog trims first, on a code point, with an
// ellipsis that says it did.
const (
	maxTitleBytes  = 160
	maxAuthorBytes = 120
)

// FeedTitle is the title string the OPDS catalog emits for a book. The
// filename-mode document ids are built from this same string, because
// CrossPoint names the downloaded file from what the feed said — so the
// catalog and the id table must never compute it two different ways.
func FeedTitle(title string) string {
	title = strings.Join(strings.Fields(title), " ")
	if title == "" {
		title = "Untitled"
	}
	return trimBytes(title, maxTitleBytes)
}

// FeedAuthor is the author string the OPDS catalog emits: every author,
// comma-joined, within CrossPoint's budget. Empty when the book has none,
// which CrossPoint's naming treats as title-only.
func FeedAuthor(authors []string) string {
	kept := make([]string, 0, len(authors))
	for _, a := range authors {
		if a = strings.Join(strings.Fields(a), " "); a != "" {
			kept = append(kept, a)
		}
	}
	return trimBytes(strings.Join(kept, ", "), maxAuthorBytes)
}

// trimBytes shortens s to at most max bytes on a code point boundary,
// ending in "…" when anything was cut.
func trimBytes(s string, max int) string {
	if len(s) <= max {
		return s
	}
	const ellipsis = "…"
	cut := max - len(ellipsis)
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return strings.TrimRight(s[:cut], " ,") + ellipsis
}
