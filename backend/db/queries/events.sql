-- name: InsertEvent :exec
INSERT INTO session_events (session_id, turn_index, seq, type, data) VALUES (?, ?, ?, ?, ?);

-- name: InsertEventWithMessageID :exec
INSERT INTO session_events (session_id, turn_index, seq, type, data, message_id) VALUES (?, ?, ?, ?, ?, ?);

-- name: ListEventsBySession :many
SELECT * FROM session_events WHERE session_id = ? ORDER BY turn_index, seq;

-- name: ListRecentEventsBySession :many
SELECT e.* FROM session_events e
WHERE e.session_id = ?
  AND e.turn_index >= (
    SELECT COALESCE(MAX(sub.turn_index), 0) - CAST(? AS INTEGER) + 1
    FROM session_events sub WHERE sub.session_id = e.session_id
  )
ORDER BY e.turn_index, e.seq;

-- name: CountTurnsBySession :one
SELECT CAST(COALESCE(MAX(turn_index) + 1, 0) AS INTEGER) FROM session_events WHERE session_id = ?;

-- name: MaxTurnIndex :one
SELECT CAST(COALESCE(MAX(turn_index), -1) AS INTEGER) FROM session_events WHERE session_id = ?;

-- name: SessionSummariesByProject :many
SELECT
  s.id AS session_id,
  CAST(COALESCE(MAX(e.turn_index) + 1, 0) AS INTEGER) AS turn_count,
  CAST(COALESCE(SUM(CASE WHEN e.type = 'result' THEN json_extract(e.data, '$.cost') ELSE 0 END), 0) AS REAL) AS total_cost
FROM sessions s
LEFT JOIN session_events e ON e.session_id = s.id
WHERE s.project_id = ?
GROUP BY s.id;

-- name: AllSessionSummaries :many
SELECT
  s.id AS session_id,
  CAST(COALESCE(MAX(e.turn_index) + 1, 0) AS INTEGER) AS turn_count,
  CAST(COALESCE(SUM(CASE WHEN e.type = 'result' THEN json_extract(e.data, '$.cost') ELSE 0 END), 0) AS REAL) AS total_cost
FROM sessions s
LEFT JOIN session_events e ON e.session_id = s.id
GROUP BY s.id;
