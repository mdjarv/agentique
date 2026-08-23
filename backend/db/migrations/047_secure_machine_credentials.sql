-- +goose Up
-- Cryptographically pin each paired server and retain the public auth-session
-- id needed to rotate or revoke the corresponding remote bearer. Existing
-- rows remain readable but must be re-paired before the client will reconnect.
ALTER TABLE machines ADD COLUMN session_id TEXT NOT NULL DEFAULT '';
ALTER TABLE machines ADD COLUMN identity_key TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE machines DROP COLUMN identity_key;
ALTER TABLE machines DROP COLUMN session_id;
