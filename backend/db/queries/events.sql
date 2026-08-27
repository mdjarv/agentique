-- name: InsertEvent :exec
INSERT INTO session_events (session_id, turn_index, seq, type, data) VALUES (?, ?, ?, ?, ?);

-- name: InsertEventWithMessageID :exec
INSERT INTO session_events (session_id, turn_index, seq, type, data, message_id) VALUES (?, ?, ?, ?, ?, ?);

-- name: ListEventsBySession :many
SELECT * FROM session_events WHERE session_id = ? ORDER BY turn_index, seq, id;

-- name: ListRecentEventsBySession :many
SELECT e.* FROM session_events e
WHERE e.session_id = ?
  AND e.turn_index >= (
    SELECT COALESCE(MAX(sub.turn_index), 0) - CAST(? AS INTEGER) + 1
    FROM session_events sub WHERE sub.session_id = e.session_id
  )
ORDER BY e.turn_index, e.seq, e.id;

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

-- name: TodaySpendByProvider :many
-- What this server itself spent today, per provider, from its own turn results.
-- Deliberately NOT a scan of the CLI's transcripts: agentique is the thing that
-- ran these turns, so it already knows, and this answers for every provider
-- rather than only the one that writes JSONL.
-- 'now' is local (no 'utc' modifier) so "today" means the operator's day.
SELECT
  s.provider AS provider,
  CAST(COALESCE(SUM(
    COALESCE(json_extract(e.data, '$.inputTokens'), 0) +
    COALESCE(json_extract(e.data, '$.outputTokens'), 0)
  ), 0) AS INTEGER) AS tokens,
  CAST(COUNT(*) AS INTEGER) AS prompts
FROM session_events e
JOIN sessions s ON s.id = e.session_id
WHERE e.type = 'result'
  AND date(e.created_at) = date('now', 'localtime')
GROUP BY s.provider;
