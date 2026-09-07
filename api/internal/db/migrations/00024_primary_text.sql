-- +goose Up

-- One designated canonical text per book.
--
-- A book can legitimately have more than one text-side file attached: the
-- same NAS directory routinely holds "X.epub" and "X.mobi", and owning both
-- is a fact worth recording. But every position in the Books arena — reader
-- location, alignment anchor, page anchor, percent complete — is a byte
-- offset into ONE canonical text, so exactly one of those files has to be
-- the one the offsets mean.
--
-- Until now that choice was implicit: four separate queries each said
-- "ORDER BY id LIMIT 1" with slightly different filters (present-only in
-- EpubMediaFileForBook, present-and-parsed in the page-anchor seed, no
-- filter at all in the achievement and insight sizing). With one file per
-- book they always agreed. With two they can disagree — and the day the
-- epub is briefly missing, the reader silently reads the mobi's text while
-- percent_complete is still computed against the epub's char_count.
--
-- is_primary_text makes the choice explicit and stable. The partial unique
-- index is what enforces it: at most one primary text file per book, checked
-- by SQLite rather than by whichever query happens to run.
--
-- Audio rows are untouched — the index's WHERE clause scopes it to the text
-- side, and an audiobook's ordering is track_number's job.
ALTER TABLE media_files ADD COLUMN is_primary_text INTEGER NOT NULL DEFAULT 0
    CHECK (is_primary_text IN (0, 1));

CREATE UNIQUE INDEX idx_media_files_primary_text
    ON media_files(book_id)
    WHERE kind = 'epub' AND is_primary_text = 1;

-- The backfill deliberately reproduces the OLD resolver's answer rather than
-- applying the new format preference (epub over azw3 over azw over mobi).
-- Any offset already stored in book_progress or page_anchors was measured
-- against whichever text EpubMediaFileForBook was handing out yesterday —
-- lowest id among the present rows — so promoting a different file here
-- would move every existing reader's position by the difference between two
-- canonicalizations. Format preference applies to attachments made from here
-- on; existing books keep the text their offsets already refer to, and
-- changing one is an explicit switch that migrates the offset with it.
--
-- COALESCE(missing_at IS NULL, ...) is not needed: ordering by the flag puts
-- present rows first and falls back to a missing one only when every file of
-- the book is missing, which is the same fallback the resolver now makes.
UPDATE media_files SET is_primary_text = 1 WHERE id IN (
    SELECT id FROM (
        SELECT id, ROW_NUMBER() OVER (
                   PARTITION BY book_id
                   ORDER BY (missing_at IS NOT NULL), id
               ) AS rn
        FROM media_files
        WHERE kind = 'epub' AND book_id IS NOT NULL
    )
    WHERE rn = 1
);

-- +goose Down

DROP INDEX IF EXISTS idx_media_files_primary_text;

ALTER TABLE media_files DROP COLUMN is_primary_text;
