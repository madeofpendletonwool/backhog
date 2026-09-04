-- +goose Up

-- Which arena a project belongs to. Projects are arena-scoped by design: a
-- count goal is "finish N games" or "finish N books", never both, and the
-- arena the project was created in decides which library feeds its progress.
-- Existing rows predate the column and were only ever game-computed, so the
-- default is 'game' — the honest description of how they have behaved.
ALTER TABLE projects ADD COLUMN media_scope TEXT NOT NULL DEFAULT 'game'
    CHECK (media_scope IN ('game','book'));

-- +goose Down

ALTER TABLE projects DROP COLUMN media_scope;
