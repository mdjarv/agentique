-- name: CreateSession :one
INSERT INTO sessions (id, project_id, name, work_dir, worktree_path, worktree_branch, worktree_base_sha, state, model, permission_mode, auto_approve)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?) RETURNING *;

-- name: GetSession :one
SELECT * FROM sessions WHERE id = ?;

-- name: ListSessionsByProject :many
SELECT * FROM sessions WHERE project_id = ? ORDER BY created_at ASC;

-- name: UpdateSessionState :exec
UPDATE sessions SET state = ?, updated_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now') WHERE id = ?;

-- name: UpdateSessionName :exec
UPDATE sessions SET name = ?, updated_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now') WHERE id = ?;

-- name: UpdateClaudeSessionID :exec
UPDATE sessions SET claude_session_id = ?, updated_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now') WHERE id = ?;

-- name: UpdateSessionModel :exec
UPDATE sessions SET model = ?, updated_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now') WHERE id = ?;

-- name: UpdateSessionPermissionMode :exec
UPDATE sessions SET permission_mode = ?, updated_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now') WHERE id = ?;

-- name: UpdateSessionAutoApprove :exec
UPDATE sessions SET auto_approve = ?, updated_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now') WHERE id = ?;

-- name: SetWorktreeMerged :exec
UPDATE sessions SET worktree_merged = 1, updated_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now') WHERE id = ?;

-- name: UpdateWorktreeBaseSHA :exec
UPDATE sessions SET worktree_base_sha = ?, updated_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now') WHERE id = ?;

-- name: UpdateSessionPRUrl :exec
UPDATE sessions SET pr_url = ?, updated_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now') WHERE id = ?;

-- name: DeleteSession :exec
DELETE FROM sessions WHERE id = ?;
