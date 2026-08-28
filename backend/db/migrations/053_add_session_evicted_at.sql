-- Why a session's process went away, for the one case agentique caused.
--
-- Idle eviction reclaims an idle session's CLI through the same StopSession a
-- person's stop button takes, so the row it leaves is indistinguishable from a
-- deliberate stop. Every surface then read it as one: the chat pane announced
-- "Session interrupted" and offered a Resume button for a session nothing had
-- interrupted, which the next message would have resumed anyway.
--
-- NULL means the stop was not an eviction -- a person stopped it, or a restart
-- reaped it, both of which really did end something. A value is the UTC RFC3339
-- seconds stamp of the sweep that reclaimed it, because SQLite compares TEXT
-- lexicographically and that is the one format that sorts as time.
--
-- It is cleared when the session is resumed, so it always describes the most
-- recent stop rather than an older one.
--
-- Keep this comment ASCII. sqlc expands `SELECT *` by byte offset, so a
-- multi-byte character shifts those offsets and corrupts the generated code
-- for LATER queries.

-- +goose Up
ALTER TABLE sessions ADD COLUMN evicted_at TEXT;

-- +goose Down
ALTER TABLE sessions DROP COLUMN evicted_at;
