-- +goose Up
ALTER TABLE sessions ADD COLUMN worktree_requested INTEGER NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE sessions DROP COLUMN worktree_requested;
