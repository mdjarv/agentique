-- +goose Up
ALTER TABLE projects ADD COLUMN folder TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE projects DROP COLUMN folder;
