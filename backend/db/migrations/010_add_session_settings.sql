-- +goose Up
ALTER TABLE sessions ADD COLUMN effort TEXT NOT NULL DEFAULT '';
ALTER TABLE sessions ADD COLUMN max_budget REAL NOT NULL DEFAULT 0;
ALTER TABLE sessions ADD COLUMN max_turns INTEGER NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE sessions DROP COLUMN effort;
ALTER TABLE sessions DROP COLUMN max_budget;
ALTER TABLE sessions DROP COLUMN max_turns;
