-- Machine presentation (docs/multi-machine.md): how THIS host displays the
-- machines it knows — a nickname and an icon. Presentation is local by
-- design: the host that paired the satellites decides what to call them, and
-- nothing is written to or read from the machines themselves.
--
-- The host's own name and icon can't live in `machines`, because every client
-- consumer of that catalog treats a row as a reachable remote (opens a socket
-- to its base_url, fetches with its bearer token). A single-row table keeps
-- self out of the peer catalog while staying local, editable, and
-- restart-free — config and AGENTIQUE_MACHINE_LABEL still seed the label.

-- +goose Up
ALTER TABLE machines ADD COLUMN icon TEXT NOT NULL DEFAULT '';

CREATE TABLE host_presentation (
    id    INTEGER PRIMARY KEY CHECK (id = 1),
    label TEXT NOT NULL DEFAULT '',
    icon  TEXT NOT NULL DEFAULT ''
);

-- +goose Down
DROP TABLE IF EXISTS host_presentation;
ALTER TABLE machines DROP COLUMN icon;
