# Presentation sync — same stars on every machine

Status: **designed, not implemented.** All seven decisions settled 2026-08-23
after review (proposal artifact `22e45640-3598-4fce-b791-6851d6eb0e02`);
phases M1–M5 below, none started. Extends `docs/multi-machine.md`, which
stays the architecture of record for pairing, routing and offline behaviour.

## Goal

A repo starred on the laptop at the office is starred on the home desktop
that evening, on the VPS, and on the phone. Two-way replication between
sovereign servers — no machine in charge, nothing that stops working when one
box is down.

## The topology this has to survive

| Node         | Role                          | Awake                | In the sync            |
| ------------ | ----------------------------- | -------------------- | ---------------------- |
| zbook        | laptop, serves its own UI     | workweek, office hrs | peer                   |
| home desktop | workstation, serves its own UI| evenings, weekends   | peer                   |
| VPS          | review machine, serves the PWA| always               | peer (always-on rendezvous) |
| phone        | client only — peek, prompt, go| whenever             | **not a participant**  |

Two facts drive the design. **The volatile machines are almost never awake
together**, so peers exchanging only their *own* edits would leave zbook and
the desktop permanently out of step. And **the VPS is always up**, so it is
the node every other node reliably meets.

Therefore the protocol **gossips**: every exchange ships everything a machine
has learned, including changes authored by a third machine. A star set on
zbook at 14:00 reaches the desktop at 19:00 by way of the VPS, without those
two ever being awake together.

The VPS is a **rendezvous, not a master** — nothing in the protocol knows it
is special, it holds no authority, and if it dies the other two still
converge whenever they overlap. The phone has no database and no registers:
it points at the VPS, reads what has converged, and caches it.

## What syncs

The line is **presentation versus execution**. How a thing looks in a list is
an opinion and should travel. Where code runs, which worktree, what the CLI
may do — those belong to the machine that bears the consequences, and
replicating them would be a correctness bug.

| State                          | Syncs                  | Note                                                    |
| ------------------------------ | ---------------------- | ------------------------------------------------------- |
| Star / pin                     | yes                    | The thing this exists for.                               |
| Project name                   | yes                    | "AllTix API" vs "alltix-api" is the same divergence.     |
| Colour, icon, folder           | yes                    | Pure display.                                            |
| Machine nickname & icon        | yes                    | Reverses migration 046's host-local rule, for peers.     |
| Machine catalog entries        | identity only          | Never tokens — see below.                                |
| Project slug                   | **derived, not replicated** | Re-derived locally from the synced name.            |
| Project path                   | never                  | Filesystem fact of one machine.                          |
| Model, presets, max sessions   | not yet                | Behaviour, not looks. Deliberately out of M1.            |
| Sessions, worktrees, git state | never                  | Sovereign. This is the federation line.                  |
| Bearer tokens, admin secret    | never                  | Credential fan-out multiplies blast radius.              |
| Brain / memory                 | never                  | Its own decision, already deferred.                      |
| Panel widths, drafts, theme    | never                  | Per-device (`agentique:ui`), not per-account.            |

`projects.sort_order` is excluded: drag-order is an array, and arrays are what
LWW handles worst. If it is ever wanted, it needs fractional indexing, not
another register.

## Naming things across machines

A register can only replicate if both machines agree what it is *about*.
Project ids are per-server UUIDs, so the key is the canonical git remote —
the same one `groupProjects` already merges rows by.

```
entity_key := "repo:" + projects.remote_url    // "repo:github.com/mdjarv/agentique"
              "machine:" + machine_id
              "host:self"                      // this server's own label/icon
```

`gitops.CanonicalRemoteURL` collapses SSH and HTTPS clones, appends
`::packages/ui` for a monorepo subdirectory, and yields `""` for a checkout
with no usable remote. **No remote means no cross-machine name, so that
project's presentation stays local, permanently** — there is nothing to fix
that with.

Known consequence, deferred: two checkouts of one repo *on one machine* (a
second clone, a worktree registered as its own project) share one
presentation record. Uncommon and untested; M1 accepts it. The escape hatch
needs no schema change — a per-project `local_only` flag is one more register
field.

## The replication model

Presentation is a set of independent scalars with no invariants between them
— the one shape where the simplest CRDT is also the right one: a
**last-writer-wins register per field**, keyed `(entity_key, field)`. Fields
merge independently, so starring on the VPS and recolouring on zbook both
survive; record-level LWW would silently drop one.

**Two clocks, and conflating them is the bug to avoid.**

```
hlc   → total order over VALUES    (who wins; identical on every machine)
seq   → local arrival order        (what to send; different on every machine)
cursor[peer] = last seq this server has handed to that peer
```

