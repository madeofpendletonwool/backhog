-- +goose NO TRANSACTION

-- +goose Up

-- media_skipped.reason gains 'drm_pdf': a .pdf carrying an /Encrypt
-- dictionary, refused whole at scan time (pdf.ErrDRM's stance). The DRM
-- refusal keeps its per-container name — 'drm_epub' said which lock was
-- found, 'drm_mobi' said it for Kindle files, and an encrypted PDF deserves
-- the same honesty. SQLite cannot alter a CHECK constraint, so this is the
-- rebuild recipe from 00015, 00020 and 00023 again. Rows are replaced per
-- root on each scan, so nothing is backfilled: the next scan re-labels
-- every encrypted PDF itself.
PRAGMA foreign_keys=OFF;

-- +goose StatementBegin
CREATE TABLE media_skipped_new (
    id         INTEGER PRIMARY KEY,
    root       TEXT NOT NULL,
    path       TEXT NOT NULL,
    ext        TEXT NOT NULL,
    reason     TEXT NOT NULL CHECK (reason IN ('unsupported_extension','drm_epub','drm_mobi','drm_pdf','format_unhandled','sidecar_metadata')),
    size_bytes INTEGER NOT NULL,
    mtime      INTEGER NOT NULL,
    seen_at    TIMESTAMP NOT NULL,
    UNIQUE (root, path)
);
-- +goose StatementEnd

INSERT INTO media_skipped_new (id, root, path, ext, reason, size_bytes, mtime, seen_at)
SELECT id, root, path, ext, reason, size_bytes, mtime, seen_at FROM media_skipped;

DROP TABLE media_skipped;

ALTER TABLE media_skipped_new RENAME TO media_skipped;

CREATE INDEX idx_media_skipped_root ON media_skipped(root);

PRAGMA foreign_keys=ON;

-- +goose Down

PRAGMA foreign_keys=OFF;

-- Rows carrying the new reason have no pre-00028 spelling; they are dropped
-- rather than relabelled, because the next scan rewrites this table for
-- every root anyway.
DELETE FROM media_skipped WHERE reason NOT IN ('unsupported_extension','drm_epub','drm_mobi','format_unhandled','sidecar_metadata');

-- +goose StatementBegin
CREATE TABLE media_skipped_old (
    id         INTEGER PRIMARY KEY,
    root       TEXT NOT NULL,
    path       TEXT NOT NULL,
    ext        TEXT NOT NULL,
    reason     TEXT NOT NULL CHECK (reason IN ('unsupported_extension','drm_epub','drm_mobi','format_unhandled','sidecar_metadata')),
    size_bytes INTEGER NOT NULL,
    mtime      INTEGER NOT NULL,
    seen_at    TIMESTAMP NOT NULL,
    UNIQUE (root, path)
);
-- +goose StatementEnd

INSERT INTO media_skipped_old (id, root, path, ext, reason, size_bytes, mtime, seen_at)
SELECT id, root, path, ext, reason, size_bytes, mtime, seen_at FROM media_skipped;

DROP TABLE media_skipped;

ALTER TABLE media_skipped_old RENAME TO media_skipped;

CREATE INDEX idx_media_skipped_root ON media_skipped(root);

PRAGMA foreign_keys=ON;
