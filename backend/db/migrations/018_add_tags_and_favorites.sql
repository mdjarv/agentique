-- +goose Up
ALTER TABLE projects ADD COLUMN favorite INTEGER NOT NULL DEFAULT 0;

CREATE TABLE tags (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    color TEXT NOT NULL,
    sort_order INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);

CREATE TABLE project_tags (
    project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    tag_id TEXT NOT NULL REFERENCES tags(id) ON DELETE CASCADE,
    PRIMARY KEY (project_id, tag_id)
);

-- +goose Down
DROP TABLE project_tags;
DROP TABLE tags;
ALTER TABLE projects DROP COLUMN favorite;
