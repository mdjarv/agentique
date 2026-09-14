-- The steward's findings (docs/peers.md, the steward).
--
-- One row per finding that held, opened when its condition started and
-- resolved when it stopped. An open finding is resolved_at = ''. At most one
-- open row per (kind, subject), which is a partial unique index rather than a
-- rule in Go: a second pass racing the first must not open a duplicate.
--
-- facts is a JSON object of what the sensor saw, for the words somebody else
-- writes. Resolved rows are kept for a while as history and aged out.
--
-- Timestamps are UTC RFC3339 SECONDS, so they compare as text.
--
-- Keep this comment ASCII. sqlc expands `SELECT *` by byte offset, so one
-- multi-byte character shifts those offsets and corrupts the generated code
-- for LATER queries.

-- +goose Up

CREATE TABLE steward_findings (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    kind        TEXT NOT NULL,
    subject     TEXT NOT NULL DEFAULT '',
    severity    TEXT NOT NULL,
    remedy      TEXT NOT NULL,
    facts       TEXT NOT NULL DEFAULT '{}',
    opened_at   TEXT NOT NULL,
    resolved_at TEXT NOT NULL DEFAULT ''
);

CREATE UNIQUE INDEX idx_steward_findings_open ON steward_findings(kind, subject) WHERE resolved_at = '';

CREATE INDEX idx_steward_findings_resolved ON steward_findings(resolved_at);

-- +goose Down

DROP INDEX IF EXISTS idx_steward_findings_resolved;
DROP INDEX IF EXISTS idx_steward_findings_open;
DROP TABLE IF EXISTS steward_findings;
