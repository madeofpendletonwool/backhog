-- +goose Up

CREATE TABLE users (
    id            TEXT PRIMARY KEY,
    email         TEXT NOT NULL UNIQUE COLLATE NOCASE,
    username      TEXT NOT NULL UNIQUE COLLATE NOCASE,
    password_hash TEXT NOT NULL,
    created_at    TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE sessions (
    id         TEXT PRIMARY KEY,
    user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    expires_at TIMESTAMP NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX idx_sessions_user ON sessions(user_id);
CREATE INDEX idx_sessions_expiry ON sessions(expires_at);

-- Games are a shared cache keyed by IGDB id: two users who add the same game
-- share one row here. All per-user state lives in library_entries.
CREATE TABLE games (
    id                    INTEGER PRIMARY KEY,
    name                  TEXT NOT NULL,
    slug                  TEXT,
    summary               TEXT,
    cover_url             TEXT,
    cover_local_path      TEXT,
    accent_hex            TEXT,
    first_release_date    INTEGER,
    igdb_rating           REAL,
    time_to_beat_main     INTEGER,
    time_to_beat_complete INTEGER,
    raw_json              TEXT,
    fetched_at            TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX idx_games_name ON games(name COLLATE NOCASE);

CREATE TABLE genres (
    id   INTEGER PRIMARY KEY,
    name TEXT NOT NULL
);
CREATE TABLE game_genres (
    game_id  INTEGER NOT NULL REFERENCES games(id) ON DELETE CASCADE,
    genre_id INTEGER NOT NULL REFERENCES genres(id) ON DELETE CASCADE,
    PRIMARY KEY (game_id, genre_id)
);
CREATE INDEX idx_game_genres_genre ON game_genres(genre_id);

CREATE TABLE platforms (
    id   INTEGER PRIMARY KEY,
    name TEXT NOT NULL
);
CREATE TABLE game_platforms (
    game_id     INTEGER NOT NULL REFERENCES games(id) ON DELETE CASCADE,
    platform_id INTEGER NOT NULL REFERENCES platforms(id) ON DELETE CASCADE,
    PRIMARY KEY (game_id, platform_id)
);
CREATE INDEX idx_game_platforms_platform ON game_platforms(platform_id);

CREATE TABLE library_entries (
    id             TEXT PRIMARY KEY,
    user_id        TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    game_id        INTEGER NOT NULL REFERENCES games(id) ON DELETE CASCADE,
    status         TEXT NOT NULL DEFAULT 'backlog'
                        CHECK (status IN ('backlog','playing','played','dropped')),
    platform_id    INTEGER REFERENCES platforms(id) ON DELETE SET NULL,
    user_rating    INTEGER CHECK (user_rating IS NULL OR (user_rating BETWEEN 1 AND 10)),
    notes          TEXT NOT NULL DEFAULT '',
    -- Fractional index: reordering rewrites one row, not the whole queue.
    queue_position REAL,
    started_at     TIMESTAMP,
    finished_at    TIMESTAMP,
    created_at     TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at     TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (user_id, game_id)
);
CREATE INDEX idx_entries_user_status ON library_entries(user_id, status);
CREATE INDEX idx_entries_user_queue ON library_entries(user_id, queue_position);
CREATE INDEX idx_entries_game ON library_entries(game_id);

CREATE TABLE lists (
    id          TEXT PRIMARY KEY,
    user_id     TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name        TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    kind        TEXT NOT NULL CHECK (kind IN ('manual','smart')),
    rules_json  TEXT,
    created_at  TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX idx_lists_user ON lists(user_id);

CREATE TABLE list_items (
    list_id  TEXT NOT NULL REFERENCES lists(id) ON DELETE CASCADE,
    entry_id TEXT NOT NULL REFERENCES library_entries(id) ON DELETE CASCADE,
    position REAL NOT NULL,
    PRIMARY KEY (list_id, entry_id)
);
CREATE INDEX idx_list_items_position ON list_items(list_id, position);

-- +goose Down
DROP TABLE list_items;
DROP TABLE lists;
DROP TABLE library_entries;
DROP TABLE game_platforms;
DROP TABLE platforms;
DROP TABLE game_genres;
DROP TABLE genres;
DROP TABLE games;
DROP TABLE sessions;
DROP TABLE users;
