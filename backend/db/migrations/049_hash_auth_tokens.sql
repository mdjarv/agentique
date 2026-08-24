-- +goose Up
-- Store bearer credentials as digests, not as the credentials themselves.
--
-- auth_sessions.token, pairing_tokens.token and invite_tokens.token were the
-- literal secrets a client presents. Anything that reads the database file —
-- a backup copied elsewhere, a disk, a DB handed over for debugging — reads
-- live credentials. A digest is enough to VERIFY a presented token, so the
-- recoverable form has no reason to be here.
--
-- SHA-256 with no salt or stretching is correct for this input and would be
-- wrong for a password: these are 256-bit (pairing/invite: ~60-bit) random
-- values from crypto/rand, so there is no dictionary to attack and per-row
-- salting would only prevent the lookup this table exists to do.
--
-- The plaintext column is DROPPED rather than backfilled, because SQLite has
-- no SHA-256 and a Go backfill would have to keep the secrets readable through
-- one more startup to remove them. Consequence, stated plainly: every existing
-- session is invalidated by this migration. Browsers log in again, and each
-- paired machine must be re-paired (`agentique pair`). One-time cost, paid
-- once, for credentials that stop being recoverable at rest.
--
-- machines.token is deliberately NOT hashed: it is an OUTBOUND credential this
-- server presents to a remote, so it must stay recoverable. Protecting it
-- needs a different mechanism than hashing (see CLAUDE.md, security invariants).

DROP INDEX IF EXISTS idx_auth_sessions_id;

CREATE TABLE auth_sessions_hashed (
    token_hash TEXT PRIMARY KEY,
    id TEXT,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    expires_at TEXT NOT NULL,
    label TEXT NOT NULL DEFAULT '',
    kind TEXT NOT NULL DEFAULT 'cookie',
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);
DROP TABLE auth_sessions;
ALTER TABLE auth_sessions_hashed RENAME TO auth_sessions;
CREATE UNIQUE INDEX idx_auth_sessions_id ON auth_sessions(id);

CREATE TABLE pairing_tokens_hashed (
    token_hash TEXT PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    expires_at TEXT NOT NULL,
    used_at TEXT,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);
DROP TABLE pairing_tokens;
ALTER TABLE pairing_tokens_hashed RENAME TO pairing_tokens;

CREATE TABLE invite_tokens_hashed (
    token_hash TEXT PRIMARY KEY,
    created_by TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    expires_at TEXT NOT NULL,
    used_by TEXT REFERENCES users(id) ON DELETE SET NULL,
    used_at TEXT,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);
DROP TABLE invite_tokens;
ALTER TABLE invite_tokens_hashed RENAME TO invite_tokens;

-- +goose Down
-- Downgrading restores the plaintext-token shape but cannot restore the tokens
-- themselves — a digest does not invert. Rows are dropped, not migrated.
DROP INDEX IF EXISTS idx_auth_sessions_id;

CREATE TABLE auth_sessions_plain (
    token TEXT PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    expires_at TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    id TEXT,
    label TEXT NOT NULL DEFAULT '',
    kind TEXT NOT NULL DEFAULT 'cookie'
);
DROP TABLE auth_sessions;
ALTER TABLE auth_sessions_plain RENAME TO auth_sessions;
CREATE UNIQUE INDEX idx_auth_sessions_id ON auth_sessions(id);

CREATE TABLE pairing_tokens_plain (
    token TEXT PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    expires_at TEXT NOT NULL,
    used_at TEXT,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);
DROP TABLE pairing_tokens;
ALTER TABLE pairing_tokens_plain RENAME TO pairing_tokens;

CREATE TABLE invite_tokens_plain (
    token TEXT PRIMARY KEY,
    created_by TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    expires_at TEXT NOT NULL,
    used_by TEXT REFERENCES users(id) ON DELETE SET NULL,
    used_at TEXT,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);
DROP TABLE invite_tokens;
ALTER TABLE invite_tokens_plain RENAME TO invite_tokens;
