// Package pdf parses PDF files into the same spine structure the EPUB and
// MOBI parsers produce: ordered documents of paragraph-ish blocks with TOC
// titles, per-document images, and heading evidence. It is pure parsing —
// no normalization, no storage, no filesystem — mirroring internal/books/epub
// and internal/books/mobi, and it returns *epub.Document because that type
// is the arena's shared extraction model: a PDF that emits it earns the
// canonicalizer, the chapter rows and the block index unchanged.
//
// The document model is one Doc per page ("pdf:page:N"), because a page is
// the only structural boundary a PDF guarantees. Chapter shape comes from
// the outline when the file has one (pdfcpu resolves destinations to page
// numbers); without one, the page boundaries themselves are the chapter
// marks, exactly as a MOBI with no TOC falls back to its pagebreaks.
//
// Reading order, dehyphenation and running-head stripping are ours, applied
// here in the parser and never inside the shared normalizer. Dehyphenation
// especially: line-end "hyphen-ated" splits are rejoined into one word
// before books.Normalize ever runs, because its dash rule would otherwise
// freeze the split into the canonical text and every offset after it.
//
// Two libraries sit underneath, both pure Go and GPL-compatible:
//
//   - github.com/ledongthuc/pdf (BSD-3, the maintained rsc.io/pdf fork)
//     reads the object model and decodes content-stream text into
//     positioned runs — font encodings, ToUnicode CMaps, embedded TrueType
//     charmaps. Unmappable glyphs surface as U+FFFD, which is the signal
//     the quality gate keys on.
//   - github.com/pdfcpu/pdfcpu (Apache-2.0) reads outlines only. pdfcpu
//     cannot extract text at all — it is a manipulation toolkit without a
//     text layer — but its bookmark reader resolves destinations (including
//     named ones) to page numbers, which the text reader does not expose.
//
// DRM is refused whole: any /Encrypt dictionary is an ErrDRM, including
// owner-password-only "restrictions" files that decrypt with an empty user
// password. Those open freely and are refused anyway — DRM-free by
// decision, no half-support.
package pdf

import (
	"errors"
	"fmt"
	"io"
	"strings"

	gopdf "github.com/ledongthuc/pdf"

	"github.com/collinpendleton/backhog/api/internal/books/epub"
)

// ErrDRM reports a PDF carrying an /Encrypt dictionary — whether it demands
// a user password or only carries owner-password restrictions. The file is
// skipped and reported (the caller maps it to the drm_pdf skip reason),
// never worked around.
var ErrDRM = errors.New("pdf: file is encrypted (has an /Encrypt dictionary)")

// Classification is the quality gate's verdict on a PDF's text layer. A
// plausible-wrong extraction silently poisons search, alignment and the
// knowledge layer, so "did it parse" and "can its text be trusted" are
// different questions and only the first one has an error code.
type Classification string

const (
	// TextNative means the text layer extracted cleanly and the canonical
	// text may be built from it.
	TextNative Classification = "text-native"
	// ImageNative means the file's content is its pages, not its (absent or
	// broken) text layer: comics, scans, garbage-extraction victims. Never
	// store a canonical text; route down the paged path.
	ImageNative Classification = "image-native"
	// Corrupt means the file could not be read structurally.
	Corrupt Classification = "corrupt"
)

// NotTextError reports a parse that cannot yield a trustworthy text layer.
// Class says which skip path the caller takes; Reason is human-readable
// detail for the skip report.
type NotTextError struct {
	Class  Classification
	Reason string
}

func (e *NotTextError) Error() string {
	return fmt.Sprintf("pdf: %s: %s", e.Class, e.Reason)
}

// ParseResult carries the spine document together with the quality gate's
// verdict, for callers that need to route on the classification rather than
// only the error.
type ParseResult struct {
	Doc *epub.Document
	// Class is TextNative on the success path.
	Class Classification
	// Reason explains a non-text-native classification; empty when clean.
	Reason string
}

// Parse reads a PDF held in r and returns its spine structure. A DRM
// file fails with ErrDRM; a structurally broken one with a *NotTextError
// of class corrupt; a file whose text layer is absent or garbage fails
// with a *NotTextError of class image-native — never a plausible-wrong
// document. Only a clean, text-native parse returns a document.
func Parse(r io.ReaderAt, size int64) (*epub.Document, error) {
	res, err := ParseWithQuality(r, size)
	if err != nil {
		return nil, err
	}
	if res.Class != TextNative {
		return nil, &NotTextError{Class: res.Class, Reason: res.Reason}
	}
	return res.Doc, nil
}

// ParseWithQuality is Parse with the classification surfaced. For an
// image-native file it returns both a result (the parsed document, whose
// pages carry their images but no trustworthy blocks) and the *NotTextError
// — the error is the gate's verdict, the result is the inventory a paged
// reader will want. DRM and corrupt outcomes return a nil result.
func ParseWithQuality(r io.ReaderAt, size int64) (res *ParseResult, err error) {
	// Both underlying libraries signal malformed input with panics as much
	// as with errors; an untrusted file must never take the ingester down,
	// so everything below runs under one net.
	defer func() {
		if x := recover(); x != nil {
			res, err = nil, &NotTextError{Class: Corrupt, Reason: fmt.Sprintf("malformed PDF: %v", x)}
		}
	}()

	rd, err := gopdf.NewReader(r, size)
	if err != nil {
		if isEncryptionError(err) {
			return nil, fmt.Errorf("%w: %s", ErrDRM, strings.TrimPrefix(err.Error(), "encrypted PDF: "))
		}
		return nil, &NotTextError{Class: Corrupt, Reason: err.Error()}
	}
	// A file that opened with an empty user password still carries DRM:
	// owner-password "restrictions" decrypt transparently, and by decision
	// they are refused exactly like locked files.
	if !rd.Trailer().Key("Encrypt").IsNull() {
		return nil, fmt.Errorf("%w: encryption dictionary present (restrictions)", ErrDRM)
	}

	pageCount := rd.NumPage()
	if pageCount == 0 {
		return nil, &NotTextError{Class: Corrupt, Reason: "document has no pages"}
	}

	ex := extractBook(rd, pageCount)
	docs := buildDocs(rd, ex)
	rejoinCrossPageHyphens(docs)
	hasOutline := !rd.Trailer().Key("Root").Key("Outlines").IsNull()
	report := attachTOC(r, size, hasOutline, docs)

	class, reason := ex.gate.verdict()
	if class != TextNative {
		return &ParseResult{
				Doc:    &epub.Document{Docs: docs, TOC: report},
				Class:  class,
				Reason: reason,
			},
			&NotTextError{Class: class, Reason: reason}
	}
	return &ParseResult{Doc: &epub.Document{Docs: docs, TOC: report}, Class: TextNative}, nil
}

// isEncryptionError recognizes the reader's encrypted-file failures, which
// arrive as ErrInvalidPassword or as version-specific messages.
func isEncryptionError(err error) bool {
	return errors.Is(err, gopdf.ErrInvalidPassword) ||
		strings.Contains(err.Error(), "encrypted PDF")
}
