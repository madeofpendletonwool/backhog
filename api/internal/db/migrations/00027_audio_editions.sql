-- +goose Up

-- One designated audiobook per book: which recording you are actually
-- listening to.
--
-- This is 00024's problem on the audio side, one shape harder. Owning the
-- same book twice is ordinary there (the .epub and the .mobi beside it) and
-- ordinary here too — the 2003 reading and the 2019 full-cast one, an
-- abridgement beside the unabridged rip, the same book narrated by two
-- different people. But an audiobook is not one file. It is N files that
-- behave like a single tape, so the thing being chosen is a *set*, and the
-- choice cannot live on media_files the way is_primary_text does.
--
-- audio_editions is that set. A row is one recording of one book; the files
-- point at it, and exactly one edition per book is primary — the tape the
-- timeline is built from, the one the player plays, the one an alignment is
-- measured against. The partial unique index enforces the "exactly one",
-- checked by SQLite rather than by whichever query happens to run.
--
-- Until now the choice did not exist. Attaching a second recording numbered
-- its tracks 1..N alongside the first edition's 1..N, and the timeline query
-- (ORDER BY track_number, path) interleaved the two: chapter one of Dufris,
-- chapter one of Guidall, chapter two of Dufris. Grouping is therefore not
-- only how a user picks a version, it is what makes owning two of them
-- survivable at all.
CREATE TABLE audio_editions (
    id         INTEGER PRIMARY KEY,
    book_id    TEXT NOT NULL REFERENCES books(id) ON DELETE CASCADE,
    is_primary INTEGER NOT NULL DEFAULT 0 CHECK (is_primary IN (0, 1)),
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE UNIQUE INDEX idx_audio_editions_primary
    ON audio_editions(book_id)
    WHERE is_primary = 1;
CREATE INDEX idx_audio_editions_book ON audio_editions(book_id);

-- NULL for every text-side and unattached row: an edition is a fact about
-- attached audio. ON DELETE SET NULL matches book_id's behaviour — deleting
-- a book detaches its files, it never deletes the inventory.
--
-- The edition deliberately carries no label column. What distinguishes two
-- recordings on a NAS is already written down — the directory a rip lives in
-- ("Anathem (Unabridged) [William Dufris]"), the file name of a single m4b,
-- the narrator in the tags — so the label is derived from the files at read
-- time rather than stored, copied and left to go stale. A user-set name is
-- one ADD COLUMN away on the day derivation stops being enough.
ALTER TABLE media_files ADD COLUMN audio_edition_id INTEGER
    REFERENCES audio_editions(id) ON DELETE SET NULL;

CREATE INDEX idx_media_files_audio_edition
    ON media_files(audio_edition_id, track_number)
    WHERE audio_edition_id IS NOT NULL;

-- The backfill groups each book's attached audio by the directory it lives
-- in, which is how rips arrive and how the attach queue already groups
-- candidates (internal/media groupCandidates). One directory of mp3s is one
-- edition; a lone .m4b is an edition of one file.
--
-- rtrim(path, replace(path, '/', '')) is dirname: replace() yields the path's
-- characters minus its slashes, and rtrim strips those from the right until
-- it hits a slash it cannot strip. "King/Anathem/01.mp3" -> "King/Anathem/".
CREATE TEMP TABLE audio_groups AS
SELECT book_id, root, rtrim(path, replace(path, '/', '')) AS dir
FROM media_files
WHERE kind = 'audio' AND book_id IS NOT NULL
GROUP BY book_id, root, dir;

INSERT INTO audio_editions (id, book_id, is_primary)
SELECT rowid, book_id, 0 FROM audio_groups;

UPDATE media_files SET audio_edition_id = (
    SELECT g.rowid FROM audio_groups g
    WHERE g.book_id = media_files.book_id
      AND g.root = media_files.root
      AND g.dir = rtrim(media_files.path, replace(media_files.path, '/', ''))
)
WHERE kind = 'audio' AND book_id IS NOT NULL;

-- Which edition becomes primary reproduces what was playing yesterday, for
-- the same reason 00024's backfill reproduced the old resolver's answer: a
-- stored listening position is (file, seconds-into-that-file), and the
-- percentage on screen was computed against the interleaved timeline. The
-- edition owning the track that sorted first — ORDER BY track_number, path,
-- exactly the timeline query — is the one the player opened on, so it is the
-- one the position means. Missing-ness is deliberately not consulted: the
-- old timeline included missing files in their slots too.
UPDATE audio_editions SET is_primary = 1 WHERE id IN (
    SELECT audio_edition_id FROM (
        SELECT audio_edition_id, ROW_NUMBER() OVER (
                   PARTITION BY book_id
                   ORDER BY track_number, path
               ) AS rn
        FROM media_files
        WHERE kind = 'audio' AND book_id IS NOT NULL AND audio_edition_id IS NOT NULL
    )
    WHERE rn = 1
);

DROP TABLE audio_groups;

-- +goose Down

DROP INDEX IF EXISTS idx_media_files_audio_edition;

ALTER TABLE media_files DROP COLUMN audio_edition_id;

DROP INDEX IF EXISTS idx_audio_editions_book;
DROP INDEX IF EXISTS idx_audio_editions_primary;
DROP TABLE IF EXISTS audio_editions;
