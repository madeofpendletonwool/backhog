-- +goose Up

-- The alignment queue and its results. Forced alignment is expensive —
-- Whisper and ffmpeg neither fit nor belong in the small CGO-free API
-- image — so it runs outside the API process, in an optional worker
-- container that pulls jobs off this queue through the /internal API.
-- Everything here is additive: a deployment that never enables
-- alignment (no ALIGN_WORKER_TOKEN, no worker container) keeps a fully
-- working Books arena — the queue simply stays empty.

-- alignment_jobs is the queue. One row per requested alignment, with
-- enough liveness state to survive a crashed worker: heartbeat_at is the
-- worker's "I am still alive" signal, and a job whose heart stops is
-- reclaimed (back to queued, or failed once attempts have run out) the
-- next time anyone claims. state is the worker's pipeline position; the
-- API only ever moves queued -> claimed (and reclaim/complete moves).
-- audio_timeline_hash pins the audiobook the job was enqueued against:
-- the ordered (file, duration) sequence, so re-attaching or re-ordering
-- tracks is detectable against the alignment that was produced.
CREATE TABLE alignment_jobs (
    id                  TEXT PRIMARY KEY,
    entry_id            TEXT NOT NULL REFERENCES library_entries(id) ON DELETE CASCADE,
    epub_text_id        TEXT NOT NULL REFERENCES epub_texts(id) ON DELETE CASCADE,
    audio_timeline_hash TEXT NOT NULL,
    state               TEXT NOT NULL DEFAULT 'queued'
                        CHECK (state IN ('queued','claimed','transcribing','aligning',
                                         'ready','failed','low_confidence')),
    progress            REAL NOT NULL DEFAULT 0 CHECK (progress >= 0 AND progress <= 1),
    stage_detail        TEXT NOT NULL DEFAULT '',
    error               TEXT,
    attempts            INTEGER NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    claimed_by          TEXT,
    claimed_at          TIMESTAMP,
    heartbeat_at        TIMESTAMP,
    created_at          TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at          TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- FIFO claim order. The partial unique index enforces the queue's own
-- invariant: at most one active job per entry, so a user hammering the
-- enqueue endpoint cannot stack duplicates.
CREATE INDEX idx_alignment_jobs_queue ON alignment_jobs(state, created_at, id);
CREATE UNIQUE INDEX idx_alignment_jobs_active_entry
    ON alignment_jobs(entry_id)
    WHERE state IN ('queued','claimed','transcribing','aligning');

-- alignments is the finished product: an alignment's own summary plus
-- its anchors (below). The row is created while the worker streams its
-- results (it shares the job's id, starting life as 'aligning') and is
-- finalized by /internal complete with one of the terminal states.
-- 'low_confidence' is a usable alignment whose anchors should be treated
-- skeptically, not a failure.
CREATE TABLE alignments (
    id              TEXT PRIMARY KEY,
    entry_id        TEXT NOT NULL REFERENCES library_entries(id) ON DELETE CASCADE,
    epub_text_id    TEXT NOT NULL REFERENCES epub_texts(id) ON DELETE CASCADE,
    state           TEXT NOT NULL
                    CHECK (state IN ('aligning','ready','low_confidence','failed')),
    coverage        REAL NOT NULL DEFAULT 0 CHECK (coverage >= 0 AND coverage <= 1),
    mean_confidence REAL NOT NULL DEFAULT 0 CHECK (mean_confidence >= 0 AND mean_confidence <= 1),
    model           TEXT NOT NULL DEFAULT '',
    created_at      TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_alignments_entry ON alignments(entry_id, created_at DESC);

-- alignment_anchors is the map the position translator interpolates
-- over: char_offset (byte offset into the canonical text) to
-- audio_seconds, expressed on the book's GLOBAL timeline — the same
-- single notion of "where in the audiobook" the player and the stored
-- positions use, never per-track offsets. audio_seconds is GLOBAL on
-- the book timeline. 3–8k rows for an 11-hour book is comfortably
-- within SQLite's range; the composite keys serve both interpolation
-- directions (char -> seconds, seconds -> char).
CREATE TABLE alignment_anchors (
    alignment_id  TEXT NOT NULL REFERENCES alignments(id) ON DELETE CASCADE,
    char_offset   INTEGER NOT NULL CHECK (char_offset >= 0),
    audio_seconds REAL NOT NULL CHECK (audio_seconds >= 0),
    confidence    REAL NOT NULL DEFAULT 1 CHECK (confidence >= 0 AND confidence <= 1),
    PRIMARY KEY (alignment_id, char_offset)
);

CREATE INDEX idx_alignment_anchors_by_time ON alignment_anchors(alignment_id, audio_seconds);

-- transcript_segments is the raw Whisper output kept for debugging a
-- bad alignment (and for a future "search the audio" feature). It is
-- written by the worker in streamed batches and is never read on any
-- hot path, so it is cheap to keep and would be the first thing to drop.
CREATE TABLE transcript_segments (
    id           INTEGER PRIMARY KEY,
    alignment_id TEXT NOT NULL REFERENCES alignments(id) ON DELETE CASCADE,
    audio_start  REAL NOT NULL CHECK (audio_start >= 0),
    audio_end    REAL NOT NULL CHECK (audio_end >= audio_start),
    text         TEXT NOT NULL DEFAULT ''
);

CREATE INDEX idx_transcript_segments_alignment ON transcript_segments(alignment_id, audio_start);

-- +goose Down

DROP TABLE IF EXISTS transcript_segments;
DROP TABLE IF EXISTS alignment_anchors;
DROP TABLE IF EXISTS alignments;
DROP TABLE IF EXISTS alignment_jobs;
