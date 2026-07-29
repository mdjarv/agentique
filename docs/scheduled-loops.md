# Scheduled loops — recurring prompts with first-class observability

Status: **M1 implemented** (backend `internal/schedule` + session registry
refactor + full frontend; see the CLAUDE.md "Scheduled Loops" section for the
operational invariants). Grounding research 2026-07-29 (live probe of the
Claude Code CLI scheduler over stream-json); adversarial design review
2026-07-30 (four reviewers, dispositions in the review log at the bottom);
implementation review workflow ran over the M1 diff before merge. M2
(ScheduleReport/ScheduleNext, deep-links, standing consent, codex rotation)
and M3 are not yet started.

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
sessions as fresh turns.** Then idle eviction becomes a *feature* of long
loops: the session evicts between fires and lazy-resumes on the next one — a
weeks-long hourly loop holds zero warm processes.

## Design overview

Four pieces:

1. **Scheduler core** (new `backend/internal/schedule` package): persisted
   `schedules` + `schedule_runs` tables, a ticker service modeled on
   `brain.Automation` (initial-delay timer + ticker), restart-safe via a
   persisted `next_run_at`.
2. **Delivery**: a due schedule starts a **fresh turn** on the target session
   through an idle-gated, single-flight claim (never `EnqueueMessage`, never
   mid-turn — see "Delivery" below). `ensureLive` gives lazy resume of
   evicted/stopped sessions for free; a busy session keeps the run `queued`
   until the next idle boundary (Claude Code's "fires between turns"
   semantics, on both providers).
3. **Turn identity + completion registry** (keystone precursor refactor): turn
   starts return `(turnIndex, prompt row id)`, and a multi-subscriber
   completion registry keyed by turn index delivers `{finalText, status,
   errorKind, duration}` to whoever initiated the turn. The scheduler, the
   discussion orchestrator, and the deep-link primitive all sit on this.
4. **Observability**: every fire is a visible, origin-tagged turn; every run is
   a row with a one-way status lifecycle, outcome summary, and a deep-link to
   its turn; failures and needs-you states surface through attention semantics
   with **defined set/clear rules**; scheduled fires are excluded from the
   ambient unread/sort signals so a loop cannot drown real work.

## Data model (migration `040_create_schedules.sql` — 039 is taken)

```sql
CREATE TABLE schedules (
    id            TEXT PRIMARY KEY,              -- uuid
    project_id    TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    session_id    TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    name          TEXT NOT NULL,
    prompt        TEXT NOT NULL,
    cron          TEXT NOT NULL DEFAULT '',      -- 5-field, local tz; '' for once/dynamic
    mode          TEXT NOT NULL DEFAULT 'recurring',  -- recurring | once | dynamic
    enabled       INTEGER NOT NULL DEFAULT 1,
    pause_reason  TEXT NOT NULL DEFAULT '',      -- '' | user | completed | expired |
                                                 --   session-completed | auto-failures | dynamic-ended | pending-approval
    attention     TEXT NOT NULL DEFAULT '',      -- '' | action_needed | failed  (see UX section)
    attention_run_id TEXT NOT NULL DEFAULT '',
    next_run_at   TEXT NOT NULL DEFAULT '',      -- '' = parked (no next fire); else UTC time
    expires_at    TEXT NOT NULL DEFAULT '',
    last_run_at   TEXT NOT NULL DEFAULT '',
    last_viewed_at TEXT NOT NULL DEFAULT '',     -- "since you last looked" divider
    consecutive_failures INTEGER NOT NULL DEFAULT 0,
    created_by    TEXT NOT NULL DEFAULT 'user',  -- user | agent (audit for the approval story)
    created_at    TEXT NOT NULL,
    updated_at    TEXT NOT NULL
);
CREATE INDEX idx_schedules_due ON schedules(enabled, next_run_at);

CREATE TABLE schedule_runs (
    id            TEXT PRIMARY KEY,
    schedule_id   TEXT NOT NULL REFERENCES schedules(id) ON DELETE CASCADE,
    session_id    TEXT NOT NULL,
    scheduled_for TEXT NOT NULL,                 -- slot key (see below)
    created_at    TEXT NOT NULL,                 -- retention prunes by this, not slot time
    fired_at      TEXT NOT NULL DEFAULT '',
    finished_at   TEXT NOT NULL DEFAULT '',
    status        TEXT NOT NULL,                 -- queued | firing | running | ok |
                                                 --   action_needed | error | deferred |
                                                 --   interrupted | skipped
    overdue       INTEGER NOT NULL DEFAULT 0,    -- exceeded max-run-duration (flag, not status)
    attempts      INTEGER NOT NULL DEFAULT 0,    -- delivery attempts (retry/backoff state)
    next_attempt_at TEXT NOT NULL DEFAULT '',
    turn_index    INTEGER NOT NULL DEFAULT -1,   -- deep-link anchor (session_id + turn_index)
    summary       TEXT NOT NULL DEFAULT '',
    reason        TEXT NOT NULL DEFAULT '',      -- dynamic-pacing reason / skip reason
    error         TEXT NOT NULL DEFAULT '',
    error_kind    TEXT NOT NULL DEFAULT '',      -- '' | rate_limit | overloaded | context | other
    late_report   TEXT NOT NULL DEFAULT '',      -- annotation: report/completion after terminal
    duration_ms   INTEGER NOT NULL DEFAULT 0
);
CREATE UNIQUE INDEX idx_schedule_runs_slot ON schedule_runs(schedule_id, scheduled_for);
CREATE INDEX idx_schedule_runs_recent ON schedule_runs(schedule_id, created_at DESC);
```

