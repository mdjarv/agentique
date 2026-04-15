-- +goose Up
ALTER TABLE sessions ADD COLUMN permission_mode TEXT NOT NULL DEFAULT 'default';
ALTER TABLE sessions ADD COLUMN auto_approve INTEGER NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE sessions DROP COLUMN permission_mode;
ALTER TABLE sessions DROP COLUMN auto_approve;
