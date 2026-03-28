-- name: ListTags :many
SELECT * FROM tags ORDER BY sort_order ASC, name ASC;

-- name: GetTag :one
SELECT * FROM tags WHERE id = ?;

-- name: CreateTag :one
INSERT INTO tags (id, name, color) VALUES (?, ?, ?) RETURNING *;

-- name: UpdateTag :one
UPDATE tags SET name = ?, color = ?, updated_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now') WHERE id = ? RETURNING *;

-- name: DeleteTag :exec
DELETE FROM tags WHERE id = ?;

-- name: ListAllProjectTags :many
SELECT * FROM project_tags;

-- name: ListProjectTags :many
SELECT t.* FROM tags t
JOIN project_tags pt ON pt.tag_id = t.id
WHERE pt.project_id = ?
ORDER BY t.sort_order ASC, t.name ASC;

-- name: AddTagToProject :exec
INSERT OR IGNORE INTO project_tags (project_id, tag_id) VALUES (?, ?);

-- name: RemoveTagFromProject :exec
DELETE FROM project_tags WHERE project_id = ? AND tag_id = ?;

-- name: ClearProjectTags :exec
DELETE FROM project_tags WHERE project_id = ?;
