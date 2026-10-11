-- +goose Up

-- The KOReader sync protocol's state (MAD-831), beside book_progress rather
-- than inside it: book_progress stays the one position truth, written
-- through by every push, and these tables only remember what the protocol
-- needs that the truth cannot say.

-- kosync_progress is the last push a device made for an entry, kept
-- verbatim. When Backhog's position has not moved since (char_offset still
-- matches), a pull hands the device back its own XPointer and percentage —
-- exact, where re-deriving them from a block offset would be paragraph
-- precision. It also anchors the reading-session estimate: two pushes from
-- the same device bracket a stretch of reading.
CREATE TABLE kosync_progress (
    entry_id    TEXT PRIMARY KEY REFERENCES library_entries(id) ON DELETE CASCADE,
    doc_hash    TEXT NOT NULL,
    progress    TEXT NOT NULL,
    percentage  REAL NOT NULL CHECK (percentage >= 0 AND percentage <= 1),
    device      TEXT NOT NULL DEFAULT '',
    device_id   TEXT NOT NULL DEFAULT '',
    -- Where Backhog placed the push in its canonical text.
    char_offset INTEGER NOT NULL CHECK (char_offset >= 0),
    pushed_at   TIMESTAMP NOT NULL
);

-- kosync_bindings links a document id to an entry by something other than
-- recomputing it from the file: the push's own metadata (a unique title and
-- author match) or the owner linking it by hand. kosync_documents is
-- rebuilt from the file on every download; these survive that, because
-- nothing about the file can reproduce them.
CREATE TABLE kosync_bindings (
    user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    doc_hash   TEXT NOT NULL,
    entry_id   TEXT NOT NULL REFERENCES library_entries(id) ON DELETE CASCADE,
    source     TEXT NOT NULL CHECK (source IN ('metadata','manual')),
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (user_id, doc_hash)
);

-- kosync_unmatched holds pushes for documents nothing resolved, so the
-- device is told 200 (no error on a 6-inch screen) and the owner can link
-- them later. One row per (user, document), refreshed by each push.
CREATE TABLE kosync_unmatched (
    user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    doc_hash   TEXT NOT NULL,
    title      TEXT NOT NULL DEFAULT '',
    authors    TEXT NOT NULL DEFAULT '',
    filename   TEXT NOT NULL DEFAULT '',
    progress   TEXT NOT NULL DEFAULT '',
    percentage REAL NOT NULL DEFAULT 0,
    device     TEXT NOT NULL DEFAULT '',
    seen_at    TIMESTAMP NOT NULL,
    PRIMARY KEY (user_id, doc_hash)
);

-- +goose Down

DROP TABLE IF EXISTS kosync_unmatched;
DROP TABLE IF EXISTS kosync_bindings;
DROP TABLE IF EXISTS kosync_progress;
