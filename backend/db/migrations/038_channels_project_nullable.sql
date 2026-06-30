-- Make channels.project_id nullable so web-only discussion channels can exist
-- without a project. SQLite cannot drop a NOT NULL constraint in place, so this
-- rebuilds the table (the canonical 12-step ALTER procedure).
--
-- The FK action stays ON DELETE CASCADE (NOT the design doc's SET NULL): a
-- web-only channel's project_id is NULL, so the cascade never fires for it,
-- while repo-backed channels keep cascading away with their project — SET NULL
-- would instead orphan them (stale worktree refs) on project delete.
--
-- foreign_keys MUST be OFF during the rebuild: DROP TABLE channels with FKs ON
-- performs an implicit DELETE that would cascade into channel_members and
-- messages (channel_id -> channels CASCADE), wiping every channel's roster and
-- timeline. PRAGMA foreign_keys is a no-op inside a transaction, hence
-- NO TRANSACTION (the pool is pinned to a single connection, so the PRAGMA /
-- BEGIN / COMMIT all run on the same conn).

-- +goose NO TRANSACTION
-- +goose Up
PRAGMA foreign_keys=OFF;

BEGIN;

CREATE TABLE channels_new (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL DEFAULT '',
    project_id TEXT REFERENCES projects(id) ON DELETE CASCADE,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);

INSERT INTO channels_new (id, name, project_id, created_at)
    SELECT id, name, project_id, created_at FROM channels;

DROP TABLE channels;

ALTER TABLE channels_new RENAME TO channels;

CREATE INDEX idx_channels_project ON channels(project_id);

COMMIT;

PRAGMA foreign_keys=ON;

-- +goose Down
PRAGMA foreign_keys=OFF;

BEGIN;

CREATE TABLE channels_old (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL DEFAULT '',
    project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);

-- Drop rows with a NULL project_id (web-only channels) — they cannot exist
-- under the NOT NULL constraint being restored.
INSERT INTO channels_old (id, name, project_id, created_at)
    SELECT id, name, project_id, created_at FROM channels WHERE project_id IS NOT NULL;

DROP TABLE channels;

ALTER TABLE channels_old RENAME TO channels;

CREATE INDEX idx_channels_project ON channels(project_id);

COMMIT;

PRAGMA foreign_keys=ON;
