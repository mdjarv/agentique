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

## Security invariants

An authenticated operator has arbitrary code execution by design — they run
agents. So the boundaries that matter are the *other* ones: an unauthenticated
caller, a cross-origin page, another local user, and **a prompt-injected
agent** (agents act on untrusted repo/web/tool content and are not a trusted
principal). Judge a change against those, not against "is the caller logged
in".

**A `{id}` route parameter is not one path segment.** Go's `ServeMux` matches
on the escaped path and unescapes the capture, so `%2F` arrives as a real
separator: `/api/x/..%2F..%2Fy` yields `../../y` from `r.PathValue`. Any path
parameter that becomes a filesystem path must be validated for what it is (a
UUID, a timestamp) *before* the join — and never by checking the joined result
against a root derived from the same untrusted value. Record ids are also glob
patterns in `filestore`, so `*` and `?` are rejected too.

**Agent-written bytes are never served as active content.** Session files come
from agents and are served from the app's own origin, where script can drive
the whole authenticated API. `internal/session/files_content_type.go` holds an
allowlist of provably inert types; everything else is an octet-stream
attachment. Adding `.html`, `.svg`, or a sniffable type to that list is a
stored-XSS hole. Response headers (`nosniff`, `frame-ancestors`,
`Referrer-Policy`, the SPA CSP) live in one middleware so a new route inherits
them; `script-src` allows index.html's bootstrap by **hash computed from the
embedded bundle**, never by `'unsafe-inline'`.

**Credentials never reach argv or a group-readable file.** The data dir is
owner-only (`paths.SecureDataDir`, called from serve, never a constructor) and
the DB plus its sidecars are 0600. The per-session MCP bearer goes to the CLI
as a 0600 *file path*, because `/proc/<pid>/cmdline` is world-readable; if that
write fails, fall back to the stdio transport, never to inline JSON.

**Inbound credentials are stored as digests, never recoverable.**
`auth_sessions`, `pairing_tokens` and `invite_tokens` hold `token_hash`
(`auth.HashToken` — plain SHA-256, correct because every token is
crypto/rand output, not a password). Anything writing those rows directly must
hash first; there is no plaintext column to fall back to.

`machines.token` is the exception and must stay plaintext: it is an OUTBOUND
credential this server presents to a remote. Hashing cannot protect it and
neither can encryption at rest — the key would live in the same directory, at
the same uid the agents run as. **That is the open boundary:** a prompt-injected
agent can read the data dir, so it can read every paired machine's bearer. The
fix is a privilege split (separate uid or sandbox per session), not another
storage trick. Do not describe the data dir as protected from agents.

**Downloads fail closed.** A missing or unfetchable checksum aborts an install
(`install.sh`, `install.ps1`); release asset and checksum URLs must be HTTPS
(loopback excepted). Unclassified 500s return a fixed message — `err.Error()`
is for the log line, not the response body.

**No user row is written before its ceremony verifies.** `credentialCount == 0`
puts the server in rekey mode; persisting a user at `register/begin` meant one
abandoned registration opened that window forever. Registration creates the row
in `finish`, re-checks the first-user precondition there, and the rekey path
itself takes a one-time code from `agentique auth rekey` — never a display
name.

**`is_admin` is not a containment boundary, and must not be treated as one.**
Any authenticated user can create a session, and a session runs agents with
tool access — that is code execution on the host. So a non-admin already has
everything `requireFullAccess` guards, by a longer route. Nothing in the schema
disagrees: `sessions` and `projects` have no owner column, and no list or WS
topic filters by user.

Keep the existing guards — they are consistent and they cost nothing — but do
not add a feature whose safety *depends* on them, and do not describe the role
to users as a restriction. A real boundary means adding ownership to every
entity and scoping every list, subscription and command; that is a feature, not
a check. Until then, treat "can log in" as "owns the machine".

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
turn. Busy comes from the runtime's own turn lifecycle
(`runtime.Session.TurnInFlight` via `Session.TurnInFlight`), never from session
state: `State()` reports Idle for one dispatch before the completion that
caused it is broadcast. The pipeline's `turnOpen` is a different concern —
outcome attribution — and is not a busy signal.

