# Peers

How the assistant acts on a paired machine, and how every machine reports on its
own health.

**Status: built (P1–P4), 2026-09-14.** Two verdicts settled
in design rounds on 2026-09-13/14: **C** (one assistant; the owner guards its
sessions) and **S1** (sensors on every machine, one mind at home). The four
questions that were open were answered "defaults" on 2026-09-14 and are recorded
at the end. The rounds, with the rejected options and their costs, are the
artifact "Assistant Across Machines".

This extends [multi-machine.md](multi-machine.md), which stays the record for
pairing, identity and routing, and [assistant.md](assistant.md), which stays the
record for the verb table, tiers, budgets and the journal.

## Why

The operator's sidebar shows every machine's sessions, because the browser dials
each machine itself. The assistant asks one server, and until 0f7e58c7 that server
knew only its own database: a session running on zbook did not exist for
`list_sessions` or `find_session`. Listing was fixed first (`peer_sessions.go`),
and then everything that acts still stopped at the database, by construction:

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

- Minted with `POST /api/auth/peer-credential {label, replaceSessionId?}`,
  authorized like pairing (an admin session or the admin secret).
  `replaceSessionId` rotates, and can only name a peer credential.
- Accepted **only** on `/api/peer/*` and on revoking itself
  (`DELETE /api/auth/session`). Not on a ws-ticket mint: the peer surface has no
  socket. `auth.credentialAllowed` is the one rule, and it runs where every
  request authenticates (`authenticateRequest`, and ticket redemption), so no
  route can forget it. A browser credential is refused on the peer surface in
  the same place. A peer path must be clean and unencoded, because the check
  reads the decoded path and the mux does not route it verbatim.
- `TestPeerCredentialIsRefusedOutsideThePeerSurface` is the route matrix at the
  real HTTP boundary. Adding a route means adding it there.
