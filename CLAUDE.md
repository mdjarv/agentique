# CLAUDE.md

## Task Completion Requirements

- `just check` (biome + tsc) must pass before considering tasks completed.
- `cd backend && go test ./... -count=1 -short` for Go changes — the justfile recipe doesn't accept flags, so run directly. `-short` skips the integration test that needs a live Claude CLI.
- `just sqlc` after modifying SQL queries in `backend/db/queries/`.
- ALWAYS use `just` commands (not raw `npx`/`tsc`) — they `cd` into the correct directory. Running `npx biome` from the project root fails silently.

## Project Snapshot

Agentique is a lightweight GUI for managing concurrent Claude Code agents across multiple projects. Go backend wraps [claudecli-go](https://github.com/allbin/claudecli-go), React frontend connects via WebSocket, deploys as a single embedded binary.

This repository is a VERY EARLY WIP. Proposing sweeping changes that improve long-term maintainability is encouraged.

## Core Priorities

1. Performance first.
2. Reliability first.
3. Keep behavior predictable under load and during failures (session restarts, reconnects, partial streams).

If a tradeoff is required, choose correctness and robustness over short-term convenience.

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

## Architecture

- **backend/cmd/agentique**: Entry point, DB init, default project creation.
- **backend/internal/server**: HTTP mux, SPA handler, embedded frontend assets.
- **backend/internal/ws**: WebSocket handler, hub (connection registry + broadcasting), wire message types.
- **backend/internal/gitops**: Pure git/gh CLI wrappers (merge, branch, worktree, diff, PR), no session dependencies.
- **backend/internal/session**: Session lifecycle (Service), GitService (orchestrates gitops), event streaming, state machine.
- **backend/internal/project**: Project CRUD routes.
- **backend/internal/store**: SQLite via sqlc — generated query code, migrations via goose.
- **frontend/src/components/chat/**: Chat UI — message rendering, composer, turn blocks, tool display.
- **frontend/src/components/layout/**: Sidebar, project tree, session status.
- **frontend/src/hooks/**: useWebSocket (connection + reconnect), useChatSession, useProjects.
- **frontend/src/stores/**: Zustand — app-store (projects), chat-store (sessions + turns), streaming-store (assistant text), selectors.
- **frontend/src/lib/**: Types, WS client (request/response correlation), event schemas, utils.

## Dev Workflow

```
just dev            # Run both servers in parallel (with auto-stop of previous)
just dev-frontend   # Vite HMR on :9200
just dev-backend    # Go server on :9201
```

Frontend connects WebSocket directly to :9201 (avoids Vite proxy flakiness).

## Key Commands

| Command | Purpose |
|---------|---------|
| `just dev-frontend` | Vite dev server (:9200) |
| `just dev-backend` | Go backend (:9201) |
| `just build` | Full production build (single binary) |
| `just check` | Biome lint + tsc typecheck |
| `just test-backend` | Go tests |
| `just test-e2e` | Playwright e2e tests |
| `just sqlc` | Regenerate sqlc query code |
| `just reset` | Delete all .db files |

## Frontend Conventions

- Biome: 2-space indent, 100-char line width, double quotes, semicolons, organize imports.
- Path alias: `~/` maps to `src/`.
- shadcn/ui primitives in `components/ui/`.
- `routeTree.gen.ts` is auto-generated — do not edit.

## Backend Conventions

- Standard Go style. Constructor pattern (`NewManager`, `NewServer`, etc.).
- sqlc generates type-safe query code from SQL in `backend/db/queries/`.
- Migrations in `backend/db/migrations/` (goose, sequential numbering).
- Simple `log` package — no structured logging for now.

## Reference

- [ROADMAP.md](ROADMAP.md) — vision, architecture diagram, milestones.
- [docs/websocket-protocol.md](docs/websocket-protocol.md) — WS method reference.
- [docs/database-schema.md](docs/database-schema.md) — tables, sqlc queries.
- [docs/claudecli-go-api.md](docs/claudecli-go-api.md) — Go wrapper API docs.
