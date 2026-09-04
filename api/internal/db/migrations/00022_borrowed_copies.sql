-- +goose Up

-- Provenance for physical_copies. A copy was previously a printing the
-- user owns, full stop — which made "I have this on paper" a lie that
-- expires in three weeks for a reader who lives at the library, and the
-- only honest alternatives were lying (own a book you don't) or losing
-- the whole paper bridge for that printing. Three columns turn the lie
-- into a state:
--
--   acquisition  how the printing was acquired. 'owned' is what every
--                existing row is; 'borrowed' is a library copy. The
--                value set is closed on purpose — extending it later
--                (e.g. 'lent_out') is a 00020-style CHECK rebuild, and
--                adding values nothing can set yet would just be
--                writing documentation in the schema.
--   due_at       the library return deadline. Display-only for now;
--                nothing orders a queue by it yet.
--   returned_at  NULL = in hand. Returning stamps it; a re-checkout of
--                the same printing clears it and sets a new due_at.
--                The row and its page map survive both transitions —
--                the map is a property of the printing, not of the
--                particular lump of paper — and only "Forget this
--                copy" ever deletes a map.
--
-- A plain ADD COLUMN, like 00020's meta_version and 00021's
-- media_scope: constant defaults satisfy SQLite, no CHECK moves on an
-- existing column, so no table rebuild is warranted.

ALTER TABLE physical_copies ADD COLUMN acquisition TEXT NOT NULL DEFAULT 'owned'
    CHECK (acquisition IN ('owned','borrowed'));

ALTER TABLE physical_copies ADD COLUMN due_at TIMESTAMP;

ALTER TABLE physical_copies ADD COLUMN returned_at TIMESTAMP;

-- +goose Down

ALTER TABLE physical_copies DROP COLUMN returned_at;
ALTER TABLE physical_copies DROP COLUMN due_at;
ALTER TABLE physical_copies DROP COLUMN acquisition;
