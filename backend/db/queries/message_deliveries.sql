-- name: InsertMessageDelivery :exec
INSERT INTO message_deliveries (message_id, recipient_session_id, status)
VALUES (?, ?, ?);

-- name: UpdateDeliveryStatus :exec
UPDATE message_deliveries
SET status = ?, delivered_at = strftime('%Y-%m-%dT%H:%M:%f', 'now')
WHERE message_id = ? AND recipient_session_id = ?;

-- name: ListPendingDeliveriesForSession :many
SELECT md.*, m.content, m.sender_name, m.message_type, m.metadata, m.channel_id
FROM message_deliveries md
JOIN messages m ON m.id = md.message_id
WHERE md.recipient_session_id = ? AND md.status = 'pending'
ORDER BY m.created_at ASC;
