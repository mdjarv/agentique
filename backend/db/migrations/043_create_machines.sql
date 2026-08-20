-- +goose Up
-- Server-side machine catalog (multi-machine): paired remote machines are
-- account state, not device state. A phone PWA and a desktop browser logging
-- into the same primary must see the same machines, so the catalog (incl.
-- each machine's bearer token for reaching it) lives here; the client's
-- localStorage copy is only an offline cache.
CREATE TABLE machines (
    machine_id TEXT PRIMARY KEY,
    label      TEXT NOT NULL DEFAULT '',
    base_url   TEXT NOT NULL,
    token      TEXT NOT NULL DEFAULT '',
    added_at   TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);

-- +goose Down
DROP TABLE IF EXISTS machines;
