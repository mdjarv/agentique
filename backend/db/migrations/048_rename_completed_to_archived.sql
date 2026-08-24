-- Split the sidebar's "user filed this away" bit off the CLI process lifecycle.
--
-- completed_at carried two jobs: the user's archive gesture AND the runtime's
-- clean-exit transition (handleRuntimeStateChange stamped it on StateDone), so
-- a provider CLI exiting on its own filed the session into the collapsed
-- Archived section with no user involved. The column now means exactly one
-- thing: the user archived it. State alone says what the process is doing.
--
-- Existing values carry over. Rows that got their bit from a clean CLI exit are
-- no longer distinguishable from real archives — they stay archived, which is
-- how they have been presenting since the sidebar shipped.

-- +goose Up
ALTER TABLE sessions RENAME COLUMN completed_at TO archived_at;

-- +goose Down
ALTER TABLE sessions RENAME COLUMN archived_at TO completed_at;
