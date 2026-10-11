-- +goose Up

-- E-reader devices (MAD-828, MAD-829): what a KOReader or CrossPoint device
-- needs to act as its user and to name the book it is reading.
--
-- kosync_key is the MD5 hex of a device token's plaintext. The KOReader sync
-- protocol authenticates with x-auth-user plus x-auth-key = md5(password),
-- and KOReader sends nothing else — so a device that uses its token as the
-- sync password presents md5(token), which the SHA-256 token_hash cannot be
-- checked against. MD5 is the protocol's own choice, not ours, and it is
-- tolerable here because the input is a 256-bit random secret rather than a
-- password: there is no dictionary to run against it. The column is NULL
-- for every token not minted as a device, so only device tokens answer to
-- the x-auth headers at all.
ALTER TABLE api_tokens ADD COLUMN kosync_key TEXT;

CREATE UNIQUE INDEX idx_api_tokens_kosync_key
    ON api_tokens(kosync_key) WHERE kosync_key IS NOT NULL;

-- kosync_documents maps the document ids a sync client computes to the
-- library entry they name. A KOReader-protocol client never says which book
-- it is reading; it sends a hash, either of the file's bytes ('binary', the
-- partial MD5 over 1 KiB samples) or of the filename it saved the book under
-- ('filename', one row per CrossPoint naming format). Rows are computed from
-- the exact file the download route serves and carry that file's (size,
-- mtime) identity, so a re-attached or replaced EPUB is recomputed rather
-- than matched against stale bytes.
--
-- Keyed per entry, not per file: two users holding the same EPUB share the
-- binary hash, and each of them must resolve it to their own entry.
CREATE TABLE kosync_documents (
    doc_hash        TEXT NOT NULL,
    entry_id        TEXT NOT NULL REFERENCES library_entries(id) ON DELETE CASCADE,
    kind            TEXT NOT NULL CHECK (kind IN ('binary','filename')),
    -- The CrossPoint OpdsFilenameFormat a filename hash was built with
    -- ('author_title', 'title_author', 'title_only'); '' for binary.
    filename_format TEXT NOT NULL DEFAULT '',
    media_file_id   INTEGER NOT NULL REFERENCES media_files(id) ON DELETE CASCADE,
    file_size       INTEGER NOT NULL,
    file_mtime      INTEGER NOT NULL,
    computed_at     TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (doc_hash, entry_id)
);

CREATE INDEX idx_kosync_documents_entry ON kosync_documents(entry_id);

-- +goose Down

DROP TABLE IF EXISTS kosync_documents;
DROP INDEX IF EXISTS idx_api_tokens_kosync_key;
ALTER TABLE api_tokens DROP COLUMN kosync_key;
