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

-- What a surface has missed. 'heartbeat' rows are left out, for the same reason
-- the digest and the head's news leave them out: a tick that woke up, looked and
-- decided nothing is the assistant's own bookkeeping, and what it DID has its
-- own entry beside it. They stay in ListAssistantJournalSince, which is the
-- journal page and the audit trail for every model the heartbeat paid for.
-- name: ListAssistantJournalUnseen :many
SELECT * FROM assistant_journal
WHERE json_extract(seen_by, '$.' || sqlc.arg(surface)) IS NULL
  AND kind != 'heartbeat'
ORDER BY at DESC, id DESC
LIMIT sqlc.arg(lim);

-- How many entries this surface has never been shown. The rail's notch reads
-- it once per connection; the journal pushes keep it current after that.
--
-- Same exclusion, and here it is the load-bearing one: a badge is a claim on
-- attention, so a heartbeat that ticks every fifteen minutes would put a
-- permanent notch on the assistant's row and teach the operator to ignore it.
-- name: CountAssistantJournalUnseen :one
SELECT COUNT(*) FROM assistant_journal
WHERE json_extract(seen_by, '$.' || sqlc.arg(surface)) IS NULL
  AND kind != 'heartbeat';

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

-- Proposals: the uncontained tier's card, and the record of its yes or no.
-- See docs/assistant.md's M3 contract and migration 057.

-- name: InsertAssistantProposal :one
INSERT INTO assistant_proposals (
    id, created_at, verb, session_id, project_id, channel_id,
    args, rationale, evidence, status, expires_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 'open', ?)
RETURNING *;

-- name: GetAssistantProposal :one
SELECT * FROM assistant_proposals WHERE id = ?;

-- One open proposal for the same verb and the same target, if there is one.
--
-- Asking twice for the same thing is one card, not two: a second row would
-- give the operator two buttons for one decision, and accepting either would
-- leave the other pointing at work already done.
-- name: GetOpenAssistantProposalFor :one
SELECT * FROM assistant_proposals
WHERE status = 'open'
  AND verb = sqlc.arg(verb)
  AND session_id = sqlc.arg(session_id)
  AND channel_id = sqlc.arg(channel_id)
ORDER BY created_at DESC, id DESC
LIMIT 1;

-- Open first, then whatever was decided, newest first within each half. A
-- surface renders the open ones as cards and the rest as history, and one read
-- answers both.
-- name: ListAssistantProposals :many
SELECT * FROM assistant_proposals
ORDER BY (status != 'open'), created_at DESC, id DESC
LIMIT sqlc.arg(lim);

-- The decision. Guarded on status = 'open' so two surfaces cannot both decide
-- one proposal: the second write touches nothing, and the caller re-reads the
-- row it did not change.
--
-- It answers how many rows it changed, so the caller does not have to assume
-- the guard matched. Zero means somebody else settled this proposal first,
-- which is a different thing from the write failing -- and after an action has
-- already been performed, the difference is worth a log line that says which.
-- name: DecideAssistantProposal :execrows
UPDATE assistant_proposals
SET status = sqlc.arg(status),
    decided_at = sqlc.arg(decided_at),
    decided_via = sqlc.arg(decided_via),
    outcome = sqlc.arg(outcome)
WHERE id = sqlc.arg(id) AND status = 'open';

-- Lazy expiry, applied on a decide: an open proposal past its expiry is not an
-- offer any more. There is no timer behind this -- a proposal nobody looks at
-- costs nothing, and a sweep would be a second writer on a table whose whole
-- content is decisions.
-- name: ExpireAssistantProposals :exec
UPDATE assistant_proposals
SET status = 'expired', decided_at = sqlc.arg(at)
WHERE status = 'open' AND expires_at != '' AND expires_at <= sqlc.arg(at);

-- Stamps the window the last digest covered, so the next one starts where it
-- finished. Written only by Digest.
-- name: SetAssistantDigestAt :exec
INSERT INTO assistant_state (id, last_digest_at, created_at, updated_at)
VALUES (1, sqlc.arg(last_digest_at), sqlc.arg(now), sqlc.arg(now))
ON CONFLICT(id) DO UPDATE SET
  last_digest_at = excluded.last_digest_at,
  updated_at = excluded.updated_at;

