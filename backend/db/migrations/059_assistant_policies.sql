-- Standing instructions with budgets, and where a session came from
-- (docs/assistant.md, the M4 contract).
--
-- Two things, each with one writer:
--
--   assistant_policies -- what the assistant may do on its own, and how much
--   sessions.origin    -- who started a session: nobody named, or the assistant
--
-- A policy is the operator's own sentence, so the text is free and the budgets
-- are not: they are what stops a standing instruction spending an afternoon of
-- allowance on its own. The heartbeat reads the enabled ones and nothing else
-- does; a disabled row is kept, because turning one off is not throwing it away.
--
-- Timestamps are UTC RFC3339 SECONDS, as every other assistant timestamp is:
-- SQLite compares TEXT lexicographically, and that is the one shape that sorts
-- as time.
--
-- sessions.origin is '' or 'assistant' and is NOT NULL with an empty default,
-- so no reader has to handle NULL: '' is "a person asked for this", which is
-- every session that existed before this migration. It is deliberately not a
-- foreign key to a policy -- a session outlives the standing instruction that
-- made it, and the policy it was created under is in the journal entry that
-- recorded the creation, where it cannot be rewritten by an edit.
--
-- Keep this comment ASCII. sqlc expands `SELECT *` by byte offset, so one
-- multi-byte character shifts those offsets and corrupts the generated code
-- for LATER queries.

-- +goose Up

CREATE TABLE assistant_policies (
    id               TEXT PRIMARY KEY,
    -- What the operator calls it. This is also the name the head supplies when
    -- it acts under one, so it is what a budget refusal names back.
    name             TEXT NOT NULL DEFAULT '',
    -- The standing instruction itself, in the operator's words. Capped by the
    -- server at 8 KiB; a policy is a paragraph, not a document.
    text             TEXT NOT NULL DEFAULT '',
    -- 1 when the heartbeat should read it. Off means inert, never deleted.
    enabled          INTEGER NOT NULL DEFAULT 0,
    -- How many sessions this policy may have running at once, and how many it
    -- may create in a day. Both are counted from the journal's session_created
    -- entries, which name the policy, so an edit cannot retroactively widen
    -- what was already spent.
    budget_in_flight INTEGER NOT NULL DEFAULT 1,
    budget_per_day   INTEGER NOT NULL DEFAULT 3,
    -- When the heartbeat last acted under it. UTC RFC3339 seconds, '' for
    -- never.
    last_fired_at    TEXT NOT NULL DEFAULT '',
    created_at       TEXT NOT NULL DEFAULT '',
    updated_at       TEXT NOT NULL DEFAULT ''
);

-- The heartbeat's own read: the enabled ones, on every tick that has something
-- to triage.
CREATE INDEX idx_assistant_policies_enabled ON assistant_policies(enabled);

-- '' for a session a person asked for, 'assistant' for one the assistant
-- created. A row's own third line says so, and the budgets count it.
ALTER TABLE sessions ADD COLUMN origin TEXT NOT NULL DEFAULT '';

-- The in-flight budget's read: which assistant-origin sessions are still live.
CREATE INDEX idx_sessions_origin ON sessions(origin);

-- +goose Down

DROP INDEX IF EXISTS idx_sessions_origin;
ALTER TABLE sessions DROP COLUMN origin;
DROP INDEX IF EXISTS idx_assistant_policies_enabled;
DROP TABLE IF EXISTS assistant_policies;
