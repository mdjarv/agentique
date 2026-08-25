# Multi-machine

One UI controls agentique servers on several machines at once. The shape is
client-side fan-out to sovereign servers, not federation: every server keeps sole
ownership of its database, worktrees, CLI subprocesses, reaper and scheduler. The
server that serves the SPA is the **primary**; the others are **remote machines**.
A shared tailnet is the assumed transport, but nothing depends on it. Any address
the client can reach works.

The first half of this document is built and running. **Presentation sync**, at
the end, is designed and not implemented.

## Machine identity

Each server has a stable identity: a UUID in `<datadir>/machine-id`, a P-256
signing key in `<datadir>/machine-identity-key.pem`, and a human label from
`machine-label`, else `PRETTY_HOSTNAME`, else the hostname. All three resolve in
`serve.go` and never in `server.New`, because constructors have no filesystem side
effects.

Corrupt key material is fatal. Silently replacing it would turn an attack or a
disk failure into an unnoticed identity change.

`GET /.well-known/agentique/environment` is the unauthenticated "is an agentique
server here, and which one" probe, carrying machine id, identity key, label,
version, platform and capabilities. Capabilities are optional booleans and a
client treats a missing key as unsupported, so features degrade one at a time
without version comparisons. `/api/health` repeats the identity fields for
authenticated consumers.

Clients pin both the machine id and the identity key. Before sending a bearer or
opening a socket, the client sends a fresh random nonce to the identity-proof
endpoint and verifies the P-256 signature, so a saved machine cannot silently
reattach to a different server at the same URL. Verified TLS is still mandatory
for remote URLs: the proof binds identity, TLS protects the rest of the exchange
from a live relay.

## Pairing and auth

The network is transport only. Even on a trusted tailnet, **the server is the
authorization boundary.** A tailnet is a network boundary, and those are not the
same thing.

`agentique pair` mints a one-time human-typeable token: 12 characters, five-minute
TTL, consumed atomically in SQL. The CLI authorizes against the *running* server
over HTTP by presenting the data dir's `admin-secret`, deliberately rather than
becoming a second writer to the live database. An admin web session can mint one
too.

`POST /api/auth/pair` exchanges the token for a 30-day bearer session and returns
its public session id plus a signed proof of the caller's nonce. Re-pairing sends
the old session id so the server replaces that user's bearer instead of
accumulating live ones.

The auth middleware accepts `Authorization: Bearer` as a peer of the session
cookie. **An invalid bearer never falls back to the cookie**: the credential you
present is the one that gets judged.

WebSockets never carry a bearer in a URL. Clients redeem a one-time five-minute
ticket, and redemption re-validates the session against the database, so revoking
a bearer also kills pre-minted tickets. Established sockets are tracked by session
and closed on revocation or expiry. Outstanding tickets and live sockets are both
capped, so an unauthenticated or compromised client cannot grow memory without
bound.

`DELETE /api/auth/session` lets a bearer revoke itself without granting it access
to administrative session management.

Cross-origin: same-origin and configured RP origins may use cookies. Other origins
fail closed unless the request explicitly carries a bearer, and an accompanying
cookie is ignored rather than used as fallback authority. Only the small public
descriptor and pairing surface is otherwise cross-origin. JSON mutation endpoints
reject browser-simple content types. A WebSocket from another origin must redeem a
valid one-time ticket.

`--disable-auth` is accepted only on a loopback listener, and a request with a
non-loopback Host is rejected to stop DNS rebinding. Such a server advertises
`pairing: false` and is not a multi-machine peer.

## The machine catalog

Paired machines are **account state, not device state**. The catalog lives in the
primary's `machines` table, guarded by the full-access role, and holds each
remote's bearer token, public session id and pinned identity key. Any full-access
client that signs into the primary inherits every paired machine. Browser
localStorage keeps only public metadata; bearer tokens live in memory and are
reloaded from the primary after authentication.

Catalog writes accept only HTTPS remote origins (HTTP is limited to loopback),
reject URL credentials, paths, queries and fragments, and bound every field.
Presentation-only edits use a separate PATCH surface that cannot overwrite
credentials.

Removing a machine proves its pinned identity and revokes the remote bearer first.
A failure keeps the local row, so an unreachable machine cannot leave a silently
orphaned credential. But a *refused* revoke is tolerated: a credential the remote
already rejects is already revoked, and failing there would strand an entry that
can be neither used nor removed.

