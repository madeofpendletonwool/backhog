-- +goose Up

-- The Books arena file inventory. Files are never uploaded: the library lives
-- on the NAS and is bind-mounted read-only into the container, so this table
-- is a *pointed-at* index, not a store of owned data. The scanner walks the
-- configured roots (MEDIA_DIR) and upserts by (root, path).
--
-- Rows are never deleted. A path that disappears is marked missing_at instead,
-- so a temporarily unmounted NAS does not destroy the book_id associations
-- users have made; the next scan that sees the file again clears the flag.
--
-- book_id is deliberately a plain TEXT column with NO foreign key. The books
-- table arrives in 00011, which is being built on a concurrent branch; if this
-- migration lands on main first, an FK would reference a table that does not
-- exist and every boot would fail. The attach stage adds the FK once books is
-- guaranteed to be present.
--
-- mtime is unix nanoseconds: change detection compares (size, mtime) exactly
-- as the filesystem reported it, without string-format round-trips.
-- sha256 is nullable because it is computed lazily by the attach flow —
-- hashing a 40GB library on every scan is unacceptable.
-- container_metadata is the JSON form of the embedded ID3/MP4 tags
-- (title, artist, album artist, track number, album, ...), or NULL when the
-- file carries none. duration_seconds is audio-only and stays NULL when the
-- tags do not yield it; the align worker fills those in later.
-- +goose StatementBegin
CREATE TABLE media_files (
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

-- Looking up the files attached to one book.
CREATE INDEX idx_media_files_book ON media_files(book_id) WHERE book_id IS NOT NULL;
-- The attach UI lists present, not-yet-attached files by kind.
CREATE INDEX idx_media_files_attach ON media_files(kind) WHERE book_id IS NULL AND missing_at IS NULL;

-- +goose Down

DROP TABLE IF EXISTS media_files;
