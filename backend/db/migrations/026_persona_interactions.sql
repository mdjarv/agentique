-- +goose Up

CREATE TABLE persona_interactions (
    id TEXT PRIMARY KEY,
    profile_id TEXT NOT NULL REFERENCES agent_profiles(id) ON DELETE CASCADE,
    team_id TEXT NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    asker_type TEXT NOT NULL DEFAULT 'user',
    asker_id TEXT NOT NULL DEFAULT '',
    question TEXT NOT NULL,
    action TEXT NOT NULL DEFAULT '',
    confidence REAL NOT NULL DEFAULT 0,
    response TEXT NOT NULL DEFAULT '',
    redirect_to TEXT NOT NULL DEFAULT '',
    response_time_ms INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);
CREATE INDEX idx_persona_interactions_profile ON persona_interactions(profile_id);
CREATE INDEX idx_persona_interactions_team ON persona_interactions(team_id);

-- +goose Down

DROP TABLE IF EXISTS persona_interactions;
