-- +goose Up
-- +goose StatementBegin

-- Backfill messages from existing agent_message session_events.
-- Take only "sent" copies (agent-to-agent) and fromUser copies (user broadcasts).
-- Prefer channelId from JSON data; fall back to channel_members JOIN.
-- GROUP BY deduplicates user broadcasts (which were stored N times).
INSERT OR IGNORE INTO messages (id, channel_id, sender_type, sender_id, sender_name, content, message_type, metadata, created_at)
SELECT
    'migrated-' || CAST(se.id AS TEXT),
    COALESCE(
        NULLIF(json_extract(se.data, '$.channelId'), ''),
        cm.channel_id
    ),
    CASE WHEN json_extract(se.data, '$.fromUser') = 1 THEN 'user' ELSE 'session' END,
    CASE WHEN json_extract(se.data, '$.fromUser') = 1 THEN ''
         ELSE COALESCE(json_extract(se.data, '$.senderSessionId'), se.session_id) END,
    COALESCE(json_extract(se.data, '$.senderName'), ''),
    COALESCE(json_extract(se.data, '$.content'), ''),
    COALESCE(NULLIF(json_extract(se.data, '$.messageType'), ''), 'message'),
    json_object(
        'targetSessionId', COALESCE(json_extract(se.data, '$.targetSessionId'), ''),
        'targetName', COALESCE(json_extract(se.data, '$.targetName'), '')
    ),
    se.created_at
FROM session_events se
LEFT JOIN channel_members cm ON cm.session_id = se.session_id
WHERE se.type = 'agent_message'
  AND se.message_id IS NULL
  AND (
    json_extract(se.data, '$.direction') = 'sent'
    OR json_extract(se.data, '$.fromUser') = 1
  )
  AND COALESCE(
        NULLIF(json_extract(se.data, '$.channelId'), ''),
        cm.channel_id
  ) IS NOT NULL
GROUP BY COALESCE(
            NULLIF(json_extract(se.data, '$.channelId'), ''),
            cm.channel_id
         ),
         se.created_at,
         COALESCE(json_extract(se.data, '$.content'), '');

-- Link the original session_events to their migrated messages.
UPDATE session_events
SET message_id = 'migrated-' || CAST(id AS TEXT)
WHERE type = 'agent_message'
  AND message_id IS NULL
  AND (
    json_extract(data, '$.direction') = 'sent'
    OR json_extract(data, '$.fromUser') = 1
  )
  AND EXISTS (SELECT 1 FROM messages WHERE messages.id = 'migrated-' || CAST(session_events.id AS TEXT));

-- +goose StatementEnd

-- +goose Down
DELETE FROM messages WHERE id LIKE 'migrated-%';
UPDATE session_events SET message_id = NULL WHERE message_id LIKE 'migrated-%';
