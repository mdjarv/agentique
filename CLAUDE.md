# CLAUDE.md

How to work in this repo. Facts about what the product is, how to run and
configure it live in README.md; subsystem designs live in `docs/*.md` — this
file holds only behavior: conventions, invariants, and gotchas you cannot
infer by reading the code.

## Task Completion

- `just check` (biome + tsc) must pass before considering tasks completed.
- `cd backend && go test ./... -count=1 -short` for Go changes — run
  directly, not via justfile. `-short` skips the integration test that needs
  a live provider CLI.
- After editing SQL under `backend/db/`, run `just sqlc`. After changing Go
  wire types, run `just typegen`. Generated files are never edited by hand.
- ALWAYS use `just` commands (not raw `npx`/`tsc`) — they `cd` into the
  correct directory. Running `npx biome` from the project root fails
  silently.

## Core Priorities

1. Performance first.
2. Reliability first.
3. Keep behavior predictable under load and during failures (session
   restarts, reconnects, partial streams).
4. Fix structural problems when found, don't work around them.

If a tradeoff is required, choose correctness and robustness over short-term
convenience.

## Domain Context

**Costs are irrelevant.** We use API subscriptions. Don't surface
costs/prices in UI, CLI output, or mockups. The `totalCost` field exists in
the data model but should not be shown to users.

## Database Access

The live SQLite database is at `~/.local/share/agentique/agentique.db` and is
shared with the running server. **Reads are encouraged** (`sqlite3` for
read-only queries). **Writes require explicit user approval** — a bad write
causes immediate data loss for all running sessions. To test writes, use a
copy or an in-memory DB.

## Engineering Practices

**Separation of concerns.** Each module/function/component has one job. Don't
mix IO with logic, state management with rendering, or transport with
business rules.

**Guard clauses and early returns.** Handle error/edge cases at the top.
Never nest happy-path logic inside conditionals when you can return early.

**Error handling is not optional.** Don't swallow errors silently. Don't
panic for recoverable failures. Propagate context with errors.

**No destructive side effects in constructors.** Startup sweeps, reapers,
identity/secret file creation, and boot reconciliation run from the serve
command's production block — never from `server.New` or any constructor a
test might call. (A stray sweep in a constructor once nuked real worktrees
from a test run.)

**Zustand selectors must return stable references.** Never return `{}`,
`[]`, or the result of `.map()`/`.filter()`/`Object.values()` as a fallback
or computed value — these create a new reference every render and cause
infinite re-render loops. Use a module-level constant for fallbacks; for
computed values use `useShallow` or memoize outside the selector.

## Running a second server locally

Single-instance is enforced on the **data directory** (flock), not the
listen address — two servers on different ports still share one data dir's
DB, worktrees, and CLI subprocesses, and a scratch-DB server has an empty
picture of what the data dir owns (a sandboxed verify server once reaped
every live session of the running service). Always isolate with
`AGENTIQUE_HOME=<tmp>`; `--db`/`--addr` alone do **not** isolate. `just dev`
targets the production data dir and fails fast against the running service.
`--test-mode` swaps in the mock CLI connector — it is not a sandbox flag.

## Subsystem invariants

Each subsystem's full design lives in its doc; what follows is only the rules
a change must not break.

### CLI subprocess lifecycle — `docs/process-lifecycle.md`

Each session's provider CLI runs in its own process group, spawned with a
background context so it outlives requests, and is only closed cooperatively.
The orphan reaper matches a process only when the CLI marker, group
leadership, and this server's data-dir owner stamp all hold — matching fails
closed, and "orphan" means reparented away from us, not `PPID == 1` (systemd
subreaper). Idle eviction is opt-in and lazy-resumes on the next message.

**A restart is not a pause.** That same startup reap is why: the new process
comes up and kills the CLI groups the old one left, so restarting mid-turn
does not suspend the turn, it ends it. Sessions survive (worktrees, history
and metadata are on disk); the current turn does not. Anything that restarts
the server — in-app upgrade, rollback, a future self-restart — must consult
`Manager.BusyTurns()` first and say, in those words, that the cost is the
turn. Busy comes from the turn lifecycle (`EventPipeline.TurnOpen`), never
from session state, which is updated asynchronously and lags it.

