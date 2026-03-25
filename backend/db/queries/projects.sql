-- name: ListProjects :many
SELECT * FROM projects ORDER BY updated_at DESC;

-- name: GetProject :one
SELECT * FROM projects WHERE id = ?;

-- name: GetProjectBySlug :one
SELECT * FROM projects WHERE slug = ?;

-- name: CreateProject :one
INSERT INTO projects (id, name, path, slug) VALUES (?, ?, ?, ?) RETURNING *;

-- name: UpdateProjectSlug :one
UPDATE projects SET slug = ?, updated_at = datetime('now') WHERE id = ? RETURNING *;

-- name: DeleteProject :exec
DELETE FROM projects WHERE id = ?;
