-- +goose NO TRANSACTION

-- +goose Up

-- A PDF is a printing: its per-page text ranges are the exact counterpart
-- of a paper copy's page map, known at parse time instead of guessed from
-- a catalogue's page count. Two structures carry that:

-- pdf_pages holds the per-page [char_start, char_end) ranges of a
-- text-native PDF's canonical text — one level finer than epub_chapters,
-- which stays the reader-facing partition. It follows the epub_chapters
-- precedent for multi-row ranged data (a table, not a sidecar: the
-- page-map seed reads it by key, and it must be replaced atomically with
-- the parse it belongs to). Keyed by media file like pdf_files; the rows
-- are rewritten wholesale on every re-parse, so stale ranges can never
-- outlive the text they were measured against.
-- +goose StatementBegin
CREATE TABLE pdf_pages (
    media_file_id INTEGER NOT NULL REFERENCES media_files(id) ON DELETE CASCADE,
    page_number   INTEGER NOT NULL CHECK (page_number > 0),
    char_start    INTEGER NOT NULL CHECK (char_start >= 0),
    char_end      INTEGER NOT NULL CHECK (char_end >= char_start),
    PRIMARY KEY (media_file_id, page_number)
);
-- +goose StatementEnd

-- page_anchors learns a third source: 'pdf', a seed written from a
-- text-native PDF's own pages. A seed is not a scan — nobody has looked
-- at this copy's paper — so it lands at the catalogue stretch's low
-- confidence class and yields to any real scan. SQLite cannot alter a
-- CHECK constraint in place, so the table is rebuilt (the 00004 recipe:
-- foreign keys off, copy into a new table, swap the names, put the index
-- back — outside goose's transaction because PRAGMA foreign_keys is a
-- no-op inside one).
PRAGMA foreign_keys=OFF;

-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS page_anchors_new (
    physical_copy_id TEXT NOT NULL REFERENCES physical_copies(id) ON DELETE CASCADE,
    printed_page     INTEGER NOT NULL CHECK (printed_page > 0),
    char_offset      INTEGER NOT NULL CHECK (char_offset >= 0),
    source           TEXT NOT NULL CHECK (source IN ('ocr','manual','pdf')),
    confidence       REAL NOT NULL DEFAULT 1 CHECK (confidence >= 0 AND confidence <= 1),
    created_at       TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (physical_copy_id, printed_page)
);
-- +goose StatementEnd

INSERT INTO page_anchors_new (physical_copy_id, printed_page, char_offset, source, confidence, created_at)
SELECT physical_copy_id, printed_page, char_offset, source, confidence, created_at FROM page_anchors;

DROP TABLE IF EXISTS page_anchors;

ALTER TABLE page_anchors_new RENAME TO page_anchors;

CREATE INDEX IF NOT EXISTS idx_page_anchors_offset ON page_anchors(physical_copy_id, char_offset);

PRAGMA foreign_keys=ON;

-- +goose Down

PRAGMA foreign_keys=OFF;

-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS page_anchors_old (
    physical_copy_id TEXT NOT NULL REFERENCES physical_copies(id) ON DELETE CASCADE,
    printed_page     INTEGER NOT NULL CHECK (printed_page > 0),
    char_offset      INTEGER NOT NULL CHECK (char_offset >= 0),
    source           TEXT NOT NULL CHECK (source IN ('ocr','manual')),
    confidence       REAL NOT NULL DEFAULT 1 CHECK (confidence >= 0 AND confidence <= 1),
    created_at       TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (physical_copy_id, printed_page)
);
-- +goose StatementEnd

-- Seeds have no equivalent in the old schema; they are derived rows a
-- re-seed can regrow, so they are dropped rather than folded into a
-- source they are not.
INSERT INTO page_anchors_old (physical_copy_id, printed_page, char_offset, source, confidence, created_at)
SELECT physical_copy_id, printed_page, char_offset, source, confidence, created_at
FROM page_anchors WHERE source <> 'pdf';

DROP TABLE IF EXISTS page_anchors;

ALTER TABLE page_anchors_old RENAME TO page_anchors;

CREATE INDEX IF NOT EXISTS idx_page_anchors_offset ON page_anchors(physical_copy_id, char_offset);

PRAGMA foreign_keys=ON;

DROP TABLE IF EXISTS pdf_pages;
