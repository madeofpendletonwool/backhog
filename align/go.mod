module github.com/collinpendleton/backhog/align

go 1.26.4

// The worker normalizes its transcripts with the API's own pinned
// normalizer rather than a copy of it: booktext.Normalize is the single
// definition of the canonical text's rules, and an alignment computed
// against rules that had drifted from the EPUB's would be silently,
// unrepairably wrong. The replace keeps that a local source dependency —
// nothing is published, and align/Dockerfile builds from the repo root so
// ../api is inside the build context.
require github.com/collinpendleton/backhog/api v0.0.0

require golang.org/x/text v0.41.0 // indirect

replace github.com/collinpendleton/backhog/api => ../api