Notes:

- **Slot keys.** For cron slots, `scheduled_for` = the slot time — the unique
  index makes crash-replay idempotent (insert `ON CONFLICT DO NOTHING`; a
  conflict means the slot is already claimed). For `run-now` and dynamic
  fires, `scheduled_for` = the claim timestamp; a same-second collision reads
  as "already claimed" and is correct to drop.
- **Parked schedules.** `next_run_at = ''` means "no next fire" and is
  excluded by the due query. Used by: `once` after firing, dynamic runs while
  in flight would instead pre-write the fallback (see dynamic section), paused
  and expired schedules.
- **Timestamp format is pinned**: every schedules/runs column and every query
  parameter uses `time.Now().UTC().Format(time.RFC3339)` (seconds precision,
  `Z` suffix). The codebase mixes formats today (millis-no-Z in
  `session_events`, seconds-Z in brain_jobs) and SQLite compares TEXT
  lexicographically — mixed precision breaks `next_run_at <= ?` ordering.
- **Retention:** prune to the newest ~200 runs per schedule by `created_at`
  (not `scheduled_for` — a catch-up row for an old slot would be born oldest
  and pruned first, deleting exactly the honest-gap record).
- **No 7-day expiry by default.** Claude Code's expiry bounds *invisible*
  forgotten loops; agentique schedules are visible and auto-pause on failure.
  `expires_at` is enforced in the due query (`AND (expires_at = '' OR
  expires_at > now)`) plus an explicit pause transition
  (`pause_reason='expired'`) so expiry is a visible state, not a silent stop.
- `session_id` is required in v1 — a schedule targets one persistent session.
  Fresh-session-per-run (Routines-style) is a future mode; see the codex
  discussion in Hazards.
- **Cron parsing: in-house, no dependency.** Same restricted grammar Claude
  Code supports: wildcards, values, `*/step`, ranges, lists; vixie DOM-or-DOW
  OR-rule; no `L`/`W`/`?`/name aliases (~200 pure lines,
  `internal/schedule/cronspec.go`). `Next()` does a calendar field-search via
  `time.Date` in server-local tz. DST behavior, stated honestly:
  spring-forward's nonexistent times normalize forward; fall-back's repeated
  hour fires once per *wall-clock* combo, which for an hourly cron means a 2h
  real-time gap once a year (90m for `*/30`) — accepted and documented.
  Because Go's resolution of *ambiguous* wall-clock times is unspecified, the
  advance carries an **unconditional strictly-future guard**: recompute until
  `next_run_at > now` in UTC. Test vectors are **hand-derived from tzdata**
  (Europe/Stockholm boundaries) — not generated from robfig/cron, whose own
  DST behavior is unreliable on exactly these cases.

## Delivery — idle-gated, single-flight, fresh turns only

**Never `EnqueueMessage`.** Its running-session branches are the wrong
semantics for a scheduled fire on both providers: claude gets native mid-turn
injection (`sess.SendMessage` — the prompt lands *inside the human's current
turn*: no prompt row, no turn boundary, outcome attributed to the wrong turn)
and codex gets `QueuePendingMessage`, which coalesces with queued human
messages into one replayed turn. `EnqueueMessage` is the human-composer
primitive; the scheduler gets its own.

