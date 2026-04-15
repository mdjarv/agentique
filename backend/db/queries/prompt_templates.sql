-- name: ListPromptTemplates :many
SELECT * FROM prompt_templates ORDER BY sort_order ASC, name ASC;

-- name: GetPromptTemplate :one
SELECT * FROM prompt_templates WHERE id = ?;

-- name: CreatePromptTemplate :one
INSERT INTO prompt_templates (id, name, description, content, settings, tags)
VALUES (?, ?, ?, ?, ?, ?) RETURNING *;

-- name: UpdatePromptTemplate :one
UPDATE prompt_templates
SET name = ?, description = ?, content = ?, settings = ?, tags = ?, updated_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now')
WHERE id = ? RETURNING *;

-- name: DeletePromptTemplate :exec
DELETE FROM prompt_templates WHERE id = ?;

-- name: ReorderPromptTemplates :exec
UPDATE prompt_templates SET sort_order = ?, updated_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now') WHERE id = ?;
