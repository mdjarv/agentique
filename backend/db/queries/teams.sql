-- name: CreateTeam :one
INSERT INTO teams (id, name, description) VALUES (?, ?, ?) RETURNING *;

-- name: GetTeam :one
SELECT * FROM teams WHERE id = ?;

-- name: ListTeams :many
SELECT * FROM teams ORDER BY name ASC;

-- name: UpdateTeam :one
UPDATE teams
SET name = ?, description = ?,
    updated_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now')
WHERE id = ?
RETURNING *;

-- name: DeleteTeam :exec
DELETE FROM teams WHERE id = ?;

-- name: AddTeamMember :exec
INSERT INTO team_members (team_id, agent_profile_id, sort_order)
VALUES (?, ?, ?)
ON CONFLICT (team_id, agent_profile_id) DO UPDATE SET sort_order = excluded.sort_order;

-- name: RemoveTeamMember :exec
DELETE FROM team_members WHERE team_id = ? AND agent_profile_id = ?;

-- name: ListTeamMembers :many
SELECT ap.* FROM agent_profiles ap
JOIN team_members tm ON tm.agent_profile_id = ap.id
WHERE tm.team_id = ?
ORDER BY tm.sort_order ASC, ap.name ASC;

-- name: ListTeamsForAgent :many
SELECT t.* FROM teams t
JOIN team_members tm ON tm.team_id = t.id
WHERE tm.agent_profile_id = ?
ORDER BY t.name ASC;