**The delivery primitive** is `ensureLive` + a fresh-turn-only `Query` variant:
`validateAndPrepareQuery` already refuses atomically (under `s.mu`) when the
session is running, merging, or being evicted — so a plain `Query` *is* the
correct atomic try-deliver: success means the turn started; refusal means
"still busy — stay `queued`, retry". The variant additionally returns
`(turnIndex, promptEventID)` and accepts the completion callback (see the
registry section). Known TOCTOU to fix at the root while here: `Query`
persists the prompt row and `state=running` *before* the runtime accepts the
turn, and a loser races into `StateFailed` with an orphan prompt row — the
persist must move after runtime acceptance (this hazard predates schedules and
can hit humans today).

**Single-flight.** Delivery has multiple triggers — the tick loop, the
session's `→StateIdle` transition, `schedule.run-now` (an RPC goroutine), and
the boot pass. All of them route through one claim: a per-session delivery
mutex in the scheduler plus a CAS on the run row (`queued → firing`); only the
claim winner calls `Query`, and `run-now` goes through the same path (state
CAS, not a direct fire). Fires within a tick are serialized; fires across
triggers are serialized by the claim.

**Fire pipeline** for one due schedule (steps 1–2 in one `RunInTx`
transaction — crash between them must not double-fire or lose the slot):

1. Insert the run row (`status=queued`, slot key per above, `ON CONFLICT DO
   NOTHING` → already claimed) **and** advance `next_run_at` (strictly-future
   guard) atomically. Push `schedule.run`.
