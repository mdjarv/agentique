-- name: InsertMessage :one
INSERT INTO messages (id, channel_id, sender_type, sender_id, sender_name, content, message_type, metadata)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: GetMessage :one
SELECT * FROM messages WHERE id = ?;

-- name: ListMessagesByChannel :many
SELECT * FROM messages WHERE channel_id = ? ORDER BY created_at ASC;

-- name: DeleteMessagesByChannel :exec
DELETE FROM messages WHERE channel_id = ?;
