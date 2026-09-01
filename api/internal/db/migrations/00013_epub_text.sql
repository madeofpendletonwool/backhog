-- +goose Up

-- The canonical text index for the Books arena. Every position in the arena —
-- reader location, alignment anchor, OCR page anchor — is a byte offset into
-- one normalized canonical text produced by the EPUB parser
-- (internal/books/epub) and the pinned books.Normalize rules. This migration
-- stores the index; the text itself is a file, not a BLOB (see below).

-- epub_texts is one row per parsed EPUB media_file. parser_version is bumped
-- whenever the normalizer or extractor changes behaviour: any offset stored
-- against an old version is stale by definition, and EnsureForMediaFile
-- re-parses on mismatch so the canonical text and every derived offset are
-- rebuilt together. normalized_sha256 identifies the exact canonical text the
-- offsets refer to.
-- char_count/word_count are in bytes / whitespace-separated tokens of the
-- canonical text.
CREATE TABLE epub_texts (
    id                 TEXT PRIMARY KEY,
    media_file_id      INTEGER NOT NULL REFERENCES media_files(id) ON DELETE CASCADE,
    char_count         INTEGER NOT NULL CHECK (char_count >= 0),
    word_count         INTEGER NOT NULL CHECK (word_count >= 0),
    normalized_sha256  TEXT NOT NULL,
    parsed_at          TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    parser_version     TEXT NOT NULL,
    UNIQUE (media_file_id)
);

-- epub_chapters is the spine index: one row per spine document, in reading
-- order. [char_start, char_end) partitions [0, char_count) exactly —
-- contiguous, no gaps, no overlaps (asserted by a property test). A document
-- whose trailing separator space is owned by the range keeps it inside
-- char_end; empty (image-only) documents have char_start == char_end.
-- title/depth come from the NCX/nav TOC entry targeting the document.
CREATE TABLE epub_chapters (
    id           TEXT PRIMARY KEY,
    epub_text_id TEXT NOT NULL REFERENCES epub_texts(id) ON DELETE CASCADE,
    spine_index  INTEGER NOT NULL,
    href         TEXT NOT NULL,
    title        TEXT NOT NULL DEFAULT '',
    char_start   INTEGER NOT NULL CHECK (char_start >= 0),
    char_end     INTEGER NOT NULL CHECK (char_end >= char_start),
    depth        INTEGER NOT NULL DEFAULT 0,
    UNIQUE (epub_text_id, spine_index)
);

CREATE INDEX idx_epub_chapters_text ON epub_chapters(epub_text_id, spine_index);

-- The canonical text itself lives as a companion FILE, not a row: novels run
-- to multi-megabyte strings, and SQLite with a single connection would drag
-- that through the WAL on every read while ranged endpoints only ever need a
-- slice. The reader mmaps nothing special — it is a plain UTF-8 text file at
-- {EPUB_TEXT_DIR}/{id}.txt, next to the database (same volume), with only the
-- pointer in this table. The block-offset sidecar {id}.blocks.json sits
-- beside it and carries the char-offset ↔ (href, block) index.

-- +goose Down

DROP TABLE IF EXISTS epub_chapters;
DROP TABLE IF EXISTS epub_texts;
