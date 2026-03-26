-- name: CountUsers :one
SELECT COUNT(*) FROM users;

-- name: CreateUser :one
INSERT INTO users (id, display_name, is_admin) VALUES (?, ?, ?) RETURNING *;

-- name: GetUser :one
SELECT * FROM users WHERE id = ?;

-- name: CreateWebAuthnCredential :exec
INSERT INTO webauthn_credentials (id, user_id, public_key, attestation_type, aaguid, sign_count, transport)
VALUES (?, ?, ?, ?, ?, ?, ?);

-- name: ListCredentialsByUser :many
SELECT * FROM webauthn_credentials WHERE user_id = ?;

-- name: GetCredentialByID :one
SELECT * FROM webauthn_credentials WHERE id = ?;

-- name: UpdateCredentialSignCount :exec
UPDATE webauthn_credentials SET sign_count = ? WHERE id = ?;

-- name: CreateAuthSession :exec
INSERT INTO auth_sessions (token, user_id, expires_at) VALUES (?, ?, ?);

-- name: GetAuthSession :one
SELECT s.token, s.user_id, s.expires_at, s.created_at,
       u.display_name, u.is_admin
FROM auth_sessions s
JOIN users u ON u.id = s.user_id
WHERE s.token = ? AND s.expires_at > strftime('%Y-%m-%dT%H:%M:%SZ', 'now');

-- name: DeleteAuthSession :exec
DELETE FROM auth_sessions WHERE token = ?;

-- name: DeleteExpiredAuthSessions :exec
DELETE FROM auth_sessions WHERE expires_at <= strftime('%Y-%m-%dT%H:%M:%SZ', 'now');

-- name: CreateInviteToken :exec
INSERT INTO invite_tokens (token, created_by, expires_at) VALUES (?, ?, ?);

-- name: GetInviteToken :one
SELECT * FROM invite_tokens WHERE token = ? AND used_by IS NULL AND expires_at > strftime('%Y-%m-%dT%H:%M:%SZ', 'now');

-- name: UseInviteToken :exec
UPDATE invite_tokens SET used_by = ?, used_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now') WHERE token = ?;

-- name: ListInviteTokens :many
SELECT * FROM invite_tokens WHERE created_by = ? ORDER BY created_at DESC;
