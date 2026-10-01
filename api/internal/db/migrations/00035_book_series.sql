-- +goose Up

-- Book series membership (MAD-469): the read surface the series memory
-- stands on. A book series has no external id authority in backhog's world
-- — Open Library's work records carry no series field — so membership is
-- declared by the one deterministic source the library already trusts: the
-- Calibre sidecars the media scan parses (series / series_index), written
-- onto the shared work row at attach time and healed by a boot walker for
-- libraries attached before this landed. Identity is the normalized name;
-- nothing here is user state, which is why it lives on books rather than a
-- per-user table.
--
-- series_number is Calibre's series_index — the work's rank inside the
-- series, frequently fractional (1.5 for the between-the-numbers novella)
-- and absent for sidecars that only name the series. NULL means "in the
-- series, rank unknown"; ordering falls back to first_publish_year.
ALTER TABLE books ADD COLUMN series_name TEXT NOT NULL DEFAULT '';
ALTER TABLE books ADD COLUMN series_number REAL
    CHECK ((series_number IS NULL OR series_number >= 0)
           AND (series_name <> '' OR series_number IS NULL));

CREATE INDEX idx_books_series ON books(series_name COLLATE NOCASE)
    WHERE series_name <> '';

-- +goose Down

DROP INDEX idx_books_series;
ALTER TABLE books DROP COLUMN series_number;
ALTER TABLE books DROP COLUMN series_name;
