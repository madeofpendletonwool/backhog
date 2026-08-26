-- +goose Up

-- One row per achievement a user has earned. The catalogue itself lives in Go
-- (internal/achievements): achievement_id is a code key into that catalogue,
-- deliberately not a foreign key, so renaming or retiring a catalogue entry
-- never breaks history. entry_id points at the game that triggered the unlock
-- and is nullable because count-based achievements ("finish 5") still deserve a
-- triggering entry but deletion must not cascade the unlock away.
CREATE TABLE achievement_unlocks (
    id             TEXT PRIMARY KEY,
    user_id        TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    achievement_id TEXT NOT NULL,
    entry_id       TEXT REFERENCES library_entries(id) ON DELETE SET NULL,
    unlocked_at    TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (user_id, achievement_id)
);
CREATE INDEX idx_achievement_unlocks_user ON achievement_unlocks(user_id, unlocked_at);

-- +goose Down

DROP TABLE achievement_unlocks;
