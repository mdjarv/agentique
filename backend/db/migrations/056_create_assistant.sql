-- The assistant's own state (docs/assistant.md).
--
-- Four things, each with one lifetime and one writer:
--
--   channels.kind      -- which channel is the assistant's conversation
--   assistant_state    -- the single row that names it, plus the head's model
--   assistant_journal  -- append-only episodic memory: what happened
--   assistant_follows  -- the watch list, made durable
--
-- Timestamps are UTC RFC3339 SECONDS, as the scheduler's are: SQLite compares
-- TEXT lexicographically, and that is the one shape that sorts as time. The
-- messages table predates the rule and uses fractional seconds, which is why
-- nothing here joins the two on time.
--
-- The journal carries no foreign key to sessions. It answers "what happened",
-- and a session deleted next week did not stop having finished today -- a
-- cascade would quietly rewrite history. assistant_follows is the opposite: a
-- follow is a live subscription to a row, and a row that is gone can never be
-- followed again, so it cascades.
--
-- Keep this comment ASCII. sqlc expands `SELECT *` by byte offset, so one
-- multi-byte character shifts those offsets and corrupts the generated code
-- for LATER queries.

-- +goose Up

-- '' for an ordinary channel, 'assistant' for the conversation. A kind rather
-- than a flag because the set is not closed: the teams work already wants to
-- tell a discussion from a repo channel, and every list filters on this one
-- column either way.
ALTER TABLE channels ADD COLUMN kind TEXT NOT NULL DEFAULT '';

CREATE INDEX idx_channels_kind ON channels(kind);

-- One row, id = 1, following voice_settings and host_presentation: there is
-- one assistant per server. Empty values mean "not set yet" rather than a
-- meaningful zero, so every column is NOT NULL with an empty default and no
-- reader has to handle NULL.
CREATE TABLE assistant_state (
    id                INTEGER PRIMARY KEY CHECK (id = 1),
    -- The conversation channel. Empty until the first use creates it.
    channel_id        TEXT NOT NULL DEFAULT '',
    -- The head's model, as a FAMILY NAME ("opus"), never a version or a slug:
    -- a new upstream model must not require an agentique release. Empty means
    -- "whatever a new session would get".
    model             TEXT NOT NULL DEFAULT '',
    last_heartbeat_at TEXT NOT NULL DEFAULT '',
    last_digest_at    TEXT NOT NULL DEFAULT '',
    -- When each surface last looked, as a JSON object of surface name to UTC
    -- RFC3339 seconds. The journal's seen_by answers "has this ROW been shown"
    -- and cannot answer "what has been said in the conversation since", which
    -- is the other half of SinceLast and has nowhere else to live: a surface
    -- that has never looked has no journal row to carry its mark.
    surface_marks     TEXT NOT NULL DEFAULT '{}',
    created_at        TEXT NOT NULL DEFAULT '',
    updated_at        TEXT NOT NULL DEFAULT ''
);

-- Append-only. One row per thing that happened, never updated except to stamp
-- seen_by. Rows compact by age into a day_summary row (M4), which is why `at`
-- is separate from created_at: a summary row is written today and is about a
-- day two weeks ago.
CREATE TABLE assistant_journal (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    -- When the thing happened. UTC RFC3339 seconds.
    at         TEXT NOT NULL,
    -- One of the closed kind set in internal/assistant.
    kind       TEXT NOT NULL,
    -- The subjects, empty where the entry has none. No FK: see the header.
    session_id TEXT NOT NULL DEFAULT '',
    project_id TEXT NOT NULL DEFAULT '',
    -- One line, for a digest or a preamble.
    summary    TEXT NOT NULL DEFAULT '',
    -- Whatever the writer wants back, as a JSON object.
    payload    TEXT NOT NULL DEFAULT '{}',
    -- 1 when the summary is agent-written text about content nobody here
    -- authored. It stays 1 through compaction: a summary of untrusted text is
    -- untrusted text.
    untrusted  INTEGER NOT NULL DEFAULT 0,
    -- 1 when this is worth a second look -- set by the operator on a message
    -- or by the assistant on an entry. Notable rows are exempt from
    -- compaction.
    notable    INTEGER NOT NULL DEFAULT 0,
    -- JSON object of surface name to the UTC RFC3339 seconds it was shown.
    seen_by    TEXT NOT NULL DEFAULT '{}',
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);

-- Reading order is always newest-first by `at`, and the id breaks ties within
-- a second -- which is common, since a turn ending writes one entry and the
-- report before it wrote another.
CREATE INDEX idx_assistant_journal_at ON assistant_journal(at DESC, id DESC);

-- The dedupe read: has this session already got an entry of this kind. Used by
-- the session.state observer, whose pushes repeat.
CREATE INDEX idx_assistant_journal_session_kind ON assistant_journal(session_id, kind);

-- The watch list. One row per followed session: the old in-process follow set,
-- made durable so a report arriving between calls still has somewhere to land.
CREATE TABLE assistant_follows (
    session_id TEXT PRIMARY KEY REFERENCES sessions(id) ON DELETE CASCADE,
    -- UTC RFC3339 seconds.
    since      TEXT NOT NULL,
    -- 1 once a surface has been told what this session is doing, so a greeting
    -- does not re-brief on every call.
    briefed    INTEGER NOT NULL DEFAULT 0,
    -- Who asked: 'dispatch' (the assistant started the work), 'operator', or a
    -- surface name. Free text, because the set of surfaces is not closed.
    source     TEXT NOT NULL DEFAULT ''
);

-- +goose Down

DROP TABLE IF EXISTS assistant_follows;
DROP TABLE IF EXISTS assistant_journal;
DROP TABLE IF EXISTS assistant_state;
DROP INDEX IF EXISTS idx_channels_kind;
ALTER TABLE channels DROP COLUMN kind;
