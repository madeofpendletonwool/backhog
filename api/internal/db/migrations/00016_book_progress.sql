-- +goose Up

-- One position, three views. book_progress stores exactly one number as the
-- truth — char_offset, a byte offset into the canonical text 00013 indexes —
-- and everything else about "where am I" is derived from it on read. A reader
-- that stops at 412,900 and a car ride that resumes from the same book are
-- the same position, so there is no separate audio bookmark that can drift
-- out of sync with the text one.
--
-- The exception is the honest one. Until an alignment exists (Stage 7) there
-- is no map from char offset to audiobook second, so a listener's position
-- cannot be expressed as a char offset at all. Those writes land in
-- raw_audio_seconds / raw_audio_file_id instead, and the API flags them
-- derived: false rather than fabricating an offset. The pair is deliberately
-- track-relative — seconds *within* raw_audio_file_id — because a global
-- offset silently moves when a track is re-measured or the attach order
-- changes, while (file, offset-in-file) stays true.
--
-- char_offset_source records what produced the offset, which is what lets a
-- later alignment tell a precisely-known offset ('read') from one that was
-- back-computed ('listen') or matched from an OCR scan ('scan').
--
-- One row per entry: the PK is the entry id, so progress is created and
-- deleted with the library entry it belongs to. User scoping comes through
-- that entry — library_entries.user_id is the only place ownership lives.
CREATE TABLE book_progress (
    entry_id           TEXT PRIMARY KEY REFERENCES library_entries(id) ON DELETE CASCADE,
    char_offset        INTEGER NOT NULL DEFAULT 0 CHECK (char_offset >= 0),
    char_offset_source TEXT NOT NULL DEFAULT 'manual'
                           CHECK (char_offset_source IN ('read','listen','scan','manual')),
    raw_audio_seconds  REAL CHECK (raw_audio_seconds IS NULL OR raw_audio_seconds >= 0),
    raw_audio_file_id  INTEGER REFERENCES media_files(id) ON DELETE SET NULL,
    percent_complete   REAL NOT NULL DEFAULT 0
                           CHECK (percent_complete >= 0 AND percent_complete <= 100),
    updated_at         TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    -- A file id always carries the timestamp it was measured at. The
    -- reverse is deliberately allowed: detaching an audiobook nulls the
    -- file id (SET NULL) and leaves an orphan timestamp meaning "this far
    -- in, but we no longer know which file", which the read path reports
    -- as nothing to resume from. Rejecting it here instead would make
    -- detaching a file fail on the constraint. Clients cannot write a
    -- half-pair; the store layer rejects that before it reaches SQLite.
    CHECK (raw_audio_file_id IS NULL OR raw_audio_seconds IS NOT NULL)
);

-- reading_sessions mirrors play_sessions: per user, per entry, one row per
-- stretch of consumption. It is what feeds "hours owed" and the reading
-- dashboard, so it slots into the existing spine rather than starting a
-- parallel one. mode separates the two ways a book is consumed — an hour
-- read and an hour listened are both an hour, but the dashboard wants to
-- tell them apart.
--
-- Unlike play_sessions, which logs a whole-day granularity and a minute
-- count typed in after the fact, a reading session is instrumented by the
-- reader and the player: it knows precisely when it started and stopped, and
-- how far the position moved. chars_advanced is 0 for a session that ended
-- where it began (re-reading a chapter) and is never negative — going
-- backwards is not negative progress, it is no progress.
CREATE TABLE reading_sessions (
    id             TEXT PRIMARY KEY,
    user_id        TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    entry_id       TEXT NOT NULL REFERENCES library_entries(id) ON DELETE CASCADE,
    started_at     TIMESTAMP NOT NULL,
    ended_at       TIMESTAMP NOT NULL,
    mode           TEXT NOT NULL CHECK (mode IN ('read','listen')),
    chars_advanced INTEGER NOT NULL DEFAULT 0 CHECK (chars_advanced >= 0),
    seconds        INTEGER NOT NULL CHECK (seconds >= 0 AND seconds <= 86400),
    created_at     TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CHECK (ended_at >= started_at)
);

CREATE INDEX idx_reading_sessions_entry ON reading_sessions(entry_id, started_at DESC);
CREATE INDEX idx_reading_sessions_user ON reading_sessions(user_id, started_at DESC);

-- +goose Down

DROP TABLE IF EXISTS reading_sessions;
DROP TABLE IF EXISTS book_progress;
