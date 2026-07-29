# Scheduled loops — recurring prompts with first-class observability

Status: **design proposal** (nothing implemented). Grounding research 2026-07-29,
including a live probe of the Claude Code CLI scheduler over stream-json.

## Goal

Let a user (or an agent, gated) put a session on a long-running schedule —
"babysit this PR", "check the deploy hourly", "nightly maintenance pass" — the
way Claude Code's `/loop` / cron tools and Routines do, but **agentique-owned**:
durable across restarts and evictions, and with real human insight into what is
happening and how it is going (run history, outcome summaries, health, links to
the exact turns).

## Why not the CLI's built-in scheduler

Claude Code ships session-scoped cron (`CronCreate`/`CronList`/`CronDelete`,
`/loop`, `ScheduleWakeup`; docs: code.claude.com/docs/en/scheduled-tasks).
Verified live 2026-07-29 against the real CLI in the stream-json mode agentique
uses (`scratchpad/cron_probe.jsonl`, ~4-minute run):

- The cron tools **exist and fire** in headless stream-json mode. A `* * * * *`
  task produced one agent-initiated turn per minute on the same wire.
- Each fire arrives as `system.init → assistant → result` — **no `user` event
  carries the scheduled prompt**. Agentique's pipeline never ran
  `persistQueryStart` for that turn, so there is no prompt row, no
  `session.turn-started` push, and undefined timeline rendering.
- The `CronCreate` tool result states verbatim: *"Session-only (not written to
  disk, dies when Claude exits). Auto-expires after 7 days."*

That last point is disqualifying. Agentique deliberately stops CLI processes:
idle eviction (`internal/session/idle_evict.go`), Stop, server restarts. Any of
these silently kills an in-CLI cron task, and lazy-resume (`ensureLive`) only
triggers on the next *message* — nothing ever wakes the loop again. The CLI
scheduler also requires keeping a warm `claude` + Playwright/Chromium subtree
resident for the loop's whole lifetime, which is exactly the steady-state bloat
idle eviction exists to prevent (`docs/process-lifecycle.md`).

So the substrate inverts: **agentique owns the schedule and fires prompts into
sessions through the normal message path.** Then idle eviction becomes a
*feature* of long loops: the session evicts between fires and lazy-resumes on
the next one — a weeks-long hourly loop holds zero warm processes.

## Design overview

Three pieces:

1. **Scheduler core** (new `backend/internal/schedule` package): persisted
   `schedules` + `schedule_runs` tables, a ticker service modeled on
   `brain.Automation` (initial-delay timer + ticker, `automation.go:69`),
   restart-safe via a persisted `next_run_at`.
