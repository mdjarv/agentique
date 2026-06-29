-- +goose Up
CREATE TABLE brain_jobs (
    id         TEXT PRIMARY KEY,
    kind       TEXT NOT NULL,                       -- "learn" | "outcome"
    scope      TEXT NOT NULL,
    payload    TEXT NOT NULL,                       -- JSON: {project_id, events:[]TranscriptEvent}
    attempts   INTEGER NOT NULL DEFAULT 0,
    last_error TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);
CREATE INDEX idx_brain_jobs_created ON brain_jobs(created_at, id);

-- +goose Down
DROP TABLE IF EXISTS brain_jobs;
