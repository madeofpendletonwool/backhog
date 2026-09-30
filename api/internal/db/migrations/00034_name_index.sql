-- +goose Up

-- The name index (MAD-670): the classic back-of-the-book index, built
-- deterministically on ingest — no model, just heuristics over the display
-- text whose answers are canonical offsets like every other address in the
-- arena. It answers "where has this name come up so far?" and "when did I
-- first meet this character?" with the same spoiler clamp every read path
-- speaks: a name first appearing past the reading position does not exist
-- yet, because the occurrences past the bound are dropped before any
-- answer is built.
--
-- One row per occurrence, keyed by media file like the FTS index, so a
-- re-parse (same epub_texts row) and a detach land in place exactly the way
-- book_text_fts rows do. `name` is the folded identity key (booktext.
-- Normalize of the display form) so "Elizabeth" and "ELIZABETH" are one
-- name, while `display` keeps the book's own capitals for the list. The
-- chapter is derived at read time from char_start against the chapter
-- ranges, which partition the text exactly — storing it would only
-- denormalize a fact the parse can always restate.
--
-- Currency lives on epub_texts.names_version: '' until the current
-- extractor's pass has landed (a book whose heuristics yield zero names is
-- legitimately empty, so row presence alone cannot mean "indexed"). A
-- re-parse resets it to '' and the next ensure re-indexes.

CREATE TABLE name_occurrences (
    id            INTEGER PRIMARY KEY,
    media_file_id INTEGER NOT NULL REFERENCES media_files(id) ON DELETE CASCADE,
    name          TEXT NOT NULL,
    display       TEXT NOT NULL,
    char_start    INTEGER NOT NULL CHECK (char_start >= 0),
    char_end      INTEGER NOT NULL CHECK (char_end > char_start)
);

CREATE INDEX idx_name_occurrences_file ON name_occurrences(media_file_id, char_start);
CREATE INDEX idx_name_occurrences_name ON name_occurrences(media_file_id, name);

ALTER TABLE epub_texts ADD COLUMN names_version TEXT NOT NULL DEFAULT '';

-- Names a reader has hidden on one book: the index is heuristics, and a
-- false positive ("Chapter", a stray capitalization) is the reader's call
-- to remove. Per user per book, keyed by the folded name; stored rather
-- than regenerated, because the extractor's next pass must not resurrect
-- what a person has judged wrong.
CREATE TABLE hidden_book_names (
    user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    book_id    TEXT NOT NULL REFERENCES books(id) ON DELETE CASCADE,
    name       TEXT NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (user_id, book_id, name)
);

-- +goose Down

DROP TABLE IF EXISTS hidden_book_names;
ALTER TABLE epub_texts DROP COLUMN names_version;
DROP TABLE IF EXISTS name_occurrences;
