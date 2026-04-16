-- +goose Up
ALTER TABLE projects ADD COLUMN max_sessions INTEGER NOT NULL DEFAULT 25;

-- +goose Down
ALTER TABLE projects DROP COLUMN max_sessions;
