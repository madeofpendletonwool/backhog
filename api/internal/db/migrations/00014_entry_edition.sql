-- +goose Up

-- The printing a book entry is anchored to. Page numbers (and the page maps
-- that hang off them) belong to an edition, not a work, so the link is
-- recorded at add time — nullable, since a reader may not know or care which
-- printing they own. Deleting the edition row detaches the entry rather than
-- deleting it, matching the platform_id precedent for games.

-- SQLite allows ADD COLUMN with a REFERENCES clause only when the default is
-- NULL, which is exactly the semantics wanted here; existing rows (all games
-- today) carry a NULL edition_id.
ALTER TABLE library_entries ADD COLUMN edition_id TEXT
    REFERENCES book_editions(id) ON DELETE SET NULL;

-- +goose Down

ALTER TABLE library_entries DROP COLUMN edition_id;
