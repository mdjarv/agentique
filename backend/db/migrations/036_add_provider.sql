-- +goose Up
ALTER TABLE sessions ADD COLUMN provider TEXT NOT NULL DEFAULT 'claude';

-- +goose Down
ALTER TABLE sessions DROP COLUMN provider;
