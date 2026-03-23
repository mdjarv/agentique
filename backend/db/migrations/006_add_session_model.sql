-- +goose Up
ALTER TABLE sessions ADD COLUMN model TEXT NOT NULL DEFAULT 'opus';

-- +goose Down
ALTER TABLE sessions DROP COLUMN model;
