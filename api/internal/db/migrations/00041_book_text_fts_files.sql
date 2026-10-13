-- +goose Up

-- Which media files have rows in book_text_fts. The FTS table keys its rows
-- by media_file_id, but as an UNINDEXED column: FTS5 can only answer
-- "WHERE media_file_id = ?" by reading every row's body. The startup index
-- walk asked that once per library file, so on a real library it held the
-- single database connection for minutes and the healthcheck timed out
-- behind it. This ordinary table answers the same question from its
-- primary key, and the cascade drops a row with its media file.
CREATE TABLE book_text_fts_files (
    media_file_id INTEGER PRIMARY KEY REFERENCES media_files(id) ON DELETE CASCADE
);

-- One pass over the existing index, paid once here instead of per file per
-- boot.
INSERT OR IGNORE INTO book_text_fts_files (media_file_id)
SELECT DISTINCT CAST(f.media_file_id AS INTEGER)
FROM book_text_fts f
WHERE CAST(f.media_file_id AS INTEGER) IN (SELECT id FROM media_files);

-- +goose Down

DROP TABLE IF EXISTS book_text_fts_files;
