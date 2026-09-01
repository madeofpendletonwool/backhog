-- +goose NO TRANSACTION

-- +goose Up

-- Books become the second media type on library_entries. SQLite cannot alter
-- CHECK constraints in place, so this is the standard rebuild recipe from
-- 00002 and 00004: foreign keys off, copy into a new table, swap the names,
-- put the indexes back — outside goose's transaction because PRAGMA
-- foreign_keys is a no-op inside one.
--
-- The queue, lists, projects, status history and achievements all key on
-- library_entries.id, so they carry over untouched. Only the subject columns
-- change: game_id goes nullable, book_id arrives nullable with no FK yet (the
-- books table lands in 00011, which also adds the FK), and media_type says
-- which one is live.
PRAGMA foreign_keys=OFF;

-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS library_entries_new (
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
    -- Exactly one subject column is set, and it must match media_type.
    CHECK ((media_type = 'game' AND game_id IS NOT NULL AND book_id IS NULL)
        OR (media_type = 'book' AND book_id IS NOT NULL AND game_id IS NULL)),
    UNIQUE (user_id, media_type, game_id, book_id)
);
-- +goose StatementEnd

INSERT INTO library_entries_new
    (id, user_id, media_type, game_id, book_id, status, platform_id, user_rating,
     notes, queue_position, started_at, finished_at, created_at, updated_at)
SELECT id, user_id, 'game', game_id, NULL, status, platform_id, user_rating,
       notes, queue_position, started_at, finished_at, created_at, updated_at
FROM library_entries;

DROP TABLE IF EXISTS library_entries;

ALTER TABLE library_entries_new RENAME TO library_entries;

CREATE INDEX IF NOT EXISTS idx_entries_user_status ON library_entries(user_id, status);
CREATE INDEX IF NOT EXISTS idx_entries_user_queue ON library_entries(user_id, queue_position);
CREATE INDEX IF NOT EXISTS idx_entries_game ON library_entries(game_id);
CREATE INDEX IF NOT EXISTS idx_entries_user_media_status ON library_entries(user_id, media_type, status);
-- SQLite treats NULLs as distinct inside UNIQUE, so the table-level constraint
-- above cannot actually stop a user from adding the same game twice (book_id
-- is NULL on game rows). These partial unique indexes are the real guard.
CREATE UNIQUE INDEX IF NOT EXISTS idx_entries_user_game ON library_entries(user_id, game_id) WHERE game_id IS NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS idx_entries_user_book ON library_entries(user_id, book_id) WHERE book_id IS NOT NULL;

PRAGMA foreign_keys=ON;

-- +goose Down

PRAGMA foreign_keys=OFF;

-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS library_entries_old (
    id             TEXT PRIMARY KEY,
    user_id        TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    game_id        INTEGER NOT NULL REFERENCES games(id) ON DELETE CASCADE,
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
    UNIQUE (user_id, game_id)
);
-- +goose StatementEnd

-- Book rows have no home in the games-only schema; they are dropped, not
-- folded — collapsing them into game entries would point a game_id at nothing.
INSERT INTO library_entries_old
    (id, user_id, game_id, status, platform_id, user_rating,
     notes, queue_position, started_at, finished_at, created_at, updated_at)
SELECT id, user_id, game_id, status, platform_id, user_rating,
       notes, queue_position, started_at, finished_at, created_at, updated_at
FROM library_entries WHERE media_type = 'game';

DROP TABLE IF EXISTS library_entries;

ALTER TABLE library_entries_old RENAME TO library_entries;

CREATE INDEX IF NOT EXISTS idx_entries_user_status ON library_entries(user_id, status);
CREATE INDEX IF NOT EXISTS idx_entries_user_queue ON library_entries(user_id, queue_position);
CREATE INDEX IF NOT EXISTS idx_entries_game ON library_entries(game_id);

PRAGMA foreign_keys=ON;
