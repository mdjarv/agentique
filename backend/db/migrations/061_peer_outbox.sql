-- What a paired server follows here, and what it has not read yet
-- (docs/peers.md, the owner's surface).
--
--   peer_follows -- a paired server sent to or created this session, so what
--                   the session does next is news to it
--   peer_outbox  -- that news, one row per follower, in the order it happened
--
-- The outbox is a log, not a queue: rows are never marked read. A follower
-- reads everything after the last seq it applied and keeps that cursor itself,
-- so a poll that is lost, retried or duplicated changes nothing here, and a
-- server that was asleep for a day catches up with one read. Rows age out after
-- a retention window instead, which is the only delete.
--
-- credential_id is the public id of the peer credential (auth_sessions.id) the
-- follow was made with. It is not a foreign key: auth_sessions keys on the token
-- digest, and a revoked credential's undelivered rows simply age out.
--
-- Timestamps are UTC RFC3339 SECONDS, so they compare as text.
--
-- Keep this comment ASCII. sqlc expands `SELECT *` by byte offset, so one
-- multi-byte character shifts those offsets and corrupts the generated code
-- for LATER queries.

-- +goose Up

CREATE TABLE peer_follows (
    session_id    TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    credential_id TEXT NOT NULL,
    -- The standing instruction the latest send carried, '' for asked-for work.
    -- Recorded for the owner's own answer about what is in flight under a
    -- policy; never read as permission.
    policy_id     TEXT NOT NULL DEFAULT '',
    followed_at   TEXT NOT NULL,
    PRIMARY KEY (session_id, credential_id)
);

CREATE TABLE peer_outbox (
    seq           INTEGER PRIMARY KEY AUTOINCREMENT,
    credential_id TEXT NOT NULL,
    kind          TEXT NOT NULL,
    session_id    TEXT NOT NULL,
    payload       TEXT NOT NULL DEFAULT '{}',
    at            TEXT NOT NULL
);

-- The poll's read: one follower's rows after its cursor.
CREATE INDEX idx_peer_outbox_credential_seq ON peer_outbox(credential_id, seq);

-- Retention's read.
CREATE INDEX idx_peer_outbox_at ON peer_outbox(at);

-- +goose Down

DROP INDEX IF EXISTS idx_peer_outbox_at;
DROP INDEX IF EXISTS idx_peer_outbox_credential_seq;
DROP TABLE IF EXISTS peer_outbox;
DROP TABLE IF EXISTS peer_follows;
