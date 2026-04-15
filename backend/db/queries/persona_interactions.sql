-- name: InsertPersonaInteraction :one
INSERT INTO persona_interactions (id, profile_id, team_id, asker_type, asker_id, question, action, confidence, response, redirect_to, response_time_ms)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: ListPersonaInteractions :many
SELECT * FROM persona_interactions
WHERE team_id = ?
ORDER BY created_at DESC
LIMIT ? OFFSET ?;

-- name: ListPersonaInteractionsForProfile :many
SELECT * FROM persona_interactions
WHERE profile_id = ?
ORDER BY created_at DESC
LIMIT ? OFFSET ?;
