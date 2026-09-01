-- +goose NO TRANSACTION

-- +goose Up

-- The shared book metadata cache: works and editions, plus the deferred
-- library_entries.book_id FK promised in 00010. Mirrors how games works: one
-- shared catalogue table per granularity, filled by the metadata provider,
-- never user-scoped.

-- books is the Work (Open Library work key, e.g. OL12345W). Subjects stay a
-- JSON column rather than a join table: nothing filters on them yet, and the
-- games precedent (extras_json) shows how to promote a JSON blob to relations
-- the day faceting actually needs it.
CREATE TABLE IF NOT EXISTS books (
    id                 TEXT PRIMARY KEY,
    title              TEXT NOT NULL,
    authors_json       TEXT NOT NULL DEFAULT '[]',
    description        TEXT NOT NULL DEFAULT '',
    cover_url          TEXT NOT NULL DEFAULT '',
    cover_local_path   TEXT NOT NULL DEFAULT '',
    accent_hex         TEXT NOT NULL DEFAULT '',
    first_publish_year INTEGER,
    subjects_json      TEXT NOT NULL DEFAULT '[]',
    raw_json           TEXT NOT NULL DEFAULT '',
    fetched_at         TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- book_editions is the Printing (Open Library edition key, e.g. OL12345M).
-- Physical-copy page maps key off the edition, not the work.
CREATE TABLE IF NOT EXISTS book_editions (
    id             TEXT PRIMARY KEY,
    book_id        TEXT NOT NULL REFERENCES books(id) ON DELETE CASCADE,
    isbn10         TEXT NOT NULL DEFAULT '',
    isbn13         TEXT NOT NULL DEFAULT '',
    publisher      TEXT NOT NULL DEFAULT '',
    published_year INTEGER,
    page_count     INTEGER,
    binding        TEXT NOT NULL DEFAULT '',
    language       TEXT NOT NULL DEFAULT '',
    cover_url      TEXT NOT NULL DEFAULT '',
    raw_json       TEXT NOT NULL DEFAULT '',
    fetched_at     TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_book_editions_book ON book_editions(book_id);
CREATE INDEX IF NOT EXISTS idx_book_editions_isbn10 ON book_editions(isbn10);
CREATE INDEX IF NOT EXISTS idx_book_editions_isbn13 ON book_editions(isbn13);

-- 00010 left library_entries.book_id as a logical reference because the books
-- table did not exist yet. This rebuild re-creates the table with the real FK,
-- using the same recipe as 00002/00004/00010 (foreign keys off, copy, swap,
-- reindex) outside goose's transaction because PRAGMA foreign_keys is a no-op
-- inside one.
--
-- Book rows created before this migration cannot be carried over: until now no
-- code path could write a book_id, and the books table they would have to
-- reference is created empty by this very migration, so every pre-existing
-- book_id is by definition an orphan. They are dropped rather than kept as
-- permanent FK violations — the same call 00010's Down made about book rows it
-- could not represent.
PRAGMA foreign_keys=OFF;

-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS library_entries_new (
    id             TEXT PRIMARY KEY,
    user_id        TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    media_type     TEXT NOT NULL DEFAULT 'game'
                       CHECK (media_type IN ('game','book')),
    game_id        INTEGER REFERENCES games(id) ON DELETE CASCADE,
    book_id        TEXT REFERENCES books(id) ON DELETE CASCADE,
    status         TEXT NOT NULL DEFAULT 'backlog'
                       CHECK (status IN ('backlog','playing','played','dropped','wishlist','ignored')),
    platform_id    INTEGER REFERENCES platforms(id) ON DELETE SET NULL,
    user_rating    INTEGER CHECK (user_rating IS NULL OR (user_rating BETWEEN 1 AND 10)),
    notes          TEXT NOT NULL DEFAULT '',
    queue_position REAL,
    started_at     TIMESTAMP,
    finished_at    TIMESTAMP,
    created_at     TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at     TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CHECK ((media_type = 'game' AND game_id IS NOT NULL AND book_id IS NULL)
        OR (media_type = 'book' AND book_id IS NOT NULL AND game_id IS NULL)),
    UNIQUE (user_id, media_type, game_id, book_id)
);
-- +goose StatementEnd

INSERT INTO library_entries_new
    (id, user_id, media_type, game_id, book_id, status, platform_id, user_rating,
     notes, queue_position, started_at, finished_at, created_at, updated_at)
SELECT id, user_id, media_type, game_id, book_id, status, platform_id, user_rating,
       notes, queue_position, started_at, finished_at, created_at, updated_at
FROM library_entries WHERE media_type = 'game';

DROP TABLE IF EXISTS library_entries;

ALTER TABLE library_entries_new RENAME TO library_entries;

CREATE INDEX IF NOT EXISTS idx_entries_user_status ON library_entries(user_id, status);
CREATE INDEX IF NOT EXISTS idx_entries_user_queue ON library_entries(user_id, queue_position);
CREATE INDEX IF NOT EXISTS idx_entries_game ON library_entries(game_id);
CREATE INDEX IF NOT EXISTS idx_entries_user_media_status ON library_entries(user_id, media_type, status);
CREATE UNIQUE INDEX IF NOT EXISTS idx_entries_user_game ON library_entries(user_id, game_id) WHERE game_id IS NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS idx_entries_user_book ON library_entries(user_id, book_id) WHERE book_id IS NOT NULL;

PRAGMA foreign_keys=ON;

-- +goose Down

-- Undo the FK first (same rebuild recipe, back to the exact 00010 shape where
-- book_id is a plain logical column), then drop the cache tables. Unlike the
-- Up direction, book rows here are legitimate — they were written against the
-- books table this migration created — so they survive as logical references,
-- which is precisely what 00010 allowed.
PRAGMA foreign_keys=OFF;

-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS library_entries_old (
    id             TEXT PRIMARY KEY,
    user_id        TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    media_type     TEXT NOT NULL DEFAULT 'game'
                       CHECK (media_type IN ('game','book')),
    game_id        INTEGER REFERENCES games(id) ON DELETE CASCADE,
    book_id        TEXT,
    status         TEXT NOT NULL DEFAULT 'backlog'
                       CHECK (status IN ('backlog','playing','played','dropped','wishlist','ignored')),
    platform_id    INTEGER REFERENCES platforms(id) ON DELETE SET NULL,
    user_rating    INTEGER CHECK (user_rating IS NULL OR (user_rating BETWEEN 1 AND 10)),
    notes          TEXT NOT NULL DEFAULT '',
    queue_position REAL,
    started_at     TIMESTAMP,
    finished_at    TIMESTAMP,
    created_at     TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at     TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CHECK ((media_type = 'game' AND game_id IS NOT NULL AND book_id IS NULL)
        OR (media_type = 'book' AND book_id IS NOT NULL AND game_id IS NULL)),
    UNIQUE (user_id, media_type, game_id, book_id)
);
-- +goose StatementEnd

INSERT INTO library_entries_old
    (id, user_id, media_type, game_id, book_id, status, platform_id, user_rating,
     notes, queue_position, started_at, finished_at, created_at, updated_at)
SELECT id, user_id, media_type, game_id, book_id, status, platform_id, user_rating,
       notes, queue_position, started_at, finished_at, created_at, updated_at
FROM library_entries;

DROP TABLE IF EXISTS library_entries;

ALTER TABLE library_entries_old RENAME TO library_entries;

CREATE INDEX IF NOT EXISTS idx_entries_user_status ON library_entries(user_id, status);
CREATE INDEX IF NOT EXISTS idx_entries_user_queue ON library_entries(user_id, queue_position);
CREATE INDEX IF NOT EXISTS idx_entries_game ON library_entries(game_id);
CREATE INDEX IF NOT EXISTS idx_entries_user_media_status ON library_entries(user_id, media_type, status);
CREATE UNIQUE INDEX IF NOT EXISTS idx_entries_user_game ON library_entries(user_id, game_id) WHERE game_id IS NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS idx_entries_user_book ON library_entries(user_id, book_id) WHERE book_id IS NOT NULL;

DROP TABLE IF EXISTS book_editions;
DROP TABLE IF EXISTS books;

PRAGMA foreign_keys=ON;
