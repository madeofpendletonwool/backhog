-- +goose Up

-- The physical-copy half of the page bridge. Page numbers are a property
-- of a printing, not a work: "page 120" means nothing until you know
-- which edition's pages, so anchors attach to a copy of a printing the
-- user actually holds, and the map for one printing can never bleed into
-- another's. This stage is deliberately independent of alignment: the
-- anchors below map printed page -> canonical char offset, while
-- alignment maps char offset -> audio seconds; the position translator
-- interpolates over either.

-- physical_copies is a printing the user owns. One row per (user, entry,
-- edition): owning the same printing twice adds nothing — the page map is
-- a property of the printing, not of the particular lump of paper — while
-- a second printing of the same work is a second row with its own map.
-- edition_id is required and cascades: an anchor without its printing is
-- page numbers of nothing, and edition rows only disappear with their
-- work row (the catalogue is upserted, never culled), so the cascade is
-- defensive, not a live path.
CREATE TABLE physical_copies (
    id         TEXT PRIMARY KEY,
    user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    entry_id   TEXT NOT NULL REFERENCES library_entries(id) ON DELETE CASCADE,
    edition_id TEXT NOT NULL REFERENCES book_editions(id) ON DELETE CASCADE,
    notes      TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (user_id, entry_id, edition_id)
);

CREATE INDEX idx_physical_copies_entry ON physical_copies(entry_id);

-- page_anchors is the page map: one row per printed page of one copy,
-- pointing at the canonical character offset where that page begins.
-- The composite PRIMARY KEY makes re-scanning a page an overwrite rather
-- than an accumulation — a bad anchor is corrected by the next scan, so
-- the map self-heals instead of collecting contradictions. (The position
-- translator additionally drops any anchor that breaks the map's strict
-- monotonicity, so one noisy page cannot poison its neighbours'
-- interpolations.)
CREATE TABLE page_anchors (
    physical_copy_id TEXT NOT NULL REFERENCES physical_copies(id) ON DELETE CASCADE,
    printed_page     INTEGER NOT NULL CHECK (printed_page > 0),
    char_offset      INTEGER NOT NULL CHECK (char_offset >= 0),
    source           TEXT NOT NULL CHECK (source IN ('ocr','manual')),
    confidence       REAL NOT NULL DEFAULT 1 CHECK (confidence >= 0 AND confidence <= 1),
    created_at       TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (physical_copy_id, printed_page)
);

CREATE INDEX idx_page_anchors_offset ON page_anchors(physical_copy_id, char_offset);

-- +goose Down

DROP TABLE IF EXISTS page_anchors;
DROP TABLE IF EXISTS physical_copies;
