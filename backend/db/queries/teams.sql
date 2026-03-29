-- name: CreateTeam :one
INSERT INTO teams (id, name, project_id) VALUES (?, ?, ?) RETURNING *;

-- name: GetTeam :one
SELECT * FROM teams WHERE id = ?;

-- name: ListTeamsByProject :many
SELECT * FROM teams WHERE project_id = ? ORDER BY created_at ASC;

-- name: DeleteTeam :exec
DELETE FROM teams WHERE id = ?;

-- name: UpdateTeamName :exec
UPDATE teams SET name = ? WHERE id = ?;

-- name: ListTeamMembers :many
SELECT * FROM sessions WHERE team_id = ? ORDER BY created_at ASC;

-- name: SetSessionTeam :exec
UPDATE sessions SET team_id = ?, team_role = ?, updated_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now') WHERE id = ?;

-- name: ClearSessionTeam :exec
UPDATE sessions SET team_id = NULL, team_role = '', updated_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now') WHERE id = ?;

-- name: ListAgentMessagesByTeam :many
SELECT se.* FROM session_events se
JOIN sessions s ON se.session_id = s.id
WHERE s.team_id = ? AND se.type = 'agent_message'
ORDER BY se.created_at ASC;
