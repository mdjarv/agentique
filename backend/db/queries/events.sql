-- name: InsertEvent :exec
INSERT INTO session_events (session_id, turn_index, seq, type, data) VALUES (?, ?, ?, ?, ?);

-- name: ListEventsBySession :many
SELECT * FROM session_events WHERE session_id = ? ORDER BY turn_index, seq;

-- name: MaxTurnIndex :one
SELECT CAST(COALESCE(MAX(turn_index), -1) AS INTEGER) FROM session_events WHERE session_id = ?;
