-- +goose Up
CREATE TABLE channel_members (
    channel_id TEXT NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
    session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    role TEXT NOT NULL DEFAULT '',
    joined_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    PRIMARY KEY (channel_id, session_id)
);
CREATE INDEX idx_channel_members_session ON channel_members(session_id);
CREATE INDEX idx_channel_members_channel ON channel_members(channel_id);

-- Migrate existing 1:1 memberships into the join table.
INSERT INTO channel_members (channel_id, session_id, role, joined_at)
SELECT channel_id, id, channel_role, created_at FROM sessions WHERE channel_id IS NOT NULL;

-- Drop the old index (columns left in place to avoid SQLite table rebuild).
DROP INDEX IF EXISTS idx_sessions_channel;

-- +goose Down
-- Restore 1:1 columns from join table (picks first membership per session).
UPDATE sessions SET
  channel_id = (SELECT cm.channel_id FROM channel_members cm WHERE cm.session_id = sessions.id LIMIT 1),
  channel_role = COALESCE((SELECT cm.role FROM channel_members cm WHERE cm.session_id = sessions.id LIMIT 1), '');
CREATE INDEX idx_sessions_channel ON sessions(channel_id);

DROP TABLE IF EXISTS channel_members;