Why two: if the cursor walked the HLC, a register forwarded from a machine
that woke up late would already be *behind* the cursor its peer was given,
and would never ship. Not lost — never delivered, with everything looking
healthy. So `seq` is assigned on every local write **and every merge** ("when
did I learn this"), which is exactly what makes third-party registers
forward.

The HLC is physical ms + a same-millisecond counter + machine id as final
tiebreak. Wall-clock LWW would let a machine with a drifted clock win every
conflict forever, invisibly.

Deletes: unstarring is a value change, so registers cover it. Removing a
machine from the catalog is a real delete and needs a tombstone
(`deleted=true`) with its own HLC, or a peer that still holds the row
resurrects it. Tombstones are kept, not vacuumed — there are tens of these.

Exchange: push when connected, pull on reconnect, per-peer cursor heals the
gap. Symmetric and idempotent, so a failed half is simply retried. Forwarded
registers keep their original HLC and `origin` — relaying is not authoring,
and the desktop still knows that star came from zbook at 14:00.

**This is a log-compacted topic.** `(entity_key, field)` is the compaction
key, `seq` is an offset, `sent_cursor` is a consumer offset, idempotent merge
plus retry is at-least-once with idempotent consumers. That is the part of
the Kafka model worth having, and it costs three columns and an index rather
than a broker.

## Schema

```sql
CREATE TABLE presentation (
    entity_key  TEXT NOT NULL,
    field       TEXT NOT NULL,   -- favorite|pinned|name|color|icon|folder|label|deleted
    value       TEXT NOT NULL,   -- scalars as TEXT; "0"/"1" for booleans
    hlc_ms      INTEGER NOT NULL,
    hlc_ctr     INTEGER NOT NULL,
    origin      TEXT NOT NULL,   -- authoring machine_id (tiebreak + provenance)
    seq         INTEGER NOT NULL,-- LOCAL arrival order: drives delivery
    updated_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now')),
    PRIMARY KEY (entity_key, field)
);
CREATE UNIQUE INDEX idx_presentation_seq ON presentation(seq);

CREATE TABLE presentation_seq (      -- monotonic per-server counter
    id INTEGER PRIMARY KEY CHECK (id = 1),
    next INTEGER NOT NULL DEFAULT 1
);

CREATE TABLE peer_sync_state (
    machine_id   TEXT PRIMARY KEY,
    sent_cursor  INTEGER NOT NULL DEFAULT 0,
    recv_cursor  INTEGER NOT NULL DEFAULT 0,
    unioned_at   TEXT,            -- first-exchange union already done
    last_sync_at TEXT,
    last_error   TEXT NOT NULL DEFAULT ''
);
```

Values are TEXT because the field set is heterogeneous and tiny. All
timestamps are UTC RFC3339 seconds — SQLite compares TEXT lexicographically.

## Read and write path

1. Project has a `remote_url` and a register exists for `(repo:key, field)` →
   use it.
2. Otherwise the project's own column, exactly as today.

So a no-remote project behaves precisely as it does now and the wire shape
the frontend consumes does not change: the server resolves before
serialising, and `useLogicalProjects` keeps merging rows as it does today.
The representative rule in `logical-derive.ts` gets *simpler* — with
presentation attached to the repo, both copies already carry identical
values, and the representative only decides routing.

Writes go to the register for any project with a remote, to the column
otherwise. The WS handlers (`project.set-favorite`, `project.set-pinned`) and
the REST update handler funnel through one `presentation.Set(entityKey,
field, value)` that stamps the HLC and assigns the next `seq`.

**A landing `name` register re-derives the local slug** through the existing
`Slugify` + `uniqueSlug` path. Replicating the slug as a value cannot work —
it carries a per-server uniqueness constraint, and an LWW register cannot
honour a constraint that depends on other rows; the register and the database
would disagree permanently. Deriving enforces uniqueness where it is defined
and still matches URLs everywhere except on collision. Because that can
change the URL of a project someone is looking at, the project layout route
rewrites the address in place when the rename arrives live, and a cold load
of a dead slug redirects to `/` rather than today's dead-end status page.

**Backfill.** Each server seeds registers from its own columns at migration.
The first exchange between a given pair is then a **union, not a merge**:
booleans OR together, non-empty text beats empty. Pure LWW would decide it by
whichever backfill ran second, so a carefully-curated set of stars could
vanish because the other machine wrote its empty values a minute later. Every
exchange after that is ordinary LWW; `peer_sync_state.unioned_at` makes it
once-only.

## Transport and trust

**Mutual pairing.** Today `agentique pair` mints a token on the machine being
added; that machine never learns who redeemed it. The extension: the pairing
exchange also mints a credential for the other side and returns it with the
pairing machine's `machineId`, label and base URL. One gesture, both sides
hold one credential each, and each verifies the other's `machineId` on
connect.

**Pair all three pairwise.** Two edges through the always-on node would
converge everything; the third costs one command and covers "VPS down" and
"both on the same LAN". The protocol has no notion of a hub, so the
deployment shape stays changeable. Because each machine minted its own
credentials, **no secret ever moves between machines** — which is why
replicated catalog entries carry identity only.

**The peer credential is scoped**: `auth_sessions.kind = 'peer'`, accepted
only on `/api/sync/*` and the public descriptor. Note honestly what that does
and does not buy: driving sessions from any UI needs a full bearer in each
direction anyway, so once machines are mutually paired for driving, scoping
removes no risk that is not already present. Take it for **separable
revocation** (turn off replication without unpairing) and for not
authenticating an unattended server-side loop with the same credential a
browser uses — not for blast radius.

Both sides dial; the outbound path exists in `internal/machine`, though sync
must use verified TLS rather than discovery's deliberately-unverified probe.
Client-mediated relaying (the browser carrying deltas, needing no new inbound
trust) was rejected: it converges only while a client is open with both
machines reachable, which for an office-hours laptop and an evenings desktop
is close to never.

## Failure modes

| Situation                        | Behaviour                                                              |
| -------------------------------- | ---------------------------------------------------------------------- |
| Peer unreachable                 | Local write applies immediately; retried on next connect. Never blocks. |
| zbook and desktop never overlap  | Converge transitively through the VPS.                                  |
| VPS down while both sleep        | Nothing converges, nothing is lost; the direct edge covers overlap.     |
| Laptop offline a week            | One pull on wake reconciles everything.                                 |
| Silent overwrite                 | `origin` + time let the row say "renamed on zbook · 2h ago". Not undo.  |
| Clock skew                       | HLC absorbs it; both sides compute the same winner.                     |
| Renamed elsewhere while viewing  | Slug re-derives; the route rewrites the URL in place.                   |
| Peer credential revoked          | Sync fails closed and surfaces as a machine fault.                      |
| Machine unpaired                 | Tombstone propagates; registers stay (keyed by repo), so re-pairing is not a fresh start. |
| Repo remote changes              | Entity key changes with it — presentation appears to reset. Rare.       |
| Sync loop / storm                | Cursors are monotonic, merges idempotent; a known value is a no-op.     |

## Invariants a change here must keep

- **Execution never replicates.** Sync moves opinions, never work.
- **Delivery is by local `seq`, conflict is by HLC.** Collapsing them breaks
  forwarding silently.
- **Forwarding preserves authorship** — relayed registers keep their original
  HLC and `origin`.
- **No node is special.** The always-on machine is a rendezvous by
  availability, never by protocol; any two peers must converge without it.
- **The peer credential stays scoped.** A future feature "just needing" one
  more route is a design change, not a patch.
- **Identity is pinned in both directions**, and a mismatch fails closed.
- **Slugs are server-local.** Nothing replicated may rewrite a route directly.
- **Local writes never block on a peer.**
- **Merges are deterministic and idempotent.**
- **No remote, no sync** — and the UI says so rather than implying it will
  catch up.

## Phases

- **M1 — Registers, local only.** Table, HLC, resolver, write path, backfill,
  slug re-derivation. No network. Ships invisible: behaviour identical, every
  existing test still passes. Commits to nothing — a register table with one
  writer *is* the centralised design, so the topology stays deferrable.
- **M2 — Mutual pairing + scoped peer credential.** Standalone and testable
  without any sync: pair two servers, confirm each holds a scoped credential,
  confirm it is rejected on every non-sync route.
- **M3 — The exchange.** Push/pull, cursors, union-on-first-sync,
  forwarding. Verified with **three** servers on isolated `AGENTIQUE_HOME`
  dirs and one clone each of the same remote; the test that matters is the
  topology's own — star on A, kill A, wake C, confirm C learned it through B.
- **M4 — Catalog sync.** Machine identity, labels, icons, tombstones. May
  turn out to be decoration: with all three paired pairwise, each catalog is
  already complete.
- **M5 — Provenance UI.** "renamed on zbook · 2h ago", sync state in machine
  settings, local-only badge for no-remote projects.

M1 and M2 are independent and can run in parallel worktrees; M3 needs both.

## Rejected alternatives

- **Host-local presentation** (each host owns how it displays things, nothing
  crosses). Simplest, and the original proposal — rejected because the same
  repo then reads differently in each of four UIs.
- **Hub-mastered account state / orchestrator + workers.** Deletes this whole
  document, and was rejected for the single point of failure: zbook's
  sessions must stay reachable locally when the VPS is down, and vice versa.
  Not foreclosed — the schema is identical under both models.
- **Kafka as infrastructure.** The model is mirrored deliberately (see above);
  the broker is not worth hosting for a table that changes a few dozen times
  a week.
