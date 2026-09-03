-- +goose NO TRANSACTION

-- +goose Up

-- Three format decisions, made explicit in the schema.
--
-- 1. media_files.meta_version: the version of the metadata extractor that
--    produced container_metadata. The scanner's fast path skips any file
--    whose (size, mtime) is unchanged and so would never re-read an already
--    inventoried EPUB — meaning a new extractor would only ever apply to
--    files that happened to change afterwards. Making the fast path also
--    require a version match turns a bump in the Go constant into exactly
--    one re-read per file, then silence. This mirrors books.ParserVersion,
--    which guards the canonical text the same way. A plain ADD COLUMN: no
--    CHECK moves, so no table rebuild is warranted.
--
-- 2. media_skipped.reason gains two values. 'unsupported_extension' was
--    doing the work of three different statements — "this is DRM we refuse",
--    "this is a format we chose not to parse" and "this is not a book" all
--    rendered as one shrug. SQLite cannot alter a CHECK constraint, so this
--    is the rebuild recipe from 00015.
--
--    'format_unhandled' is a recognised Kindle format (.mobi, .azw, .azw3).
--    Reading one means PalmDOC LZ77 plus HUFF/CDIC Huffman decompression
--    plus KF8 fragment reassembly, and no pure-Go reader for any of that
--    exists to lean on. That is a library in its own right; until it exists
--    the honest answer is to name the format and point at the EPUB of the
--    same book, not to half-support it.
--
--    'sidecar_metadata' is an .opf. Eighteen of them in a Calibre library
--    are not eighteen missing books — they are the answer key. The skip row
--    keeps the file accounted for while media_sidecars holds what it said.
--
-- 3. media_sidecars: the parsed .opf metadata, keyed by the file it came
--    from. Replaced per root on each scan like media_skipped, because it
--    describes what is on disk right now and carries no user state. It is
--    read back in one query at match time — the matcher touches no
--    filesystem, and the candidates endpoint is polled every 1.5s while a
--    scan runs.
ALTER TABLE media_files ADD COLUMN meta_version INTEGER NOT NULL DEFAULT 0;

PRAGMA foreign_keys=OFF;

-- +goose StatementBegin
CREATE TABLE media_skipped_new (
    id         INTEGER PRIMARY KEY,
    root       TEXT NOT NULL,
    path       TEXT NOT NULL,
    ext        TEXT NOT NULL,
    reason     TEXT NOT NULL CHECK (reason IN ('unsupported_extension','drm_epub','format_unhandled','sidecar_metadata')),
    size_bytes INTEGER NOT NULL,
    mtime      INTEGER NOT NULL,
    seen_at    TIMESTAMP NOT NULL,
    UNIQUE (root, path)
);
-- +goose StatementEnd

INSERT INTO media_skipped_new (id, root, path, ext, reason, size_bytes, mtime, seen_at)
SELECT id, root, path, ext, reason, size_bytes, mtime, seen_at FROM media_skipped;

DROP TABLE media_skipped;

ALTER TABLE media_skipped_new RENAME TO media_skipped;

CREATE INDEX idx_media_skipped_root ON media_skipped(root);

-- +goose StatementBegin
CREATE TABLE media_sidecars (
    id           INTEGER PRIMARY KEY,
    root         TEXT NOT NULL,
    path         TEXT NOT NULL,
    title        TEXT NOT NULL DEFAULT '',
    author       TEXT NOT NULL DEFAULT '',
    series       TEXT NOT NULL DEFAULT '',
    series_index TEXT NOT NULL DEFAULT '',
    language     TEXT NOT NULL DEFAULT '',
    -- Normalized and validated before it is written, so a non-empty value is
    -- safe to hand straight to an ISBN lookup.
    isbn         TEXT NOT NULL DEFAULT '',
    -- An Open Library work key (OL12345W): an exact identity, not a query.
    work_key     TEXT NOT NULL DEFAULT '',
    seen_at      TIMESTAMP NOT NULL,
    UNIQUE (root, path)
);
-- +goose StatementEnd

CREATE INDEX idx_media_sidecars_root ON media_sidecars(root);

PRAGMA foreign_keys=ON;

-- +goose Down

DROP TABLE IF EXISTS media_sidecars;

PRAGMA foreign_keys=OFF;

-- Rows carrying one of the new reasons have no pre-00020 spelling; they are
-- dropped rather than relabelled, because the next scan rewrites this table
-- for every root anyway.
DELETE FROM media_skipped WHERE reason NOT IN ('unsupported_extension','drm_epub');

-- +goose StatementBegin
CREATE TABLE media_skipped_old (
    id         INTEGER PRIMARY KEY,
    root       TEXT NOT NULL,
    path       TEXT NOT NULL,
    ext        TEXT NOT NULL,
    reason     TEXT NOT NULL CHECK (reason IN ('unsupported_extension','drm_epub')),
    size_bytes INTEGER NOT NULL,
    mtime      INTEGER NOT NULL,
    seen_at    TIMESTAMP NOT NULL,
    UNIQUE (root, path)
);
-- +goose StatementEnd

INSERT INTO media_skipped_old (id, root, path, ext, reason, size_bytes, mtime, seen_at)
SELECT id, root, path, ext, reason, size_bytes, mtime, seen_at FROM media_skipped;

DROP TABLE media_skipped;

ALTER TABLE media_skipped_old RENAME TO media_skipped;

CREATE INDEX idx_media_skipped_root ON media_skipped(root);

PRAGMA foreign_keys=ON;

-- meta_version carries no index or constraint, so it drops in place.
ALTER TABLE media_files DROP COLUMN meta_version;
