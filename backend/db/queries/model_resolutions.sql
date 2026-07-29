-- name: UpsertModelResolution :exec
INSERT INTO model_resolutions (provider, slug, resolved_id, last_seen_at)
VALUES (?, ?, ?, strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
ON CONFLICT (provider, slug) DO UPDATE SET
    resolved_id = excluded.resolved_id,
    last_seen_at = excluded.last_seen_at;

-- name: ListModelResolutions :many
SELECT * FROM model_resolutions ORDER BY provider, slug;
