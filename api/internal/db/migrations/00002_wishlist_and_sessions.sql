-- +goose NO TRANSACTION

-- +goose Up

-- SQLite cannot alter a CHECK constraint in place, so extending the status enum
-- with 'wishlist' means rebuilding library_entries. This is the standard
-- rebuild recipe: foreign keys off, copy into a new table, swap the names, put
-- the indexes back. It runs outside goose's transaction because PRAGMA
-- foreign_keys is a no-op inside one.
PRAGMA foreign_keys=OFF;

-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS library_entries_new (
    id             TEXT PRIMARY KEY,
    user_id        TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    game_id        INTEGER NOT NULL REFERENCES games(id) ON DELETE CASCADE,
    status         TEXT NOT NULL DEFAULT 'backlog'
                        CHECK (status IN ('backlog','playing','played','dropped','wishlist')),
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

INSERT INTO library_entries_new (id, user_id, game_id, status, platform_id, user_rating, notes, queue_position, started_at, finished_at, created_at, updated_at) SELECT id, user_id, game_id, status, platform_id, user_rating, notes, queue_position, started_at, finished_at, created_at, updated_at FROM library_entries;

DROP TABLE IF EXISTS library_entries;

ALTER TABLE library_entries_new RENAME TO library_entries;

CREATE INDEX IF NOT EXISTS idx_entries_user_status ON library_entries(user_id, status);
CREATE INDEX IF NOT EXISTS idx_entries_user_queue ON library_entries(user_id, queue_position);
CREATE INDEX IF NOT EXISTS idx_entries_game ON library_entries(game_id);

PRAGMA foreign_keys=ON;

-- Playtime is logged by hand, one session at a time. Tracking a running process
-- would overcount badly: a game left paused while you do something else is not
-- time played. A rough honest number beats a precise wrong one.
-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS play_sessions (
    id         TEXT PRIMARY KEY,
    user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    entry_id   TEXT NOT NULL REFERENCES library_entries(id) ON DELETE CASCADE,
    played_on  DATE NOT NULL,
    minutes    INTEGER NOT NULL CHECK (minutes > 0 AND minutes <= 1440),
    note       TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
-- +goose StatementEnd

CREATE INDEX IF NOT EXISTS idx_play_sessions_entry ON play_sessions(entry_id, played_on DESC);
CREATE INDEX IF NOT EXISTS idx_play_sessions_user ON play_sessions(user_id);

-- +goose Down

DROP TABLE IF EXISTS play_sessions;

PRAGMA foreign_keys=OFF;

-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS library_entries_old (
    id             TEXT PRIMARY KEY,
    user_id        TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    game_id        INTEGER NOT NULL REFERENCES games(id) ON DELETE CASCADE,
    status         TEXT NOT NULL DEFAULT 'backlog'
                        CHECK (status IN ('backlog','playing','played','dropped')),
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

-- Wishlist rows have no equivalent in the old schema; fold them into backlog.
INSERT INTO library_entries_old (id, user_id, game_id, status, platform_id, user_rating, notes, queue_position, started_at, finished_at, created_at, updated_at) SELECT id, user_id, game_id, CASE WHEN status = 'wishlist' THEN 'backlog' ELSE status END, platform_id, user_rating, notes, queue_position, started_at, finished_at, created_at, updated_at FROM library_entries;

DROP TABLE IF EXISTS library_entries;

ALTER TABLE library_entries_old RENAME TO library_entries;

CREATE INDEX IF NOT EXISTS idx_entries_user_status ON library_entries(user_id, status);
CREATE INDEX IF NOT EXISTS idx_entries_user_queue ON library_entries(user_id, queue_position);
CREATE INDEX IF NOT EXISTS idx_entries_game ON library_entries(game_id);

PRAGMA foreign_keys=ON;
