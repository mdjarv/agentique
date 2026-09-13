-- When the journal was last folded (docs/assistant.md, the M5 contract).
--
-- One column, and it is a DAY mark rather than a window: the pass folds whole
-- calendar days that are already older than fourteen days, so what the
-- heartbeat needs to know is only whether today's pass has run. A tick after
-- local midnight whose mark is before that midnight runs one, stamping first --
-- so a pass that fails is retried tomorrow rather than on every tick.
--
-- Empty is "never compacted", which is every install that arrives here: the
-- journal has been append-only since migration 056 and nothing has ever
-- bounded it.
--
-- Keep this comment ASCII. sqlc expands `SELECT *` by byte offset, so one
-- multi-byte character shifts those offsets and corrupts the generated code
-- for LATER queries.

-- +goose Up

ALTER TABLE assistant_state ADD COLUMN last_compacted_at TEXT NOT NULL DEFAULT '';

-- The two reads that name a kind: does this day already have a summary, and
-- which summaries are past their retention. Both are `kind = 'day_summary'`
-- with a range over `at`, which idx_assistant_journal_at cannot serve -- it
-- leads with `at`, so a question about one kind is a scan of every row in the
-- range. The pass's other reads are `kind != 'day_summary'` over a day, which
-- no index on kind can narrow and which the existing one already answers as a
-- range.
CREATE INDEX idx_assistant_journal_kind_at ON assistant_journal(kind, at);

-- +goose Down

DROP INDEX IF EXISTS idx_assistant_journal_kind_at;
ALTER TABLE assistant_state DROP COLUMN last_compacted_at;
