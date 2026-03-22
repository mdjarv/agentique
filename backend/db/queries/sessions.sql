-- name: CreateSession :one
INSERT INTO sessions (id, project_id, name, work_dir, worktree_path, worktree_branch, state)
VALUES (?, ?, ?, ?, ?, ?, ?) RETURNING *;

-- name: GetSession :one
SELECT * FROM sessions WHERE id = ?;

-- name: ListSessionsByProject :many
SELECT * FROM sessions WHERE project_id = ? ORDER BY created_at ASC;

-- name: UpdateSessionState :exec
UPDATE sessions SET state = ?, updated_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now') WHERE id = ?;

-- name: UpdateSessionName :exec
UPDATE sessions SET name = ?, updated_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now') WHERE id = ?;

-- name: DeleteSession :exec
DELETE FROM sessions WHERE id = ?;