2. Attempt delivery via the claim. Refused-busy → stay `queued` (UI: "waiting
   for session to go idle"), retry on later ticks and on `→StateIdle`.
   Delivery errors (resume raced the git-op `TryLock`, worktree recovery
   failed) → bounded retry with backoff (30s / 2m / 10m — durable in
   `attempts` / `next_attempt_at` so the UI can show "retrying in 2m" and
   restarts don't forget), then `status=error`.
3. On delivery: `status=running`, `fired_at` stamped, `turn_index` recorded.
4. Completion resolves the run via the registry (statuses below).

**Queued-run bounds.** The run-duration clock starts at `fired_at`, not
`queued`. A `queued` run not delivered before its schedule's *next* slot
resolves `skipped` ("not delivered before next slot") — same rule as the
existing per-schedule "previous run still running → skip". Pausing or
disabling a schedule resolves any `queued` run as `skipped('paused')`.

**Catch-up policy.** If `next_run_at` is more than one cron interval in the
past (server was down), fire once for the **most recent** missed slot (the
freshest context — matches Quartz/K8s-CronJob misfire conventions), advance to
the next future occurrence, and record one aggregate `skipped` row for the
older missed slots ("server offline, 6 slots missed") so history shows the gap
honestly. `once` schedules catch up only within a bounded staleness window
(default 1h) — a "remind me at 15:00" firing at 21:00 next day is wrong;
outside the window the run is `skipped` with attention.

**Run status lifecycle (one-way).** `queued → firing → running → terminal`,
where terminal ∈ `ok | action_needed | error | deferred | interrupted |
skipped`. Terminal is written exactly once; anything arriving later (a late
turn completion, a late `ScheduleReport`) lands in `late_report` as an
annotation and never rewrites status, duration, or counters.

- **`ok`** — turn completed clean.
- **`action_needed`** — the run needs a human. Set by `ScheduleReport`, or
  **detected automatically**: if the session's `PendingState()` (approvals,
  questions, plan review — all block indefinitely, fullAuto does not bypass
  questions) is non-nil while a scheduled turn is in flight, the run resolves
  `action_needed` ("waiting on: <tool/question>") and the duration clock
  freezes. Never counts toward auto-pause — nothing failed.
- **`error`** — the turn genuinely failed. Counts toward auto-pause.
- **`deferred`** — transient provider condition: rate-limit window,
  overloaded. The run is rescheduled, not failed: `next_run_at =
  max(ResetsAt, RetryAfter, min-interval)`; never counts toward auto-pause. A
  subscription's 5h usage window plus an hourly cron must not kill the loop.
- **`interrupted`** — a human hit stop on the scheduled turn
  (`TurnStatusInterrupted`, both adapters emit it). Deliberate; never counts
  toward auto-pause.
- **`skipped`** — slot not run (busy predecessor, offline gap, paused, stale
  once). `fired_at`/`duration_ms` stay zero; the row renders reason + slot
  time only.

**Timeout is a flag, not a status.** At `max-run-duration` (default 30m) the
run is marked `overdue=1` and raises attention — the turn is left alone and
still resolves the run when it completes. This kills two misclassifications:
a healthy heavyweight nightly run no longer becomes a fake `error`, and three
slow nights no longer auto-pause a working loop. A run that never completes
stays visibly overdue (attention), which is the honest state.

**Auto-pause** counts only `error` terminals: `consecutive_failures++` on
`error`, reset on `ok`/`action_needed`; at `max-consecutive-failures`
(default 3) → `enabled=0, pause_reason='auto-failures'`, attention `failed`.

**Restart sweep** (in `serve.go`, before the scheduler's first tick — never in
`server.New`): runs in `firing`/`running` at boot were delivered and their CLI
died with the server (`KillMode=control-group`) → `error("server restarted
mid-run")`, but **excluded from `consecutive_failures`** — the documented
OOM→restart crash-loop must not auto-pause every active schedule. Runs in
`queued` were *never delivered* → re-armed, not errored: left `queued` for the
boot pass to deliver (the daily-9am loop that crashed at 9:01 still runs its
9am slot at boot instead of silently skipping the day).

**Session lifecycle coupling** (corrected seams). Auto-pause on completion
hooks the **two user-intent paths only**: the mark-done RPC flow and the merge
finalize path in `git_service.go`, synchronously, with
`pause_reason='session-completed'`. Explicitly **not** `mgr.OnSessionComplete`
/ runtime `StateDone` — that fires on any clean CLI exit (which lazy-resume
handles transparently) and would pause healthy loops. Pausing resolves queued
runs as skipped. The fire path independently re-checks `completed` /
`worktree_merged` at delivery time inside its transaction and converts the run
to `skipped('session completed')` + auto-pause — necessary because `Query`
otherwise *unsets* both flags (`persistQueryStart`), i.e. an in-flight fire
racing the user's merge would silently reopen a session the user considers
done. Session deletion needs no scheduler code — FK cascade
(`PRAGMA foreign_keys=ON` is verified set in `store.Open`).

**Brain recall interaction.** Evict-between-fires resets the per-`Session`
recall seen-set, so every fire would re-inject the same `<brain>` block and
bump `uses` on the same facts 24×/day with no corresponding `MemoryUsed` —
polluting the outcome-signal calibration and paying tokens per fire. Fires
carry `QueryOrigin{Kind:"schedule"}` into `injectRecall`: recall runs with a
**per-schedule persisted seen-set** (delta across evictions) and skips
`BumpUses` for schedule-origin injections. The tool footer (below) is excluded
from the recall query text.

**No jitter.** Claude Code jitters to protect the API from synchronized fires
across thousands of sessions. Agentique is one host with few schedules;
deliberately skipped.

## Turn identity + completion registry (keystone refactor)

`Session.SetTurnCompleteHook` today is a single `atomic.Pointer` (contended by
the discussion orchestrator), fires synchronously on the runtime event-loop
goroutine, and `runtime.TurnCompletedEvent` carries **no turn id** — so
"subscribers get completions for turns they initiated" is unimplementable
without threading identity through. The refactor, a self-contained precursor
with its own tests:

- The pipeline passes its `turnIdx` through `OnTurnComplete`; the registry is
  **keyed by turn index**, and the delivery `Query` variant takes the
  completion callback as an argument so registration is atomic with turn start
  (no register-before/after races with `flushPendingMessages` replays, other
  schedules, or human turns).
- Callbacks dispatch **async** (buffered channel per subscriber) — a
  subscriber doing SQLite writes must not stall the session's event stream.
- `Session.Close` synthesizes a terminal "session closed" delivery to open
  subscriptions (today a manual Stop mid-turn strands the discussion
  orchestrator until its own timeout).
- Registrations do not survive the `Session` object; the scheduler registers
  through the delivery call per fire (it never holds a `Session` pointer
  across evictions).
- The payload is `{finalText, status, errorKind, duration}`:
  - `finalText`: **codex gap fixed here** — `TurnCompletedEvent.Text` is
    populated only by the claude adapter; the pipeline accumulates the turn's
    last `AssistantTextEvent` as the fallback text (also fixes the latent
    empty-reply bug in `dbSessionPersona` for codex personas).
  - `errorKind`: classified from the turn's error/rate-limit events
    (`rate_limit` / `overloaded` / `context` / `other`). The signals exist on
    both providers but are dropped before this layer today (claude:
    `wireErrorEvent`-only mapping; codex: typed `CodexErrorInfo` discarded in
    the adapter) — the clean fix threads kinds through
    `runtime.ErrorEvent.Kind` in agentkit + the two CLI wrappers (small
    upstream hand-offs); until then the pipeline classifies from the events it
    already sees.
- The plan-mode wrinkle: plan approval auto-fires a second "Proceed with
  implementation" turn that the registry will not attribute to the run. v1
  accepts this (`action_needed` fires first anyway, which is the honest
  state); noted for the record.

This refactor also delivers turn identity for deep-links and prompt-row
plumbing (`persistQueryStart` gains a returned event id), independently useful
beyond schedules.

## Creating schedules — both paths in M1

- **UI form**: create/edit dialog from the `/schedules` page and the session
  action menu — name, prompt, cadence (interval presets + raw cron + **"once,
  at…"** datetime), target session prefilled in session context.
- **Chat-first**: the user asks in the composer; the agent calls a
  **`ScheduleCreate` MCP tool** (name, prompt, `cron` *or* `at` for one-shots,
  self-target only in v1). **The tool is non-blocking**: the claude CLI's MCP
  client has a ~60s per-call timeout and agentkit's mcphttp transport is
  POST-only (no progress notifications to extend it), so a handler that parks
  waiting for a human fails on any realistic approval latency — and codex has
  no MCP-tool interceptor surface at all. Instead the handler creates the
  schedule **paused** (`pause_reason='pending-approval'`), returns "created,
  awaiting approval" immediately, and surfaces the approval banner; approval
  enables it (first fire per its cadence), denial deletes it. The agent
  observes the outcome via the schedule state (or is told in the next turn).
- One-shot reminders ("remind me at 15:00 to push the release branch") are the
  `mode='once'` path: fires once, then `enabled=0, pause_reason='completed'`,
  `next_run_at=''`; drops out of the default `/schedules` view into a
  "finished" filter. Bounded catch-up (1h) per above.
- M2: standing consent — an "Always allow this session to schedule itself"
  option on the approval banner, stored as a behavior preset, bounded
  (self-target only, cadence ≥ floor, ≤ N active schedules).

## Dynamic pacing (agent-chosen interval) — M2

Claude Code's self-paced `/loop` maps 1:1: `mode='dynamic'` schedules have no
cron — the agent paces itself via an auto-allowed MCP tool
**`ScheduleNext(runId, delaySeconds, reason)`** (+ `stop: true`), mirroring
`ScheduleWakeup`. Semantics against the CC docs:

- **Start**: creation sets `next_run_at = now` — first fire immediate.
- **Prompt**: the schedule row holds it; the scheduler re-sends each fire.
- **Fallback pre-write**: each dynamic fire writes `next_run_at = fired_at +
  dynamic-fallback` (default 20m) *at fire time*; `ScheduleNext` overwrites
  it. Forgot-to-reschedule needs no detector — the fallback simply fires; if
  that run doesn't reschedule either, the loop parks
  (`pause_reason='dynamic-ended'`, visible) — CC's v2.1.202 semantics minus
  the silence. This also keeps `next_run_at` non-stale mid-turn, so the tick
  loop never spams skip rows while the agent is still deciding.
- **Clamp**: `[min-interval, dynamic-max-delay]` (default 1m–6h; CC caps at
  1h — we allow longer because a parked session costs nothing).
- **Reason**: stored on the run row, rendered persistently (see UX).
- **Attribution**: `runId` is mandatory — the fire footer embeds it, the
  handler validates the run is `running` and the schedule is dynamic, and
  rejects otherwise with an instructive error. This closes the ambiguity of
  N schedules on one session and of stale footers in a persistent
  conversation prompting tool calls during *human* turns. Defense-in-depth:
  a partial unique index allows one dynamic schedule per session.
- CC's jitter and 7-day expiry are deliberately dropped.

## Outcome capture — "how is it going" in one line

- v1 fallback (always on, both providers per the registry work): summary =
  trimmed tail of the turn's final text; status from the registry's
  status/errorKind per the lifecycle above.
- Preferred channel (M2): **`ScheduleReport(runId, status, summary)`**
  (`ok | action-needed | failed`), footer-instructed, auto-allowed, `runId`
  mandatory (same attribution rules as `ScheduleNext`). Tool report wins over
  the text fallback; reports for already-terminal runs land in `late_report`.

## Wire, API, and timeline tagging

**RPCs** (`handlers_schedule.go` / `messages_schedule.go`): `schedule.create`,
`.list`, `.update`, `.delete`, `.pause`, `.resume`, `.run-now`, `.runs`
(paged), `.mark-viewed`. **Pushes use `Broadcast`**, not per-project
`Publish` — the global `/schedules` page is the consumer, and every existing
global-page domain (team.*, persona.interaction, brain.*) broadcasts; leaning
on the frontend's subscribe-all-projects behavior would be an undocumented
invariant, and runs would otherwise need a join just to find their topic.
Pushes: `schedule.updated` (full `ScheduleInfo`), `schedule.run` (run
transitions). Registered in typegen; `useScheduleSubscriptions` wired into
`useGlobalSubscriptions` **including its `onConnect` refetch branch**.

**Origin tagging** rides the **persisted prompt row + turn pushes**, not
`WireUserMessageEvent` (which only fires on the mid-turn paths the scheduler
never uses — as originally specced the badge would have appeared only in the
forbidden case and vanished on every history reload). `QueryOrigin{Kind:
"schedule", ScheduleID, RunID, ScheduleName}` is stored in the prompt event
payload and surfaced on `HistoryTurn` and `PushTurnStarted`. The frontend
renders the user bubble with a lucide `Clock` badge + schedule name (emoji is
off-idiom for this codebase).

**Deep-links key on `(session_id, turn_index)`** — not event ids: prompt rows
are folded into `HistoryTurn.Prompt` (never get a wire id), live events carry
no id at all, and `InsertEvent` doesn't RETURNING. `turn_index` is already
persisted and stable. `HistoryTurn` and `PushTurnStarted` gain `turnIndex`;
`TurnBlock` gets `data-turn-index`; `MessageList` gains `scrollToTurn` (its
`LazyTurn` virtualization force-mounts the target; **scroll-memory restore is
suspended when `?turn=` is present** — the two would otherwise fight; note the
200px placeholder estimates make anchored positioning settle as turns
materialize).

## Frontend — insight without noise

The review's sharpest verdict: run rows are *data*; insight lives in the
badge/inbox, the timeline, and the session's ambient state — and a naive loop
degrades all three. These rules are **M1**, not polish:

**Attention semantics (set/hold/clear defined).** Attention lives on the
**schedule row** (`attention` + `attention_run_id`), not per run — one inbox
item per schedule, deduped while set (an hourly `action_needed` does not
re-flip until cleared). Rules:

- `action_needed`: set by the run lifecycle; **clears on viewing** the target
  session or its Loops tab (`schedule.mark-viewed`, same seam as
  `hasUnseenCompletion`) and **self-heals** when a later run resolves `ok`.
- `failed` (auto-paused / delivery-dead): clears **only on an explicit act** —
  resume, edit, or delete. Viewing doesn't fix a broken loop, so viewing
  doesn't clear it. Rendered with the red `failed` styling, label "Loop
  paused" — *not* the orange pulse, which today means "agent blocked right
  now" and must keep meaning that.
- Session badge integration: worst-of aggregation across the session's
  schedules, ranked **below** approval/question in the priority cascade. New
  `AttentionKind` entries flow through `useActivityStreamItems` (which today
  derives attention only from pendingApproval/pendingQuestion — the new kinds
  need a real `SessionInfo`/store field, not just a badge color).

**Ambient-signal exclusion.** Schedule-origin results are second-class in the
existing signal layer: they do **not** set `hasUnseenCompletion` (unless the
run resolved `action_needed`/`error`), do **not** bump the Active-section sort
key, and per-fire activity items are coalesced ("Deploy check — 12 runs, all
ok"). Without this, every `result` bolds the row and an hourly loop pins
itself to the top of Active, permanently bold, killing the "what responded
overnight" signal for everything else.

**Timeline at real cadences.** Consecutive schedule-origin turns collapse into
one expandable group row ("Deploy check — 12 runs since 09:00, all ok"),
rendered from run data, expanding lazily to real turns; plus a "hide scheduled
runs" filter toggle. Two code facts make this load-bearing: `session.history`
fetches the **entire** session unbounded on open, and `LazyTurn` mounts are a
one-way latch (scrolled-past turns never unmount) — a 5-minute loop is 288
turns/day into both. Server-side history elision of collapsed runs is M2; the
collapse + filter are M1. Creation UX warns below a 15-minute cadence on a
persistent session (`min-interval` floor stays 1m for run-now/dynamic tests).

**The parked state must not read as dead.** An evicted loop session currently
renders "Stopped" + a ResumeBanner — 59 minutes of every hour looking broken.
M1 adds a presentational "parked" state: when a session has an enabled
schedule and is stopped/evicted, the badge shows a clock variant with next
fire ("Next 10:30"), the mobile subline shows "next in 25m · <reason>", the
ResumeBanner is suppressed (the schedule *will* resume it), and the desktop
`SessionHeader` gets a chip in the read-only indicators zone. The dynamic
pacing reason lives here — inside the session where the user actually is, not
only on a page they visit weekly.

**Run history that answers the morning question.** Loops tab: a "since you
last looked" divider (`last_viewed_at`), the last-10-runs strip
(green/amber/red), a StatCard row (fires 24h / ok-rate / p50 duration / next
fire — a loop degrading from 20s to 8m runs must be visible), and expandable
run rows (status icon, slot + fired time, duration, summary; expanded shows
the turn's final text inline — M1's navigation substitute until deep-links
land in M2). Retention/pagination: the tab pages via `schedule.runs`.

**Surfaces.** `/schedules` global page (route + `AppSidebar` icon; sections:
active / needs-attention / finished). Per-session **Loops tab** — the *4th*
tab (`chat | todos | changes` today; `git` is a legacy alias), and the
`showTabs` gate must learn about schedules or a clean session with only a
schedule renders no tab strip at all. Mobile: the tab strip already scrolls
horizontally; the name-tap sheet gains the overnight digest line ("8 runs,
7 ok, 1 needs you").

## Config

```toml
[scheduler]
disabled = false            # AGENTIQUE_SCHEDULER_DISABLED — negative-form key:
                            # a TOML "enabled = true" default would decode an
                            # absent key as false in Go and turn the feature off
tick-interval = "20s"       # AGENTIQUE_SCHEDULER_TICK_INTERVAL
min-interval = "1m"         # hard floor; UI warns below 15m on persistent sessions
max-run-duration = "30m"    # -> overdue flag + attention (not error)
max-consecutive-failures = 3
run-history = 200           # retained runs per schedule (pruned by created_at)
once-catchup-window = "1h"
dynamic-max-delay = "6h"
dynamic-fallback = "20m"
```

Env wins over file, resolved explicitly in `serve.go` (`firstNonEmpty` /
`envIntOr`, hard exit on unparseable durations — same as idle-evict).

## Hazards and non-goals

- **In-session `CronCreate` today**: if a user asks their agentique session to
  "/loop", the CLI schedules it — it fires as unlabeled turns while the
  process lives and silently dies on evict/stop/restart. Near-term: preamble
  note that scheduling goes through agentique. Future: intercept the
  `CronCreate` tool_use and offer promotion to a real schedule.
- **Codex targets**: the delivery path is provider-neutral and the registry
  work fixes codex outcome text, but **long-horizon codex loops have an
  observability hole**: codexcli-go currently drops the typed
  `contextWindowExceeded` error code and surfaces compaction/token-usage
  notifications as unknown events — a codex loop hitting its context limit
  reads as opaque failures. v1 therefore ships codex targets with `errorKind`
  best-effort and **recommends claude for long-horizon loops**; the honest
  codex answer is utilization-based conversation rotation (`ResetConversation`
  above a threshold — both adapters expose `Usage`/`ContextWindow` on turn
  completion) in M2, plus small upstream hand-offs (codexcli-go: typed error
  info, compaction events; agentkit: `ErrorEvent.Kind`, codex
  `TurnCompleted.Text`).
- **Agent-created schedules for other sessions** (a lead scheduling loops on
  workers) needs `@spawn`-grade authorization design — deferred; v1
  `ScheduleCreate` is self-targeting only, human-approved per call.
- **Channel-targeted schedules** and **fresh-session-per-run** are future
  modes; the schema keeps them open, the code doesn't speculate.

## Phasing

- **M1 — durable loop, visible runs, no noise**: migration 040 + sqlc;
  turn-identity + completion-registry refactor (keystone, own tests);
  `internal/schedule` service (tick, idle-gated single-flight delivery,
  catch-up, retry/backoff, run lifecycle incl. `deferred`/`interrupted`/
  overdue, auto-pause, restart sweep with queued-re-arm, corrected
  session-lifecycle seams, recall exclusion); origin tagging on prompt row +
  `HistoryTurn`/`PushTurnStarted`; both creation paths (form incl. once-mode +
  non-blocking `ScheduleCreate` with approval banner); `/schedules` page +
  Loops tab (run history, since-last-looked, run strip, stats row, inline
  final text); attention semantics with clear rules; ambient-signal exclusion;
  timeline collapse + filter; parked-state presentation; broadcast pushes.
- **M2 — smart loops + polish**: `ScheduleReport` + `ScheduleNext` (shared
  attribution rules); turn deep-links (`turnIndex` + `scrollToTurn` +
  `?turn=`); server-side history elision; standing self-schedule consent;
  codex context rotation; health popover extras; upstream adapter hand-offs
  land.
- **M3 — entry points + future modes**: composer "Run on a schedule…",
  `CronCreate` interception/promotion, channel-targeted and
  fresh-session-per-run groundwork.

## Verification plan

Unit: cron parse + next-fire + catch-up (table-driven; **hand-derived tzdata
vectors** for Europe/Stockholm spring/fall including the ambiguous-hour
strictly-future guard; vixie DOM-or-DOW cases), run-lifecycle transitions
(one-way, late-report annotations), auto-pause counters per status class,
retention by created_at, slot-key idempotency. `-race`: delivery claim vs
idle-evict vs human `Query` vs run-now vs `flushPendingMessages` on one
session (the `beginIdleEvict` mutual-exclusion test is the template);
registry dispatch under session close. Live end-to-end (multiple runs, per
house rule): 1-minute schedule through evict → lazy-resume → fire; kill -9
mid-loop → verify queued re-arm + catch-up-once + sweep exclusions; forced
approval-block → `action_needed` not error; rate-limit window → `deferred`
with correct `next_run_at`; timeline collapse + parked badge + attention
set/clear on both desktop and mobile.

## Review log — adversarial round, 2026-07-30

Four independent reviewers (concurrency/lifecycle, schema/cron/wire,
provider/outcome, product-insight UX) instructed to disprove the design
against the code. Dispositions:

**Confirmed critical, design changed:** delivery primitive
(`EnqueueMessage` mid-turn injection / codex coalescing — replaced with
idle-gated single-flight `Query`); no single-flight across delivery triggers
(+ pre-existing `Query` TOCTOU adopted as root fix); blocked-on-human turns
misclassified as `error` (→ `PendingState()` detection, `action_needed`);
restart sweep erasing never-delivered slots (→ queued re-arm, sweep-error
exclusion from auto-pause); turn-complete registry lacking turn identity (→
turnIndex keying, atomic registration, async dispatch, close synthesis);
wrong auto-pause seam (`OnSessionComplete` = clean CLI exit, not user intent;
+ fire-path resurrection race on completed/merged); deep-link event-id scheme
unworkable (→ `(session, turn_index)`); `next_run_at` had no parked
representation (once-mode refire-forever, dynamic skip-row spam → `''`
sentinel + fallback pre-write); codex summaries empty
(`TurnCompletedEvent.Text` claude-only → pipeline accumulation); rate-limit
auto-pause (→ `deferred` status); blocking `ScheduleCreate` impossible
(~60s MCP client timeout, POST-only transport → non-blocking
pending-approval); attention/ambient/timeline/parked-state UX unspecified or
noise-generating (→ the four M1 rules in the frontend section); brain-recall
uses-inflation on every fire (→ origin-aware recall).

**Confirmed major/minor, design changed:** migration 040 (039 taken);
timestamp format pin; `UNIQUE(schedule_id, scheduled_for)` + transactional
fire step; retry columns; catch-up most-recent-missed; DST ambiguous-hour
guard + honest blackout numbers + tzdata (not robfig) vectors; origin on
prompt row not `WireUserMessageEvent`; `Broadcast` not `Publish`; run-now/
pause/queued edge rules; `runId`-mandatory MCP attribution; once-mode UX;
4th-tab + `showTabs` gate + lucide `Clock`; scroll-memory suspension;
timeout→overdue flag; `interrupted` status; retention by `created_at`.

**Attacks that failed** (reported by reviewers as such): FK cascade is real
(`PRAGMA foreign_keys=ON` verified in `store.Open`); the global-page
staleness attack doesn't land (frontend subscribes every conn to every
project) — though the design switches to `Broadcast` anyway rather than lean
on that undocumented invariant.

**Accepted, deferred with rationale:** plan-approval second-turn attribution
(v1 accepts — `action_needed` fires first); server-side history elision (M2);
codex long-horizon rotation (M2 + upstream hand-offs).

## Open questions for review

1. Codex as a loop target in v1: ship with the "recommend claude for
   long-horizon" caveat (current plan), or gate schedule creation to claude
   sessions until the upstream adapter fixes land?
2. Timeline collapse in M1 is the largest single frontend slice — acceptable,
   or ship filter-toggle-only first and collapse in M2? (Current plan: both
   in M1; the review argues collapse is the difference between insight and
   noise.)
3. Default `max-run-duration` 30m — now only an overdue/attention threshold,
   so lower stakes; keep 30m?
