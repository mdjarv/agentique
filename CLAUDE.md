# CLAUDE.md

## Task Completion Requirements

- `just check` (biome + tsc) must pass before considering tasks completed.
- `cd backend && go test ./... -count=1 -short` for Go changes — the justfile recipe doesn't accept flags, so run directly. `-short` skips the integration test that needs a live Claude CLI.
- `just sqlc` after modifying SQL queries in `backend/db/queries/`.
- ALWAYS use `just` commands (not raw `npx`/`tsc`) — they `cd` into the correct directory. Running `npx biome` from the project root fails silently.
- Put scratch files (screenshots, exports, temp data) in `tmp/` — it's gitignored. Never commit images to the repo root.
- **After merge:** remind the user to restart the backend if Go files changed. Frontend-only changes hot-reload automatically via the dev server.

## Project Snapshot

Agentique is a lightweight GUI for managing concurrent Claude Code agents across multiple projects. Go backend wraps [claudecli-go](https://github.com/mdjarv/claudecli-go), React frontend connects via WebSocket, deploys as a single embedded binary.

This repository is a VERY EARLY WIP. Proposing sweeping changes that improve long-term maintainability is encouraged.

## Core Priorities

1. Performance first.
2. Reliability first.
3. Keep behavior predictable under load and during failures (session restarts, reconnects, partial streams).

If a tradeoff is required, choose correctness and robustness over short-term convenience.

## Domain Context

- **Costs are irrelevant.** We use API subscriptions. Don't surface costs/prices in UI, CLI output, or mockups. The `totalCost` field exists in the data model but should not be shown to users.
- **Session references** use the format `project-slug/short-id` (e.g., `agentique/550e8400`). The short ID is the first 8 characters of the session UUID. When a user pastes a reference like this, resolve it by: finding the project by slug, then prefix-matching the short ID against session IDs in that project.

## Database Access

The live SQLite database is at `~/.local/share/agentique/agentique.db`. Sessions running in Agentique share this file with the running server.

**Reads are encouraged.** Use `sqlite3 ~/.local/share/agentique/agentique.db` for read-only queries when it helps answer questions about sessions, projects, events, or state. Key tables: `projects`, `sessions`, `session_events`, `teams`, `tags`, `project_tags`.

**Writes require explicit user approval.** Never INSERT, UPDATE, DELETE, DROP, or ALTER without asking first. A bad write to the live DB causes immediate data loss for all running sessions. If you need to test write operations, use a copy or an in-memory DB.

## Engineering Practices

These are non-negotiable. Apply them in all new code and improve existing code when you touch it.

**Separation of concerns.** Each module/function/component has one job. Don't mix IO with logic, don't mix state management with rendering, don't mix transport with business rules. If a function does two things, split it.

**Guard clauses and early returns.** Handle error cases and edge cases at the top. Never nest happy-path logic inside conditionals when you can return/continue early. Deep nesting is a code smell.

**DRY — but with judgment.** Duplicate logic across files is a code smell. Extract shared logic into a separate module. But don't abstract prematurely — two instances are a coincidence, three are a pattern. When you do extract, the abstraction must be a genuine simplification, not just indirection.

**Testability by design.** Accept dependencies via constructor/parameters, not global state. Keep pure logic separate from side effects. If something is hard to test, that's a design problem — fix the design, don't skip the test.

**Small functions, clear names.** Functions should do one thing and be named for what they do. If you need a comment to explain what a block does, extract it into a named function instead.

**Explicit over implicit.** Prefer clear, obvious code over clever code. No magic strings, no hidden coupling, no side effects that aren't obvious from the call site.

**Error handling is not optional.** Handle errors at the appropriate level. Don't swallow errors silently. Don't panic for recoverable failures. Propagate context with errors so failures are diagnosable.

**Immutability where practical.** Prefer const/readonly. Don't mutate function arguments. Build new state instead of mutating existing state, especially in React components and Zustand stores.

## Maintainability

Don't be afraid to change existing code. Don't take shortcuts by just adding local logic to solve a problem. If adding a feature reveals structural problems, fix the structure.

## Frontend Conventions

- Biome: 2-space indent, 100-char line width, double quotes, semicolons, organize imports.
- Path alias: `~/` maps to `src/`.
- shadcn/ui primitives in `components/ui/`.
- `routeTree.gen.ts` is auto-generated — do not edit.
- **Zustand selectors must return stable references.** Never return `{}`, `[]`, or the result of `.map()`/`.filter()`/`Object.values()` etc. as a fallback or computed value — these create a new reference every render, causing infinite re-render loops. Use a module-level constant (e.g. `const EMPTY_FOO: Foo[] = []`) for fallbacks. For computed arrays/objects, use `useShallow` or memoize outside the selector.

## Backend Conventions

- Standard Go style. Constructor pattern (`NewManager`, `NewServer`, etc.).
- sqlc generates type-safe query code from SQL in `backend/db/queries/`.
- Migrations in `backend/db/migrations/` (goose, sequential numbering).
- Simple `log` package — no structured logging for now.

## Reference

- [README.md](README.md) — architecture, tech stack, dev workflow, commands.
- [ROADMAP.md](ROADMAP.md) — vision, milestones, investigations.
