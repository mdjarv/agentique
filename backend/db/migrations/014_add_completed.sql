-- +goose Up
ALTER TABLE sessions ADD COLUMN completed_at TEXT;
UPDATE sessions SET completed_at = updated_at WHERE worktree_merged = 1;

-- +goose Down
ALTER TABLE sessions DROP COLUMN completed_at;