This host's own name and icon take the same full-access guard as the catalog,
because that rewrites how this machine identifies itself to every client.

`machines.token` is the one credential that stays plaintext, necessarily: it is
what this server *presents* to the remote, so it has to be recoverable. Everything
inbound is a SHA-256 digest instead. The outbound bearers are protected only by
the data directory being owner-only and the database 0600, which does **not**
protect them from an agent running at the same uid.

## Cross-machine project identity

`projects.remote_url` holds the canonical key of the checkout's primary git
remote, computed at project create and re-resolved by a startup refresh.

Remote preference is `upstream`, then `origin`, then alphabetically first.
Normalization makes SSH and HTTPS clones of one repo equal
(`git@github.com:Org/Repo.git` matches `https://github.com/org/repo`), stripping
credentials, ports, case and `.git`; GitHub semantics are the deliberate bias. A
project rooted in a repo subdirectory gets the relative path appended, as in
`host/org/repo::packages/ui`, so monorepo-subdir projects never merge with each
other while the same subdir on another machine still matches. An empty key, which
means no usable remote, never groups.

## Tailnet discovery

`GET /api/machines/discover` enumerates online tailnet peers from
`tailscale status --json` and probes each concurrently for the well-known
descriptor: https then http, on the server's own port plus the 9201 and 19201
defaults, with TLS unverified because discovery only reads the public descriptor.

Discovery is a **hint layer**. It feeds Add-machine suggestions and grants
nothing; pairing still authorizes. Probe bodies, descriptor fields, redirects and
deadlines are all bounded, and an insecure discovery hint is never an acceptable
remote address for pairing.

## Client architecture

`lib/machines/registry.ts` owns one `WsClient` per machine, created once and
reconnected **in place**, never replaced. Components hold clients through a ref,
so a swapped instance would strand them on a dead socket. Remote sockets resolve
their URL asynchronously per attempt with a fresh one-time ticket; a resolution
failure schedules ordinary backoff, 500ms doubling to a 30s cap, so a sleeping
laptop costs about two probes a minute.

**A refused credential never opens a socket.** It fails at the ticket mint, so
`ws.onclose` and therefore `onDisconnect` never run. Diagnosis hangs off
`WsClient.onAttemptFailed` as well, or the one fault worth naming,
`credential-rejected`, could never be recorded and the machine would pulse
"reconnecting" forever with no re-pair button.

A passing identity proof clears only the faults it disproves. Identity says who
answered, never whether they still accept our credential, and `machineFetch`
re-proves identity on every retry, so clearing a credential fault there would
erase the diagnosis a second after it was made. A rejected credential is cleared
by proof of the opposite: a connection that authenticated, or a re-pair.

`useWebSocket()` returns a routing facade. **Requests** dispatch to the machine
that owns the entity in the payload: `projectId` directly, or `sessionId` through
its session's project to that project's machine, and anything else to the primary.
**Subscriptions** fan in from every machine's socket, including machines paired
later. **Lifecycle** (connection state, onConnect, reconnect) delegates to the
primary only.

Remote machine lifecycle is per-machine: each reconnect re-syncs only that
machine's projects and sessions. A flaky remote must never trigger the primary's
reconnect-and-refetch path or reset primary streaming state.

REST resolves the same way. Content that loads through an element `src`, such as
an image preview, is fetched as a Blob into an object URL, because an `<img>`
cannot carry the bearer header a remote machine needs.

### Data model

Only `Project` carries a client-side machine tag, applied at ingest; sessions
derive theirs through the project. Entity ids are globally unique UUIDs, so store
maps need no re-keying.

Remote project slugs get a stable machine suffix at ingest, like
`agentique~ab12cd34`, so slug-addressed routes stay unambiguous. The primary's
slugs are never rewritten and every pre-existing URL stays valid.

### Grouping is a view, never routing

`groupProjects` merges same-remote projects across machines into one logical
project. **Every command still targets one physical machine, project and
session.** The primary's copy, when present, is the representative and drives the
group's name, colour, icon, folder, star and navigation slug.

`useLogicalProjects` turns that into the row view-model every project-listing
surface consumes, so no surface can quietly go back to listing checkouts. The
new-session palette, the `/projects` inventory and the Run-in menu all list
repos. Thread rows take a session's label, colour and icon from its
representative while routing by its own qualified slug.

A row is `away` only when *every* member's machine is away, so a repo that also
lives locally is always launchable, and an unknown machine id counts as away
rather than reachable. Where a repo spans machines, `/projects` gives each
checkout its own launchable line with machine, path and reachability.

