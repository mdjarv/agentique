-- Which operating system a paired machine runs, so clients can mark each host
-- with a platform glyph beside its name.
--
-- A fact about the machine, not presentation: like identity_key it is captured
-- from the pairing descriptor (platform.os = GOOS: "linux", "windows",
-- "darwin") and is not user-editable. Empty means unknown -- a row written by
-- a client that predates the field -- and clients render a generic host glyph
-- for it rather than guessing.
--
-- Keep this comment ASCII. sqlc expands `SELECT *` by byte offset, so a
-- multi-byte character shifts those offsets and corrupts the generated code
-- for LATER queries.

-- +goose Up
ALTER TABLE machines ADD COLUMN platform_os TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE machines DROP COLUMN platform_os;
