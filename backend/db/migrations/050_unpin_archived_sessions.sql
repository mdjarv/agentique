-- Archiving now releases the pin (see SetSessionArchived), but rows archived
-- before that change kept theirs, so already-filed-away work is still sitting
-- in the sidebar's priority section. Reconcile the existing state to the new
-- invariant: no session is both pinned and archived.
--
-- Down is deliberately empty: which of these rows the user had pinned is not
-- recoverable, and re-pinning archived sessions would restore the bug rather
-- than the data.

-- +goose Up
UPDATE sessions SET pinned = 0, pin_order = 0 WHERE archived_at IS NOT NULL AND pinned != 0;

-- +goose Down
