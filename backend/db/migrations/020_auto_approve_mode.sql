-- +goose Up
ALTER TABLE sessions ADD COLUMN auto_approve_mode TEXT NOT NULL DEFAULT 'manual';
UPDATE sessions SET auto_approve_mode = 'auto' WHERE auto_approve = 1;

-- +goose Down
ALTER TABLE sessions DROP COLUMN auto_approve_mode;