-- Standing instructions and the heartbeat's mark (docs/assistant.md, the M4
-- contract, and migration 059).

-- Name order, so the list a person reads and the list the heartbeat reads are
-- in the same order. The id breaks a tie between two policies named the same.
-- name: ListAssistantPolicies :many
SELECT * FROM assistant_policies ORDER BY name ASC, id ASC;

-- name: GetAssistantPolicy :one
SELECT * FROM assistant_policies WHERE id = ?;

-- One statement for a new policy and an edit, because the client sends the
-- whole row either way: a policy is one form with a Save button, and a
-- partial update would need a field-by-field patch nobody asked for.
-- created_at is left alone on an edit, so a row keeps the day it was written.
-- name: UpsertAssistantPolicy :one
INSERT INTO assistant_policies (
    id, name, text, enabled, budget_in_flight, budget_per_day, created_at, updated_at
) VALUES (
    sqlc.arg(id), sqlc.arg(name), sqlc.arg(text), sqlc.arg(enabled),
    sqlc.arg(budget_in_flight), sqlc.arg(budget_per_day), sqlc.arg(now), sqlc.arg(now)
)
ON CONFLICT(id) DO UPDATE SET
  name = excluded.name,
  text = excluded.text,
  enabled = excluded.enabled,
  budget_in_flight = excluded.budget_in_flight,
  budget_per_day = excluded.budget_per_day,
  updated_at = excluded.updated_at
RETURNING *;

-- name: DeleteAssistantPolicy :exec
DELETE FROM assistant_policies WHERE id = ?;

-- When the heartbeat last acted under this policy. Bookkeeping the operator
-- reads, never a budget: what a budget counts is journal entries, which an
-- edit cannot rewrite.
-- name: TouchAssistantPolicy :exec
UPDATE assistant_policies
SET last_fired_at = sqlc.arg(last_fired_at), updated_at = sqlc.arg(now)
WHERE id = sqlc.arg(id);

-- The heartbeat's mark: when the last tick measured from. Stamped by every
-- tick, including the ones that ran no model at all.
-- name: SetAssistantHeartbeatAt :exec
INSERT INTO assistant_state (id, last_heartbeat_at, created_at, updated_at)
VALUES (1, sqlc.arg(last_heartbeat_at), sqlc.arg(now), sqlc.arg(now))
ON CONFLICT(id) DO UPDATE SET
  last_heartbeat_at = excluded.last_heartbeat_at,
  updated_at = excluded.updated_at;

-- The heartbeat's gate: has anything happened since the last beat.
--
-- The assistant's OWN heartbeat entries are excluded, and that exclusion is
-- what makes the gate able to close: a tick journals its verdict and stamps
-- the mark in the same second, so counting its own row would leave every
-- later tick with one entry to triage and a Haiku call to pay for it. The
-- lower boundary is inclusive, as the digest's is, on the same reasoning --
-- triaging one entry twice costs a tick, dropping one loses news.
-- name: CountAssistantJournalSince :one
SELECT COUNT(*) FROM assistant_journal
WHERE at >= sqlc.arg(since) AND kind != 'heartbeat';

-- A policy's day budget: how many sessions this standing instruction has
-- created since a stamp.
--
-- Counted in SQL rather than by paging the journal into Go, and that is the
-- point rather than an optimisation. The page was 2000 rows over a fourteen-day
-- window and FAILED CLOSED when it filled, so a machine busy enough to journal
-- 143 entries a day would have refused every budgeted verb from then on, with
-- one log line to explain it -- and the day-summary compaction that would have
-- bounded the window is not built. A count has no page to fill.
--
-- The payload path is '$.policyId', which is `payloadPolicyID` in
-- internal/assistant: the key is spelled in both places, so a rename is two
-- edits or a budget that counts nothing.
-- name: CountPolicySessionsCreatedSince :one
SELECT COUNT(*) FROM assistant_journal
WHERE kind = 'session_created'
  AND at >= sqlc.arg(since)
  AND json_extract(payload, '$.policyId') = sqlc.arg(policy_id);
