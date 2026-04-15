-- +goose Up
ALTER TABLE projects ADD COLUMN slug TEXT NOT NULL DEFAULT '';

-- Seed existing rows with the first 8 chars of their UUID.
-- Users can rename to something meaningful via project settings.
UPDATE projects SET slug = id WHERE slug = '';

CREATE UNIQUE INDEX projects_slug_idx ON projects(slug);

-- +goose Down
DROP INDEX IF EXISTS projects_slug_idx;
ALTER TABLE projects DROP COLUMN slug;
