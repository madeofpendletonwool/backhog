// Package fixtures embeds the synthetic ebook fixtures shared by tests
// across packages — the media scanner and the books ingester both need the
// same bytes, and one copy under one name beats duplicates that can drift.
// Import it from tests only: nothing in the API itself reads these.
//
// Every file here is an original synthetic work generated deterministically
// by mobi-go's fixture generator (github.com/madeofpendletonwool/mobi-go,
// tools/fixgen) at v0.1.0; no real book's bytes are committed. They are
// byte-identical to that repo's own test corpus, which is verified against
// the KindleUnpack oracle — the same fixture that library tests its parser
// with is the one these tests parse.
package fixtures

import _ "embed"

// MOBI6Palmdoc is a DRM-free MOBI6 book: PalmDOC-compressed multi-record
// text with trailing bookkeeping bytes, full EXTH metadata, image
// resources, and a hierarchical INDX NCX table of contents.
//
//go:embed mobi/mobi6-palmdoc.mobi
var MOBI6Palmdoc []byte

// AZW3KF8 is a DRM-free pure-KF8 (AZW3) book: reassembled XHTML sections
// with fragments cut inside tags, a non-linear section, an NCX TOC, and a
// guide index.
//
//go:embed mobi/azw3-kf8.azw3
var AZW3KF8 []byte

// MOBI6DRM is a MOBI6 book carrying PalmDOC encryption — mobi.Open refuses
// it whole with ErrDRM, which is exactly what the tests need.
//
//go:embed mobi/mobi6-drm.mobi
var MOBI6DRM []byte
