-- name: CreateChannel :one
INSERT INTO channels (id, name, project_id) VALUES (?, ?, ?) RETURNING *;

-- name: GetChannel :one
SELECT * FROM channels WHERE id = ?;

-- Ordinary channels only. The assistant's conversation is a channel with
-- kind = 'assistant' and is not one of a project's channels: it has no project,
-- it has no roster, and a row for it in the channel list would offer every
-- channel gesture (dissolve, add a member) against the thread.
-- name: ListChannelsByProject :many
SELECT * FROM channels WHERE project_id = ? AND kind = '' ORDER BY created_at ASC;

-- name: DeleteChannel :exec
DELETE FROM channels WHERE id = ?;

-- name: UpdateChannelName :exec
UPDATE channels SET name = ? WHERE id = ?;

-- name: AddChannelMember :exec
INSERT INTO channel_members (channel_id, session_id, role) VALUES (?, ?, ?)
ON CONFLICT (channel_id, session_id) DO UPDATE SET role = excluded.role;

-- name: RemoveChannelMember :exec
DELETE FROM channel_members WHERE channel_id = ? AND session_id = ?;

-- name: RemoveSessionFromAllChannels :exec
DELETE FROM channel_members WHERE session_id = ?;

-- name: ListChannelMemberSessions :many
SELECT s.*, cm.role AS member_role FROM sessions s
JOIN channel_members cm ON cm.session_id = s.id
WHERE cm.channel_id = ?
ORDER BY cm.joined_at ASC;

-- name: ListSessionChannels :many
SELECT cm.channel_id, cm.role FROM channel_members cm WHERE cm.session_id = ?;

-- name: ListAgentMessagesByChannel :many
SELECT se.* FROM session_events se
JOIN channel_members cm ON cm.session_id = se.session_id
WHERE cm.channel_id = ? AND se.type = 'agent_message'
ORDER BY se.created_at ASC;
