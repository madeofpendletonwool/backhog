-- +goose Up

-- A cover someone picked by hand. Metadata refreshes (search, ISBN lookups,
-- the add flow) upsert the work row with whatever cover Open Library promotes
-- today; once a person has chosen, that choice must outlive them.
ALTER TABLE books ADD COLUMN cover_pinned INTEGER NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE books DROP COLUMN cover_pinned;
