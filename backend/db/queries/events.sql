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

-- Two correlated subqueries rather than a join: the join read every event
-- row of every session in the project (117ms for one project on a live
-- database) where the turn count needs only the index and the cost only the
-- result rows.
-- name: SessionSummariesByProject :many
SELECT
  s.id AS session_id,
  CAST((SELECT COALESCE(MAX(turn_index) + 1, 0) FROM session_events WHERE session_id = s.id) AS INTEGER) AS turn_count,
  CAST((SELECT COALESCE(SUM(json_extract(data, '$.cost')), 0) FROM session_events WHERE session_id = s.id AND type = 'result') AS REAL) AS total_cost
FROM sessions s
WHERE s.project_id = ?;

-- name: AllSessionSummaries :many
SELECT
  s.id AS session_id,
  CAST((SELECT COALESCE(MAX(turn_index) + 1, 0) FROM session_events WHERE session_id = s.id) AS INTEGER) AS turn_count,
  CAST((SELECT COALESCE(SUM(json_extract(data, '$.cost')), 0) FROM session_events WHERE session_id = s.id AND type = 'result') AS REAL) AS total_cost
FROM sessions s;

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

-- name: GetSessionEvent :one
SELECT * FROM session_events WHERE id = ? AND session_id = ?;