2. **Fire path**: a due schedule enqueues its prompt via the existing
   `Service.EnqueueMessage` machinery (`service.go:697`) with an origin marker.
   `ensureLive` gives lazy resume of evicted/stopped sessions for free; a busy
   session delays delivery to the next idle boundary (Claude Code's "fires
   between turns" semantics).
3. **Observability**: every fire is a normal, visible turn in the session
   timeline tagged "scheduled"; every run is a row with status, duration,
   outcome summary, and a deep-link to its turn; failures surface through the
   existing attention/badge system; schedules are managed from a `/schedules`
   page and a per-session tab.

## Data model (migration `039_create_schedules.sql`)

```sql
CREATE TABLE schedules (
    id            TEXT PRIMARY KEY,              -- uuid
    project_id    TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    session_id    TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    name          TEXT NOT NULL,
    prompt        TEXT NOT NULL,
    cron          TEXT NOT NULL DEFAULT '',      -- 5-field, local tz; '' when dynamic
    mode          TEXT NOT NULL DEFAULT 'recurring',  -- recurring | once | dynamic
    enabled       INTEGER NOT NULL DEFAULT 1,
    pause_reason  TEXT NOT NULL DEFAULT '',      -- '' | 'user' | 'auto: N consecutive failures' | ...
    next_run_at   TEXT NOT NULL,                 -- UTC RFC3339; source of truth for firing
    expires_at    TEXT NOT NULL DEFAULT '',      -- optional hard stop
    last_run_at   TEXT NOT NULL DEFAULT '',
    consecutive_failures INTEGER NOT NULL DEFAULT 0,
    created_at    TEXT NOT NULL,
    updated_at    TEXT NOT NULL
);
CREATE INDEX idx_schedules_due ON schedules(enabled, next_run_at);

CREATE TABLE schedule_runs (
    id            TEXT PRIMARY KEY,
    schedule_id   TEXT NOT NULL REFERENCES schedules(id) ON DELETE CASCADE,
    session_id    TEXT NOT NULL,
    scheduled_for TEXT NOT NULL,                 -- the slot this run satisfies
    fired_at      TEXT NOT NULL DEFAULT '',      -- when the prompt was delivered
    finished_at   TEXT NOT NULL DEFAULT '',
    status        TEXT NOT NULL,                 -- queued | running | ok | action_needed | error | skipped
    turn_event_id TEXT NOT NULL DEFAULT '',      -- wire id of the fired turn's prompt event (deep-link anchor)
    summary       TEXT NOT NULL DEFAULT '',      -- one-line outcome
    error         TEXT NOT NULL DEFAULT '',
    duration_ms   INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX idx_schedule_runs_by_schedule ON schedule_runs(schedule_id, scheduled_for DESC);
```

Notes:

- `session_id` is required in v1 — a schedule targets one persistent session,
  matching `/loop` semantics (the loop accumulates context; compaction handles
  growth on claude). A Routines-style "fresh session per run" mode is a listed
  future extension, not v1.
- **Retention:** prune to the newest ~200 runs per schedule on insert. No
  silent gaps — a skipped or missed fire is recorded as a run row, not omitted.
- **No 7-day expiry by default.** Claude Code's expiry bounds *invisible*
  forgotten loops; agentique schedules are visible in the UI and auto-pause on
  failure, so the safety argument doesn't apply. `expires_at` remains available.
- **Cron parsing: in-house, no dependency** (decided 2026-07-30). We need
  exactly parse + `Next()`; robfig/cron is 90% scheduler machinery we replace,
  is dormant (fine — frozen spec — but we'd own a fork on the first quirk
  anyway), and doesn't solve DST either (long-open issues). Implement the
  **same restricted grammar Claude Code supports**: wildcards, values,
  `*/step`, ranges, lists; vixie DOM-or-DOW OR-rule; *no* `L`/`W`/`?`/name
  aliases — keeps agent-authored expressions portable, bounds the parser to
  ~200 pure, table-testable lines (`internal/schedule/cronspec.go`).
  `Next()` does a calendar field-search via `time.Date` in server-local tz
  (stored UTC): spring-forward's nonexistent times normalize forward,
  fall-back's repeated hour fires once — both acceptable for a loop scheduler
  and locked in by tests. The design is DST-tolerant by construction anyway
  (persisted `next_run_at`, 20s tick, fire-once catch-up). Test vectors may be
  generated once against robfig/cron as an oracle in a throwaway script,
  never in `go.mod`.

## Scheduler service

New package `backend/internal/schedule`, narrow querier interface following
`internal/session/queries.go`. Started from `serve.go`/`server.New` wiring like
`brain.Automation` — constructor side-effect-free, sweep started explicitly.

**Tick loop.** Initial 30s delay timer, then a ~20s ticker (config). Each pass:
`SELECT … WHERE enabled=1 AND next_run_at <= now` (indexed), fire each due
schedule, advance `next_run_at`.

**Catch-up policy** (matches Claude Code): if `next_run_at` is far in the past
(server was down), fire **once** for the oldest missed slot, then advance to
the next *future* occurrence. Never storm. The skipped slots get one aggregate
`skipped` run row ("server offline, 6 slots missed") so the history shows the
gap honestly.

**Fire pipeline** for one due schedule:

1. Insert `schedule_runs` row (`status=queued`, `scheduled_for=next_run_at`),
   advance `next_run_at`, push `schedule.run`.
2. Deliver: call the enqueue path with an origin marker (see wire section).
   - Session idle/stopped/evicted → `ensureLive` resumes (worktree recovery,
     preamble rebuild, MCP port — all existing), `Query` starts the turn.
   - Session mid-turn (human is using it, or previous run still going) → leave
     the run `queued`; delivery retries on subsequent ticks and on the
     session's `→StateIdle` transition. The UI shows "waiting for session to
     go idle". If the *previous run of the same schedule* is still unfinished
     when the next slot comes due, the new slot is recorded `skipped`
     ("previous run still running") — never two live runs per schedule.
   - Enqueue error (e.g. `resumeSession` losing the git-op `TryLock`,
     `service.go:1320`) → bounded retry with backoff (30s, 2m, 10m), then
     `status=error`.
3. On delivery success: `status=running`, `fired_at` stamped, `turn_event_id`
   recorded from the persisted prompt event.
4. Completion: run resolves to `ok` / `action_needed` / `error` with
   `summary`, `duration_ms` (see outcome capture).

**Turn-completion capture.** `Session.SetTurnCompleteHook` (`session.go:700`)
is a single `atomic.Pointer` today and the discussion orchestrator already uses
it — the scheduler must not fight over it. Structural fix (per repo priorities):
generalize it into a small multi-subscriber registry (slice under `s.mu`, or
per-turn-id map) so discussion, scheduler, and future consumers each get
`{finalText, isError, duration}` for turns they initiated. This is a
self-contained precursor refactor with its own tests.

**Outcome status + summary** ("how is it going" in one line):

- v1 fallback (always on): summary = trimmed tail of the turn's final assistant
  text; status = `error` if the result errored / session hit `StateFailed`,
  else `ok`.
- Preferred channel (M2): an auto-allowed MCP tool **`ScheduleReport`**
  (pattern: `MemoryUsed` in `mcphttp`) the fired prompt is footer-instructed to
  call: `ScheduleReport(status: ok|action-needed|failed, summary: <one line>)`.
  `action-needed` drives attention without failing the run. Tool report wins
  over the text fallback when present.

**Failure handling.**

- Taxonomy: delivery-failed (resume/enqueue error), turn-errored, timeout (no
  completion within `max-run-duration`, default 30m — run marked `error`, the
  session's turn is left alone).
- `consecutive_failures` increments on `error`, resets on `ok`/`action_needed`.
  At `max-consecutive-failures` (default 3): **auto-pause** (`enabled=0`,
  `pause_reason='auto: 3 consecutive failures'`) + attention surfacing. A
  broken loop degrades loudly to *stopped*, never to a silent retry storm.

**Restart safety.** `next_run_at` is persisted; the boot pass fires anything
due (with catch-up). Runs left `running`/`queued` at boot are marked `error`
("server restarted mid-run") by a startup sweep in `serve.go` (production
block, next to `SweepOrphans` — never in `server.New`).

**Session lifecycle coupling** (decided 2026-07-30): when the target session is
marked completed or its worktree merged, its schedules **auto-pause**
(`pause_reason='session completed'`) — a PR-babysitting loop goes quiet when
the PR merges, visibly and reversibly (one click to resume). Hook point: the
existing completion path (`SetSessionCompleted` / `mgr.OnSessionComplete`, the
same seam the brain learn hook uses) plus the merge flow. Session deletion
needs no scheduler code — the `ON DELETE CASCADE` FK removes schedules + runs.

**Interplay with idle eviction.** None needed — that's the point. `beginIdleEvict`
and `validateAndPrepareQuery` are already mutually exclusive; a fire on an
evicted session goes through resume. After a run completes the session idles
normally and gets evicted before the next fire. `lastActiveAt` being in-memory
is irrelevant here because firing is driven by persisted `next_run_at`.

**No jitter.** Claude Code jitters to protect the API from synchronized
wall-clock fires across thousands of sessions. Agentique is one host with few
schedules; a deterministic offset would only make timing confusing. Skipped
deliberately (fires within one tick are already serialized by the loop).

## Creating schedules — both paths in M1 (decided 2026-07-30)

- **UI form**: create/edit dialog reachable from the `/schedules` page and the
  session action menu — name, prompt, cadence (interval presets + raw cron),
  target session prefilled in session context.
- **Chat-first**: the user types "check this PR every 30m" and the agent calls
  a **`ScheduleCreate` MCP tool** (name, prompt, cron, target = own session).
  Approval is **server-side in the tool handler**, not the CLI permission pump
  — agentique MCP tools are auto-allowed, and fullAuto short-circuits
  `handleToolPermission` anyway (the `@spawn` lesson), so the handler itself
  must park the request and surface a UI approval banner
  (`SpawnWorkerApprovalBanner` pattern, `authorizeSpawn`-style flow). The tool
  blocks until approve/deny/timeout; approved schedules emit `schedule.updated`
  like any other. Agents may only target their own session in v1.

## Dynamic pacing (agent-chosen interval) — M2

Claude Code's self-paced `/loop` (agent picks the next delay + prints a reason)
maps cleanly: `mode='dynamic'` schedules expose an auto-allowed MCP tool
**`ScheduleNext(delaySeconds, reason)`** (+ `stop: true` to end the loop),
mirroring `ScheduleWakeup`. The scheduler clamps the delay to config bounds
(default 1m–6h), writes `next_run_at`, and stores the reason on the run row —
the UI then shows *"next in 25m — waiting for CI run to finish"*, which is
exactly the insight the built-in tool prints into a terminal but agentique can
render persistently. If a dynamic run ends without calling `ScheduleNext`, one
fallback fire is scheduled at `dynamic-fallback` (default 20m), and the loop
auto-pauses if that run doesn't reschedule either (Claude Code's semantics).

## Wire, API, and timeline tagging

**RPCs** (ws `handlerRegistry` + `handlers_schedule.go` / `messages_schedule.go`):
`schedule.create`, `schedule.list`, `schedule.update`, `schedule.delete`,
`schedule.pause`, `schedule.resume`, `schedule.run-now`, `schedule.runs`
(paged history). Pushes on the project topic: `schedule.updated` (full
`ScheduleInfo`), `schedule.run` (run transitions). All registered in
`cmd/typegen/main.go` (`addPushEvent`) → `just typegen`.

**Tagging fired turns.** Extend the enqueue path with an origin
(`QueryOrigin{Kind: "schedule", ScheduleID, RunID}`), persisted into the
turn's prompt event and carried on `WireUserMessageEvent` as
`origin?: "schedule"`, `scheduleId?`, `runId?`. Frontend renders the user
bubble with a small "⏰ <schedule name>" badge (precedent: the `queued`
dashed-bubble branch and `extractBrainBlock`/`BrainCard` peeling in
`UserMessage.tsx`). `writeLegacyAgentMessageEvents` is untouched — this is a
normal session turn, not a channel message.

**Deep-link to a run's turn** — the one missing primitive. `Turn.id` is
client-generated today (`chat-types.ts`, `history.ts:32`); nothing can link to
a turn. Fix: the run row stores the persisted prompt event's wire id
(`turn_event_id`); `TurnBlock` gains `data-turn-id={events[0].id}`;
`MessageList` gains a `scrollToTurn(eventId)` (its `LazyTurn` virtualization
must force-mount the target). "View run" navigates to the session with
`?turn=<id>`. This primitive is independently useful (channel links, search).

## Frontend

- **`/schedules` page** (`routes/schedules.tsx` → `components/schedules/`):
  `PageHeader` + `StatCard` layout copied from `teams.tsx`. Rows: name, target
  session pill, human cadence ("every hour"), next-fire countdown, last-run
  status dot, tiny last-10-runs strip (green/amber/red), pause/resume/run-now/
  delete (destructive actions `AlertDialog`-guarded, `BrainSnapshots` pattern).
  Sidebar icon link in `AppSidebar` `SidebarHeader` next to Brain/Templates.
- **Per-session "Loops" tab**: 5th entry in the `SessionTabBar` enum — the
  session-scoped view of the same data plus run history. Expandable run rows
  modeled on `team/InteractionRow.tsx` (status badge, relative time, duration,
  summary; expanded: error detail + "view turn" deep-link). Status icons reuse
  `WorkflowActivity`'s `AgentStateIcon` mapping.
- **Health**: `BrainHealth`-style popover per schedule (consecutive failures,
  last error, fires in last 24h). Auto-pause fires a sonner toast (stable-id
  `loading→error` pattern from `useGlobalSubscriptions`' browser.provisioning).
- **Attention**: new `AttentionKind: "schedule_failed"` in
  `useActivityStreamItems` + a `schedule_failed` entry in `SessionBadge`
  `CONFIG` — a failing/auto-paused loop lands in the sidebar inbox like a
  pending approval. Each fire also emits a `project.activity-item` (existing
  generic feed) so the folder sidebar shows loop activity live.
- **Store**: `stores/schedule-store.ts` (team-store pattern: `Record<id,…>` +
  WS-push upserts), `hooks/useScheduleSubscriptions.ts` wired into
  `useGlobalSubscriptions` **including its `onConnect` refetch branch**.
- Entry point: composer "+" menu gains "Run on a schedule…" (v2 polish).

## Config

```toml
[scheduler]
enabled = true              # AGENTIQUE_SCHEDULER_ENABLED
tick-interval = "20s"       # AGENTIQUE_SCHEDULER_TICK_INTERVAL
min-interval = "1m"         # floor for cron cadence and dynamic delays
max-run-duration = "30m"    # run timeout → status=error
max-consecutive-failures = 3
run-history = 200           # retained runs per schedule
dynamic-max-delay = "6h"    # clamp for ScheduleNext
dynamic-fallback = "20m"    # fire once if a dynamic run forgets to reschedule
```

Env wins over file, resolved explicitly in `serve.go` (`firstNonEmpty` /
`envIntOr` pattern, hard exit on unparseable durations — same as idle-evict).

## Hazards and non-goals

- **In-session `CronCreate` today**: if a user asks their agentique session to
  "/loop", the CLI will happily schedule it — it fires as unlabeled turns while
  the process lives and silently dies on evict/stop/restart. Near-term: note in
  the session preamble that scheduling must go through agentique. Future:
  intercept the `CronCreate` tool_use event and offer promotion to a real
  schedule ("Claude scheduled a task in-session — make it durable?").
- **Agent-created schedules for *other* sessions** (a lead scheduling loops on
  workers) needs `@spawn`-grade authorization design — deferred; v1
  `ScheduleCreate` is self-targeting only, human-approved per call.
- **Channel-targeted schedules** (fire into a channel, personas discuss) and
  **fresh-session-per-run** (Routines-style) are future modes; the schema keeps
  them open (target columns), the code doesn't speculate.
- **Codex sessions** work as targets (`EnqueueMessage` is provider-neutral;
  mid-turn delivery uses the pending-queue path). `ScheduleReport`/
  `ScheduleNext` are MCP tools, so both providers can call them.

## Phasing

- **M1 — durable loop, visible runs**: migration 039 + sqlc; `internal/schedule`
  service (tick, fire, catch-up, retries, auto-pause on failures **and on
  session completion/merge**); origin tagging on `WireUserMessageEvent`;
  passive outcome capture (final-text summary + error classification) via the
  turn-complete registry refactor; RPCs + pushes + typegen; **both creation
  paths** (UI form + `ScheduleCreate` MCP with server-side approval banner);
  `/schedules` page **and** per-session "Loops" tab with run history;
  pause/resume/run-now; `schedule_failed` attention kind; startup sweep.
- **M2 — smart loops + insight polish**: `ScheduleReport` MCP tool
  (`action_needed` state) **and `ScheduleNext`/stop dynamic pacing** (shared
  MCP wiring; UI: "next in 25m — <reason>"); turn deep-links (`data-turn-id` +
  `scrollToTurn` + `?turn=`); activity-feed items; health popover; run-strip
  visualization; toasts.
- **M3 — entry points + future modes**: composer "Run on a schedule…",
  `CronCreate` interception/promotion, groundwork for channel-targeted and
  fresh-session-per-run modes.

## Verification plan

Unit: cron parse + next-fire + catch-up math (table-driven; Europe/Stockholm
spring-forward and fall-back vectors, vixie DOM-or-DOW cases), auto-pause /
reset counters, retention pruning. `-race`: scheduler vs idle-evict vs human
`Query` on one session (the `beginIdleEvict` mutual-exclusion test is the
template). Live end-to-end (per house rule, multiple runs): a 1-minute schedule
on a real session through evict → lazy-resume → fire; kill -9 the server
mid-loop and verify catch-up fires once + interrupted runs marked; verify the
timeline badge, run history, deep-link, and auto-pause after 3 forced failures.

## Open questions for review

1. Should `action_needed` also flip the session's own badge (like a pending
   question) or only the schedule's health? Proposed: session badge too.
2. Default `max-run-duration` 30m — long enough for heavyweight nightly runs?

Resolved 2026-07-30 (review round): in-house cron parser (no dep); both
creation paths in M1 (UI form + approved `ScheduleCreate`); auto-pause on
session completion/merge; dynamic pacing promoted to M2; both management
surfaces (global page + session tab) in M1.
