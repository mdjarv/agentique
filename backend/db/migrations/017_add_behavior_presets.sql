-- +goose Up
ALTER TABLE sessions ADD COLUMN behavior_presets TEXT NOT NULL DEFAULT '{}';
ALTER TABLE projects ADD COLUMN default_behavior_presets TEXT NOT NULL DEFAULT '{}';

-- +goose Down
ALTER TABLE sessions DROP COLUMN behavior_presets;
ALTER TABLE projects DROP COLUMN default_behavior_presets;
