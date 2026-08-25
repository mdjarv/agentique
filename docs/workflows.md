# Dynamic workflows

A workflow is the Claude CLI runtime orchestrating many ephemeral subagents
inside one session's turn. agentique does not orchestrate it, does not launch it,
and does not persist it. It renders it.

Provider support is advertised through `Capabilities().Workflows`: claude true,
codex false.

## Workflows are not swarms

agentique already has a multi-agent story. These two compose; they do not merge.

| | Channels and swarms | Dynamic workflows |
|---|---|---|
| Orchestrated by | agentique's `session.Manager` | the CLI runtime, inside one session |
| Unit | a full peer session with its own worktree, CLI process and chat | an ephemeral subagent in the parent CLI process |
| Lifetime | independent, survives the lead | dies with the parent turn |
| Control | SendMessage, hierarchy, dissolve | a JS script, fire and forget |
| Coordination | the `messages` table, intros, `parent_session_id` | an on-disk manifest and journal |

Channels are a team of colleagues. A workflow is one colleague running a
structured batch job. A workflow happens *within* a single session's turn and must
not touch the session or channel pipeline.

## How it arrives

Triggering is pure prompt content: `ultracode`, "run a workflow…", or a bundled
`/<name>` like `/deep-research`. No flag, no API.
`--dangerously-skip-permissions` makes it auto-run, which is exactly what
agentique's fullAuto sessions already pass, so workflows have always fired in
fullAuto sessions.

It surfaces through the existing `task_*` stream as one synthetic task with
`TaskType == "local_workflow"`, so it already parses into a neutral
`SubagentEvent`. Nothing becomes an unknown event.

`task_progress` carries `workflow_progress[]`: phase entries with an index and
title, and agent entries with a label, phase title, state (queued, start,
progress, done, error), model, tokens, tool calls, last tool name and summary,
prompt and result previews, duration and timestamps. That is the data the UI
renders.

## The two-result lifecycle

A workflow turn produces **two** results. The first says "running in the
background". The second, after completion, carries the real answer.

This is the correctness risk, because agentique reacts to a turn going idle with
real side effects: auto-merge and refresh in git ops, brain learn-on-completion,
the attention pulse reset, and the composer unlocking so the user can send a
message that interleaves mid-workflow. If the placeholder result completed the
turn, all of that would fire while the workflow was still running.

Suppression happens on both sides:

- A `WorkflowPending` turn-completed event is short-circuited in
  `handleTerminalEvents`: no pulse reset, no turn-complete hook.
- The `WorkflowPending` result is marked transient, so it gets no database row and
  no activity-feed item. Broadcast only.
- The frontend drops a `workflowPending` result outright: it never ends the turn
  and never renders as the assistant message.

Take the **last** result, always.

Workflow subagents are auto-approved by the CLI runtime and do not individually
reach agentique's permission pump; they arrive as progress, not pending changes.

## The panel

A subagent renders as a one-line card grouped under its tool block. That is fine
for one subagent. A five-phase, thirty-eight-agent workflow needs its own view,
which `WorkflowActivity` provides when `taskType === "local_workflow"`.

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

Phase rows come from the phase entries with a state glyph and an agent count.
Agent rows come from the agent entries grouped by phase, with a state dot, label,
last tool and summary, tokens and duration; expanding one shows the prompt and
result previews. The tree updates off `task_progress`, last one wins, and settles
on `task_notification`.

It mirrors the CLI's own `/workflows` tree, so anyone who has used the feature can
read it immediately.

The view lives in a generalized collapsible right panel shared with the browser
(`rightPanelView`), with a header toggle and auto-open on a live run. Before that
generalization the panel was buried inside the collapsed inline activity group and
looked missing.

Tokens, agent counts and phase progress are shown freely. Cost never is.

## Known limitation

`task_progress` is transient and not persisted. Only `task_started` and
`task_notification` reach the database. So on reconnect or reload mid-workflow the
live tree is lost: the panel shows the header and aggregates from the persisted
events, then an empty tree until the next live workflow. Same tradeoff as ordinary
subagents.

Fixing it means rehydrating from the on-disk manifest, which is the real reason
out-of-band monitoring would be worth building.

## Open

- **`WorkflowLaunchedEvent` carries no task or tool-use id**, so it cannot be
  correlated client-side to the `local_workflow` task stream that follows. The
  upstream `claudecli.WorkflowLaunch` does have a task id, and the
  `async_launched` tool result has a tool_use_id. Until agentkit threads one
  through, the panel keys on the task events' `ToolUseID`, which is robust, and the
  launch event is on the wire but not rendered.
- **No persisted run history.** A `workflow_runs` row (run id, name, status, agent
  count, tokens, result) would give a per-session or per-project history. In-chat
  ephemeral rendering was judged enough to start.
- **Launch affordances.** Quick-launch chips for bundled workflows, or an
  `ultracode` toggle on the composer. Triggering already works through plain prompt
  text.

## A lesson worth keeping

An earlier version of this document claimed, with confidence, that interactive
mode does not stream the workflow lifecycle and that only headless `-p` does,
and pivoted the whole design to out-of-band manifest monitoring.

That was wrong. It came from a **single** interactive probe run whose model
happened to end the turn after the placeholder and not continue. One anomalous run
became a "verified" design-invalidating claim, which sent a parallel agentkit
session to reopen a settled decision.

More thorough testing (several interactive runs including a two-minute three-phase
workflow, plus an end-to-end run through the real agentkit manager) showed
interactive is **symmetric** with `-p`. Progress, the terminal notification and
the second real-answer result all arrive in-band, even when the workflow finishes
after the session has settled to idle. The in-band stream is authoritative.

The real bug was upstream. The CLI stamps `task_type` only on `task_started`;
progress, update and notification events carry it as null. agentkit gated its
workflow-in-flight *clear* on `IsWorkflow()`, so the terminal notification with an
empty task type was skipped, the in-flight flag never cleared, and the real second
turn-completed event got marked pending and suppressed. Hence the hang. Every
event was on the wire; agentkit dropped them. The "silent stream" reading was
inferred from that symptom.

Both halves were fixed upstream: agentkit correlates by the run's task id and
always honours a terminal status, and claudecli-go backfills `task_type` and
workflow name across a task's lifecycle.

**One run is not verification**, especially for a claim that contradicts an
existing design decision. Re-run it, and go through the real integration path,
before asserting.
</content>