### Launching names a machine

Listing is logical; **launching is physical**, and `launch-targets.ts` is the
seam. `launchTargets` flattens the logical rows into the checkouts a launch can
actually name, so a repo held on three machines offers three targets rather than
silently taking the representative — which is chosen for presentation, not for
being reachable. A target carries both slugs and they are not interchangeable:
`slug` routes to the checkout (a remote's is machine-qualified), `rowSlug` is the
representative's and is the only one presentation may read.

`ProjectLaunchPicker` is that list as a searchable palette (the prompt card's
target picker). The machine is part of the search text, so "agentique zbook" is
one query rather than a repo pick followed by a second control, and a
single-machine repo shows no machine chrome at all. An away machine's row is
present but refused — where the repo lives is worth knowing even when it is
asleep.

`preferredMember` is the other half: when nobody has picked, prefer a reachable
checkout over the representative. A repo that lives only on two remotes takes its
representative from list order, so defaulting to it blocks the composer on a
sleeping machine while a live copy sits one entry down. The new-session panel
fills its Run-on default in that way and re-derives it as machines come and go;
an explicit pick always wins.

A remote machine's own `favorite` flag is that host's opinion and is ignored here;
starring a merged row writes to the representative. Sessions from every member
interleave under one identity, each carrying its machine glyph. The new-session
page shows a Run-on control when the group spans machines, defaulting to the
checkout it was opened for (see `preferredMember` above for the one case that
overrides), and the new-project dialog offers a machine picker whose path input,
directory browser and validation all run against the chosen machine.

### Offline behaviour

Machines come and go; a laptop suspends mid-session. Each machine's last-known
projects and session metadata persist to a per-machine localStorage cache,
snapshotted after every successful sync and again at disconnect, hydrated
merge-only at startup. That machine's half of the sidebar stays visible and
navigable while it is unreachable, with its connection state on badges and chips.

Snapshots **sanitize live-ness**: running becomes idle, connected becomes false,
pending approvals are stripped, so a cached row never fakes a live pulse. The live
re-sync on reconnect is authoritative and replaces everything cached. Removing a
machine clears its cache and drops its sessions from the stores.

The cache is a serialization of an internal type, so it drifts whenever that type
changes, and a stale cache renders wrong rather than failing loudly. It carries
`CACHE_VERSION`. Bump it on any rename, migrate the previous shape, and refuse a
version from the future rather than hydrating a guess.

## Invariants

- Verify the pinned machine id and signing key before sending credentials, on
  every pair and connect path. Refuse mismatches.
- The server is the authorization boundary. Reachability never substitutes for
  auth, and discovery grants nothing.
- Bearer tokens never appear in URLs. Sockets use one-time tickets whose
  redemption re-checks the database.
- Nor in argv, nor in a group-readable file. The data dir is owner-only and the
  database 0600, because it stores every remote's bearer in plaintext.
- Inbound credentials are digests. Only outbound `machines.token` is recoverable,
  and nothing but the directory mode guards it.
- Revocation is live: it closes established sockets, and removing a catalog entry
  revokes the remote bearer before forgetting it locally.
- An explicit credential never falls back to another.
- Primary lifecycle isolation: a remote reconnect re-syncs only its own machine.
- Grouping is a view. Commands always target a physical entity.
- Identity and secret files are created from `serve.go`, never in constructors.
- Cached data never impersonates live data.
- Auth-disabled means loopback-only. Network listeners always authenticate.

## Deliberately out of scope

Teams and channels are per-server, because `messages` is a per-server source of
truth, so cross-machine channels would be a federation feature. The brain is
per-machine; sharing memory across machines per canonical repo is deferred until
the core is proven.

---

# Presentation sync

**Status: designed, not implemented.** Seven decisions settled 2026-08-23 after
review. Phases M1 through M5 below, none started. This section extends the
architecture above, which stays the record for pairing, routing and offline
behaviour.

## Goal

A repo starred on the laptop at the office is starred on the home desktop that
evening, on the VPS, and on the phone. Two-way replication between sovereign
servers, with no machine in charge and nothing that stops working when one box is
down.

## The topology it has to survive

| Node | Role | Awake | In the sync |
|---|---|---|---|
| laptop | serves its own UI | workweek, office hours | peer |
| desktop | serves its own UI | evenings, weekends | peer |
| VPS | serves the PWA | always | peer, and the always-on rendezvous |
| phone | client only | whenever | not a participant |

Two facts drive the design. **The volatile machines are almost never awake
together**, so peers exchanging only their *own* edits would leave the laptop and
the desktop permanently out of step. And **the VPS is always up**, so it is the
node every other node reliably meets.

Therefore the protocol **gossips**: every exchange ships everything a machine has
learned, including changes authored by a third machine. A star set on the laptop
at 14:00 reaches the desktop at 19:00 by way of the VPS, without those two ever
being awake together.

The VPS is a rendezvous, not a master. Nothing in the protocol knows it is
special, it holds no authority, and if it dies the other two still converge
whenever they overlap. The phone has no database and no registers: it points at
the VPS, reads what has converged, and caches it.

## What syncs

The line is **presentation versus execution**. How a thing looks in a list is an
opinion and should travel. Where code runs, which worktree, what the CLI may do,
those belong to the machine that bears the consequences, and replicating them
would be a correctness bug.

| State | Syncs | Note |
|---|---|---|
| Star, pin | yes | The thing this exists for. |
| Project name | yes | "AllTix API" versus "alltix-api" is the same divergence. |
| Colour, icon, folder | yes | Pure display. |
| Machine nickname and icon | yes | Reverses migration 046's host-local rule, for peers. |
| Machine catalog entries | identity only | Never tokens. |
| Project slug | derived, not replicated | Re-derived locally from the synced name. |
| Project path | never | A filesystem fact of one machine. |
| Model, presets, max sessions | not yet | Behaviour, not looks. Deliberately out of M1. |
| Sessions, worktrees, git state | never | Sovereign. This is the federation line. |
| Bearer tokens, admin secret | never | Credential fan-out multiplies blast radius. |
| Brain and memory | never | Its own decision, already deferred. |
| Panel widths, drafts, theme | never | Per-device, not per-account. |

`projects.sort_order` is excluded. Drag-order is an array, and arrays are what LWW
handles worst. If it is ever wanted it needs fractional indexing, not another
register.

## Naming things across machines

A register can only replicate if both machines agree what it is *about*. Project
ids are per-server UUIDs, so the key is the canonical git remote, the same one
grouping already merges rows by.

```
entity_key := "repo:" + projects.remote_url    // "repo:github.com/mdjarv/agentique"
              "machine:" + machine_id
              "host:self"                      // this server's own label and icon
```

**No remote means no cross-machine name, so that project's presentation stays
local, permanently.** There is nothing to fix that with.

Known consequence, deferred: two checkouts of one repo *on one machine* share one
presentation record. Uncommon and untested; M1 accepts it. The escape hatch needs
no schema change, just a per-project `local_only` register field.

## The replication model

Presentation is a set of independent scalars with no invariants between them,
which is the one shape where the simplest CRDT is also the right one: a
**last-writer-wins register per field**, keyed by entity and field. Fields merge
independently, so starring on the VPS and recolouring on the laptop both survive.
Record-level LWW would silently drop one.

**Two clocks, and conflating them is the bug to avoid.**

```
hlc   → total order over VALUES    (who wins; identical on every machine)
seq   → local arrival order        (what to send; different on every machine)
cursor[peer] = last seq this server has handed to that peer
```

If the cursor walked the HLC, a register forwarded from a machine that woke up
late would already be *behind* the cursor its peer was given, and would never
ship. Not lost, just never delivered, with everything looking healthy. So `seq` is
assigned on every local write **and every merge**, meaning "when did I learn
this", which is exactly what makes third-party registers forward.

The HLC is physical milliseconds, plus a same-millisecond counter, plus machine id
as final tiebreak. Wall-clock LWW would let a machine with a drifted clock win
every conflict forever, invisibly.

Unstarring is a value change, so registers cover it. Removing a machine from the
catalog is a real delete and needs a tombstone with its own HLC, or a peer that
still holds the row resurrects it. Tombstones are kept, not vacuumed; there are
tens of these.

Exchange is push when connected, pull on reconnect, with the per-peer cursor
healing the gap. Symmetric and idempotent, so a failed half is simply retried.
Forwarded registers keep their original HLC and origin, because relaying is not
authoring, and the desktop still knows that star came from the laptop at 14:00.

This is a log-compacted topic. Entity-and-field is the compaction key, `seq` is an
offset, the sent cursor is a consumer offset, and idempotent merge plus retry is
at-least-once with idempotent consumers. That is the part of the Kafka model worth
having, and it costs three columns and an index rather than a broker.

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

Values are TEXT because the field set is heterogeneous and tiny. All timestamps
are UTC RFC3339 seconds, because SQLite compares TEXT lexicographically.

## Read and write path

A project with a `remote_url` and an existing register for that field uses the
register. Otherwise it uses the project's own column, exactly as today.

So a no-remote project behaves precisely as it does now, and the wire shape the
frontend consumes does not change: the server resolves before serialising.
`logical-derive.ts` gets *simpler*, because with presentation attached to the repo
both copies already carry identical values and the representative only decides
routing.

Writes go to the register for any project with a remote and to the column
otherwise, funnelled through one `presentation.Set` that stamps the HLC and
assigns the next `seq`.

**A landing `name` register re-derives the local slug.** Replicating the slug as a
value cannot work: it carries a per-server uniqueness constraint, and an LWW
register cannot honour a constraint that depends on other rows, so the register and
the database would disagree permanently. Deriving enforces uniqueness where it is
defined and still matches URLs everywhere except on collision. Because that can
change the URL of a project someone is looking at, the project layout route
rewrites the address in place when a rename arrives live, and a cold load of a
dead slug redirects to `/` rather than to a dead-end status page.

**Backfill and first union.** Each server seeds registers from its own columns at
migration. The first exchange between a given pair is a **union, not a merge**:
booleans OR together, non-empty text beats empty. Pure LWW would decide it by
whichever backfill ran second, so a carefully-curated set of stars could vanish
because the other machine wrote its empty values a minute later. Every exchange
after that is ordinary LWW, and `unioned_at` makes it once-only.

## Transport and trust

**Mutual pairing.** Today `agentique pair` mints a token on the machine being
added, and that machine never learns who redeemed it. The extension: the pairing
exchange also mints a credential for the other side and returns it with the
pairing machine's identity, label and base URL. One gesture, both sides hold one
credential each, and each verifies the other's pinned signing key with a fresh
challenge before sending credentials.

**Pair all three pairwise.** Two edges through the always-on node would converge
everything; the third costs one command and covers "VPS down" and "both on the
same LAN". The protocol has no notion of a hub, so the deployment shape stays
changeable. Because each machine minted its own credentials, **no secret ever
moves between machines**, which is why replicated catalog entries carry identity
only.

**The peer credential is scoped** to `auth_sessions.kind = 'peer'`, accepted only
on `/api/sync/*` and the public descriptor. Be honest about what that buys:
driving sessions from any UI needs a full bearer in each direction anyway, so once
machines are mutually paired for driving, scoping removes no risk that is not
already present. Take it for **separable revocation** (turn off replication
without unpairing) and for not authenticating an unattended server-side loop with
the same credential a browser uses. Not for blast radius.

Both sides dial, and the outbound path already exists, though sync must use
verified TLS rather than discovery's deliberately-unverified probe.

Client-mediated relaying, where the browser carries deltas and needs no new
inbound trust, was rejected: it converges only while a client is open with both
machines reachable, which for an office-hours laptop and an evenings desktop is
close to never.

### Security gates

M2 and M3 do not ship until these have regression tests at the real HTTP boundary:

- A `kind=peer` credential is accepted only on `/api/sync/*`. A route-matrix test
  presents one to every other API family, including WebSockets, machine catalog,
  projects, sessions, files, git, MCP and administrative auth, and expects denial.
  Adding a route means updating that explicit matrix.
- Sync accepts only known entity-key prefixes and field names. UUIDs, origins,
  booleans, text lengths, batch count and total request bytes are bounded before a
  transaction begins. Unknown fields are rejected, never retained for a future
  version. One exchange is capped at 256 registers and 1 MiB.
- Incoming HLC values need non-negative milliseconds, a counter from zero through
  the signed 32-bit maximum, and a physical time no more than five minutes ahead
  of the receiver. Counter overflow advances the physical millisecond and never
  wraps. Tests cover far-future clocks, negative values, maximum counters and
  repeated same-millisecond writes.
- `origin` is provenance, so the author signs a canonical encoding of the entity
  key, field, value, HLC and origin with its pinned machine identity. Forwarders
  preserve that signature. Unknown origins and invalid signatures are rejected. If
  the UI ever displays unsigned provenance, it labels it as an unverified relay
  assertion rather than authorship.
- A cursor is committed only through the highest row actually encoded in a
  successful response, and received registers plus the receive cursor commit in one
  transaction. Empty, partial, retried, duplicated and size-limited batches each
  get a test. A cursor supplied by a peer can never make this server skip rows it
  has not emitted.
- First-union state is durable protocol history, independent of a removable
  catalog row. Unpairing and re-pairing the same machine identity must not run the
  lossy first-union rule a second time. Tests exercise interrupted union, retry,
  unpair and re-pair.
- Outbound sync uses a no-redirect HTTP client, verified TLS, the exact pinned
  machine id and signing key, bounded response bodies and deadlines. The discovery
  client and its insecure TLS hint path are never reused.

## Failure modes

| Situation | Behaviour |
|---|---|
| Peer unreachable | The local write applies immediately and is retried on next connect. Never blocks. |
| Laptop and desktop never overlap | They converge transitively through the VPS. |
| VPS down while both sleep | Nothing converges, nothing is lost. The direct edge covers overlap. |
| Laptop offline a week | One pull on wake reconciles everything. |
| Silent overwrite | Origin and time let the row say "renamed on zbook, 2h ago". Not undo. |
| Clock skew | The HLC absorbs it and both sides compute the same winner. |
| Renamed elsewhere while viewing | The slug re-derives and the route rewrites the URL in place. |
| Peer credential revoked | Sync fails closed and surfaces as a machine fault. |
| Machine unpaired | The tombstone propagates; registers stay, keyed by repo, so re-pairing is not a fresh start. |
| Repo remote changes | The entity key changes with it, so presentation appears to reset. Rare. |
| Sync loop or storm | Cursors are monotonic and merges idempotent, so a known value is a no-op. |

## Invariants this design must keep

- **Execution never replicates.** Sync moves opinions, never work.
- **Delivery is by local `seq`, conflict is by HLC.** Collapsing them breaks
  forwarding silently.
- **Forwarding preserves authorship.** Relayed registers keep their original HLC
  and origin.
- **No node is special.** The always-on machine is a rendezvous by availability,
  never by protocol, and any two peers must converge without it.
- **The peer credential stays scoped.** A future feature "just needing" one more
  route is a design change, not a patch.
- **Identity is pinned in both directions** and a mismatch fails closed.
- **Provenance is signed end to end.** A relay cannot forge the author shown to
  the user.
- **Cursors describe delivered data, never peer claims**, and cursor advancement
  and register application are transactional.
- **Untrusted input is bounded before merge.** Fields are allowlisted; batches,
  strings, clocks, counters and bodies have hard limits.
- **First-union history survives unpairing.** Re-pairing cannot replay the
  migration rule.
- **Slugs are server-local.** Nothing replicated may rewrite a route directly.
- **Local writes never block on a peer.**
- **Merges are deterministic and idempotent.**
- **No remote, no sync**, and the UI says so rather than implying it will catch
  up.

## Phases

- **M1 — registers, local only.** Table, HLC, resolver, write path, backfill,
  slug re-derivation. No network. Ships invisible: behaviour identical, every
  existing test still passes. Commits to nothing, because a register table with
  one writer *is* the centralised design, so the topology stays deferrable.
- **M2 — mutual pairing and the scoped peer credential.** Standalone and testable
  without any sync: pair two servers, confirm each holds a scoped credential,
  confirm it is rejected on every non-sync route.
- **M3 — the exchange.** Push and pull, cursors, union on first sync, forwarding.
  Verified with **three** servers on isolated `AGENTIQUE_HOME` directories and one
  clone each of the same remote. The test that matters is the topology's own: star
  on A, kill A, wake C, confirm C learned it through B.
- **M4 — catalog sync.** Machine identity, labels, icons, tombstones. May turn out
  to be decoration: with all three paired pairwise, each catalog is already
  complete.
- **M5 — provenance UI.** "Renamed on zbook, 2h ago", sync state in machine
  settings, a local-only badge for no-remote projects.

M1 and M2 are independent and can run in parallel worktrees. M3 needs both.

## Rejected

**Host-local presentation**, where each host owns how it displays things and
nothing crosses. Simplest, and the original proposal. Rejected because the same
repo then reads differently in each of four UIs.

**Hub-mastered account state**, an orchestrator with workers. Deletes this whole
design, and was rejected for the single point of failure: the laptop's sessions
must stay reachable locally when the VPS is down, and the reverse. Not foreclosed,
since the schema is identical under both models.

**Kafka as infrastructure.** The model is mirrored deliberately; the broker is not
worth hosting for a table that changes a few dozen times a week.
</content>
