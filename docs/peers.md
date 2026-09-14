# Peers

How the assistant acts on a paired machine, and how every machine reports on its
own health.

**Status: designed, not implemented.** Two verdicts settled in design rounds on
2026-09-13/14: **C** (one assistant; the owner guards its sessions) and **S1**
(sensors on every machine, one mind at home). Four questions are still open and
listed at the end with the default this document assumes. The rounds, with the
rejected options and their costs, are the artifact "Assistant Across Machines".

This extends [multi-machine.md](multi-machine.md), which stays the record for
pairing, identity and routing, and [assistant.md](assistant.md), which stays the
record for the verb table, tiers, budgets and the journal.

## Why

The operator's sidebar shows every machine's sessions, because the browser dials
each machine itself. The assistant asks one server, and until 0f7e58c7 that server
knew only its own database: a session running on zbook did not exist for
`list_sessions` or `find_session`. Listing is now fixed (`peer_sessions.go`), but
everything that acts still stops at the database, by construction:

| Where | What assumes one machine |
|---|---|
| `Directory.SessionBrief` | The single "can I act on this" test, at eight call sites. Local by definition. |
| `assistant_dispatch.go` | Sends in-process with `OriginAssistant`. A peer's WS `session.enqueue` takes no origin. |
| `server.go` report tool wiring | `AssistantReport` is registered only with the assistant or voice on. zbook has neither. |
| `AddTurnEndListener`, `bus.SubscribeAll` | The journal learns of turns from in-process hooks. A peer's turn never arrives. |
| Budget ceilings | Counted against this machine's `sessions` table. |
| `proposalChecks` | Re-checks against local git. |

And the deployment it has to serve: one machine runs the assistant (the VPS,
v0.7.1), the others run older releases with it off, and one is often asleep. A
design that needs the assistant enabled where the work runs does not fit.

## The line

**One assistant decides what to ask for; the machine that owns a session decides
what may happen to it.**

The acting server owns the conversation, the verb table and its tiers, the
budgets, the journal and the heartbeat. The owner owns execution, origin, and a
guard that no request can widen. The peer surface moves *requests and reports*,
never state — the multi-machine invariant "execution never replicates" holds.

Three roles, which one server can hold at once:

- **Acting server**: a server with the assistant on.
- **Owner**: any server running a release that serves the peer surface. Its own
  assistant flag is irrelevant.
- **Steward**: every server, always. No model.

## Credential

The peer surface is authenticated with a **peer credential**, never the browser
bearer: an `auth_sessions` row with `kind = 'peer'`, minted by the owner and
requested once by the acting server using the full bearer it already holds.

- Accepted **only** on `/api/peer/*` and the ws-ticket mint for `/api/peer/stream`.
  The route-matrix test specified in multi-machine.md's security gates covers it:
  a peer credential presented to every other API family, WebSockets included, is
  denied.
- Stored outbound in a new `machines.peer_token` column, plaintext for the reason
  `machines.token` is (an outbound credential; see CLAUDE.md).
- Every connection proves identity first (`machine.FetchRemoteJSON`), on the
  no-redirect client with verified TLS and bounded bodies.
- **An explicit credential never falls back.** Once a machine has a peer token,
  nothing on the acting side uses its full bearer server-side again. A peer too
  old to mint one stays list-only, over today's transitional read, and says so.
  That read is removed at contract.
- Unpairing revokes both credentials before the catalog row goes.

This is the separable revocation the sync design wanted: turning off peer actions
must not unpair the machine for the browser.

## The owner's surface

| Route | Does | Refuses |
|---|---|---|
| `GET /api/peer/sessions` | The owner's sessions with project name and `remote_url`, origin and policy, and a `peerSurface` version. Replaces reading `/api/sessions` plus `/api/projects`. | — |
| `POST /api/peer/sessions` | Create in a project named by id or `remote_url`: worktree only, `fullAuto`, origin assistant, policy recorded. Answers the row. | Actions not accepted; unknown project; quota. |
| `POST /api/peer/sessions/{id}/send` | Enqueue with origin assistant and optional policy. Answers the `MessageDelivery`. | Actions not accepted; archived; main worktree; not `fullAuto`; rate. |
| `POST /api/peer/proposals/check` | Runs the owner's own check for a verb on a session and answers facts plus a version. | — |
| `POST /api/peer/proposals/execute` | Re-checks against the version, then runs through the owner's `GitService`/`session.Service`. Answers `done`, `stale`, `conflict`, `needs_rebase`, `dirty_worktree` or `failed`. | Actions not accepted; stale facts perform nothing. |
| `GET /api/peer/stream` (WS) | A sequenced event stream: `session.state` subset, `turn_end`, `report`, `finding`. | — |

