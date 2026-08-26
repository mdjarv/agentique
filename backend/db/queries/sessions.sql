-- name: CreateSession :one
INSERT INTO sessions (id, project_id, name, work_dir, worktree_path, worktree_branch, worktree_base_sha, state, model, permission_mode, auto_approve_mode, effort, max_budget, max_turns, behavior_presets, agent_profile_id, parent_session_id, provider)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?) RETURNING *;

-- name: ListChildSessions :many
SELECT * FROM sessions WHERE parent_session_id = ? ORDER BY created_at ASC;

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
UPDATE sessions SET model = ?, resolved_model = '', updated_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now') WHERE id = ?;

-- name: UpdateSessionResolvedModel :exec
UPDATE sessions SET resolved_model = ?, updated_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now') WHERE id = ?;

-- name: UpdateSessionPermissionMode :exec
UPDATE sessions SET permission_mode = ?, updated_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now') WHERE id = ?;

-- name: UpdateSessionAutoApproveMode :exec
UPDATE sessions SET auto_approve_mode = ?, updated_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now') WHERE id = ?;

-- name: UpdateSessionPinned :one
UPDATE sessions SET pinned = ?, pin_order = ?, updated_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now') WHERE id = ? RETURNING *;

-- name: SetWorktreeMerged :exec
UPDATE sessions SET worktree_merged = 1, updated_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now') WHERE id = ?;

-- name: SetSessionArchived :exec
-- Archiving releases the pin. Pinned means "keep this at the top" and archived
-- means "stow this away": a session cannot be both, and leaving the pin set kept
-- filed-away work sitting in the priority section. Clearing it here rather than
-- at each caller means no archive path (the RPC, a completing merge, a local
-- session's commit) can forget to.
--
-- Keep this comment ASCII. sqlc expands `SELECT *` by byte offset, so a
-- multi-byte character anywhere in this file shifts those offsets and corrupts
-- the generated code for LATER queries.
UPDATE sessions SET archived_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now'), pinned = 0, pin_order = 0, updated_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now') WHERE id = ?;

-- name: UnsetSessionArchived :exec
UPDATE sessions SET archived_at = NULL, updated_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now') WHERE id = ?;

-- name: SetSessionUnseenCompletedAt :exec
-- Stamps "this finished while nobody was reading it". The timestamp is a
-- parameter rather than strftime('now') because the caller is the turn-end
-- seam, which already knows when the turn completed; it must be UTC RFC3339
-- seconds ("2006-01-02T15:04:05Z") like every other timestamp here, since
-- SQLite compares TEXT lexicographically.
UPDATE sessions SET unseen_completed_at = ?, updated_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now') WHERE id = ?;

-- name: ClearSessionUnseenCompletedAt :exec
-- The read receipt. Idempotent by construction: clearing an already-clear row
-- touches nothing the client can see.
UPDATE sessions SET unseen_completed_at = NULL, updated_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now') WHERE id = ?;

-- name: UnsetWorktreeMerged :exec
UPDATE sessions SET worktree_merged = 0, updated_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now') WHERE id = ?;

-- name: UpdateWorktreeBaseSHA :exec
UPDATE sessions SET worktree_base_sha = ?, updated_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now') WHERE id = ?;

-- name: UpdateSessionPRUrl :exec
UPDATE sessions SET pr_url = ?, updated_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now') WHERE id = ?;

-- name: UpdateSessionBehaviorPresets :exec
UPDATE sessions SET behavior_presets = ? WHERE id = ?;

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
  AND archived_at IS NULL
  AND state NOT IN ('done', 'stopped', 'failed')
ORDER BY created_at DESC LIMIT 1;

-- name: CountActiveSessionsByProject :one
SELECT COUNT(*) FROM sessions
WHERE project_id = ? AND archived_at IS NULL AND state NOT IN ('done', 'stopped', 'failed');

-- name: DeleteSession :exec
DELETE FROM sessions WHERE id = ?;
