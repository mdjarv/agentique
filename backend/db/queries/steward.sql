-- name: ListOpenStewardFindings :many
SELECT kind, subject, severity, remedy, facts, opened_at FROM steward_findings
WHERE resolved_at = ''
ORDER BY opened_at, id;

-- name: OpenStewardFinding :exec
INSERT INTO steward_findings (kind, subject, severity, remedy, facts, opened_at)
VALUES (sqlc.arg(kind), sqlc.arg(subject), sqlc.arg(severity), sqlc.arg(remedy), sqlc.arg(facts), sqlc.arg(opened_at))
ON CONFLICT (kind, subject) WHERE resolved_at = '' DO NOTHING;

-- name: RefreshStewardFinding :exec
UPDATE steward_findings SET severity = sqlc.arg(severity), remedy = sqlc.arg(remedy), facts = sqlc.arg(facts)
WHERE kind = sqlc.arg(kind) AND subject = sqlc.arg(subject) AND resolved_at = '';

-- name: ResolveStewardFinding :exec
UPDATE steward_findings SET resolved_at = sqlc.arg(resolved_at)
WHERE kind = sqlc.arg(kind) AND subject = sqlc.arg(subject) AND resolved_at = '';

-- name: PruneStewardFindings :execrows
DELETE FROM steward_findings WHERE resolved_at != '' AND resolved_at < ?;

-- The public ids of every peer credential this server has minted: who a
-- machine-wide event, a finding, is published to.
-- name: ListPeerCredentialIDs :many
SELECT id FROM auth_sessions
WHERE kind = 'peer' AND id IS NOT NULL AND expires_at > strftime('%Y-%m-%dT%H:%M:%SZ', 'now')
ORDER BY id;
