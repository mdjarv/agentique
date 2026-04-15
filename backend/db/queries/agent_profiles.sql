-- name: CreateAgentProfile :one
INSERT INTO agent_profiles (id, name, role, description, project_id, avatar, config)
VALUES (?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: GetAgentProfile :one
SELECT * FROM agent_profiles WHERE id = ?;

-- name: ListAgentProfiles :many
SELECT * FROM agent_profiles ORDER BY name ASC;

-- name: UpdateAgentProfile :one
UPDATE agent_profiles
SET name = ?, role = ?, description = ?, project_id = ?, avatar = ?, config = ?,
    updated_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now')
WHERE id = ?
RETURNING *;

-- name: DeleteAgentProfile :exec
DELETE FROM agent_profiles WHERE id = ?;
