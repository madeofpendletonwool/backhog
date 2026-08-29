-- +goose Up

-- Curated classification columns for the shared platform cache, keyed by
-- IGDB ids. Values are backfilled from the Go catalog in
-- api/internal/metadata/platforms.go: at startup for existing rows, and as
-- part of the platform upsert whenever IGDB sync writes one. Rows the catalog
-- does not know keep a NULL generation and empty family, which reads serve
-- as family "other".
ALTER TABLE platforms ADD COLUMN generation INTEGER;
ALTER TABLE platforms ADD COLUMN family TEXT NOT NULL DEFAULT '';
ALTER TABLE platforms ADD COLUMN manufacturer TEXT NOT NULL DEFAULT '';
ALTER TABLE platforms ADD COLUMN handheld BOOLEAN NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE platforms DROP COLUMN handheld;
ALTER TABLE platforms DROP COLUMN manufacturer;
ALTER TABLE platforms DROP COLUMN family;
ALTER TABLE platforms DROP COLUMN generation;
