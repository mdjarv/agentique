# CLAUDE.md

## Task Completion

- `just check` (biome + tsc) must pass before considering tasks completed.
- `cd backend && go test ./... -count=1 -short` for Go changes — run directly, not via justfile. `-short` skips the integration test that needs a live Claude CLI.
- ALWAYS use `just` commands (not raw `npx`/`tsc`) — they `cd` into the correct directory. Running `npx biome` from the project root fails silently.

## Core Priorities

1. Performance first.
2. Reliability first.
3. Keep behavior predictable under load and during failures (session restarts, reconnects, partial streams).
4. Fix structural problems when found, don't work around them.

If a tradeoff is required, choose correctness and robustness over short-term convenience.

## Domain Context

- **Costs are irrelevant.** We use API subscriptions. Don't surface costs/prices in UI, CLI output, or mockups. The `totalCost` field exists in the data model but should not be shown to users.

## Database Access

The live SQLite database is at `~/.local/share/agentique/agentique.db`. Sessions running in Agentique share this file with the running server.

**Reads are encouraged.** Use `sqlite3 ~/.local/share/agentique/agentique.db` for read-only queries when it helps answer questions about sessions, projects, events, or state. Key tables: `projects`, `sessions`, `session_events`, `teams`, `tags`, `project_tags`.

**Writes require explicit user approval.** Never INSERT, UPDATE, DELETE, DROP, or ALTER without asking first. A bad write to the live DB causes immediate data loss for all running sessions. If you need to test write operations, use a copy or an in-memory DB.

## Engineering Practices

**Separation of concerns.** Each module/function/component has one job. Don't mix IO with logic, state management with rendering, or transport with business rules.

**Guard clauses and early returns.** Handle error/edge cases at the top. Never nest happy-path logic inside conditionals when you can return early.

**Error handling is not optional.** Don't swallow errors silently. Don't panic for recoverable failures. Propagate context with errors.

## Frontend Conventions

- Path alias: `~/` maps to `src/`.
- `routeTree.gen.ts` is auto-generated — do not edit.
- **Zustand selectors must return stable references.** Never return `{}`, `[]`, or the result of `.map()`/`.filter()`/`Object.values()` etc. as a fallback or computed value — these create a new reference every render, causing infinite re-render loops. Use a module-level constant (e.g. `const EMPTY_FOO: Foo[] = []`) for fallbacks. For computed arrays/objects, use `useShallow` or memoize outside the selector.

## Backend Conventions

- sqlc generates type-safe query code from SQL in `backend/db/queries/` — do not edit generated files.
- Migrations in `backend/db/migrations/` (goose, sequential numbering).
