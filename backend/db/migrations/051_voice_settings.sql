-- How the live voice agent sounds and behaves (docs/voice.md).
--
-- These belong in the database rather than config.toml because they are the
-- settings a person changes to taste and wants to hear the effect of. A
-- config-file value needs a server restart, and a restart reaps every in-flight
-- CLI process group -- far too much to pay for trying a different voice.
--
-- Single row, following host_presentation: there is one live-voice persona per
-- host, not one per user or per session. Empty values mean "use the [voice]
-- config default", so an operator who set one in config.toml keeps it until
-- somebody deliberately overrides it here.
--
-- voice_name and model are free text on purpose. Both name upstream things that
-- gain new members between agentique releases, and the model-catalog rule says
-- a new upstream release must not require one of ours.

-- +goose Up
CREATE TABLE voice_settings (
    id          INTEGER PRIMARY KEY CHECK (id = 1),
    voice_name  TEXT NOT NULL DEFAULT '',
    model       TEXT NOT NULL DEFAULT '',
    -- Composed instruction fragments, not the whole prompt: the safety rules
    -- are appended after these and are not editable from here.
    personality TEXT NOT NULL DEFAULT '',
    verbosity   TEXT NOT NULL DEFAULT '',
    updated_at  TEXT NOT NULL DEFAULT ''
);

-- +goose Down
DROP TABLE IF EXISTS voice_settings;
