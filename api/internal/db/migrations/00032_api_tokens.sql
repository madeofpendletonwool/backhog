-- +goose Up

-- Personal API tokens: bearer credentials that let an external client (an
-- MCP server, a script, anything that can hold a secret) act as one user.
-- The cookie stays the browser's credential; a token is the same person's
-- key to the API from outside the web app.
--
-- Only the hash of the secret is stored, for the same reason invites hash
-- theirs: the secret is 256 bits of uniform randomness, so an unsalted
-- SHA-256 has no dictionary to precompute against, and a database that
-- leaks should not hand out working credentials. The plaintext exists in
-- exactly one place — the response that created it — and carries the bh_
-- prefix so a leaked one is greppable in logs and chat logs alike.
--
-- Scopes are a comma-separated list ('books:read'), validated in code.
-- Rows are kept after revocation rather than deleted: "I issued this,
-- it leaked, I killed it" is the only audit trail a token needs, and it
-- costs one row. last_used_at is a coarse "is this thing still alive"
-- signal for the settings page, touched at most once a minute — it is
-- not per-request activity tracking.
CREATE TABLE api_tokens (
    id           TEXT PRIMARY KEY,
    user_id      TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name         TEXT NOT NULL,
    token_hash   TEXT NOT NULL UNIQUE,
    scopes       TEXT NOT NULL DEFAULT 'books:read',
    created_at   TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_used_at TIMESTAMP,
    expires_at   TIMESTAMP,
    revoked_at   TIMESTAMP
);

CREATE INDEX idx_api_tokens_user ON api_tokens(user_id);

-- +goose Down

DROP TABLE IF EXISTS api_tokens;
