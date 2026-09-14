-- name: UpsertPeerFollow :exec
INSERT INTO peer_follows (session_id, credential_id, policy_id, followed_at)
VALUES (sqlc.arg(session_id), sqlc.arg(credential_id), sqlc.arg(policy_id), sqlc.arg(followed_at))
ON CONFLICT (session_id, credential_id) DO UPDATE
SET policy_id = excluded.policy_id, followed_at = excluded.followed_at;

-- name: ListPeerFollowers :many
SELECT credential_id FROM peer_follows WHERE session_id = ? ORDER BY credential_id;

-- name: InsertPeerOutbox :one
INSERT INTO peer_outbox (credential_id, kind, session_id, payload, at)
VALUES (sqlc.arg(credential_id), sqlc.arg(kind), sqlc.arg(session_id), sqlc.arg(payload), sqlc.arg(at))
RETURNING seq;

-- name: ListPeerOutboxSince :many
SELECT seq, kind, session_id, payload, at FROM peer_outbox
WHERE credential_id = sqlc.arg(credential_id) AND seq > sqlc.arg(since)
ORDER BY seq
LIMIT sqlc.arg(max_rows);

-- The newest seq one follower has, 0 when it has none: what a follower that
-- has never read starts from when it wants news from now on rather than the
-- whole retention window.
-- name: LatestPeerOutboxSeq :one
SELECT CAST(COALESCE(MAX(seq), 0) AS INTEGER) FROM peer_outbox WHERE credential_id = ?;

-- name: PrunePeerOutbox :execrows
DELETE FROM peer_outbox WHERE at < ?;
