-- +goose Up

-- The library-wide text search index (MAD-470): SQLite FTS5 over the
-- canonical chapter text, so "where did I read that line?" can be asked
-- across the whole shelf instead of one book at a time.
--
-- One row per epub_chapters row — the issue's chosen granularity. The
-- chapter ranges partition the canonical text exactly, so slicing the text
-- by [char_start, char_end) and inserting each slice indexes the whole book
-- with no gaps and no overlaps. The canonical text itself stays a file (see
-- 00013's rationale); this table is the *index*, written on ingest and
-- re-parse and read only by search, never by the reader.
--
-- The tokenizer is unicode61 with diacritics kept, because the canonical
-- text keeps them (booktext.Normalize only folds case, quotes, dashes and
-- punctuation) and the query is folded through the same Normalize: both
-- sides arrive already folded, and the tokenizer must not fold the text a
-- second time into a shape the in-book searcher — which re-places every
-- candidate hit exactly — cannot reproduce. CJK runs are the one exception:
-- unicode61 makes a whole run one token, so the indexer and the query
-- builder both space-separate CJK runes before they reach the table
-- (store.FoldForFTS), which keeps phrase search working through Japanese
-- and Chinese prose.

CREATE VIRTUAL TABLE book_text_fts USING fts5(
    body,
    book_id UNINDEXED,
    media_file_id UNINDEXED,
    tokenize = "unicode61 remove_diacritics 0"
);

-- +goose Down

DROP TABLE IF EXISTS book_text_fts;
