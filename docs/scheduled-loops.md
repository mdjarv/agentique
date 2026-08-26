# Scheduled loops

Put a session on a long-running schedule. "Babysit this PR", "check the deploy
hourly", "nightly maintenance pass". The way Claude Code's `/loop` and cron tools
do, but agentique-owned: durable across restarts and evictions, with real insight
into what happened and how it went.

**Status: M1 and M2 shipped.** M3 (composer entry point, `CronCreate`
interception, channel-targeted and fresh-session-per-run modes) is not started.
Codex context rotation and server-side history elision moved into M3 too.

## Why not the CLI's own scheduler

Claude Code ships session-scoped cron. Probed live against the real CLI in the
stream-json mode agentique uses:

- The cron tools exist and fire in headless stream-json mode. A `* * * * *` task
  produced one agent-initiated turn per minute on the same wire.
- Each fire arrives as `system.init → assistant → result`. **No `user` event
  carries the scheduled prompt**, so agentique's pipeline never runs
  `persistQueryStart` for that turn: no prompt row, no turn-started push,
  undefined timeline rendering.
- The tool result states verbatim: *"Session-only (not written to disk, dies when
  Claude exits). Auto-expires after 7 days."*

That last point is disqualifying. agentique deliberately stops CLI processes: idle
eviction, Stop, server restarts. Any of those silently kills an in-CLI cron task,
and lazy resume only triggers on the next *message*, so nothing ever wakes the
loop again. The CLI scheduler also needs a warm `claude` plus its
Playwright/Chromium subtree resident for the loop's whole lifetime, which is
exactly the steady-state bloat idle eviction exists to prevent.

So the substrate inverts. **agentique owns the schedule and fires prompts into
sessions as fresh turns.** Idle eviction then becomes a *feature* of long loops:
the session evicts between fires and lazy-resumes on the next one, so a
weeks-long hourly loop holds zero warm processes.

## Data model

```sql
CREATE TABLE schedules (
    id            TEXT PRIMARY KEY,
    project_id    TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    session_id    TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    name          TEXT NOT NULL,
    prompt        TEXT NOT NULL,
    cron          TEXT NOT NULL DEFAULT '',      -- 5-field, local tz; '' for once/dynamic
    mode          TEXT NOT NULL DEFAULT 'recurring',  -- recurring | once | dynamic
    enabled       INTEGER NOT NULL DEFAULT 1,
    pause_reason  TEXT NOT NULL DEFAULT '',      -- '' | user | completed | expired |
                                                 --   session-completed | auto-failures |
                                                 --   dynamic-ended | pending-approval
    attention     TEXT NOT NULL DEFAULT '',      -- '' | action_needed | failed
    attention_run_id TEXT NOT NULL DEFAULT '',
    next_run_at   TEXT NOT NULL DEFAULT '',      -- '' = parked (no next fire)
    expires_at    TEXT NOT NULL DEFAULT '',
    last_run_at   TEXT NOT NULL DEFAULT '',
    last_viewed_at TEXT NOT NULL DEFAULT '',     -- "since you last looked" divider
    consecutive_failures INTEGER NOT NULL DEFAULT 0,
    created_by    TEXT NOT NULL DEFAULT 'user',  -- user | agent
    created_at    TEXT NOT NULL,
    updated_at    TEXT NOT NULL
);
CREATE INDEX idx_schedules_due ON schedules(enabled, next_run_at);

CREATE TABLE schedule_runs (
    id            TEXT PRIMARY KEY,
    schedule_id   TEXT NOT NULL REFERENCES schedules(id) ON DELETE CASCADE,
    session_id    TEXT NOT NULL,
    scheduled_for TEXT NOT NULL,                 -- slot key
    created_at    TEXT NOT NULL,                 -- retention prunes by this, not slot time
    fired_at      TEXT NOT NULL DEFAULT '',
    finished_at   TEXT NOT NULL DEFAULT '',
    status        TEXT NOT NULL,                 -- queued | firing | running | ok |
                                                 --   action_needed | error | deferred |
                                                 --   interrupted | skipped
    overdue       INTEGER NOT NULL DEFAULT 0,    -- exceeded max-run-duration (flag, not status)
    attempts      INTEGER NOT NULL DEFAULT 0,
    next_attempt_at TEXT NOT NULL DEFAULT '',
    turn_index    INTEGER NOT NULL DEFAULT -1,   -- deep-link anchor
    summary       TEXT NOT NULL DEFAULT '',
    reason        TEXT NOT NULL DEFAULT '',      -- dynamic-pacing reason / skip reason
    error         TEXT NOT NULL DEFAULT '',
    error_kind    TEXT NOT NULL DEFAULT '',      -- '' | rate_limit | overloaded | context | other
    late_report   TEXT NOT NULL DEFAULT '',      -- report/completion after terminal
    duration_ms   INTEGER NOT NULL DEFAULT 0
);
CREATE UNIQUE INDEX idx_schedule_runs_slot ON schedule_runs(schedule_id, scheduled_for);
CREATE INDEX idx_schedule_runs_recent ON schedule_runs(schedule_id, created_at DESC);
```