### Archived vs done

Three independent facts, three owners. `state` is what the CLI process is doing
and belongs to the runtime — `done` means it exited cleanly, never "the user is
finished with this". `archived_at` is the user filing a session away (the
sidebar's Archived section) and is written **only** by an explicit gesture:
`ArchiveSession`, merge `complete`/`delete`, a local session's commit. The
runtime's `StateDone` seam must never write it — that let a subprocess exiting
hide a session in a collapsed section, and it is the same line
`SetOnSessionFinished` already draws. `worktree_merged` is the git outcome.

So: archiving never transitions state (it releases an *idle* CLI through
`StopSession`, so the resulting state is one that actually happened), unarchive
is its exact inverse, and archiving is refused while a turn is in flight —
`TurnInFlight`, not `State()`. Bulk *destructive* actions key on
`worktree_merged`, never on archived: archiving is a one-click tidy (including a
whole-shelf sweep), and only merged work is safe to delete in bulk. UI copy says
"Archive"; `done` reads as "finished" wherever it surfaces.

### Wire compatibility across peers

A client talks to **several servers at once** — the primary plus one per paired
machine — each on whatever release that machine happens to be running. So a
rename to the wire vocabulary is never atomic, and shipping one without a
transition breaks every machine that has not upgraded yet.

Renames go out as **expand/contract**, both halves at once:

- **Server expands** — emit the new name *and* the old one. Derive the alias in
  one place (a `MarshalJSON` on the wire struct, not at each construction site)
  so no broadcast path can forget it. Keep accepting the old op name in
  `handlerRegistry`.
- **Client accepts either** — via `frontend/src/lib/wire-compat.ts`, which owns
  every alias: `readArchivedAt` for fields, `LEGACY_OP` for renamed RPCs.
  `ws-rpc.define` retries under the old op name when a peer rejects the new one
  and remembers that peer, so it costs one round-trip per socket, not per click.
  Never spell an alias at a call site — the next rename must have one place to
  look.
- **Contract later**, once no supported release predates the rename.

Wire fields stay **optional**. The generated Zod schema mirrors the Go tags, so
dropping `omitempty` makes a field required and the client then rejects the
*whole* payload from any peer that does not send it — every `session.state` push
from that machine dropped, its rows frozen. An absent field means "not set".

The per-machine offline cache (`lib/machines/cache.ts`) is the same kind of
boundary: it is a serialization of an internal type, so it drifts whenever that
type changes, and a stale cache renders wrong rather than failing loudly. It
carries `CACHE_VERSION` — bump it on any rename, migrate the previous shape, and
refuse a version from the future instead of hydrating a guess.

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
and peer discovery never substitute for auth; auth-disabled listeners are
loopback-only; bearer tokens never ride URLs (sockets redeem bounded one-time
tickets, re-checked against the DB); an explicit credential never falls back
to another. Clients pin `machineId` and the signing identity, then verify a
fresh signed challenge before sending credentials on every pair/connect path.
Revoking a session closes its established sockets, and unpairing revokes the
remote bearer before deleting the local catalog row. Requests route by owning
entity through the routing facade; only `Project` carries a client-side machine tag (sessions derive
theirs via the project); remote slugs get a machine suffix, primary slugs
are never rewritten. Cross-machine grouping (by canonical git remote) is
display-only — commands always target one physical entity. Every surface
that LISTS projects lists logical ones (`useLogicalProjects`), never
checkouts; the representative owns presentation (name, colour, icon, star)
and a remote's own favorite flag is ignored. The machine
catalog is full-access account state on the primary; localStorage never
persists bearer credentials, and per-machine data caches sanitize live-ness. A
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
