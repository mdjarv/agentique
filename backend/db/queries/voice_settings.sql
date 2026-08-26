-- name: GetVoiceSettings :one
SELECT voice_name, model, personality, verbosity, updated_at
FROM voice_settings WHERE id = 1;

-- name: SetVoiceSettings :exec
INSERT INTO voice_settings (id, voice_name, model, personality, verbosity, updated_at)
VALUES (1, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
  voice_name = excluded.voice_name,
  model = excluded.model,
  personality = excluded.personality,
  verbosity = excluded.verbosity,
  updated_at = excluded.updated_at;
