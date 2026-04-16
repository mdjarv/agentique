-- name: ListProjects :many
SELECT * FROM projects ORDER BY sort_order ASC, updated_at DESC;

-- name: GetProject :one
SELECT * FROM projects WHERE id = ?;

-- name: GetProjectBySlug :one
SELECT * FROM projects WHERE slug = ?;

-- name: CreateProject :one
INSERT INTO projects (id, name, path, slug) VALUES (?, ?, ?, ?) RETURNING *;

-- name: UpdateProjectSlug :one
UPDATE projects SET slug = ?, updated_at = datetime('now') WHERE id = ? RETURNING *;

-- name: UpdateProjectName :one
UPDATE projects SET name = ?, slug = ?, updated_at = datetime('now') WHERE id = ? RETURNING *;

-- name: UpdateProjectSortOrder :exec
UPDATE projects SET sort_order = ? WHERE id = ?;

-- name: UpdateProjectBehaviorPresets :one
UPDATE projects SET default_behavior_presets = ?, updated_at = datetime('now') WHERE id = ? RETURNING *;

-- name: UpdateProjectFavorite :one
UPDATE projects SET favorite = ?, updated_at = datetime('now') WHERE id = ? RETURNING *;

-- name: UpdateProjectColor :one
UPDATE projects SET color = ?, updated_at = datetime('now') WHERE id = ? RETURNING *;

-- name: UpdateProjectIcon :one
UPDATE projects SET icon = ?, updated_at = datetime('now') WHERE id = ? RETURNING *;

-- name: UpdateProjectFolder :one
UPDATE projects SET folder = ?, updated_at = datetime('now') WHERE id = ? RETURNING *;

-- name: UpdateProjectMaxSessions :one
UPDATE projects SET max_sessions = ?, updated_at = datetime('now') WHERE id = ? RETURNING *;

-- name: DeleteProject :exec
DELETE FROM projects WHERE id = ?;
