-- +goose Up

-- Rich, display-only IGDB metadata (developer/publisher, modes, themes,
-- screenshots, similar games, …) is stored as one JSON document rather than in
-- relational tables: it is never filtered or sorted on, only rendered. Existing
-- rows get NULL and are backfilled lazily the next time their detail page opens.
ALTER TABLE games ADD COLUMN extras_json TEXT;

-- +goose Down

ALTER TABLE games DROP COLUMN extras_json;
