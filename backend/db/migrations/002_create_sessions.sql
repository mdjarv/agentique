-- +goose Up
CREATE TABLE sessions (
    id TEXT PRIMARY KEY,
    project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    name TEXT NOT NULL DEFAULT '',
    work_dir TEXT NOT NULL,
    worktree_path TEXT,
    worktree_branch TEXT,
    state TEXT NOT NULL DEFAULT 'idle',
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);
CREATE INDEX idx_sessions_project_id ON sessions(project_id);

-- +goose Down
DROP TABLE sessions;
