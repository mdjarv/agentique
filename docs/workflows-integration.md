# Dynamic Workflows — integration design

Status: **Phase 1 MVP shipped** (2026-06-30). agentkit's workflow support merged
to master; agentique consumer wired against it. §1–§5 are the original design;
the status of each piece is recorded in "## Implemented" below.

## Implemented (Phase 1 MVP — agentique consumer)

Pins bumped: `claudecli-go → v0.1.0`, `agentkit → master (8d3508fd…)`.

- **Backend** (`internal/session/`):
  - `WireTaskEvent` extended with `workflowName` / `outputFile` / `endTime` /
    `workflowProgress[]` (+ `WireWorkflowProgress` mirror of
    `runtime.WorkflowAgentProgress`); populated from `SubagentEvent` in `wire.go`.
  - `WireResultEvent.workflowPending` carried from `TurnCompletedEvent`.
  - New `WireWorkflowLaunchedEvent` (`type:"workflow_launched"`) from
    `runtime.WorkflowLaunchedEvent`.
  - **Placeholder suppression** (the two-result fix on our side): a
    `WorkflowPending` `TurnCompletedEvent` is short-circuited in
    `handleTerminalEvents` (no pulse reset / turn-complete hook), and the
    `WorkflowPending` `WireResultEvent` is marked transient in `isTransient` (no
    DB row, no activity-feed item) — broadcast-only.
  - `WireCapabilities.workflows` mirrors `runtime.Capabilities.Workflows`
    (claude=true, codex=false).
- **Frontend**:
  - `chat-types.ts` / `events.ts`: parse the new task + result fields; new
    `WorkflowProgressEntry`.
  - `apply-event.ts`: a `workflowPending` result is dropped (never ends the turn
    / renders as the assistant message).
  - `WorkflowActivity.tsx`: phase → agent tree (state glyphs, live tokens/tools,
    auto-folding completed phases), keyed on the task events' `toolUseId`;
    `SegmentRenderer` routes `local_workflow` tasks to it, others to
    `SubagentActivity`.
- **Tests**: wire mapping (workflow task / launched / pending), pipeline
  placeholder-suppression (no turn-complete, transient), apply-event placeholder
  drop. Backend `-race` clean; `just check` green.

### Flagged: agentkit interface gap (for the parallel agentkit work)

`runtime.WorkflowLaunchedEvent` carries no `TaskID`/`ToolUseID`, so it cannot be
correlated client-side to the subsequent `local_workflow` task stream. The
upstream `claudecli.WorkflowLaunch` *does* have a `TaskID` (and the
`async_launched` tool_result has a tool_use_id). **Recommend agentkit add
`TaskID` (and ideally `ToolUseID`) to `WorkflowLaunchedEvent`.** Until then the
workflow panel keys on the task events' `ToolUseID` (robust), and the launch
event is surfaced on the wire but not rendered as a separate element.

### Known MVP limitation

`task_progress` stays transient (not persisted), so the live phase/agent tree is
lost on reconnect/reload — the panel then shows header + aggregates (from the
persisted `task_notification`) but an empty tree. Phase 2 (manifest rehydration)
was explicitly **declined** by the agentkit design (a workflow can't outlive its
CLI process, so the in-band stream is authoritative). Accepted for MVP.

## 1. What landed upstream (claudecli-go v0.1.0)

