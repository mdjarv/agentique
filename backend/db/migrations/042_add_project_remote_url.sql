-- +goose Up
-- Cross-machine project identity (docs/multi-machine.md): the
-- canonical key of the checkout's primary git remote (host/org/repo,
-- normalized by gitops.CanonicalizeRemoteURL so SSH and HTTPS clones of the
-- same GitHub repo match). '' = no usable remote; such projects never group.
-- Backfilled at serve startup and refreshed on project create.
ALTER TABLE projects ADD COLUMN remote_url TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE projects DROP COLUMN remote_url;
