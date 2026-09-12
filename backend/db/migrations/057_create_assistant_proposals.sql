-- Proposals: where the yes lives (docs/assistant.md, the M3 contract).
--
-- The uncontained tier is never performed by the assistant and never refused
-- either: asking for one of its verbs writes a row here, a person decides it on
-- a surface that can show the card or read the target back, and accepting
-- re-checks the live facts before the same service the UI uses performs it.
--
-- One row per ask, and the row is the record: what was asked for, what server
-- facts it was judged on, the rationale it came with, who decided it and where,
-- and what happened. Nothing here is updated except by a decision.
--
-- Timestamps are UTC RFC3339 SECONDS, as every other assistant timestamp is:
-- SQLite compares TEXT lexicographically, and that is the one shape that sorts
-- as time.
--
-- No foreign key to sessions. A proposal answers "what was asked and what was
-- decided", and a session deleted next week did not stop having been the
-- subject -- a cascade would quietly erase the record of the delete that
-- removed it. That is the same argument assistant_journal makes, and the
-- opposite of assistant_follows, which is a live subscription to a row.
--
-- Keep this comment ASCII. sqlc expands `SELECT *` by byte offset, so one
-- multi-byte character shifts those offsets and corrupts the generated code
-- for LATER queries.

-- +goose Up

CREATE TABLE assistant_proposals (
    -- A uuid, minted by the handler that created the proposal.
    id          TEXT PRIMARY KEY,
    -- UTC RFC3339 seconds.
    created_at  TEXT NOT NULL,
    -- One of the eight uncontained verbs in internal/assistant.
    verb        TEXT NOT NULL,
    -- The subject. A session for seven of the verbs, a channel for
    -- dissolve_channel; empty for whichever one the verb does not name.
    session_id  TEXT NOT NULL DEFAULT '',
    project_id  TEXT NOT NULL DEFAULT '',
    channel_id  TEXT NOT NULL DEFAULT '',
    -- The verb's own arguments, as a JSON object.
    args        TEXT NOT NULL DEFAULT '{}',
    -- Why, in the assistant's words. Required of the head at proposal time: a
    -- card with no reason on it is a button nobody can judge.
    rationale   TEXT NOT NULL DEFAULT '',
    -- The server facts this was judged on, as a JSON object, so a card can
    -- quote them and a stale accept can be compared against them.
    evidence    TEXT NOT NULL DEFAULT '{}',
    -- open, accepted, declined, stale, failed, expired. The closed set lives in
    -- internal/assistant; this column is text because a status added there must
    -- not need a migration.
    status      TEXT NOT NULL DEFAULT 'open',
    decided_at  TEXT NOT NULL DEFAULT '',
    -- Which surface the yes (or the no) was given on.
    decided_via TEXT NOT NULL DEFAULT '',
    -- One line: what the executor did, or why nothing happened.
    outcome     TEXT NOT NULL DEFAULT '',
    -- When an undecided proposal stops being offered. UTC RFC3339 seconds.
    expires_at  TEXT NOT NULL DEFAULT ''
);

-- Every read is either "what is open" or "the newest few", and both are this
-- index: status first because open is the half a surface renders, created_at
-- descending because that is the order either half is read in.
CREATE INDEX idx_assistant_proposals_status_created
    ON assistant_proposals(status, created_at DESC);

-- +goose Down

DROP INDEX IF EXISTS idx_assistant_proposals_status_created;
DROP TABLE IF EXISTS assistant_proposals;