Path parameters are validated as UUIDs before use, on CLAUDE.md's `{id}` rule.

### The guard

What the owner refuses **whatever it is asked**, because the acting server is an
agent's delegate and is not a trusted principal:

- **Origin comes from the credential, never the body.** A peer credential's sends
  and creates are `OriginAssistant`, always. The body may add a policy id, which
  is recorded and never trusted as permission.
- **Worktree sessions only.** A main worktree is never sent to or created in.
- **`fullAuto` only**, the rule `AutoRunnable` already applies locally: a run that
  blocks on a prompt stops with nobody told.
- **Rate ceilings per credential**, as a backstop the acting server's budgets
  cannot widen: sends per minute, creates per hour, in-flight assistant sessions.
- **Actions are opt-in per machine.** `[peer] accept-actions` defaults to false:
  list, stream and findings only. `[peer] accept-policies` separately gates sends
  and creates that carry a policy, so a machine can take asked-for work but no
  autonomy (open question 4).
- **Uncontained verbs execute only on a passing owner-side re-check.** A card
  accepted at home narrows what happens; it never widens it.

### The stream is a log

The owner stamps each stream event with a per-owner `seq`. Reports and findings
are **durable on the owner** (a `assistant_report` session event, a
`steward_findings` row); state and turn ends are derived from what the owner
already persists. The acting server keeps `peer_cursors(machine_id, seq)` and
reconnects with `since`, so a stream that drops loses nothing and a replay is
idempotent. This is the cursor model presentation sync already specifies:
commit the cursor only through what was actually applied.

## Reports belong to the owner

`AssistantReport` is registered on **every** server regardless of feature flags.
A report is rate-limited by the owner's registry budget, persisted as a session
event, delivered to any local follower, and published on the stream. This fixes
the prompt that names a missing tool, and it makes a report a fact about the
session wherever it is read from.

The instruction's wording ("Someone is listening") assumes a live voice call and
is being reworked separately; it must stay true for a report read later in a
thread or journal.

## The acting side

- **`SessionBrief` becomes `Locate(id) → (row, owner)`**, where owner is local or
  a machine id. Verbs route through a per-machine `PeerActions`, the server-side
  counterpart of the browser's routing facade. The local path is unchanged.
- **One stream client per paired machine**, reconnecting in place and never
  replaced, the rule the browser's per-machine clients already follow.
- **The journal ingests peer events.** `turn_end` and `report` map to the same
  notice kinds as local ones, and the subject carries a machine id.
  `assistant_follows` gains `machine_id`.
- **Budgets span machines and fail closed.** `session_created` journal entries
  already name the policy; the ceiling is the sum of each owner's in-flight
  assistant-origin sessions for that policy. An owner that does not answer counts
  at its last known number, **never zero**.
- **Proposals on a peer** are written from the owner's `check` and executed with
  `execute`. The card names the machine and says whose facts they are.
- **The heartbeat triages peer journal entries and findings** with local ones.
  One heartbeat, so no event is acted on twice.
- **Version skew is a finding the acting side makes.** A peer whose `peerSurface`
  is missing or too old is listed, and a refusal names it: "zbook runs an older
  release, so I can list it but not act on it."

## The steward

Every machine runs one, from serve's production block, never a constructor. It has
**no model**, writes only its own findings table, and reads what the existing
collectors already know. Name pending (open question 1); **never `janitor`**,
which is already `internal/janitor`, the disk planner.

