-- +goose Up

-- One row per status transition, written inside the same transaction as the
-- change. The library_entries row only knows the current status; this table
-- is what lets predicates answer "when was it dropped, how long did the drop
-- last" for the comeback achievements. Transitions from before this table
-- existed have no rows — consumers fall back to finished_at, which was (and
-- still is) stamped on drop.
CREATE TABLE entry_status_history (
    id          TEXT PRIMARY KEY,
    entry_id    TEXT NOT NULL REFERENCES library_entries(id) ON DELETE CASCADE,
    user_id     TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    from_status TEXT NOT NULL,
    to_status   TEXT NOT NULL,
    changed_at  TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX idx_entry_status_history_entry ON entry_status_history(entry_id, changed_at);

-- +goose Down

DROP TABLE entry_status_history;
