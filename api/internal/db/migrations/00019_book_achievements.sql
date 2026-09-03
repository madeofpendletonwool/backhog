-- +goose Up

-- The achievement ledger learns which arena an unlock belongs to. The
-- catalogue itself lives in Go (internal/achievements), so this column is the
-- ledger's own annotation, stamped at insert time from the catalogue: 'game'
-- or 'book' for the arena that earned it, 'any' for achievements not tied to
-- either (the play-with-the-app eggs). Existing rows predate the books arena,
-- so the default backfills every one of them to 'game' — and because ADD
-- COLUMN rewrites nothing, the history is untouched, which is the table's one
-- hard rule. A NULL would be a lie ('game' and 'book' are both real answers),
-- so NOT NULL with a constant default is the honest shape.
ALTER TABLE achievement_unlocks ADD COLUMN domain TEXT NOT NULL DEFAULT 'game'
    CHECK (domain IN ('game','book','any'));

-- +goose Down

ALTER TABLE achievement_unlocks DROP COLUMN domain;
