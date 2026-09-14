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
  AND kind != 'day_summary'
ORDER BY at DESC, id DESC
LIMIT sqlc.arg(lim);

-- How many entries this surface has never been shown. The rail's notch reads
-- it once per connection; the journal pushes keep it current after that.
--
-- Same exclusions, and here they are the load-bearing ones: a badge is a claim
-- on attention, so a heartbeat that ticks every fifteen minutes would put a
-- permanent notch on the assistant's row and teach the operator to ignore it.
--
-- `day_summary` is left out for the other half of that rule: it is a row about
-- a fortnight ago, stamped at the day it is about, so it sorts to the BOTTOM of
-- every surface that renders the journal newest-first and is never what the
-- notch led the reader to. A fold of thirty days would claim thirty unread
-- things with nothing new to look at. The `compaction` row beside it is the one
-- that is news, and it stays counted (docs/assistant.md, M5 build notes).
-- name: CountAssistantJournalUnseen :one
SELECT COUNT(*) FROM assistant_journal
WHERE json_extract(seen_by, '$.' || sqlc.arg(surface)) IS NULL
  AND kind != 'heartbeat'
  AND kind != 'day_summary';

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
-- one log line to explain it. A count has no page to fill. Compaction (M5)
-- bounds the table now, but not this window: it folds only days OLDER than the
-- lookback, so a busy fortnight is still thousands of rows.
--
-- The payload path is '$.policyId', which is `payloadPolicyID` in
-- internal/assistant: the key is spelled in both places, so a rename is two
-- edits or a budget that counts nothing.
-- name: CountPolicySessionsCreatedSince :one
SELECT COUNT(*) FROM assistant_journal
WHERE kind = 'session_created'
  AND at >= sqlc.arg(since)
  AND json_extract(payload, '$.policyId') = sqlc.arg(policy_id);

-- Compaction: folding the journal's older days (docs/assistant.md, the M5
-- contract, and migration 060).
--
-- Four reads and three writes, and one predicate runs through all of them: a
-- RAW row is every kind except 'day_summary' with notable = 0. Notable rows are
-- exempt and stay whole, and a summary is not raw or a second pass would fold
-- its own output. The predicate is spelled in each statement rather than in a
-- view, because sqlc generates from the statement and a view would hide which
-- rows a DELETE reaches.
--
-- A day is `substr(at, 1, 10)`, the UTC date of the row's own stamp: `at` is
-- UTC RFC3339 seconds, so the first ten characters ARE the day and no date
-- function is needed. The trigger's "once a day" is a local-midnight question
-- and lives in Go; this is only which rows belong together.

-- Which days still have raw rows older than a stamp, oldest first.
--
-- One row per day with its count, so the pass can order the days itself and
-- knows before it reads a day whether that day is past what one summary may be
-- written from. GROUP BY over a range rather than a DISTINCT of every row: a
-- fortnight of a busy machine is thousands of rows and thirty answers.
-- name: ListAssistantJournalRawDaysBefore :many
SELECT CAST(substr(at, 1, 10) AS TEXT) AS day,
       CAST(COUNT(*) AS INTEGER) AS raw_rows
FROM assistant_journal
WHERE at < sqlc.arg(before)
  AND kind != 'day_summary'
  AND notable = 0
GROUP BY day
ORDER BY day ASC
LIMIT sqlc.arg(lim);

-- One page of a day's raw rows, newest first.
--
-- Paged with an (at, id) cursor rather than an offset, as the conversation's
-- history is: a day's rows are what the summary is written from, and an offset
-- over a table something else is writing to skips rows. An empty cursor starts
-- at the newest, which is also the end of the day the summary is written about.
--
-- The cursor is spelled out rather than as the row value `(at, id) < (?, ?)`
-- the messages query uses, because sqlc cannot type a row value's parameters:
-- it emitted `interface{}` for the stamp and `string` for an INTEGER id, and an
-- id compared as text is an id compared by affinity luck.
-- name: ListAssistantJournalRawForDay :many
SELECT * FROM assistant_journal
WHERE at >= sqlc.arg(day_start)
  AND at < sqlc.arg(next_day)
  AND kind != 'day_summary'
  AND notable = 0
  AND (sqlc.arg(before_at) = ''
       OR at < sqlc.arg(before_at)
       OR (at = sqlc.arg(before_at) AND id < sqlc.arg(before_id)))
ORDER BY at DESC, id DESC
LIMIT sqlc.arg(lim);

