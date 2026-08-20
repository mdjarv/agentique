-- +goose Up
-- Multi-machine M0 (docs/multi-machine.md): one-time pairing tokens
-- exchanged for bearer auth sessions, plus session metadata so sessions can be
-- listed and revoked individually. Timestamps use UTC RFC3339 seconds
-- precision — SQLite compares TEXT lexicographically.
CREATE TABLE pairing_tokens (
    token TEXT PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    expires_at TEXT NOT NULL,
    used_at TEXT,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);

-- Public identifier for list/revoke (the token itself is secret material and
-- must never be echoed back), a client-supplied label ("phone", "workstation"),
-- and the credential kind: 'cookie' (WebAuthn login) or 'bearer' (pairing).
ALTER TABLE auth_sessions ADD COLUMN id TEXT;
ALTER TABLE auth_sessions ADD COLUMN label TEXT NOT NULL DEFAULT '';
ALTER TABLE auth_sessions ADD COLUMN kind TEXT NOT NULL DEFAULT 'cookie';
UPDATE auth_sessions SET id = lower(hex(randomblob(8)));
CREATE UNIQUE INDEX idx_auth_sessions_id ON auth_sessions(id);

-- +goose Down
DROP INDEX IF EXISTS idx_auth_sessions_id;
ALTER TABLE auth_sessions DROP COLUMN kind;
ALTER TABLE auth_sessions DROP COLUMN label;
ALTER TABLE auth_sessions DROP COLUMN id;
DROP TABLE IF EXISTS pairing_tokens;
