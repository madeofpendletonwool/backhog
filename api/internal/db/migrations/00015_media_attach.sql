-- +goose NO TRANSACTION

-- +goose Up

-- The attach stage. Three things land here:
--
-- 1. media_files gains the foreign key 00012 deliberately deferred: books
--    (00011) was on a concurrent branch then and exists on every boot now.
--    SQLite cannot add a REFERENCES clause to an existing column, so this is
--    the standard rebuild recipe (00002/00004): foreign keys off, clear any
--    dangling book_id left over from the FK-free era, copy into a new table,
--    swap names, put the indexes back. Deleting a book row detaches its
--    files (SET NULL) — the inventory outlives the metadata cache.
--
-- 2. track_number: the attach flow's explicit audio ordering, 1-based in the
--    order the client supplies file ids. NULL for epubs and unattached rows.
--
-- 3. media_skipped: the unsupported half of the library (.aax, .aaxc, DRM
--    epubs, cover art...). The scanner counts them today; the attach UI
--    needs to *show* them with a reason, or a user whose library is half
--    Audible assumes the scan is broken. Rows are replaced per root on each
--    scan — nothing is attached to them, so there is no association to
--    preserve and no missing_at bookkeeping is warranted.
--
-- 4. media_ignores: "stop asking me about this file". Per user: the file
--    inventory is shared, but the decision to ignore a candidate belongs to
--    the library owner. Deleting the file row clears the ignore with it.
PRAGMA foreign_keys=OFF;

-- Defensive: rows written while book_id was FK-free may point at books that
-- never landed. They detach rather than blocking the rebuild's check.
UPDATE media_files SET book_id = NULL
 WHERE book_id IS NOT NULL AND book_id NOT IN (SELECT id FROM books);

-- +goose StatementBegin
CREATE TABLE media_files_new (
    id                 INTEGER PRIMARY KEY,
    root               TEXT NOT NULL,
    path               TEXT NOT NULL,
    kind               TEXT NOT NULL CHECK (kind IN ('epub','audio')),
    size_bytes         INTEGER NOT NULL,
    mtime              INTEGER NOT NULL,
    sha256             TEXT,
    duration_seconds   REAL,
    container_metadata TEXT,
    book_id            TEXT REFERENCES books(id) ON DELETE SET NULL,
    track_number       INTEGER,
    scanned_at         TIMESTAMP NOT NULL,
    missing_at         TIMESTAMP,
    UNIQUE (root, path),
    CHECK (track_number IS NULL OR track_number > 0)
);
-- +goose StatementEnd

INSERT INTO media_files_new (id, root, path, kind, size_bytes, mtime, sha256,
                             duration_seconds, container_metadata, book_id, scanned_at, missing_at)
SELECT id, root, path, kind, size_bytes, mtime, sha256,
       duration_seconds, container_metadata, book_id, scanned_at, missing_at
FROM media_files;

DROP TABLE media_files;

ALTER TABLE media_files_new RENAME TO media_files;

-- Files attached to one book, in track order.
CREATE INDEX idx_media_files_book ON media_files(book_id, track_number) WHERE book_id IS NOT NULL;
-- The attach UI lists present, not-yet-attached files by kind.
CREATE INDEX idx_media_files_attach ON media_files(kind) WHERE book_id IS NULL AND missing_at IS NULL;

-- +goose StatementBegin
CREATE TABLE media_skipped (
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

CREATE INDEX idx_media_skipped_root ON media_skipped(root);

-- +goose StatementBegin
CREATE TABLE media_ignores (
    user_id       TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    media_file_id INTEGER NOT NULL REFERENCES media_files(id) ON DELETE CASCADE,
    created_at    TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (user_id, media_file_id)
);
-- +goose StatementEnd

PRAGMA foreign_keys=ON;

-- +goose Down

DROP TABLE IF EXISTS media_ignores;
DROP TABLE IF EXISTS media_skipped;

PRAGMA foreign_keys=OFF;

-- +goose StatementBegin
CREATE TABLE media_files_old (
    id                 INTEGER PRIMARY KEY,
    root               TEXT NOT NULL,
    path               TEXT NOT NULL,
    kind               TEXT NOT NULL CHECK (kind IN ('epub','audio')),
    size_bytes         INTEGER NOT NULL,
    mtime              INTEGER NOT NULL,
    sha256             TEXT,
    duration_seconds   REAL,
    container_metadata TEXT,
    book_id            TEXT,
    scanned_at         TIMESTAMP NOT NULL,
    missing_at         TIMESTAMP,
    UNIQUE (root, path)
);
-- +goose StatementEnd

INSERT INTO media_files_old (id, root, path, kind, size_bytes, mtime, sha256,
                             duration_seconds, container_metadata, book_id, scanned_at, missing_at)
SELECT id, root, path, kind, size_bytes, mtime, sha256,
       duration_seconds, container_metadata, book_id, scanned_at, missing_at
FROM media_files;

DROP TABLE media_files;

ALTER TABLE media_files_old RENAME TO media_files;

CREATE INDEX idx_media_files_book ON media_files(book_id) WHERE book_id IS NOT NULL;
CREATE INDEX idx_media_files_attach ON media_files(kind) WHERE book_id IS NULL AND missing_at IS NULL;

PRAGMA foreign_keys=ON;
