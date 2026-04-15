-- +goose Up
ALTER TABLE teams RENAME TO channels;
DROP INDEX IF EXISTS idx_teams_project;
CREATE INDEX idx_channels_project ON channels(project_id);

ALTER TABLE sessions RENAME COLUMN team_id TO channel_id;
ALTER TABLE sessions RENAME COLUMN team_role TO channel_role;
DROP INDEX IF EXISTS idx_sessions_team;
CREATE INDEX idx_sessions_channel ON sessions(channel_id);

-- +goose Down
DROP INDEX IF EXISTS idx_sessions_channel;
ALTER TABLE sessions RENAME COLUMN channel_role TO team_role;
ALTER TABLE sessions RENAME COLUMN channel_id TO team_id;
CREATE INDEX idx_sessions_team ON sessions(team_id);

ALTER TABLE channels RENAME TO teams;
DROP INDEX IF EXISTS idx_channels_project;
CREATE INDEX idx_teams_project ON teams(project_id);
