-- +goose Up

-- Where a chapter's title came from, and how good the book's own table of
-- contents was.
--
-- The arena used to store one chapter row per spine document, titled by the
-- first TOC entry pointing at that document and left blank otherwise. A
-- blank one was rendered "Section 7" — numbered by raw spine position — and
-- across this library a third of all chapter rows were blank. The reasons
-- were never surfaced, so a reader opening The Stand saw ninety-four
-- numbered sections with no indication that the book's NCX contains exactly
-- one navPoint labelled "Start".
--
-- Two things are recorded now.
--
-- title_source says who named the chapter. A title lifted from a heading in
-- the markup is worth having, but it is not the same claim as one the book's
-- TOC made, and a title guessed from the shape of an opening line is weaker
-- still. Storing the provenance lets the reader mark an inferred title as
-- inferred instead of presenting every title as equally authoritative.
--
-- The toc_* columns on epub_texts record what happened when the navigation
-- document was read: which kind it was, how many entries it yielded, and the
-- error if it could not be parsed at all. That last one existed only as a
-- discarded local variable before — one stray "&hellip;" in an NCX made
-- Go's XML decoder reject the document, and the whole book quietly lost
-- every chapter title with nothing written down anywhere.
ALTER TABLE epub_chapters ADD COLUMN title_source TEXT NOT NULL DEFAULT 'none'
    CHECK (title_source IN ('toc', 'heading', 'text', 'none'));

-- toc_source is 'nav' (EPUB 3), 'ncx' (EPUB 2), 'kf8-ncx', 'mobi-toc',
-- 'mobi-pagebreak', or '' when the file declared no navigation at all.
ALTER TABLE epub_texts ADD COLUMN toc_source TEXT NOT NULL DEFAULT '';

-- How many usable entries the navigation document yielded. Zero alongside an
-- empty toc_error means the book really does have an empty TOC.
ALTER TABLE epub_texts ADD COLUMN toc_entries INTEGER NOT NULL DEFAULT 0
    CHECK (toc_entries >= 0);

-- Why the TOC could not be read, empty when it could.
ALTER TABLE epub_texts ADD COLUMN toc_error TEXT NOT NULL DEFAULT '';

-- +goose Down

ALTER TABLE epub_texts DROP COLUMN toc_error;
ALTER TABLE epub_texts DROP COLUMN toc_entries;
ALTER TABLE epub_texts DROP COLUMN toc_source;
ALTER TABLE epub_chapters DROP COLUMN title_source;