**Slot keys.** For cron slots `scheduled_for` is the slot time, and the unique
index makes crash-replay idempotent: insert `ON CONFLICT DO NOTHING`, and a
conflict means the slot is already claimed. For run-now and dynamic fires it is
the claim timestamp, where a same-second collision reads as "already claimed" and
is correct to drop.

**Parked schedules.** `next_run_at = ''` means no next fire and is excluded by the
due query. Used by `once` after firing, and by paused and expired schedules.

**Timestamps are pinned** to `time.Now().UTC().Format(time.RFC3339)`, seconds
precision with a `Z` suffix, in every column and every query parameter. The
codebase mixes formats elsewhere and SQLite compares TEXT lexicographically, so
mixed precision breaks `next_run_at <= ?` ordering.

**Retention** prunes to the newest ~200 runs per schedule by `created_at`, not by
`scheduled_for`. A catch-up row for an old slot would be born oldest and pruned
first, deleting exactly the honest-gap record.

**No 7-day expiry by default.** Claude Code's expiry bounds *invisible* forgotten
loops; agentique schedules are visible and auto-pause on failure. `expires_at` is
enforced in the due query plus an explicit pause transition, so expiry is a
visible state rather than a silent stop.

**Cron parsing is in-house**, about 200 pure lines, no dependency. The same
restricted grammar Claude Code supports: wildcards, values, `*/step`, ranges,
lists, and the vixie DOM-or-DOW OR rule. No `L`, `W`, `?` or name aliases.

DST behaviour, stated honestly: spring-forward's nonexistent times normalize
forward, and fall-back's repeated hour fires once per *wall-clock* combination,
which for an hourly cron means a two-hour real-time gap once a year, 90 minutes
for `*/30`. Because Go's resolution of *ambiguous* wall-clock times is
unspecified, the advance carries an **unconditional strictly-future guard**:
recompute until `next_run_at > now` in UTC. Test vectors are hand-derived from
tzdata, not generated from robfig/cron, whose own DST behaviour is unreliable on
exactly these cases.

## Delivery

**Never `EnqueueMessage`.** Its running-session branches are the wrong semantics
on both providers. Claude gets native mid-turn injection, so the prompt lands
*inside the human's current turn*: no prompt row, no turn boundary, and the
outcome attributed to the wrong turn. Codex gets a queued pending message that
coalesces with queued human messages into one replayed turn. `EnqueueMessage` is
the human-composer primitive; the scheduler has its own.

The delivery primitive is lazy resume plus a fresh-turn-only `Query` variant.
`validateAndPrepareQuery` already refuses atomically when the session is running,
merging or being evicted, so a plain `Query` *is* the correct atomic
try-deliver: success means the turn started, and refusal means "still busy, stay
queued, retry". The variant additionally returns the turn index and prompt row id,
and accepts the completion callback.

**Single-flight.** Delivery has several triggers: the tick loop, the session's
transition to idle, `schedule.run-now`, and the boot pass. All of them route
through one claim, a per-session delivery mutex plus a compare-and-set on the run
row from `queued` to `firing`. Only the claim winner calls `Query`, and run-now
goes through the same path rather than firing directly.

The fire pipeline for one due schedule, with steps 1 and 2 in one transaction so a
crash between them cannot double-fire or lose the slot:

1. Insert the run row as `queued` and advance `next_run_at` atomically, then push
   the run event.
2. Attempt delivery through the claim. Refused-busy stays `queued`, retried on
   later ticks and on the idle transition. Delivery *errors* (a resume racing the
   git-op lock, a failed worktree recovery) get bounded retry with backoff at 30s,
   2m and 10m, durable in the run row so the UI can say "retrying in 2m" and a
   restart does not forget, then `error`.
3. On delivery: `running`, `fired_at` stamped, `turn_index` recorded.
4. Completion resolves the run through the turn registry.

**Queued-run bounds.** The run-duration clock starts at `fired_at`, not at
`queued`. A queued run not delivered before its schedule's *next* slot resolves
`skipped`. Pausing or disabling resolves any queued run as skipped.