**A finding is a fact, not a sentence.** A closed union of kinds, each with a
severity, facts JSON, an `opened_at`, a `resolved_at`, a resolve rule, and a
remedy. The assistant writes the words. A new kind is a code change that comes
with its resolve rule and a test, on the `REST_GLYPH` precedent: a closed set
forces each kind to choose rather than inherit a blank.

| Kind | Source | Resolves when | Remedy |
|---|---|---|---|
| `cli-signed-out` | `usage.Collector` auth state | auth reads OK again | hand: "run `claude auth login` on zbook" |
| `disk-low` | free space under doctor's disk-space threshold | above threshold plus hysteresis | proposal: reclaim finished sessions |
| `loop-paused` | scheduler auto-pause | the loop is re-enabled or edited | hand |
| `session-blocked-long` | pending approval or question older than a bound | answered | hand: "needs a screen" |
| `update-waiting` | update checker / source checker | applied | proposal (restart costs the turn; `BusyTurns` rule) |
| `backup-failing` | backup job errors | next backup succeeds | hand |

What is deliberately **not** a finding: a boot reap that found orphans, a disk
gauge at a high but ordinary level, an idle eviction. Those are bookkeeping, not
news, and a finding that is always open teaches the reader to ignore findings —
the gauge rule from usage.md.

**Two kinds of remedy, never a button that can only fail.** A remedy the owner
can execute becomes a proposal card executed through the guard. A remedy only a
person at that machine can perform is said in words naming the machine.

**Each machine shows its own findings.** One glyph in its own footer, on the
footer's marks-not-sentences rule, with the rows one click away. So a machine
reports its health even while the acting server is down or unreachable.

## Failure modes

| Situation | Behaviour |
|---|---|
| Owner asleep | Listed as not answering; sends refuse naming it; reports and findings wait on the owner and replay on reconnect. |
| Acting server down | Owners keep running; findings show in each machine's own footer; nothing is acted on. |
| Owner on an older release | List-only, said by name. |
| `accept-actions` off | List, stream and findings work; every act refuses naming the setting. |
| Stream drops mid-turn | Reconnect with `since`; the turn end arrives once. |
| Owner answers a budget query late | Counted at last known, never zero. |
| Two servers with the assistant on | Unsupported. Serve warns when a peer's `/api/health` reports `assistant: true`; owners' rate ceilings bound the damage. |
| Peer credential revoked | Stream and actions fail closed and surface as a machine fault; listing falls back to nothing, not to the full bearer. |

## Security

What changes, stated so nobody widens it further: the assistant's reach extends
from this machine to every machine that sets `accept-actions`. A hostile report
read on one machine can cause **contained** work on another — a worktree session
in `fullAuto`, visibly assistant-origin, within the acting server's budget and
the owner's rate ceiling. It still cannot merge, delete, archive, reclaim or reach
a main worktree anywhere without a person accepting a card, and it cannot reach
a machine that has not opted in.

What does not change: coding agents with a shell can already read
`machines.token` from the data directory, so they already had this reach by a
longer route. The fix for that is a privilege split, not this surface. The peer
credential is for separable revocation and for not running an unattended loop on
the browser's credential; it is not a blast-radius claim.

## Phases

- **P1, owner side.** Peer credential and the route matrix. `/api/peer/*` behind
  `accept-actions`. `AssistantReport` always registered, reports persisted as
  session events. The stream with `seq`. Expand only.
- **P2, acting side.** `Locate`, `PeerActions`, stream clients and cursors, journal
  ingest, `machine_id` on follows. Listing moves to `/api/peer/sessions`.
- **P3, budgets and proposals across machines.**
- **P4, the steward.** Findings table and kinds, stream publication, footer mark,
  heartbeat triage.
- **P5, contract.** Remove the transitional full-bearer listing; rewrite the
  security sections of assistant.md and CLAUDE.md.

## Open questions

Each carries the default this document assumes until answered.

1. **The steward's name.** Default: *steward*.
2. **Where the assistant lives.** Default: whichever server enables it, one per
   account, with the serve warning above.
3. **The cross-machine boundary.** Default: accepted as described under Security,
   gated per machine by `accept-actions`.
4. **Autonomy on peers.** Default: policy-carrying work is refused unless the owner
   sets `accept-policies`; asked-for work needs only `accept-actions`.
