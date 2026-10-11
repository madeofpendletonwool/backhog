package ereader

import (
	"strings"
	"unicode/utf8"
)

// FilenameFormat is one of CrossPoint's OpdsFilenameFormat settings: how it
// names a book it downloads from an OPDS catalog.
type FilenameFormat string

const (
	FormatAuthorTitle FilenameFormat = "author_title" // "Author - Title.epub", the default
	FormatTitleAuthor FilenameFormat = "title_author" // "Title - Author.epub"
	FormatTitleOnly   FilenameFormat = "title_only"   // "Title.epub"
)

// FilenameFormats lists every format, in CrossPoint's enum order.
var FilenameFormats = []FilenameFormat{FormatAuthorTitle, FormatTitleAuthor, FormatTitleOnly}

// sanitizeMaxBytes is CrossPoint's sanitizeFilename default budget.
const sanitizeMaxBytes = 100

// BookFilename is the file CrossPoint writes for a book downloaded from an
// OPDS feed, given the author and title strings that feed carried. It is a
// port of opdsBookFilename (src/util/OpdsFilename.cpp): an empty author
// collapses every format to the title alone, the base is sanitized to at
// most 100 bytes, and ".epub" is appended after so the extension is never
// truncated.
func BookFilename(author, title string, format FilenameFormat) string {
	var base string
	switch format {
	case FormatTitleAuthor:
		base = title
		if author != "" {
			base = title + " - " + author
		}
	case FormatTitleOnly:
		base = title
	default:
		base = title
		if author != "" {
			base = author + " - " + title
		}
	}
	return SanitizeFilename(base, sanitizeMaxBytes) + ".epub"
}

// SanitizeFilename ports CrossPoint's StringUtils::sanitizeFilename:
// leading spaces and dots are skipped, / \ : * ? " < > | become '_',
// control characters (and DEL) are dropped, whole UTF-8 code points are
// kept until the next one would pass maxBytes, trailing spaces and dots are
// trimmed, and an empty result is "book".
func SanitizeFilename(name string, maxBytes int) string {
	name = strings.TrimLeft(name, " .")
	var out strings.Builder
	for _, r := range name {
		switch {
		case strings.ContainsRune(`/\:*?"<>|`, r):
			if out.Len()+1 > maxBytes {
				return finishSanitized(out.String())
			}
			out.WriteByte('_')
		case r >= 128 || (r >= 32 && r < 127):
			if out.Len()+utf8.RuneLen(r) > maxBytes {
				return finishSanitized(out.String())
			}
			out.WriteRune(r)
		}
	}
	return finishSanitized(out.String())
}

func finishSanitized(s string) string {
	s = strings.TrimRight(s, " .")
	if s == "" {
		return "book"
	}
	return s
}
