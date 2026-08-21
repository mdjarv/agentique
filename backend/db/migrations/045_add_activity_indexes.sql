-- +goose Up

-- Global activity feed (wire.list) scans session_events and messages by
-- created_at across all projects — index the bare column on both tables.
-- messages only had composite (channel_id|sender_id, created_at) indexes.
CREATE INDEX idx_session_events_created ON session_events(created_at);
CREATE INDEX idx_messages_created ON messages(created_at);

-- +goose Down
DROP INDEX idx_messages_created;
DROP INDEX idx_session_events_created;
