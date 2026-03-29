-- +goose Up
CREATE TABLE teams (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL DEFAULT '',
    project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);
CREATE INDEX idx_teams_project ON teams(project_id);

ALTER TABLE sessions ADD COLUMN team_id TEXT REFERENCES teams(id) ON DELETE SET NULL;
ALTER TABLE sessions ADD COLUMN team_role TEXT NOT NULL DEFAULT '';
CREATE INDEX idx_sessions_team ON sessions(team_id);

-- +goose Down
DROP INDEX IF EXISTS idx_sessions_team;
ALTER TABLE sessions DROP COLUMN team_role;
ALTER TABLE sessions DROP COLUMN team_id;
DROP INDEX IF EXISTS idx_teams_project;
DROP TABLE IF EXISTS teams;