**Catch-up.** If `next_run_at` is more than one interval in the past, meaning the
server was down, fire once for the **most recent** missed slot, which is the
freshest context and matches Quartz and Kubernetes CronJob misfire conventions,
advance to the next future occurrence, and record one aggregate `skipped` row for
the older missed slots so history shows the gap honestly. A `once` schedule
catches up only within a bounded staleness window, one hour by default: "remind me
at 15:00" firing at 21:00 the next day is wrong.

**No jitter.** Claude Code jitters to protect the API from synchronized fires
across thousands of sessions. agentique is one host with a few schedules.

## Run lifecycle

One-way: `queued → firing → running → terminal`. Terminal is written exactly once,
and anything arriving later — a late turn completion, a late report — lands in
`late_report` as an annotation and never rewrites status, duration or counters.

| Terminal | Meaning | Counts toward auto-pause |
|---|---|---|
| `ok` | The turn completed clean. | resets the counter |
| `action_needed` | The run needs a human. | no, nothing failed |
| `error` | The turn genuinely failed. | yes |
| `deferred` | A transient provider condition. | no |
| `interrupted` | A human hit stop on the scheduled turn. | no |
| `skipped` | The slot did not run. | no |

`action_needed` is set by the report tool, or **detected automatically**: if the
session's pending state (approvals, questions, plan review, all of which block
indefinitely, and fullAuto does not bypass questions) is non-nil while a scheduled
turn is in flight, the run resolves `action_needed` naming what it is waiting on,
and the duration clock freezes.

`deferred` covers a rate-limit window or an overloaded provider. The run is
rescheduled, not failed, to the later of the reset time, the retry-after and the
minimum interval. A subscription's 5-hour usage window plus an hourly cron must
not kill the loop.

`skipped` rows keep `fired_at` and `duration_ms` at zero and render the reason and
slot time only.

**Timeout is a flag, not a status.** At `max-run-duration` the run is marked
`overdue` and raises attention; the turn is left alone and still resolves the run
when it completes. That kills two misclassifications: a healthy heavyweight
nightly run no longer becomes a fake error, and three slow nights no longer
auto-pause a working loop. A run that never completes stays visibly overdue, which
is the honest state.

**Auto-pause** counts only `error` terminals. At `max-consecutive-failures` the
schedule is disabled with `pause_reason='auto-failures'` and attention `failed`.

## Restart and session lifecycle

**The boot sweep runs from serve, strictly before the scheduler's first tick,
never in `server.New`.**

Runs in `firing` or `running` at boot were delivered and their CLI died with the
server, so they become `error("server restarted mid-run")` — but **excluded from
`consecutive_failures`**, because the documented OOM-then-restart crash loop must
not auto-pause every active schedule.

Runs still `queued` were *never delivered*, so they are re-armed rather than
errored: left queued for the boot pass to deliver. The daily 9am loop that crashed
at 9:01 still runs its 9am slot at boot instead of silently skipping the day.

**Auto-pause on session completion hooks the two user-intent paths only**: the
archive RPC flow and the merge finalize path, synchronously, with
`pause_reason='session-completed'`. Explicitly **not** the runtime's clean-CLI-exit
seam, which fires on any clean exit that lazy resume handles transparently and
would pause healthy loops.

The fire path independently re-checks archived and merged state at delivery time
inside its transaction and converts the run to `skipped('session completed')` plus
auto-pause. That is necessary because `Query` otherwise *unsets* both flags, so an
in-flight fire racing the user's merge would silently reopen a session the user
considers done.

Session deletion needs no scheduler code: the foreign key cascade handles it, and
`PRAGMA foreign_keys=ON` is verified set at open.

## Brain recall interaction

Evicting between fires resets the per-session recall seen-set, so every fire would
re-inject the same `<brain>` block and bump uses on the same facts 24 times a day
with no corresponding `MemoryUsed`, polluting the outcome-signal calibration and
paying tokens per fire.

Fires carry a schedule origin into recall, which then runs with a **per-schedule
persisted seen-set**, delta across evictions, and skips `BumpUses` for
schedule-origin injections. The tool footer is excluded from the recall query
text.

## Turn identity and the completion registry

`Session.SetTurnCompleteHook` was a single atomic pointer contended by the
discussion orchestrator, fired synchronously on the runtime event-loop goroutine,
and `runtime.TurnCompletedEvent` carries **no turn id**. So "subscribers get
completions for turns they initiated" was unimplementable without threading
identity through. The refactor that made schedules possible:

