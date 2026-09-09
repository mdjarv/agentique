-- name: UpsertModelResolution :exec
INSERT INTO model_resolutions (provider, slug, resolved_id, last_seen_at)
VALUES (?, ?, ?, strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
ON CONFLICT (provider, slug) DO UPDATE SET
    resolved_id = excluded.resolved_id,
    last_seen_at = excluded.last_seen_at;

-- name: GetModelResolution :one
-- One learned mapping, for a session whose own run never reported a model: the
-- last_seen_at that comes back is what dates the answer.
SELECT * FROM model_resolutions WHERE provider = ? AND slug = ?;

-- name: ListModelResolutions :many
SELECT * FROM model_resolutions ORDER BY provider, slug;
