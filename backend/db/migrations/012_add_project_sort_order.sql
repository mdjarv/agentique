-- +goose Up
ALTER TABLE projects ADD COLUMN sort_order INTEGER NOT NULL DEFAULT 0;

UPDATE projects
SET sort_order = (
    SELECT rn FROM (
        SELECT id, ROW_NUMBER() OVER (ORDER BY updated_at DESC) AS rn
        FROM projects
    ) ranked WHERE ranked.id = projects.id
);

-- +goose Down
ALTER TABLE projects DROP COLUMN sort_order;