- The pipeline passes its turn index through `OnTurnComplete`, the registry is
  **keyed by turn index**, and the delivery `Query` variant takes the completion
  callback as an argument, so registration is atomic with turn start. No
  register-before-or-after races with replays, other schedules, or human turns.
- Callbacks dispatch **async**, on a buffered channel per subscriber, so a
  subscriber doing SQLite writes cannot stall the session's event stream.
- `Session.Close` synthesizes a terminal "session closed" delivery to open
  subscriptions. Before that, a manual Stop mid-turn stranded the discussion
  orchestrator until its own timeout.
- Registrations do not survive the `Session` object. The scheduler registers
  through the delivery call per fire and never holds a session pointer across
  evictions.
- The payload is final text, status, error kind and duration. `finalText` fixes a
  codex gap: `TurnCompletedEvent.Text` is populated only by the claude adapter, so
  the pipeline accumulates the turn's last assistant text as a fallback, which
  also fixed a latent empty-reply bug for codex discussion personas. `errorKind`
  is classified from the turn's error and rate-limit events.

Known wrinkle: plan approval auto-fires a second "proceed with implementation"
turn that the registry does not attribute to the run. Accepted, because
`action_needed` fires first anyway, which is the honest state.

## Creating a schedule

**From the UI**: a create-and-edit dialog from the `/schedules` page and the
session action menu. Name, prompt, cadence (interval presets, raw cron, or "once,
at…"), with the target session prefilled in session context.

**From chat**: the agent calls the `ScheduleCreate` MCP tool, self-target only.

**That tool must stay non-blocking.** The claude CLI's MCP client has a roughly
60-second per-call timeout and agentkit's transport is POST-only with no progress
notifications to extend it, so a handler that parks waiting for a human fails on
any realistic approval latency. Codex has no MCP-tool interceptor surface at all.
So the handler creates the schedule **paused** with
`pause_reason='pending-approval'`, returns immediately, and surfaces the approval
banner. Approval enables it, denial deletes it, and the agent observes the outcome
through the schedule state.

Standing consent is an "always allow this session to schedule itself" option on
that banner, stored as a behavior preset and bounded: self-target only, cadence at
or above the floor, at most N active schedules.

One-shot reminders are `mode='once'`: fire once, then disable with
`pause_reason='completed'` and park, dropping out of the default view into a
finished filter.

## Dynamic pacing

Claude Code's self-paced `/loop` maps one-to-one. A `mode='dynamic'` schedule has
no cron; the agent paces itself through the auto-allowed `ScheduleNext(runId,
delaySeconds, reason)` tool, plus `stop: true`.

- **Start.** Creation sets `next_run_at = now`, so the first fire is immediate.
- **Fallback pre-write.** Each dynamic fire writes `next_run_at = fired_at +
  dynamic-fallback` *at fire time*, and `ScheduleNext` overwrites it. Forgetting to
  reschedule needs no detector: the fallback simply fires. If that run does not
  reschedule either, the loop parks with `pause_reason='dynamic-ended'`, visibly.
  This also keeps `next_run_at` non-stale mid-turn, so the tick loop never spams
  skip rows while the agent is still deciding.
- **Clamp** to `[min-interval, dynamic-max-delay]`. Claude Code caps at an hour;
  agentique allows longer, because a parked session costs nothing.
- **Attribution.** `runId` is mandatory. The fire footer embeds it, and the handler
  validates that the run is running and the schedule is dynamic, rejecting
  otherwise with an instructive error. That closes the ambiguity of N schedules on
  one session, and of a stale footer in a persistent conversation prompting tool
  calls during a *human* turn. Defense in depth: a partial unique index allows one
  dynamic schedule per session.

## Outcome capture

The always-on fallback is the trimmed tail of the turn's final text, with status
from the registry.

The preferred channel is `ScheduleReport(runId, status, summary)`, footer-
instructed and auto-allowed, with the same mandatory-`runId` attribution rules. A
tool report wins over the text fallback, and a report for an already-terminal run
lands in `late_report`.

## Wire and origin tagging

RPCs: `schedule.create`, `.list`, `.update`, `.delete`, `.pause`, `.resume`,
`.run-now`, `.runs` (paged), `.mark-viewed`.

**Pushes use `Broadcast`, not per-project publish.** The global `/schedules` page
is the consumer, every existing global-page domain broadcasts, and leaning on the
frontend's subscribe-all-projects behaviour would be an undocumented invariant.
Runs would otherwise need a join just to find their topic.

**Origin tagging rides the persisted prompt row and turn pushes**, not
`WireUserMessageEvent`, which only fires on the mid-turn paths the scheduler never
uses. As originally specced, the badge would have appeared only in the forbidden
case and vanished on every history reload. The origin is stored in the prompt
event payload and surfaced on the history turn and the turn-started push; the
frontend renders the user bubble with a clock badge and the schedule name.

**Deep links key on session id plus turn index**, not event ids. Prompt rows are
folded into the history turn and never get a wire id, live events carry no id at
all, and the insert does not return one. Turn index is already persisted and
stable. Scroll-memory restore is suspended when `?turn=` is present, because the
two would otherwise fight.

## Frontend rules

Run rows are *data*. Insight lives in the badge, the timeline and the session's
ambient state, and a naive loop degrades all three. These are rules, not polish.

**Attention lives on the schedule row**, not per run, so there is one inbox item
per schedule, deduped while set. An hourly `action_needed` does not re-flip until
cleared.

- `action_needed` clears on viewing the target session or its Loops tab, and
  self-heals when a later run resolves `ok`.
- `failed` clears **only on an explicit act**: resume, edit or delete. Viewing does
  not fix a broken loop, so viewing does not clear it. It renders with the red
  failed styling and the label "Loop paused", **not** the orange pulse, which means
  "agent blocked right now" and has to keep meaning that.
- The session badge takes the worst across that session's schedules, ranked below
  approval and question in the priority cascade.

**Schedule-origin results are second-class in the ambient signal layer.** They
never set unseen-completion (the server's turn-completion mark and the client's
optimistic one both skip schedule-origin turns — schedule attention has its own
badge channel), do
not bump the Active-section sort key, and their per-fire activity items coalesce
("Deploy check, 12 runs, all ok"). Without this, every result bolds the row and an
hourly loop pins itself permanently to the top of Active, killing the "what
responded overnight" signal for everything else.

**Consecutive schedule-origin turns collapse** into one expandable group row,
rendered from run data and expanding lazily to real turns, plus a hide-scheduled-
runs filter. Two code facts make this load-bearing: `session.history` fetches the
entire session unbounded on open, and lazy turn mounts are a one-way latch, so a
scrolled-past turn never unmounts. A five-minute loop is 288 turns a day into
both. Creation warns below a 15-minute cadence on a persistent session; the
`min-interval` floor stays 1m for run-now and dynamic tests.

**The parked state must not read as dead.** An evicted loop session would
otherwise render "Stopped" with a resume banner, looking broken for 59 minutes of
every hour. When a session has an enabled schedule and is stopped or evicted, the
badge shows a clock variant with the next fire, the mobile subline shows "next in
25m" plus the reason, the resume banner is suppressed because the schedule will
resume it, and the desktop header gets a chip. The dynamic pacing reason lives
here, inside the session where the user actually is, not only on a page they visit
weekly.

**Run history answers the morning question.** The Loops tab has a "since you last
looked" divider, a last-ten-runs strip, a stat row (fires in 24h, ok rate, p50
duration, next fire — a loop degrading from 20s to 8m has to be visible), and
expandable run rows.

