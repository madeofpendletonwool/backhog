-- +goose Up

-- OCR lettering search for comics: the second corpus.
--
-- Image-native books (comics, manga, picture books, scans) have no canonical
-- text — that is the whole paged position model — but their pages carry drawn
-- lettering, and an optional OCR worker container can read it into a
-- SEARCH-ONLY corpus. Two structures carry that, and both are deliberately
-- outside the canonical tables:
--
--   ocr_jobs   the queue, the alignment_jobs pattern applied a second time:
--              claim, heartbeat, reclaim, one active job per media file.
--   ocr_pages  the corpus, one row per page keyed (media_file_id, page_number)
--              — the extraction-ledger pattern: each row is pinned to the
--              page image's sha256 and the OCR pipeline version, so a re-run
--              skips every page whose image did not change. Nothing about it
--              is canonical; no epub_texts row can ever point at it, which is
--              what keeps the position axis, alignment, passage matching and
--              any future knowledge layer structurally unable to load it.
CREATE TABLE ocr_jobs (
    id             TEXT PRIMARY KEY,
    entry_id       TEXT NOT NULL REFERENCES library_entries(id) ON DELETE CASCADE,
    media_file_id  INTEGER NOT NULL REFERENCES media_files(id) ON DELETE CASCADE,
    -- parser_version pins the classification the job was enqueued against; a
    -- re-classified file (re-parse, moved parser) invalidates the job.
    parser_version TEXT NOT NULL,
    state          TEXT NOT NULL DEFAULT 'queued'
        CHECK (state IN ('queued','claimed','ocring','ready','low_confidence','failed')),
    progress       REAL NOT NULL DEFAULT 0 CHECK (progress >= 0 AND progress <= 1),
    stage_detail   TEXT NOT NULL DEFAULT '',
    error          TEXT,
    coverage       REAL NOT NULL DEFAULT 0 CHECK (coverage >= 0 AND coverage <= 1),
    mean_confidence REAL NOT NULL DEFAULT 0 CHECK (mean_confidence >= 0 AND mean_confidence <= 1),
    model          TEXT NOT NULL DEFAULT '',
    attempts       INTEGER NOT NULL DEFAULT 0,
    claimed_by     TEXT,
    claimed_at     TIMESTAMP,
    heartbeat_at   TIMESTAMP,
    created_at     TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at     TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- At most one active job per media file: the corpus belongs to the file, not
-- the entry, so hammering the button (or a second reader of the same comic)
-- cannot stack duplicate runs. The alignment queue's per-entry twin.
CREATE UNIQUE INDEX idx_ocr_jobs_active
    ON ocr_jobs(media_file_id) WHERE state IN ('queued','claimed','ocring');

CREATE TABLE ocr_pages (
    media_file_id  INTEGER NOT NULL REFERENCES media_files(id) ON DELETE CASCADE,
    page_number    INTEGER NOT NULL CHECK (page_number > 0),
    -- image_sha256 pins the page image the text was read from: the ledger
    -- key that makes re-runs idempotent page by page.
    image_sha256   TEXT NOT NULL,
    ocr_version    TEXT NOT NULL,
    -- text is the worker's raw reading (tesseract output, line-joined); it is
    -- folded by the searcher at query time, never stored normalized.
    text           TEXT NOT NULL DEFAULT '',
    mean_confidence REAL NOT NULL DEFAULT 0 CHECK (mean_confidence >= 0 AND mean_confidence <= 1),
    updated_at     TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (media_file_id, page_number)
);

-- +goose Down

DROP TABLE IF EXISTS ocr_pages;
DROP TABLE IF EXISTS ocr_jobs;
