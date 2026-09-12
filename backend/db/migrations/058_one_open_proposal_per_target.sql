-- One open proposal per verb and target, enforced where it cannot be raced.
--
-- db/queries/assistant.sql says why the rule exists: "a second row would give
-- the operator two buttons for one decision, and accepting either would leave
-- the other pointing at work already done." It was enforced by a read followed
-- by an insert, which is not enforcement -- two tool calls in one head message
-- are served on their own goroutines, both read no open row, and both insert.
-- Eight concurrent asks for one session produced three and four open cards.
--
-- A partial unique index is the structural answer: it holds however many
-- writers arrive, including ones added later, and the insert that loses the
-- race is refused rather than duplicating a decision. The Go side treats that
-- refusal as the duplicate answer it already had words for.
--
-- Keep this comment ASCII. sqlc expands `SELECT *` by byte offset, so one
-- multi-byte character shifts those offsets and corrupts the generated code
-- for LATER queries.

-- +goose Up

-- Collapse whatever duplicates already exist, or the index cannot be created.
-- The newest row per (verb, session_id, channel_id) is the card that would have
-- been shown; the older ones are marked stale -- the status the accept path
-- already uses for "this was overtaken", so every surface renders them without
-- learning a new word. decided_via stays empty: nobody decided these.
UPDATE assistant_proposals
SET status = 'stale',
    decided_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now'),
    outcome = 'overtaken by a later ask for the same thing'
WHERE status = 'open'
  AND EXISTS (
    SELECT 1 FROM assistant_proposals AS later
    WHERE later.status = 'open'
      AND later.verb = assistant_proposals.verb
      AND later.session_id = assistant_proposals.session_id
      AND later.channel_id = assistant_proposals.channel_id
      AND (later.created_at > assistant_proposals.created_at
           OR (later.created_at = assistant_proposals.created_at
               AND later.id > assistant_proposals.id))
  );

CREATE UNIQUE INDEX idx_assistant_proposals_one_open
    ON assistant_proposals(verb, session_id, channel_id)
    WHERE status = 'open';

-- +goose Down

DROP INDEX IF EXISTS idx_assistant_proposals_one_open;
