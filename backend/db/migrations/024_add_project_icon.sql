-- +goose Up
ALTER TABLE projects ADD COLUMN icon TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE projects DROP COLUMN icon;
