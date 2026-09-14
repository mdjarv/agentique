-- The acting server's half of acting across machines (docs/peers.md).
--
--   machines.peer_token      -- the peer credential this server presents to that
--                               machine's /api/peer/* routes. OUTBOUND, so it is
--                               plaintext for the reason machines.token is: a
--                               digest could not be presented.
--   machines.peer_session_id -- that credential's public id on the remote, what
--                               a rotation names so the old one is deleted
--   machines.peer_cursor     -- the last outbox seq this server applied from that
--                               machine; -1 until the first read, which starts
--                               from "now" rather than replaying a week of news
--   assistant_peer_follows   -- the assistant's watch list for sessions on
--                               paired machines
--
-- assistant_peer_follows is its own table rather than assistant_follows with a
-- machine column: assistant_follows cascades from sessions(id), which is right
-- for this machine's sessions and impossible for another machine's, and the two
-- lists are read by different paths anyway.
--
-- Keep this comment ASCII. sqlc expands `SELECT *` by byte offset, so one
-- multi-byte character shifts those offsets and corrupts the generated code
-- for LATER queries.

-- +goose Up

ALTER TABLE machines ADD COLUMN peer_token TEXT NOT NULL DEFAULT '';
ALTER TABLE machines ADD COLUMN peer_session_id TEXT NOT NULL DEFAULT '';
ALTER TABLE machines ADD COLUMN peer_cursor INTEGER NOT NULL DEFAULT -1;

CREATE TABLE assistant_peer_follows (
    machine_id TEXT NOT NULL REFERENCES machines(machine_id) ON DELETE CASCADE,
    session_id TEXT NOT NULL,
    since      TEXT NOT NULL,
    source     TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (machine_id, session_id)
);

CREATE INDEX idx_assistant_peer_follows_session ON assistant_peer_follows(session_id);

-- +goose Down

DROP INDEX IF EXISTS idx_assistant_peer_follows_session;
DROP TABLE IF EXISTS assistant_peer_follows;
ALTER TABLE machines DROP COLUMN peer_cursor;
ALTER TABLE machines DROP COLUMN peer_session_id;
ALTER TABLE machines DROP COLUMN peer_token;
