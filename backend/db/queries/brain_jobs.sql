-- name: CreateBrainJob :one
INSERT INTO brain_jobs (id, kind, scope, payload, attempts) VALUES (?, ?, ?, ?, ?) RETURNING *;

-- name: ListBrainJobs :many
SELECT * FROM brain_jobs ORDER BY created_at ASC, id ASC;

-- name: UpdateBrainJobAttempts :exec
UPDATE brain_jobs SET attempts = ?, last_error = ? WHERE id = ?;

-- name: DeleteBrainJob :exec
DELETE FROM brain_jobs WHERE id = ?;
