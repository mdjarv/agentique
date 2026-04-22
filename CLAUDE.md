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

## Channels, Hierarchy, and Coordination

The `messages` table is the source of truth for channel timelines. `session_events` is maintained for legacy agent-message display, but informational channel metadata (`messageType: "introduction"`, `messageType: "spawn"`) is **not** written to session events — see `writeLegacyAgentMessageEvents` in `session/channel.go`. When adding a new informational message type, extend that skip list.

**Introductions.** Every session join emits one intro message per (session, channel) pair, deduped via `CountSessionIntroductionsInChannel`. Intro metadata carries `name`, `role`, `worktreePath`, `capabilities`, `agentProfileId`, `avatar`. Capabilities come from the session's linked agent profile (`PersonaConfig.capabilities`).

**Agent-initiated spawning (`@spawn`).** Authorization runs before UI approval via `SpawnAuthCallback`:
- Channel lead (any channel) → auto-approve, no UI prompt
- Worker (member but not lead) → reject with "ask your lead" deny message
- Not in any channel → existing UI approval flow

`SpawnWorkersRequest.channelId` is optional — when set, the lead must already be a lead in that channel and workers join it; when empty, a fresh channel is created. Every successful spawn (auto or UI-approved) emits `messageType: "spawn"` to the target channel.

**Hierarchy.** `sessions.parent_session_id` populated by `CreateSwarm` and `extendSwarm` with the lead's ID. `DeleteSession` walks descendants depth-first, calling itself recursively so each child gets its full cleanup pass (stop, worktree, branch, files, broadcast) before the parent row is removed. The `ON DELETE CASCADE` FK is a safety net, not the primary cleanup mechanism — relying on it alone would leave worktrees orphaned on disk.

**Dissolve vs. Delete (leads).** Two distinct teardown paths:
- `DissolveChannel` — stops workers, removes their worktrees/branches, deletes the channel; **lead survives as a regular session** with its worktree and history intact. Use when you want to keep the lead's output.
- `DeleteSession` on a lead — cascades through `parent_session_id` and wipes the entire subtree including the lead. Use when the whole branch of work is done.

Both are exposed on each lead node in the `/teams` hierarchy tree.

**Additive principle.** All team coordination features are channel-only and must not modify existing session rendering, event pipeline mutations, or turn management for sessions outside a channel.