- Stored outbound in `machines.peer_token` (and its public id in
  `peer_session_id`, which a rotation names), plaintext for the reason
  `machines.token` is: an outbound credential; see CLAUDE.md.
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
| `GET /api/peer/sessions` | The owner's sessions and projects (name and canonical remote, never a path), each session's origin, the owner's two opt-ins, and a `peerSurface` version. Replaces reading `/api/sessions` plus `/api/projects`. | — |
| `POST /api/peer/sessions` | Create in a project named by id or canonical remote, with a model **family** this machine's catalog resolves: worktree only, `fullAuto`, origin assistant. An optional `prompt` is sent in the same call, and a send that fails is reported beside the created session, never instead of it. `requestId` makes a retry idempotent. | Actions not accepted; policies not accepted; unknown or ambiguous project; unknown model; creates per hour; in-flight cap. |
| `POST /api/peer/sessions/{id}/send` | Enqueue with origin assistant and optional policy. Answers the `MessageDelivery`. | Actions not accepted; archived; main worktree; not `fullAuto`; rate. |
| `GET /api/peer/events?since=N&wait=S` | This credential's outbox rows after `since`, held open up to 25s when there are none. Answers `{events, latest}`. | — |
| `POST /api/peer/sessions/{id}/follow` | Subscribes the credential's server to a session it did not start. A read, not gated. | Not found. |
| `GET /api/peer/sessions/{id}/transcript` | The recent transcript (the summariser's own rendering, bounded) so the acting server can summarise. A read, not gated. | Not found. |
| `GET /api/peer/sessions/{id}/facts/{kind}` | `branch`, `delete`, `busy` or `settings`, read fresh — what a card is judged on. A read, not gated. | Unknown kind. |
| `POST /api/peer/sessions/{id}/models/resolve` | A spoken model family against this machine's catalog. | Unknown model, with the families there are. |
| `POST /api/peer/sessions/{id}/do/{verb}` | One uncontained session verb a person accepted on the acting server's card: `assistant.PerformProposal` re-runs the card's check on this machine's facts, then executes. A refusal there is reason `outcome` with the word the card shows. | Actions not accepted; not a session proposal verb; actions per hour; stale facts perform nothing. |

Refusals answer `{error, reason}` with a stable `reason` (`peer.Reason*`), so the
acting side says each in its own words. Every refusal is logged.

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
  list, events and findings only. `[peer] accept-policies` separately gates sends
  and creates that carry a policy, so a machine can take asked-for work but no
  autonomy.
- **Uncontained verbs execute only on a passing owner-side re-check.** A card
  accepted at home narrows what happens; it never widens it.

### The event feed is a log

News for a paired server is an **outbox**: `peer_follows` records that a
credential sent to or created a session, and `peer_outbox` holds one row per
follower for each report and turn end, stamped with a `seq`. Rows are never
marked read. The follower keeps its cursor and polls
`GET /api/peer/events?since=`, so a poll that is lost, retried or duplicated
changes nothing, and a server asleep for a day catches up in one read. `latest`
lets a follower with no cursor start from now rather than from the whole
retention window. Rows age out after seven days, which is the only delete.

A long poll rather than a socket, on purpose: the same cursor semantics, the
credential stays in a header instead of a ticket, revocation is judged on every
poll, and no socket tracking or origin rule is involved. A waiting poll wakes the
moment a row is written.

Session state is not in the feed. The acting side re-lists
(`GET /api/peer/sessions`) when it needs state; the feed carries what a list
cannot show after the fact: that a turn ended and how, and what the agent
reported.

## Reports belong to the owner

`AssistantReport` is registered on **every** server regardless of feature flags
(`peerAwareReporter`). A report goes to the peer outbox for every paired server
following the session (rate-limited per session), and to the local service or
registry when there is one. The agent is told it was kept if either kept it: a
local "nobody is following" must not tell it to stop reporting to someone
listening from another machine. This fixes
the prompt that names a missing tool, and it makes a report a fact about the
session wherever it is read from.

The instruction's wording ("Someone is listening") assumes a live voice call and
is being reworked separately; it must stay true for a report read later in a
thread or journal.

## The acting side

- **`SessionBrief` becomes `Locate(id) → (row, owner)`**, where owner is local or
  a machine id. Verbs route through a per-machine `PeerActions`, the server-side
  counterpart of the browser's routing facade. The local path is unchanged.
- **One event poller per paired machine**, holding a durable cursor
  (`peer_cursors`) and never replaced, the rule the browser's per-machine clients
  already follow.
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

Every machine runs one (`internal/steward`, a pass a minute), from serve's
production block, never a constructor. It has
**no model**, writes only its own findings table, and reads what the existing
collectors already know. Its name is *steward*; **never `janitor`**,
which is already `internal/janitor`, the disk planner.

**A finding is a fact, not a sentence.** A closed union of kinds, each with a
severity, facts JSON, an `opened_at`, a `resolved_at`, a resolve rule, and a
remedy. The assistant writes the words. A new kind is a code change that comes
with its resolve rule and a test, on the `REST_GLYPH` precedent: a closed set
forces each kind to choose rather than inherit a blank.

| Kind | Source | Resolves when | Remedy |
|---|---|---|---|
| `cli-signed-out` | `usage.Collector` auth state | auth reads OK again | hand: "run `claude auth login` on zbook" |
| `disk-low` | free space under `LOW_DISK_BYTES` (3 GiB), the meter's own floor | above it | reclaim when finished sessions hold space, else hand |
| `loop-paused` | scheduler auto-pause | the loop is re-enabled or edited | hand |
| `session-blocked-long` | pending approval or question for 30 minutes (the steward keeps the clock) | answered | hand: "needs a screen" |
| `update-waiting` | the release checker says behind | applied | update (costs the turn in flight) |
| `backup-failing` | no periodic backup file for three intervals | a new one lands | hand |

What is deliberately **not** a finding: a boot reap that found orphans, a disk
gauge at a high but ordinary level, an idle eviction. Those are bookkeeping, not
news, and a finding that is always open teaches the reader to ignore findings —
the gauge rule from usage.md.

**Two kinds of remedy, never a button that can only fail.** A remedy the owner
can execute becomes a proposal card executed through the guard. A remedy only a
person at that machine can perform is said in words naming the machine.

**Each machine shows its own findings.** A stethoscope leads the usage cluster's
trigger in its own footer, on the footer's marks-not-sentences rule, with the
rows one click away (`lib/steward.ts`, `GET /api/steward/findings`). Only the
kinds nothing else on that line or a row already says earn the mark:
`cli-signed-out`, `loop-paused`, `backup-failing`. `update-waiting` is
`UpdateMark`, `disk-low` is the amber meter, `session-blocked-long` is the row's
triangle. So a machine reports its health even while the acting server is down
or unreachable.

## Failure modes

| Situation | Behaviour |
|---|---|
| Owner asleep | Listed as not answering; sends refuse naming it; reports and findings wait on the owner and replay on reconnect. |
| Acting server down | Owners keep running; findings show in each machine's own footer; nothing is acted on. |
| Owner on an older release | List-only, said by name. |
| `accept-actions` off | List, events and findings work; every act refuses naming the setting. |
| A poll drops mid-turn | The next poll with `since` returns the turn end; applying it twice is a no-op on the acting side. |
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

- **P1, owner side. Built.** Peer credential and the route matrix (80ab3935);
  list, create and send behind the guard (60a2dfbd); `AssistantReport` on every
  server, the outbox and the event poll (50d29c89).
- **P2, acting side. Built.** `peerlink` mints, holds and rotates the credential
  (e6dc7c34); the directory, dispatcher and verbs act by reach (faa9533b); the
  poller feeds the journal and the call (4f54f825); the voice call acts the same
  way (e5c3e2ed).
- **P3, budgets and proposals across machines. Built.** 55d3c156, 13bfb221.
- **P4, the steward. Built.** Findings, publication and the journal kind
  (4232feba); the footer mark (c9c6b439); the disk floor shared with the meter.
- **P5, the record.** This document, assistant.md and CLAUDE.md describe what
  shipped. **The transitional full-bearer listing stays** until no paired
  release predates the peer surface: removing it now would stop this server
  listing zbook at all, which is the break the expand/contract rule exists to
  prevent.

## Settled on 2026-09-14

1. **The steward's name** is *steward*.
2. **Where the assistant lives:** whichever server enables it, one per account,
   with the serve warning above.
3. **The cross-machine boundary** is accepted as described under Security, gated
   per machine by `accept-actions`.
4. **Autonomy on peers:** policy-carrying work is refused unless the owner sets
   `accept-policies`; asked-for work needs only `accept-actions`.
