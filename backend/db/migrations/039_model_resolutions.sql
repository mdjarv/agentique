-- +goose Up
-- Observed alias -> concrete model ID mappings, learned from the provider CLI's
-- init event (e.g. "opus" -> "claude-opus-5"). Lets the model catalog show the
-- live version without shipping a new agentique release.
CREATE TABLE model_resolutions (
    provider TEXT NOT NULL,
    slug TEXT NOT NULL,
    resolved_id TEXT NOT NULL,
    last_seen_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    PRIMARY KEY (provider, slug)
);

-- The concrete model the provider actually ran this session on, as reported by
-- its init event. Differs from `model` (the requested alias).
ALTER TABLE sessions ADD COLUMN resolved_model TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE sessions DROP COLUMN resolved_model;
DROP TABLE model_resolutions;
