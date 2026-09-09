-- When the upstream model behind this session was learned.
--
-- resolved_model records WHICH concrete model the provider reported; without a
-- stamp beside it there is no way to tell a reading taken this turn from one
-- left by a run days ago, on a session whose requested slug has since been
-- pointed at something else.
--
-- NULL means the provider has not reported a model for this session yet, which
-- is exactly when resolved_model is empty: both are written by the same init
-- event, and UpdateSessionModel clears both when the requested slug changes.
--
-- UTC RFC3339 seconds, the format every other timestamp column here uses,
-- because SQLite compares TEXT lexicographically and that is the one shape
-- that sorts as time.
--
-- Keep this comment ASCII. sqlc expands `SELECT *` by byte offset, so a
-- multi-byte character shifts those offsets and corrupts the generated code
-- for LATER queries.

-- +goose Up
ALTER TABLE sessions ADD COLUMN resolved_at TEXT;

-- +goose Down
ALTER TABLE sessions DROP COLUMN resolved_at;
