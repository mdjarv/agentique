-- +goose Up
ALTER TABLE sessions ADD COLUMN claude_session_id TEXT;

-- +goose Down
ALTER TABLE sessions DROP COLUMN claude_session_id;
