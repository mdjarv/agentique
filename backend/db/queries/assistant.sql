-- The assistant's state, journal, follow list and conversation channel.
-- See docs/assistant.md and migration 056.
--
-- Keep this file ASCII. sqlc expands `SELECT *` by byte offset, so one
-- multi-byte character shifts those offsets and corrupts the generated code
-- for LATER queries.

-- name: GetAssistantState :one
SELECT * FROM assistant_state WHERE id = 1;

-- name: SetAssistantChannel :exec
INSERT INTO assistant_state (id, channel_id, created_at, updated_at)
VALUES (1, sqlc.arg(channel_id), sqlc.arg(now), sqlc.arg(now))
ON CONFLICT(id) DO UPDATE SET
  channel_id = excluded.channel_id,
  updated_at = excluded.updated_at;

-- name: SetAssistantModel :exec
INSERT INTO assistant_state (id, model, created_at, updated_at)
VALUES (1, sqlc.arg(model), sqlc.arg(now), sqlc.arg(now))
ON CONFLICT(id) DO UPDATE SET
  model = excluded.model,
  updated_at = excluded.updated_at;

-- Records that a surface has looked. json_set on the existing object rather
-- than a rewrite, so two surfaces cannot overwrite each other's mark.
--
-- The DO UPDATE clause uses ?1/?2 rather than sqlc.arg(): sqlc does NOT rewrite
-- a named parameter inside an upsert's DO UPDATE, it copies the text through,
-- and `sqlc.arg(surface)` reaching SQLite is a runtime error in a statement
-- whose failure this code only logs. The numbers are the same parameters the
-- VALUES clause names.
-- name: SetAssistantSurfaceMark :exec
INSERT INTO assistant_state (id, surface_marks, created_at, updated_at)
VALUES (1, json_set('{}', '$.' || sqlc.arg(surface), sqlc.arg(at)), sqlc.arg(now), sqlc.arg(now))
ON CONFLICT(id) DO UPDATE SET
  surface_marks = json_set(assistant_state.surface_marks, '$.' || ?1, ?2),
  updated_at = excluded.updated_at;

-- name: CreateAssistantChannel :one
INSERT INTO channels (id, name, kind) VALUES (?, ?, 'assistant') RETURNING *;

-- The conversation, found without the state row. The oldest wins: if a second
-- one was ever created, the first is the one the history is in.
-- name: GetAssistantChannel :one
SELECT * FROM channels WHERE kind = 'assistant' ORDER BY created_at ASC, id ASC LIMIT 1;

-- One message of the conversation.
--
-- Its own insert rather than InsertMessage because created_at is passed IN.
-- The table's default stamps milliseconds, and an ask and its reply can land
-- in the same millisecond -- at which point the only tiebreak left is the
-- uuid, and the two read back in no order at all. A fixed-width nanosecond
-- stamp sorts lexicographically, which for this column is the only kind of
-- sorting there is. Nothing else writes to an assistant channel, so the
-- sharper format is consistent within the timeline that reads it.
-- name: InsertAssistantMessage :one
INSERT INTO messages (id, channel_id, sender_type, sender_id, sender_name, content, message_type, metadata, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- One page of the conversation, newest first. An empty `before` starts at the
-- newest message; otherwise it is a created_at cursor.
--
-- rowid breaks the tie, not id. messages.created_at has millisecond precision
-- and a message's id is a uuid, so two turns written in the same millisecond --
-- an ask and a mirrored reply -- would come back in uuid order, which is to say
-- in no order. rowid is insertion order, which is the order they were said in.
-- name: ListAssistantMessagesBefore :many
SELECT * FROM messages
WHERE channel_id = sqlc.arg(channel_id)
  AND (sqlc.arg(before_at) = '' OR (created_at, id) < (sqlc.arg(before_at), sqlc.arg(before_id)))
ORDER BY created_at DESC, id DESC
LIMIT sqlc.arg(lim);

-- What has been said in the conversation since a surface last looked, oldest
-- first: this is read to be pasted into a preamble or a strip, in order.
-- name: ListAssistantMessagesSince :many
SELECT * FROM messages
WHERE channel_id = sqlc.arg(channel_id) AND created_at > sqlc.arg(since)
ORDER BY created_at ASC, id ASC
LIMIT sqlc.arg(lim);

-- name: InsertAssistantJournalEntry :one
INSERT INTO assistant_journal (at, kind, session_id, project_id, summary, payload, untrusted, notable)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: ListAssistantJournalSince :many
SELECT * FROM assistant_journal
WHERE at >= sqlc.arg(since)
ORDER BY at DESC, id DESC
LIMIT sqlc.arg(lim);

-- name: ListAssistantJournalUnseen :many
SELECT * FROM assistant_journal
WHERE json_extract(seen_by, '$.' || sqlc.arg(surface)) IS NULL
ORDER BY at DESC, id DESC
LIMIT sqlc.arg(lim);

-- How many entries this surface has never been shown. The rail's notch reads
-- it once per connection; the journal pushes keep it current after that.
-- name: CountAssistantJournalUnseen :one
SELECT COUNT(*) FROM assistant_journal
WHERE json_extract(seen_by, '$.' || sqlc.arg(surface)) IS NULL;

-- Stamps everything this surface has now been shown, THROUGH the newest row it
-- was handed.
--
-- Through a boundary rather than row by row, because a look is bounded and the
-- journal is not: stamping only the rows returned left a backlog larger than
-- one page unseen, so the next look answered with the next fifty OLDER entries
-- and announced last week as news. The boundary is the newest row of the look,
-- so what it means is "you are caught up to here" -- which is the only reading
-- that is monotonic in time.
--
-- Already-stamped rows are left alone: a second surface's mark must not be
-- rewritten, and re-stamping the whole history on every look would be an
-- UPDATE over the table.
-- name: MarkAssistantJournalSeenThrough :exec
UPDATE assistant_journal
SET seen_by = json_set(seen_by, '$.' || sqlc.arg(surface), sqlc.arg(at))
WHERE json_extract(seen_by, '$.' || sqlc.arg(surface)) IS NULL
  AND (at < sqlc.arg(through_at)
       OR (at = sqlc.arg(through_at) AND id <= sqlc.arg(through_id)));

-- The session.state observer's baseline: what every session's two outcome
-- facts already were before the observer started watching. A push is a whole
-- snapshot, not a transition, so without this the first push for a session
-- archived last month reads as news -- and on the first boot with the
-- assistant on, that was one entry per archived session ever.
-- name: ListSessionOutcomeBaseline :many
SELECT id, worktree_merged, archived_at FROM sessions;

-- name: UpsertAssistantFollow :exec
INSERT INTO assistant_follows (session_id, since, briefed, source)
VALUES (?, ?, 0, ?)
ON CONFLICT(session_id) DO UPDATE SET source = excluded.source;

-- name: SetAssistantFollowBriefed :exec
UPDATE assistant_follows SET briefed = ? WHERE session_id = ?;

-- name: DeleteAssistantFollow :exec
DELETE FROM assistant_follows WHERE session_id = ?;

-- name: GetAssistantFollow :one
SELECT * FROM assistant_follows WHERE session_id = ?;

-- name: ListAssistantFollows :many
SELECT * FROM assistant_follows ORDER BY since ASC;
