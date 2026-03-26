-- +goose Up
ALTER TABLE sessions ADD COLUMN last_query_at TEXT;

-- +goose Down
ALTER TABLE sessions DROP COLUMN last_query_at;
