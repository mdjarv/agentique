# Agentique Database Schema

SQLite database managed with goose (migrations) and sqlc (query generation).
Deliberately simple CRUD -- no event sourcing for MVP.

## Tables

### projects

```sql
CREATE TABLE projects (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    path TEXT NOT NULL UNIQUE,
    default_model TEXT NOT NULL DEFAULT 'sonnet',
    default_permission_mode TEXT NOT NULL DEFAULT 'default',
    default_system_prompt TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);
```

### sessions (implemented in M2)

```sql
CREATE TABLE sessions (
    id TEXT PRIMARY KEY,
    project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    name TEXT NOT NULL DEFAULT '',
    work_dir TEXT NOT NULL,               -- actual directory for Claude agent
    worktree_path TEXT,                   -- NULL if no worktree, path if created
    worktree_branch TEXT,                 -- branch name if worktree was created
    state TEXT NOT NULL DEFAULT 'idle',   -- idle, running, done, failed, stopped
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);

CREATE INDEX idx_sessions_project_id ON sessions(project_id);
```

### messages (M3 - post-MVP persistence)

```sql
CREATE TABLE messages (
    id TEXT PRIMARY KEY,
    session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    turn_index INTEGER NOT NULL,      -- which turn (query/response cycle)
    role TEXT NOT NULL,                -- 'user' | 'assistant' | 'tool_use' | 'tool_result' | 'thinking'
    content TEXT NOT NULL,
    tool_name TEXT,                    -- for tool_use/tool_result
    tool_input TEXT,                   -- JSON, for tool_use
    cost REAL,                         -- for result messages
    usage_json TEXT,                   -- JSON with token counts
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);

CREATE INDEX idx_messages_session_id ON messages(session_id);
CREATE INDEX idx_messages_session_turn ON messages(session_id, turn_index);
```

## sqlc Queries (MVP)

### projects.sql

```sql
-- name: ListProjects :many
SELECT * FROM projects ORDER BY updated_at DESC;

-- name: GetProject :one
SELECT * FROM projects WHERE id = ?;

-- name: CreateProject :one
INSERT INTO projects (id, name, path)
VALUES (?, ?, ?)
RETURNING *;

-- name: UpdateProject :one
UPDATE projects
SET name = ?,
    default_model = ?,
    default_permission_mode = ?,
    default_system_prompt = ?,
    updated_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now')
WHERE id = ?
RETURNING *;

-- name: DeleteProject :exec
DELETE FROM projects WHERE id = ?;
```

### sessions.sql (implemented)

```sql
-- name: CreateSession :one
INSERT INTO sessions (id, project_id, name, work_dir, worktree_path, worktree_branch, state)
VALUES (?, ?, ?, ?, ?, ?, ?) RETURNING *;

-- name: GetSession :one
SELECT * FROM sessions WHERE id = ?;

-- name: ListSessionsByProject :many
SELECT * FROM sessions WHERE project_id = ? ORDER BY created_at ASC;

-- name: UpdateSessionState :exec
UPDATE sessions SET state = ?, updated_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now') WHERE id = ?;

-- name: DeleteSession :exec
DELETE FROM sessions WHERE id = ?;
```

## Comparison with t3code

t3code uses event sourcing with an `orchestration_events` table and multiple
projection tables (projects, threads, messages, activities, turns, approvals).
This is powerful but complex.

Agentique uses simple CRUD tables. If we need event replay or undo later,
we can add an events table without rearchitecting.

| Aspect | t3code | Agentique |
|---|---|---|
| Pattern | Event sourcing + projections | Simple CRUD |
| Tables | 8+ (event store + projections) | 2-3 |
| Queries | Raw SQL via Effect SQL | sqlc (generated Go) |
| Migrations | Custom Effect-based | goose |
| Message history | Persisted in projections | In-memory (MVP), SQLite (M3) |
