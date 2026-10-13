-- +goose Up

-- When the recipient acknowledged a share. A share grants access but never
-- puts rows in someone else's library, so the recipient has to be told about
-- it and offered the one click that shelves it. NULL is "not yet seen": the
-- app shows the share as a notice until they add the book or dismiss it.
-- Existing shares start unseen on purpose — nobody was ever told about them.
ALTER TABLE book_shares ADD COLUMN seen_at TIMESTAMP;

-- +goose Down

ALTER TABLE book_shares DROP COLUMN seen_at;
