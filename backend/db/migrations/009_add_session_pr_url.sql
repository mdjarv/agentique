-- +goose Up
ALTER TABLE sessions ADD COLUMN pr_url TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE sessions DROP COLUMN pr_url;
