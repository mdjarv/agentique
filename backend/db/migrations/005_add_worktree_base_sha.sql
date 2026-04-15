-- +goose Up
ALTER TABLE sessions ADD COLUMN worktree_base_sha TEXT;

-- +goose Down
ALTER TABLE sessions DROP COLUMN worktree_base_sha;
