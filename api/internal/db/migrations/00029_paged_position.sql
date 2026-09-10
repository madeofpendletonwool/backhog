-- +goose Up

-- The paged position model: books that have no text.
--
-- Comics, manga, picture books and scans have no canonical text — the
-- parser's quality gate classifies them image-native — so invariant 5's
-- "one position, and it's the char offset" cannot apply without faking one.
-- Their honest position is a page index, and the model says so out loud
-- rather than leaving a char_count of 0 to be mistranslated.
--
-- The shape follows the raw_audio_* precedent: one row, one honest
-- exception, flagged. book_progress gains a nullable page axis and the flag
-- naming which axis the row's position lives on, instead of a second table
-- every position-reading path would have to remember to check. In text mode
-- page_index is NULL and char_offset is the truth, byte-identically to
-- every row that existed before this migration; in page mode page_index is
-- the truth and char_offset stays 0 — no fake char deltas, ever. The
-- cross-column pairing (a page row carries a page, a text row carries no
-- page) is enforced by the store layer before it reaches SQLite, exactly
-- the way the raw_audio half-pair is.
ALTER TABLE book_progress ADD COLUMN position_mode TEXT NOT NULL DEFAULT 'text'
    CHECK (position_mode IN ('text', 'page'));
ALTER TABLE book_progress ADD COLUMN page_index INTEGER
    CHECK (page_index IS NULL OR page_index >= 0);

-- reading_sessions logs pages turned the same way it logs chars advanced:
-- a paged book's sessions carry pages_turned > 0 and chars_advanced 0,
-- because there is no text axis to advance on. mode stays 'read' — turning
-- pages is reading.
ALTER TABLE reading_sessions ADD COLUMN pages_turned INTEGER NOT NULL DEFAULT 0
    CHECK (pages_turned >= 0);

-- pdf_files is the classification home: the quality gate's verdict on a
-- PDF, persisted per media file so every consumer — the position endpoints
-- deciding which axis a book answers in, the sizing queries counting a
-- 32-page picture book as 32 pages — reads a stored fact instead of
-- re-parsing. One row per parsed PDF, keyed by media_file_id:
--
--   classification  text-native or image-native. drm and corrupt never get
--                   a row — they are refused whole at parse time.
--   page_count      the file's own page count, beside char_count/word_count
--                   the way epub_texts holds a text's length. Sizing reads
--                   it for the designated primary when the verdict is
--                   image-native; a text-native PDF's canonical text wins
--                   instead, exactly as for any other container.
--   has_text_layer  whether any page yielded text runs at all — the raw
--                   signal under the verdict, kept for honesty in the UI.
--   reason          the verdict's human-readable detail.
--   parser_version  re-classification follows the same rule epub_texts
--                   does: bump the version and the file is re-parsed.
CREATE TABLE pdf_files (
    id              TEXT PRIMARY KEY,
    media_file_id   INTEGER NOT NULL UNIQUE REFERENCES media_files(id) ON DELETE CASCADE,
    classification  TEXT NOT NULL CHECK (classification IN ('text-native', 'image-native')),
    page_count      INTEGER NOT NULL CHECK (page_count > 0),
    has_text_layer  INTEGER NOT NULL CHECK (has_text_layer IN (0, 1)),
    reason          TEXT NOT NULL DEFAULT '',
    classified_at   TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    parser_version  TEXT NOT NULL
);

-- +goose Down

DROP TABLE IF EXISTS pdf_files;

ALTER TABLE reading_sessions DROP COLUMN pages_turned;
ALTER TABLE book_progress DROP COLUMN page_index;
ALTER TABLE book_progress DROP COLUMN position_mode;
