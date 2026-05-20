-- +goose Up
ALTER TABLE users ADD COLUMN sidebar_focus_mode INTEGER NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE users DROP COLUMN sidebar_focus_mode;