### Channels / teams — `docs/discussion-sessionless-personas.md`

The `messages` table is the source of truth for channel timelines.
Informational channel metadata (introductions, spawn notices) is **not**
mirrored into session events — new informational message types must extend
the existing skip list. **Additive principle:** channel features must not
modify session rendering, event-pipeline mutations, or turn management for
sessions outside a channel. Web-only discussion personas are sessionless and
claude-only, must be driven through `runtime.Manager` (a bare connector
bypasses the permission pump and tools block forever), post as the third
`sender_type: "persona"` (skipped by the legacy event mirror), and live in
project-less channels whose WS events fan out on the global topic.

### Scheduled loops — `docs/scheduled-loops.md`

Delivery is idle-gated and fresh-turn-only — never mid-turn injection; busy
refusals requeue without consuming an attempt, and evicted sessions
lazy-resume on fire. Turn identity comes from the turn registry (atomic
subscribe with turn start), never state polling. Run lifecycle is one-way to
a terminal; late reports never rewrite terminals; auto-pause counts only
real error terminals. All schedule timestamps are UTC RFC3339 seconds —
SQLite compares TEXT lexicographically. Schedule-origin turns skip brain
recall, activity bumps, and unseen-completion; schedule attention is its own
channel, not the orange pulse. The MCP schedule-create tool must stay
non-blocking (CLI MCP clients time out). Boot sweep runs from serve strictly
before the scheduler starts.

### Provider abstraction

Sessions are driven via agentkit/runtime's neutral contract — never import a
provider-native event type inside the session pipeline (the two legitimate
exceptions are marked at their sites and gated on provider == claude). New
consumer code switches on neutral events and gates provider-specific
features via `runtime.Capabilities()`. Codex mid-turn send is emulated
(queued, replayed at idle) — the wire capability is deliberately `true`
while the adapter capability is `false`.

### Model catalog — `docs/model-catalog.md`

**A new upstream model release must not require an agentique release.**
Never add a version number to a label literal — labels are derived from the
model ID the CLI reports; the alias list is stable, versions are learned.
The resolved-model learning loop must never be fatal to a session. The
frontend `ModelId` is deliberately `string`. Catalog layers degrade
weakest-first; listing models never fails.

### Multi-machine — `docs/multi-machine.md`

The server is the authorization boundary — network reachability (tailnet)
and peer discovery never substitute for auth; bearer tokens never ride URLs
(sockets redeem one-time tickets, re-checked against the DB); an explicit
credential never falls back to another. Clients pin `machineId` and verify
it on pair and connect. Requests route by owning entity through the routing
facade; only `Project` carries a client-side machine tag (sessions derive
theirs via the project); remote slugs get a machine suffix, primary slugs
are never rewritten. Cross-machine grouping (by canonical git remote) is
display-only — commands always target one physical entity. Every surface
that LISTS projects lists logical ones (`useLogicalProjects`), never
checkouts; the representative owns presentation (name, colour, icon, star)
and a remote's own favorite flag is ignored. The machine
catalog is account state on the primary; localStorage and the per-machine
data cache are offline caches, and cached snapshots sanitize live-ness. A
flaky remote re-syncs only itself and must never reset primary state.
Per-machine WS clients reconnect in place, never get replaced.

### Brain / memory — `docs/brain-memory.md` (+ `docs/brain-design-log.md`)

Liftable core in `internal/memory` (stdlib + yaml/uuid only); agentique
policy in `internal/brain`; markdown is the source of truth, everything else
(graph, areas, vectors) is a rebuildable index. Recall is fluid and
per-turn with a session seen-set (delta injection) — don't reintroduce
first-turn-only recall. Semantic similarity is pluggable and everything
degrades cleanly to keyword/Jaccard without an embedder; the recall
thresholds (veto/vouch) are embedding-model-specific. Stopwords drop
conversational filler but never domain terms (`just` is the build tool).
Strength changes on outcome, not injection; human confirmation outranks
corroboration, which is capped below it. Model choice is a required caller
parameter in the memory core, never a library default.
