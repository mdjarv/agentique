-- +goose Up
-- Scheduled loops: agentique-owned recurring/one-shot/dynamic prompts fired
-- into sessions. Design: docs/scheduled-loops.md. All timestamp columns use
-- UTC RFC3339 seconds precision ("2006-01-02T15:04:05Z") — SQLite compares
-- TEXT lexicographically, so the format must be uniform for next_run_at
-- ordering to hold. Empty string means "unset" throughout.
CREATE TABLE schedules (
    id            TEXT PRIMARY KEY,
    project_id    TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    session_id    TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    name          TEXT NOT NULL,
    prompt        TEXT NOT NULL,
    cron          TEXT NOT NULL DEFAULT '',
    mode          TEXT NOT NULL DEFAULT 'recurring',
    enabled       INTEGER NOT NULL DEFAULT 1,
    pause_reason  TEXT NOT NULL DEFAULT '',
    attention     TEXT NOT NULL DEFAULT '',
    attention_run_id TEXT NOT NULL DEFAULT '',
    next_run_at   TEXT NOT NULL DEFAULT '',
    expires_at    TEXT NOT NULL DEFAULT '',
    last_run_at   TEXT NOT NULL DEFAULT '',
    last_viewed_at TEXT NOT NULL DEFAULT '',
    consecutive_failures INTEGER NOT NULL DEFAULT 0,
    created_by    TEXT NOT NULL DEFAULT 'user',
    created_at    TEXT NOT NULL,
    updated_at    TEXT NOT NULL
);

CREATE INDEX idx_schedules_due ON schedules(enabled, next_run_at);
CREATE INDEX idx_schedules_session ON schedules(session_id);

CREATE TABLE schedule_runs (
    id            TEXT PRIMARY KEY,
    schedule_id   TEXT NOT NULL REFERENCES schedules(id) ON DELETE CASCADE,
    session_id    TEXT NOT NULL,
    scheduled_for TEXT NOT NULL,
    created_at    TEXT NOT NULL,
    fired_at      TEXT NOT NULL DEFAULT '',
    finished_at   TEXT NOT NULL DEFAULT '',
    status        TEXT NOT NULL,
    overdue       INTEGER NOT NULL DEFAULT 0,
    attempts      INTEGER NOT NULL DEFAULT 0,
    next_attempt_at TEXT NOT NULL DEFAULT '',
    turn_index    INTEGER NOT NULL DEFAULT -1,
    summary       TEXT NOT NULL DEFAULT '',
    reason        TEXT NOT NULL DEFAULT '',
    error         TEXT NOT NULL DEFAULT '',
    error_kind    TEXT NOT NULL DEFAULT '',
    late_report   TEXT NOT NULL DEFAULT '',
    duration_ms   INTEGER NOT NULL DEFAULT 0
);

-- Slot idempotency: a cron slot is claimed by inserting its run row; a crash
-- between insert and next_run_at advance replays as ON CONFLICT DO NOTHING.
CREATE UNIQUE INDEX idx_schedule_runs_slot ON schedule_runs(schedule_id, scheduled_for);
-- Retention prunes and history pages by creation order, not slot time — a
-- catch-up row for an old slot must not be born oldest and pruned first.
CREATE INDEX idx_schedule_runs_recent ON schedule_runs(schedule_id, created_at DESC);

-- +goose Down
DROP TABLE schedule_runs;
DROP TABLE schedules;