Current pin: `v0.0.0-20260612055547` (2026-06-12). Latest: **`v0.1.0`** (2026-06-29).
The headline feature is **dynamic workflow support**
(<https://code.claude.com/docs/en/workflows>) — the same multi-agent
orchestration the `Workflow` tool drives.

A workflow is a CLI-runtime feature, **not** an SDK/protocol layer:

- **Triggering is pure prompt content** — `ultracode`, "run a workflow…", or a
  saved/bundled `/<name>` (e.g. `/deep-research`). No new flag or API.
  `--dangerously-skip-permissions` makes it auto-run headless — which is exactly
  what agentique's **fullAuto** sessions already pass. So workflows already fire
  in fullAuto sessions today; we just don't render them well.
- **Surfaces through the existing `task_*` stream** as one synthetic task with
  `TaskType == "local_workflow"`. So it already parses to a `TaskEvent` /
  neutral `SubagentEvent` — nothing becomes `UnknownEvent`.
- **Two-turn lifecycle / two `ResultEvent`s.** The first result says "running in
  the background"; the second (after completion) carries the real answer.
  **Consumers must take the _last_ result.** ⚠️ This is the main correctness risk
  for us (see §4).
- **Rich structured progress.** `task_progress` carries `workflow_progress[]`:
  `workflow_phase` entries (`index`, `title`) and `workflow_agent` entries
  (`label`, `phaseTitle`, `state` ∈ queued/start/progress/done/error, `model`,
  `tokens`, `toolCalls`, `lastToolName`, `lastToolSummary`, `promptPreview`,
  `resultPreview`, `durationMs`, timestamps). This is the data that makes an
  *excellent* UI possible.
- **Out-of-band monitoring.** The runtime persists live run state on disk keyed
  by `runId` (survives `--no-session-persistence`). New API: `WorkflowLaunch`
  (from the `async_launched` tool result, with `ManifestPath()`/`JournalPath()`),
  `ReadWorkflowSnapshot` (one-shot), `WatchWorkflow` (polling → `WorkflowSnapshot`
  channel until terminal). Layout is an *undocumented internal* CLI detail —
  parse defensively, keep raw bytes.

Also in v0.1.0 (smaller): `ThinkingTokensEvent` (was `UnknownEvent`),
`Usage.TotalTokens()`/`String()`, `ModelFable`, `ModelDisplayName`. Backward
compatible — only additive; `task_updated`/`thinking_tokens` stop being
`UnknownEvent` (we never matched those by string, so no break for us).

## 2. Workflows vs. agentique's existing multi-agent concepts

agentique already has a rich multi-agent story. Keep these **distinct** — they
compose, they don't merge:

| | Channels / swarms (`@spawn`, Teams tab) | Dynamic workflows |
|---|---|---|
| Orchestrated by | agentique (`session.Manager`) | the **CLI runtime**, inside one session |
| Unit | a full peer **session** (own worktree, own CLI process, chat-visible, persistent) | an **ephemeral subagent** in the parent CLI process |
| Lifetime | independent; survives the lead | dies with the parent `-p`/session turn |
| Control | SendMessage, hierarchy, dissolve/delete | a JS script; fire-and-forget |
| Coordination | `messages` table, intros, parent_session_id | on-disk manifest + journal |

Mental model: **channels = a team of colleagues; a workflow = one colleague
running a structured batch job.** A workflow happens *within* a single session's
turn. The Teams tab may later show workflow runs as a leaf node type, but they
are not sessions and must not touch the session/channel pipeline (Additive
principle).

## 3. The integration chain (4 layers)

```
claudecli-go v0.1.0   →   agentkit (neutral runtime)   →   agentique backend   →   frontend
  TaskEvent.Workflow*       SubagentEvent + workflow fields    WireTaskEvent + fields    WorkflowActivity UI
  WorkflowLaunch            (new) WorkflowLaunchEvent          (new) wire launch event   phase/agent tree
  WatchWorkflow             (opt) neutral monitor              (opt) manifest poller     reconnect rehydrate
```

**The blocker is the middle.** Even the newest cached agentkit
(`20260629085656`) maps `claudecli.TaskEvent → runtime.SubagentEvent` but
**drops every workflow field** (`WorkflowName`, `WorkflowProgress`, `OutputFile`,
`EndTime`) and ignores `UserEvent.WorkflowLaunch`. agentique cannot import
`claudecli` types in the session pipeline (architecture rule —
`internal/session` stays neutral), so **the data physically cannot reach
agentique until agentkit threads it through the neutral contract.** That's the
parallel agentkit work.

### Neutral contract agentkit must expose (coordinate here)

MVP (unblocks all the good UX):

1. Bump `claudecli-go` → `v0.1.0`.
2. Extend `runtime.SubagentEvent` with: `WorkflowName string`, `OutputFile
   string`, `EndTime int64`, `WorkflowProgress []WorkflowProgressEntry`, and an
   `IsWorkflow()` helper. Mirror `WorkflowProgressEntry`/`WorkflowPhase` as
   neutral structs (do **not** re-export `claudecli.*` — same boundary discipline
   as today).
3. **Turn-lifecycle fix (critical, §4):** do not let the *first* workflow
   `ResultEvent` drive `Running→Idle`. Keep the turn `Running` until the
   workflow's terminal `task_notification` (or the second/last `ResultEvent`).
   This belongs in agentkit because it owns `ResultEvent → TurnCompletedEvent`
   and `eventloop.maybeFinishTurn`.

Phase 2 (out-of-band):

4. Neutral `WorkflowLaunchEvent` (from `UserEvent.WorkflowLaunch`): `RunID`,
   `WorkflowName`, `ScriptPath`, `TranscriptDir`, and resolved `ManifestPath` /
   `JournalPath` (resolve inside agentkit so agentique never touches claudecli
   path logic).
5. Either expose a neutral `WatchWorkflow(ctx, launch) <-chan WorkflowSnapshot`
   on the runtime, **or** just hand agentique the manifest path and let it poll
   (agentique already reads files freely). Prefer the neutral monitor to keep the
   undocumented-layout dependency on one side of the boundary.

Nice-to-have (independent of workflows): neutral `ThinkingTokensEvent`.

## 4. Correctness concerns (Performance/Reliability priorities)

1. **Two-result turn completion (must-fix).** Today
   `eventloop.maybeFinishTurn` flips `Running→Idle` on any
   `TurnCompletedEvent`, and agentique reacts to `StateIdle` with real side
   effects: `git_ops.go` auto-merge/refresh, brain learn-on-completion via
   `OnTurnComplete`, pulse reset, and **the composer unlocks → the user can send
   a message that interleaves mid-workflow.** If the first ("background")
   result completes the turn, all of that fires while the workflow is still
   running. Fix in agentkit (keep turn `Running` until terminal). agentique
   keeps a **fallback guard**: track the in-flight workflow `taskId` (from
   `task_started`) and suppress `OnTurnComplete` side effects until its
   `task_notification` arrives, in case the lifecycle differs in interactive
   mode.
   - ⚠️ **Unverified:** the two-result lifecycle was captured under headless
     `claude -p`. agentique runs a *persistent interactive* session
     (`claudecli.Session`, resume, mid-turn send). Behaviour there may differ —
     **verify before relying on either path** (capture a real workflow run
     through an agentique session, inspect the event stream).

2. **`task_progress` is transient (not persisted).** `isTransient()` drops
   `WireTaskEvent{Subtype:"task_progress"}`; only `task_started` +
   `task_notification` hit the DB. So on **reconnect mid-workflow** the live
   phase/agent tree is lost — the user sees "started" then nothing until
   "completed". Two options:
   - MVP: accept it (matches today's subagent behaviour); show a "workflow
     running — reconnect to see live progress" placeholder from the persisted
     `task_started`.
   - Phase 2: rehydrate from the on-disk manifest (`WatchWorkflow`) on reconnect
     — this is the real reason out-of-band monitoring is worth building.

3. **fullAuto / permissions.** Workflow subagents are auto-approved by the CLI
   runtime; they do **not** hit agentique's permission pump individually (they're
   progress, not `PendingChangeEvent`s). No interceptor work expected — but
   verify the `Workflow` tool itself isn't blocked by any exact-name pre-dispatch
   hook (cf. the browser-MCP ECONNREFUSED fix).

4. **Token/`totalCost`.** Workflows burn large token counts. Per domain rules:
   show tokens/agent-counts/phase progress freely; **never** surface cost.

## 5. Frontend UX — the "intuitive" part

Today a subagent renders as a one-line `SubagentActivity` card, grouped under its
`Workflow` tool block by `toolUseId` (`segments.ts` →
`SegmentRenderer.tsx`). That's fine for one subagent; a 38-agent / 5-phase
workflow needs a purpose-built view. The `workflow_progress[]` data maps onto it
directly.

**`WorkflowActivity` component** (rendered in place of `SubagentActivity` when
`taskType === "local_workflow"`), attached to the `Workflow` tool block:

```
▸ Workflow  deep-research            ● running · 24/38 agents · 410k tok · 6m48s
  ├─ ✓ Plan          2 agents
  ├─ ✓ Search        8 agents
  ├─ ◐ Deep-read     12/14 · ▓▓▓▓▓▓▓░░
  │    • v1:source-3   ◐ progress · Read · 10.5k tok
  │    • v1:source-4   ◐ progress · StructuredOutput
  │    • v1:source-5   ⏸ queued
  └─ ⏸ Synthesize     queued
```

- **Header:** name · status dot · live aggregate (done/total agents, total
  tokens, elapsed) · per-phase progress bar. Collapsed-to-header once complete,
  like a settled tool block; click to expand.
- **Phase rows** (from `workflow_phase` entries) with a state glyph and an
  agent count / progress bar.
- **Agent rows** (from `workflow_agent`, grouped by `phaseIndex`): state dot
  (queued ⏸ / running ◐ / done ✓ / error ✗, colour-coded), `label`, `lastTool` +
  `lastToolSummary`, `tokens`, `duration`. Expand an agent for
  `promptPreview` / `resultPreview`.
- **Live** off `task_progress` (`findLast` wins); **settle** on
  `task_notification`. Reuse the `ToolIcon` set for `lastToolName`.
- This mirrors the CLI's own `/workflows` tree, so it's instantly legible to
  anyone who's used the feature.

Composer/launch affordances (phase 2, optional): quick-launch chips for bundled
workflows (`/deep-research`), or an "ultracode" toggle on the composer that
prepends the keyword. Triggering already works via plain prompt text — confirm
agentique passes `/deep-research …` through untouched.

## 6. Phasing (MVP-first)

- **Phase 0 — unblock (agentkit, parallel):** bump claudecli v0.1.0; thread
  workflow fields through `SubagentEvent`; turn-lifecycle fix. *Nothing visible
  in agentique works without this.*
- **Phase 1 — MVP observability (agentique):** mirror the new fields onto
  `WireTaskEvent` (+ `typegen`); `WorkflowActivity` phase/agent tree; backend
  fallback turn-guard; verify the real interactive lifecycle end-to-end with a
  live `/deep-research` run in an isolated session. Persist nothing new.
- **Phase 2 — robustness & reach:** out-of-band monitor (`WorkflowLaunchEvent` +
  manifest poll) for reconnect rehydration and fire-and-forget; optionally
  persist a `workflow_runs` row (runId, name, status, agentCount, tokens,
  result) for a per-session/project history; Teams-tab leaf node; launch chips.
- **Phase 3 — polish:** thinking-tokens ticker, model display names, saved-
  workflow library UI.

## 7. Open questions for review

1. Scope for now — Phase 1 MVP only, or also Phase 2 out-of-band monitoring?
2. Do we want a persisted `workflow_runs` history, or is in-chat ephemeral
   rendering enough for v1?
3. Confirm the agentkit neutral contract in §3 with the parallel agentkit work
   before either side commits.
