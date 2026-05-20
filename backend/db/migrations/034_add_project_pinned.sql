-- +goose Up
ALTER TABLE projects ADD COLUMN pinned INTEGER NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE projects DROP COLUMN pinned;