## Configuration

Every key is under `[scheduler]` in `config.toml` with an
`AGENTIQUE_SCHEDULER_*` environment override that wins; README's configuration
section has the table.

One naming note: the key is `disabled`, the negative form, deliberately. A TOML
`enabled = true` default would decode an absent key as `false` in Go and turn the
feature off.

## Hazards and non-goals

- **In-session `CronCreate` today.** If a user asks their agentique session to
  `/loop`, the CLI schedules it: unlabeled turns while the process lives, silently
  dead on evict, stop or restart. Near term, a preamble note that scheduling goes
  through agentique. M3 intercepts the tool use and offers promotion to a real
  schedule.
- **Codex targets have an observability hole.** The delivery path is
  provider-neutral and the registry work fixed codex outcome text, but codexcli-go
  drops the typed context-exceeded error code and surfaces compaction and
  token-usage notifications as unknown events, so a codex loop hitting its context
  limit reads as opaque failures. Codex targets ship with best-effort `errorKind`,
  and **claude is recommended for long-horizon loops**. The honest fix is
  utilization-based conversation rotation in M3, plus small upstream hand-offs.
- **Agent-created schedules for other sessions**, a lead scheduling loops on
  workers, needs spawn-grade authorization design. Deferred; `ScheduleCreate` is
  self-targeting only.
- **Channel-targeted schedules** and **fresh-session-per-run** are future modes.
  The schema keeps them open; the code does not speculate.
</content>
