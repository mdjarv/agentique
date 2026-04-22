-- +goose Up
ALTER TABLE sessions ADD COLUMN parent_session_id TEXT REFERENCES sessions(id) ON DELETE CASCADE;
CREATE INDEX idx_sessions_parent_session_id ON sessions(parent_session_id);

-- +goose Down
DROP INDEX IF EXISTS idx_sessions_parent_session_id;
ALTER TABLE sessions DROP COLUMN parent_session_id;
