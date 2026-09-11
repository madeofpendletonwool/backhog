module github.com/collinpendleton/backhog/ocr

go 1.26.4

// The OCR lettering worker. Unlike the alignment worker it imports
// nothing from ../api: it reads page images over the API's own /internal
// streaming endpoint (the same companion-or-extract bytes the paged
// reader serves) and hands raw lettering back for the API to store, so
// the pinned normalizer never needs to cross this boundary — the API
// folds the corpus at search time with its own booktext.Normalize.
