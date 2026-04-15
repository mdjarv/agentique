-- +goose Up

-- Canonical message storage — one row per message, replaces dual-copy model.
CREATE TABLE messages (
    id TEXT PRIMARY KEY,
    channel_id TEXT NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
    sender_type TEXT NOT NULL,
    sender_id TEXT NOT NULL,
    sender_name TEXT NOT NULL DEFAULT '',
    content TEXT NOT NULL,
    message_type TEXT NOT NULL DEFAULT 'message',
    metadata TEXT NOT NULL DEFAULT '{}',
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%f', 'now'))
);

CREATE INDEX idx_messages_channel ON messages(channel_id, created_at);
CREATE INDEX idx_messages_sender ON messages(sender_id, created_at);

-- Delivery fan-out — one row per recipient per message.
CREATE TABLE message_deliveries (
    message_id TEXT NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
    recipient_session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    status TEXT NOT NULL DEFAULT 'pending',
    delivered_at TEXT,
    PRIMARY KEY (message_id, recipient_session_id)
);

CREATE INDEX idx_deliveries_recipient ON message_deliveries(recipient_session_id, status);

-- Link session events to canonical messages for correlation.
ALTER TABLE session_events ADD COLUMN message_id TEXT REFERENCES messages(id) ON DELETE SET NULL;

-- +goose Down

-- SQLite cannot DROP COLUMN if it has an FK constraint in older versions.
-- Rebuild session_events without the message_id column.
CREATE TABLE session_events_backup AS SELECT id, session_id, turn_index, seq, type, data, created_at FROM session_events;
DROP TABLE session_events;
CREATE TABLE session_events (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    turn_index INTEGER NOT NULL,
    seq INTEGER NOT NULL,
    type TEXT NOT NULL,
    data TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%f', 'now'))
);
INSERT INTO session_events (id, session_id, turn_index, seq, type, data, created_at)
    SELECT id, session_id, turn_index, seq, type, data, created_at FROM session_events_backup;
DROP TABLE session_events_backup;
CREATE INDEX idx_session_events_session ON session_events(session_id, turn_index, seq);

DROP TABLE IF EXISTS message_deliveries;
DROP TABLE IF EXISTS messages;
