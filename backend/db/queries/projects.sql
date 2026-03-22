-- name: ListProjects :many
SELECT * FROM projects ORDER BY updated_at DESC;

-- name: GetProject :one
SELECT * FROM projects WHERE id = ?;

-- name: CreateProject :one
INSERT INTO projects (id, name, path) VALUES (?, ?, ?) RETURNING *;

-- name: DeleteProject :exec
DELETE FROM projects WHERE id = ?;
