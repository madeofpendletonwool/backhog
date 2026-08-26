-- +goose Up

-- A series is what IGDB calls a franchise ("Mass Effect") or a collection
-- ("Mass Effect Trilogy"). Either identifier can identify the same series, so
-- both are kept and individually unique; a series known only by name has NULL
-- for both until a member game supplies one.
CREATE TABLE series (
    id                 TEXT PRIMARY KEY,
    igdb_collection_id INTEGER UNIQUE,
    igdb_franchise_id  INTEGER UNIQUE,
    name               TEXT NOT NULL,
    slug               TEXT NOT NULL DEFAULT ''
);
CREATE INDEX idx_series_name ON series(name COLLATE NOCASE);

-- Membership is a property of the shared game cache, not of any user: every
-- cached game that belongs to a series is listed, owned or not. release_order
-- is the game's rank within the series by IGDB first_release_date, recomputed
-- whenever membership is written.
CREATE TABLE series_games (
    series_id     TEXT NOT NULL REFERENCES series(id) ON DELETE CASCADE,
    game_id       INTEGER NOT NULL REFERENCES games(id) ON DELETE CASCADE,
    kind          TEXT NOT NULL DEFAULT 'game' CHECK (kind IN ('game','dlc','expansion')),
    release_order INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (series_id, game_id)
);
CREATE INDEX idx_series_games_game ON series_games(game_id);

-- Per-user series settings: which play order the journey is shown in.
CREATE TABLE user_series (
    user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    series_id  TEXT NOT NULL REFERENCES series(id) ON DELETE CASCADE,
    play_order TEXT NOT NULL DEFAULT 'release'
                   CHECK (play_order IN ('release','chronological','recommended','custom','good_ones')),
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (user_id, series_id)
);

-- Custom play order positions, one row per game the user has pinned down.
-- Same fractional-index scheme as the play queue: a reorder writes one row.
CREATE TABLE user_series_order (
    user_id   TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    series_id TEXT NOT NULL REFERENCES series(id) ON DELETE CASCADE,
    game_id   INTEGER NOT NULL REFERENCES games(id) ON DELETE CASCADE,
    position  REAL NOT NULL,
    PRIMARY KEY (user_id, series_id, game_id)
);
CREATE INDEX idx_user_series_order_pos ON user_series_order(user_id, series_id, position);

-- DLC / expansion relations. The parent always exists in the game cache; the
-- child may not yet (it is fetched on demand), so game_id deliberately has no
-- foreign key — rows for uncached children simply join to nothing until the
-- child is fetched.
CREATE TABLE game_dlc (
    parent_game_id INTEGER NOT NULL REFERENCES games(id) ON DELETE CASCADE,
    game_id        INTEGER NOT NULL,
    kind           TEXT NOT NULL CHECK (kind IN ('dlc','expansion')),
    PRIMARY KEY (parent_game_id, game_id)
);
CREATE INDEX idx_game_dlc_game ON game_dlc(game_id);

-- +goose Down

DROP TABLE game_dlc;
DROP TABLE user_series_order;
DROP TABLE user_series;
DROP TABLE series_games;
DROP TABLE series;
