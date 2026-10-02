-- +goose Up

-- The claims store (MAD-466): backhog stores and serves structured claims
-- about a book; it never produces them. Any extractor — an MCP client
-- running the extract_claims prompt, a script, the Arda pipeline — runs
-- outside and writes through the batch import endpoint. The rows below are
-- what survives that door: only items whose quoted span matches the
-- canonical text at their offsets, inside one chapter, with no reveal
-- ahead of its evidence.
--
-- chapter_index / chapter_hash are the staleness key: the 1-based chapter
-- the evidence sits in and the sha256 of that chapter's canonical text at
-- import time. A re-ingest that changes the chapter changes its hash, and
-- the claim is hidden on read — never deleted, because the next extractor
-- run may re-anchor it and a person's shelf should not lose knowledge to
-- a parser bump.
CREATE TABLE book_claims (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    entry_id TEXT NOT NULL REFERENCES library_entries(id) ON DELETE CASCADE,
    statement TEXT NOT NULL CHECK (length(trim(statement)) > 0),
    subject TEXT,
    predicate TEXT,
    object TEXT,
    char_start INTEGER NOT NULL CHECK (char_start >= 0),
    char_end INTEGER NOT NULL,
    quote TEXT NOT NULL CHECK (length(trim(quote)) > 0),
    chapter_index INTEGER NOT NULL CHECK (chapter_index >= 1),
    chapter_hash TEXT NOT NULL,
    source TEXT NOT NULL CHECK (length(trim(source)) > 0),
    source_version TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CHECK (char_end > char_start),
    -- Claim identity is deterministic — the same evidence carrying the
    -- same statement — so a re-import is a no-op (or a hash refresh after
    -- a re-ingest), not a duplicate.
    UNIQUE (entry_id, char_start, char_end, statement)
);

CREATE INDEX idx_book_claims_entry ON book_claims(entry_id, char_start);

-- Claim versions: the append-only reveal log. When a claim is superseded
-- or a later chapter reveals what the evidence only hinted at, the later
-- statement lands here, keyed by its own reveal offset — nothing is ever
-- overwritten, so a reader at any position sees exactly the truth as of
-- the text they have read: the base claim, plus every version whose
-- reveal they have passed.
CREATE TABLE claim_versions (
    id TEXT PRIMARY KEY,
    claim_id TEXT NOT NULL REFERENCES book_claims(id) ON DELETE CASCADE,
    statement TEXT NOT NULL CHECK (length(trim(statement)) > 0),
    char_start INTEGER NOT NULL CHECK (char_start >= 0),
    char_end INTEGER NOT NULL,
    quote TEXT NOT NULL CHECK (length(trim(quote)) > 0),
    chapter_index INTEGER NOT NULL CHECK (chapter_index >= 1),
    chapter_hash TEXT NOT NULL,
    reveal_offset INTEGER NOT NULL CHECK (reveal_offset >= 0),
    source TEXT NOT NULL CHECK (length(trim(source)) > 0),
    source_version TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CHECK (char_end > char_start),
    -- A reveal cannot precede its evidence: the grounding quote must sit
    -- at or before the offset the truth arrives at.
    CHECK (reveal_offset >= char_start),
    UNIQUE (claim_id, reveal_offset, statement)
);

CREATE INDEX idx_claim_versions_claim ON claim_versions(claim_id, reveal_offset);

-- Entities: the people, places and things claims are about, with the
-- alias table that lets "Lizzy" in book two be "Elizabeth" of book one.
-- Keyed to the reader's own shelf (user, book), like every derived row in
-- the Books arena; matched to claims by folded name at read time, so a
-- claim's subject/object strings stay the extractor's own words.
CREATE TABLE book_entities (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    book_id TEXT NOT NULL REFERENCES books(id) ON DELETE CASCADE,
    name TEXT NOT NULL CHECK (length(trim(name)) > 0),
    kind TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (user_id, book_id, name)
);

CREATE TABLE entity_aliases (
    entity_id TEXT NOT NULL REFERENCES book_entities(id) ON DELETE CASCADE,
    alias TEXT NOT NULL CHECK (length(trim(alias)) > 0),
    PRIMARY KEY (entity_id, alias)
);

CREATE INDEX idx_book_entities_book ON book_entities(user_id, book_id);

-- +goose Down

DROP INDEX idx_book_entities_book;
DROP TABLE entity_aliases;
DROP TABLE book_entities;
DROP INDEX idx_claim_versions_claim;
DROP TABLE claim_versions;
DROP INDEX idx_book_claims_entry;
DROP TABLE book_claims;
