-- +goose Up

-- Accounts grow three things at once, because they are one feature: an
-- installation with more than one person on it.
--
-- Until now every account was identical and every account could do
-- everything, which is fine while the only account is the person who owns
-- the NAS. The moment a second person is invited, two facts that were
-- previously the same fact come apart: *using* the library and *managing*
-- the files under it. A friend should be able to read, listen, track,
-- rate, queue and collect achievements; he should not be able to point
-- Backhog at a different directory, detach the EPUB the offsets are
-- measured against, or promote a different primary text and silently move
-- every reader's position.
--
-- Hence three roles, in order of what they may touch:
--
--   admin  — everything, plus the accounts themselves and the server
--            settings. There is always at least one; the store refuses
--            to demote, disable or delete the last one.
--   member — the full application, including the attach flow and the
--            media scan. This is what every existing account is, so an
--            upgrade changes nothing for anyone already using the app.
--   reader — the full application *except* the file layer: no attach,
--            no detach, no primary-text change, no scan, no browsing raw
--            NAS paths, no queueing an alignment run. Everything else —
--            statuses, queues, lists, projects, progress, reading
--            sessions, physical copies, page anchors, achievements — is
--            theirs exactly as it is anyone else's.
--
-- The oldest account becomes the admin: on a single-user install that is
-- the owner, and on a multi-user one it is whoever set the server up.
ALTER TABLE users ADD COLUMN role TEXT NOT NULL DEFAULT 'member'
    CHECK (role IN ('admin', 'member', 'reader'));

-- Disabling is deliberately not deletion. Deleting an account cascades
-- through its whole library — entries, progress, reading sessions, page
-- anchors, achievements — and that is the right behaviour for "this person
-- is gone", but the wrong one for "log this person out for now". A disabled
-- account keeps everything and simply cannot authenticate: the session
-- resolver stops recognising it, which invalidates every live session
-- without touching a row in `sessions`.
ALTER TABLE users ADD COLUMN disabled_at TIMESTAMP;

UPDATE users SET role = 'admin'
 WHERE id = (SELECT id FROM users ORDER BY created_at, id LIMIT 1);

-- Server settings that an admin can change from the UI. The environment is
-- the wrong home for these: turning self-service registration off is a
-- decision made once the friend has signed up, not a decision made when the
-- container is redeployed, and asking someone to edit a compose file and
-- restart the API to close their front door is how it stays open.
--
-- Values are stored as text and parsed by the reader, because there are two
-- of them and a typed column per setting is worse than a parse.
--
--   registration_enabled — whether POST /api/auth/register accepts a
--     request with no invite token. Defaults on, so an existing install
--     keeps working exactly as it did; the admin turns it off once
--     everyone who needs an account has one. An empty user table always
--     accepts a registration regardless, or a fresh install with the
--     setting off would have no way to create its first admin.
--   default_role — the role a self-service registration lands on. It
--     defaults to `reader` rather than `member`: a stranger who finds an
--     open registration form should not inherit the ability to rearrange
--     the file library, and an admin who wants the older behaviour can say
--     so in one click.
CREATE TABLE app_settings (
    key        TEXT PRIMARY KEY,
    value      TEXT NOT NULL,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

INSERT INTO app_settings (key, value) VALUES
    ('registration_enabled', 'true'),
    ('default_role', 'reader');

-- Invites are how accounts are made once the front door is shut. An admin
-- creates one carrying the role the account will land on; the recipient
-- follows the link and picks their own username and password, so no one
-- has to send a password over chat and no one ends up sharing one.
--
-- Only the hash of the token is stored. The token is a bearer credential —
-- whoever holds the link becomes the account — and a database that leaks
-- should not hand out working invites. It is shown exactly once, when it
-- is created.
--
-- Rows are kept after they are used: "who invited this account, and when"
-- is the only audit trail a self-hosted app of this size needs, and it
-- costs one row per person.
CREATE TABLE invites (
    id          TEXT PRIMARY KEY,
    token_hash  TEXT NOT NULL UNIQUE,
    -- Optional: pre-fills the sign-up form and records who it was for.
    email       TEXT NOT NULL DEFAULT '',
    role        TEXT NOT NULL CHECK (role IN ('admin', 'member', 'reader')),
    note        TEXT NOT NULL DEFAULT '',
    created_by  TEXT REFERENCES users(id) ON DELETE SET NULL,
    expires_at  TIMESTAMP NOT NULL,
    accepted_at TIMESTAMP,
    accepted_by TEXT REFERENCES users(id) ON DELETE SET NULL,
    revoked_at  TIMESTAMP,
    created_at  TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_invites_open ON invites(expires_at)
    WHERE accepted_at IS NULL AND revoked_at IS NULL;

-- Who attached a file. media_files.book_id is a property of the *book*, not
-- of any one library, which is what makes two people reading the same
-- audiobook share one timeline and one alignment. It also meant, until now,
-- that adding a work from Open Library was enough to stream whatever
-- someone else had attached to it: the text and audio endpoints resolved
-- entry -> book -> files with no further question asked.
--
-- That was invisible while every account belonged to the same person. With
-- a second account it is the difference between a library and a shared
-- drive, so attachments gain an owner and access gains a rule (see
-- book_shares below).
--
-- NULL means nobody owns it: pre-existing rows on an install that never had
-- an admin, and rows whose owner was deleted. Those stay open to everyone,
-- because a file with no owner has nobody to ask.
ALTER TABLE media_files ADD COLUMN attached_by TEXT REFERENCES users(id) ON DELETE SET NULL;

UPDATE media_files
   SET attached_by = (SELECT id FROM users WHERE role = 'admin' ORDER BY created_at, id LIMIT 1)
 WHERE book_id IS NOT NULL;

CREATE INDEX idx_media_files_attached_by ON media_files(attached_by)
    WHERE attached_by IS NOT NULL;

-- One share: "the files I attached to this book may be read and listened to
-- by this person". Keyed on the book rather than on my library entry,
-- because the files hang off the book and because the recipient reads
-- through their own entry — their own status, their own position, their own
-- reading sessions. Nothing about my copy is visible to them and nothing
-- about theirs is visible to me.
--
-- Revoking a share deletes this row and nothing else. The recipient keeps
-- their entry and their progress; the book simply goes back to being one
-- with no files attached, which is an ordinary state the app already
-- renders. Re-sharing later picks up exactly where they left off.
CREATE TABLE book_shares (
    id             TEXT PRIMARY KEY,
    book_id        TEXT NOT NULL REFERENCES books(id) ON DELETE CASCADE,
    owner_id       TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    shared_with_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at     TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (book_id, owner_id, shared_with_id),
    CHECK (owner_id <> shared_with_id)
);

CREATE INDEX idx_book_shares_recipient ON book_shares(shared_with_id, book_id);
CREATE INDEX idx_book_shares_owner ON book_shares(owner_id, book_id);

-- +goose Down

DROP TABLE IF EXISTS book_shares;

DROP INDEX IF EXISTS idx_media_files_attached_by;
ALTER TABLE media_files DROP COLUMN attached_by;

DROP TABLE IF EXISTS invites;
DROP TABLE IF EXISTS app_settings;

ALTER TABLE users DROP COLUMN disabled_at;
ALTER TABLE users DROP COLUMN role;
