# Agentique — Roadmap

## Vision

A lightweight GUI for managing concurrent coding agents across multiple projects.
Inspired by [pingdotgg/t3code](https://github.com/pingdotgg/t3code) but built on
[allbin/agentkit/runtime](https://github.com/allbin/agentkit)'s neutral provider
contract, with first-class Claude and Codex support and the door open for more.

**Why not just use t3code?** Its Node.js + Effect-TS backend had stalling/crashing
process-management issues. We have a battle-tested Go runtime (`agentkit/runtime`)
with Claude (`claudecli-go`) and Codex (`codexcli-go`) adapters behind one neutral
surface, so feature parity moves at SDK pace and provider choice is per-session.

See [README.md](README.md) for architecture, setup, and configuration; as-built
channel/team behavior lives in [CLAUDE.md](CLAUDE.md).

## Shipped

Well past MVP — milestones M0–M5 are complete. Full detail is in git history; the
headlines:

- **M0–M3 — Core chat.** Multi-session chat over WebSocket, git-worktree
  isolation per session, tool-permission approve/deny, resume + event
  persistence, worktree diff viewer, reconnect with backoff, keyboard shortcuts.
- **M4 — Agent workflow.** Merge worktree branches + create PRs, todo
  visualization, partial-message streaming, rate-limit banner, tool
  classification, state-machine enforcement, **session templates / saved
  prompts**, a **hung-session watchdog**, and **prompt-handoff cards** (agents
  emit fenced `prompt` blocks → one-click "Start Session").
- **M5 — Channels & hierarchy.** First-class channels with structured agent
  introductions + capabilities, `@spawn` worker delegation with lead
  auto-approval, a `parent_session_id` hierarchy, and the Teams-tab tree with
  cascade-delete and dissolve actions.
- **Teams phases 0–2** (behind `[experimental] teams = true`): agent profiles,
  Haiku-powered personas for discovery/triage, and multi-agent channel
  coordination. Background:
  [`plans/persistent-teams-brainstorm.md`](plans/persistent-teams-brainstorm.md),
  [`plans/channels.md`](plans/channels.md).
- **The Brain** — persistent, project-scoped agent memory (recall/encode/
  consolidate). See [`docs/brain-memory.md`](docs/brain-memory.md).

## What's next

The one coherent unbuilt feature arc is the back half of **Persistent Teams**
(experimental). Swarms are ephemeral + hierarchical; teams are persistent +
peer-to-peer. Phases 0–2 shipped; remaining:

| Phase | Focus | Remaining work |
|---|---|---|
| **3: Cross-project DMs** | Cross-project messaging | `channels.project_id` is still `NOT NULL` — make nullable or add a junction table. Metadata-driven 1:1 routing, or auto-created 1:1 channels. The offline message queue already exists (`message_deliveries`). |
| **4: Topology presets** | Named preamble modes | `communicationMode` is a defined-but-unread field. Inject per-mode routing instructions: `spoke` (default), `mesh` (workers talk directly), `spoke+request` (ask lead before peer messaging). Preamble text only — no routing enforcement. |
| **5: Autonomy tuning** | Gated auto-spawn | Persona confidence threshold as the autonomy dial (concurrent cap already enforced via `max_sessions`), spawn idempotency key, partial-failure reporting in spawn audit messages. |

Design stance carried from phases 0–2: user-gated for now (messages queue for
offline agents; the user decides when to spawn); personas are the autonomy
gateway (a confidence threshold rather than binary flags); observe discovery
patterns before widening automation.

## Maybe

Optional and unscheduled — pick up if the itch is real.

- **Split-pane session layout** — two sessions side by side.
- **MCP server management UI** — surface reconnect/toggle/status over WS. The
  claude adapter already reconnects MCP live; this just exposes it.
- **Desktop app via Tauri** / **xterm.js terminal** — larger bets that diverge
  from the current web + embedded-binary shape. Parked unless the product
  pivots toward an IDE-like surface.

## Dropped (superseded)

- **`/btw` for auto-naming** — auto-naming already ships via a Haiku
  `BlockingRunner` (autotitle); the `/btw` protocol isn't needed.
- **File checkpointing / rewind** — redundant now that every session runs in an
  isolated git worktree; git already provides rollback.

## Investigations

Open design questions, not committed work.

### Sibling-session awareness

Sessions knowing about other active sessions in the same project — to avoid
duplicate work and align on shared interfaces. Build the preamble dynamically at
connect time with a summary of active siblings. **Hard parts:** descriptors go
stale (initial prompt ≠ current focus); the preamble is fixed at connect time, so
mid-conversation changes aren't visible without per-query system-prompt updates;
token cost grows with sibling count; over-coordination wastes tokens. Likely
opt-in, only when >1 sibling exists.

### Cross-session delegation for housekeeping

A merge needs a clean project root. If a local (worktree-less) session owns the
uncommitted changes, the merge UI could message it — "commit so I can merge" —
now that inter-session messaging exists (channels). **Simpler alternative:** a
"commit all & merge" compound action that auto-commits with a generated message.

## Provider Runtime Notes

- Sessions are driven through `agentkit/runtime`'s neutral `CLISession` /
  `CLIConnector` contract. Adapters live under `agentkit/runtime/cli/{claude,codex}`.
- Provider CLI init takes ~30-40s on first connect; the frontend needs a long
  timeout for `session.create`.
- Detect turn boundaries via `runtime.TurnCompletedEvent` (mapped from
  `claudecli.ResultEvent` / codex `turn/completed`).
- Codex supports resume and rate-limit events; mid-turn send is emulated
  (queued + replayed at the next idle boundary). Codex does **not** support
  natively: fork, plan mode, thinking, subagents, compaction events, MCP
  reconnect, or tool-progress ticks. The UI must check `Capabilities()`
  rather than assume; see `docs/2026-05-25-capabilities-wire-shape.md`.
- Known gaps (tracked in `docs/tech-debt.md`):
  - `reasoning_delta` and `turn_diff` events flow through the backend
    pipeline but have no frontend renderers yet.
  - Codex error classification is generic (every codex error maps to
    `api_error`) because codexcli-go lacks the error sentinels claudecli
    exposes.
- Sources:
  - [github.com/allbin/agentkit](https://github.com/allbin/agentkit) — runtime + adapters
  - [github.com/allbin/claudecli-go](https://github.com/allbin/claudecli-go) — claude provider
  - [github.com/allbin/codexcli-go](https://github.com/allbin/codexcli-go) — codex provider
</content>
