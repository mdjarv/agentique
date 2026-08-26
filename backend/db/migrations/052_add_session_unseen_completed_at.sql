-- Unseen completion moves server-side.
--
-- It used to live only in the browser (chat-store's hasUnseenCompletion), so
-- "this finished while you were looking elsewhere" was known to one tab and
-- died with a reload. The deck's Needs-you band and the voice switchboard both
-- rank on it, and neither can read another client's memory, so the fact needs
-- one owner: this column.
--
-- NULL means "nothing is waiting to be read". A value is the UTC RFC3339
-- seconds stamp of the completion, because SQLite compares TEXT
-- lexicographically and that is the one format that sorts as time.
--
-- Keep this comment ASCII. sqlc expands `SELECT *` by byte offset, so a
-- multi-byte character shifts those offsets and corrupts the generated code
-- for LATER queries.

-- +goose Up
ALTER TABLE sessions ADD COLUMN unseen_completed_at TEXT;

-- +goose Down
ALTER TABLE sessions DROP COLUMN unseen_completed_at;
