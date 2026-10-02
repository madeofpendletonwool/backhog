-- +goose Up

-- Quiz results (MAD-471): the AI-free back half of "quiz me". A client's
-- model writes the questions and grades the answers; backhog stores the
-- count and turns it into achievements and Reading Season stats. Nothing
-- here grades or generates anything — the row is what the client says
-- happened, which is exactly how honest the achievements built on it can
-- be (documented as for-fun, not proof).
--
-- chapter_start / chapter_end are the inclusive 1-based chapter span the
-- quiz covered, NULL when the client did not say. They are bookkeeping
-- for display, never validated against the spine: the value of the row is
-- the counts, and the clamp story lives on the read paths, not here.
CREATE TABLE book_quiz_results (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES users(id),
    entry_id TEXT NOT NULL REFERENCES library_entries(id),
    questions INTEGER NOT NULL CHECK (questions > 0),
    correct INTEGER NOT NULL CHECK (correct >= 0 AND correct <= questions),
    chapter_start INTEGER,
    chapter_end INTEGER,
    source TEXT NOT NULL DEFAULT 'mcp',
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CHECK ((chapter_start IS NULL) = (chapter_end IS NULL)),
    CHECK (chapter_start IS NULL OR (chapter_start >= 1 AND chapter_end >= chapter_start))
);

CREATE INDEX idx_quiz_results_user ON book_quiz_results(user_id, created_at);

-- +goose Down

DROP INDEX idx_quiz_results_user;
DROP TABLE book_quiz_results;
