-- name: CreateSession :one
INSERT INTO sessions (id, project_id, name, work_dir, worktree_path, worktree_branch, worktree_base_sha, state, model, permission_mode, auto_approve_mode, effort, max_budget, max_turns, behavior_presets, agent_profile_id)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?) RETURNING *;

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

-- name: UpdateSessionAutoApproveMode :exec
UPDATE sessions SET auto_approve_mode = ?, updated_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now') WHERE id = ?;

-- name: SetWorktreeMerged :exec
UPDATE sessions SET worktree_merged = 1, updated_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now') WHERE id = ?;

-- name: SetSessionCompleted :exec
UPDATE sessions SET completed_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now'), updated_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now') WHERE id = ?;

-- name: UnsetSessionCompleted :exec
UPDATE sessions SET completed_at = NULL, updated_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now') WHERE id = ?;

-- name: UnsetWorktreeMerged :exec
UPDATE sessions SET worktree_merged = 0, updated_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now') WHERE id = ?;

-- name: UpdateWorktreeBaseSHA :exec
UPDATE sessions SET worktree_base_sha = ?, updated_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now') WHERE id = ?;

-- name: UpdateSessionPRUrl :exec
UPDATE sessions SET pr_url = ?, updated_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now') WHERE id = ?;

-- name: RecoverStaleSessions :exec
UPDATE sessions SET state = 'stopped', updated_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now')
WHERE state IN ('running', 'merging');

-- name: ListAllSessions :many
SELECT * FROM sessions ORDER BY updated_at DESC;

-- name: UpdateSessionLastQueryAt :exec
UPDATE sessions SET last_query_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now'), updated_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now') WHERE id = ?;

-- name: UpdateSessionWorktree :exec
UPDATE sessions
SET work_dir = ?, worktree_path = ?, worktree_branch = ?, worktree_base_sha = ?, worktree_merged = 0,
    updated_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now')
WHERE id = ?;

-- name: GetActiveSessionByAgentProfile :one
SELECT * FROM sessions
WHERE agent_profile_id = ?
  AND completed_at IS NULL
  AND state NOT IN ('done', 'stopped', 'failed')
ORDER BY created_at DESC LIMIT 1;

-- name: DeleteSession :exec
DELETE FROM sessions WHERE id = ?;