-- The shape of a WHOLE day: how many rows of each kind, and whether any of
-- them was agent-written.
--
-- Read from the day rather than from what was rendered, and that is the point.
-- A summary's PROSE is written from at most the newest `maxCompactDayRows` of a
-- day, where the DELETE below takes every raw row -- so a busy day's payload
-- would otherwise describe two thousand rows and lose the rest of the day's
-- shape with nothing saying it had. `untrusted` is the same argument in the
-- safe direction: one untrusted row anywhere in the day makes the summary
-- untrusted, which is what the contract says ("when any folded row was").
-- name: CountAssistantJournalRawKindsForDay :many
SELECT CAST(kind AS TEXT) AS kind,
       CAST(COUNT(*) AS INTEGER) AS entries,
       CAST(MAX(untrusted) AS INTEGER) AS untrusted
FROM assistant_journal
WHERE at >= sqlc.arg(day_start)
  AND at < sqlc.arg(next_day)
  AND kind != 'day_summary'
  AND notable = 0
GROUP BY kind;

-- Every standing instruction a WHOLE day's raw rows named, in the order they
-- were first named.
--
-- Same whole-day argument, and here it is the one that costs something: the
-- `policies` key exists so a longer budget lookback can find a folded day's
-- spending, and a list built from the newest two thousand rows would be a list
-- with the oldest ids silently missing. The payload path is '$.policyId',
-- which is `payloadPolicyID` in internal/assistant.
-- name: ListAssistantJournalRawPolicyIDsForDay :many
SELECT CAST(json_extract(payload, '$.policyId') AS TEXT) AS policy_id
FROM assistant_journal
WHERE at >= sqlc.arg(day_start)
  AND at < sqlc.arg(next_day)
  AND kind != 'day_summary'
  AND notable = 0
  AND json_extract(payload, '$.policyId') IS NOT NULL
GROUP BY policy_id
ORDER BY MIN(at) ASC, policy_id ASC;

-- Whether this day has already been folded.
--
-- The pass inserts the summary BEFORE it deletes the rows it was written from,
-- so a pass that dies between the two leaves a day holding both -- and the next
-- pass has to delete the leftovers without paying for a second summary. This is
-- what tells the two apart.
-- name: CountAssistantDaySummaries :one
SELECT COUNT(*) FROM assistant_journal
WHERE kind = 'day_summary'
  AND at >= sqlc.arg(day_start)
  AND at < sqlc.arg(next_day);

-- The fold itself: the day's raw rows go.
--
-- Answers how many rows it removed, because that number is what the pass
-- reports and journals -- and it is the only count that is true after the fact,
-- where the one read before the delete is what the summary could see.
-- name: DeleteAssistantJournalRawForDay :execrows
DELETE FROM assistant_journal
WHERE at >= sqlc.arg(day_start)
  AND at < sqlc.arg(next_day)
  AND kind != 'day_summary'
  AND notable = 0;

-- Retention: a summary older than the keep window goes too.
--
-- Ninety days, and the raw rows it folded are long gone -- which is why this
-- runs only on a pass that can also write summaries: deleting the one compact
-- record of the oldest days on a machine that cannot make any more would lose
-- them for nothing.
-- name: DeleteAssistantDaySummariesBefore :execrows
DELETE FROM assistant_journal
WHERE kind = 'day_summary' AND at < sqlc.arg(before);

-- When the journal was last folded. Stamped BEFORE the pass runs, so a pass
-- that fails is retried tomorrow rather than on every tick for the rest of the
-- day.
-- name: SetAssistantCompactedAt :exec
INSERT INTO assistant_state (id, last_compacted_at, created_at, updated_at)
VALUES (1, sqlc.arg(last_compacted_at), sqlc.arg(now), sqlc.arg(now))
ON CONFLICT(id) DO UPDATE SET
  last_compacted_at = excluded.last_compacted_at,
  updated_at = excluded.updated_at;

-- name: UpsertAssistantPeerFollow :exec
INSERT INTO assistant_peer_follows (machine_id, session_id, since, source)
VALUES (sqlc.arg(machine_id), sqlc.arg(session_id), sqlc.arg(since), sqlc.arg(source))
ON CONFLICT (machine_id, session_id) DO UPDATE SET source = excluded.source;

-- name: DeleteAssistantPeerFollow :exec
DELETE FROM assistant_peer_follows WHERE session_id = ?;

-- name: ListAssistantPeerFollows :many
SELECT machine_id, session_id, since, source FROM assistant_peer_follows ORDER BY since;
