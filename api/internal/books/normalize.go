// Package books holds the canonical-text machinery of the Books arena: the
// EPUB ingester that builds the canonical text and its offset indexes, and
// the parser version that invalidates stored offsets when the rules below
// change. The normalizer itself lives one level up, in booktext.
//
// # The canonical text
//
// Every position — reader location, alignment anchor, OCR page anchor — is a
// byte offset into one normalized canonical text per EPUB. Offsets are byte
// offsets into the UTF-8 encoding of that text (Go string indexing), not rune
// counts.
package books

import "github.com/collinpendleton/backhog/api/booktext"

// ParserVersion identifies the normalizer/extractor behaviour a canonical
// text was produced with. Bump it whenever booktext.Normalize or the EPUB
// block extraction changes: rows parsed under an older version are
// re-parsed on next access, rebuilding every derived offset.
const ParserVersion = "2"

// Normalize is booktext.Normalize, re-exported so the arena's own code
// keeps reading books.Normalize. It is a call, not a copy: the alignment
// worker normalizes its transcripts with the very same function through
// its own import of booktext, and two implementations of these rules that
// drifted apart would silently rot every stored offset in the arena.
func Normalize(s string) string { return booktext.Normalize(s) }
