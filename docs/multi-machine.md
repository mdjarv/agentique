# Multi-Machine

One agentique UI controls servers on several machines at once. The design is
client-side fan-out to sovereign servers — no federation: every server keeps
sole ownership of its database, worktrees, CLI subprocesses, reaper, and
scheduler. The server that serves the SPA is the **primary**; the others are
**remote machines**. A shared tailnet is the assumed transport, but nothing
depends on it — any address the client can reach works.

## Server side

### Machine identity

Each server has a stable identity: a UUID read-or-created in
`<datadir>/machine-id`, plus a human label (`[server] machine-label` /
`AGENTIQUE_MACHINE_LABEL`, else `PRETTY_HOSTNAME`, else hostname). Both are
resolved in `serve.go` — never in `server.New` (constructors have no
filesystem side effects) — and served on two surfaces:

- `GET /.well-known/agentique/environment` (unauthenticated): the universal
  "is an agentique server here, and which one" probe. Carries
  `{machineId, label, version, platform, capabilities}`. Capabilities are
  optional booleans; clients treat a missing key as unsupported, so features
  degrade per-feature without version comparisons.
- `/api/health` repeats `machineId`/`machineLabel`/`version` for
  authenticated consumers.

Clients pin `machineId` and verify it when pairing and connecting — a saved
machine can never silently reattach to a different server at the same URL.

### Pairing and auth

The network is transport only. Even on a trusted tailnet, **the server is the
authorization boundary** — a tailnet is a network boundary, not an
authorization boundary.

- `agentique pair` mints a one-time, human-typeable token (12 chars, 5-min
  TTL, consumed atomically in SQL). The CLI authorizes against the *running*
  server over HTTP by presenting `<datadir>/admin-secret`
  (`X-Agentique-Admin-Secret`) — deliberately not a second writer to the live
  database. An admin web session can mint too.
- `POST /api/auth/pair` exchanges the token for a 30-day bearer session (a
  row in `auth_sessions` with `kind=bearer`; the public `id` column is what
  listings and revocation use — the token itself is never echoed back).
- The auth middleware accepts `Authorization: Bearer` as a peer of the
  session cookie. An invalid bearer never falls back to the cookie: a
  presented credential is the one judged.
- WebSockets never carry the bearer in a URL. Clients redeem a one-time
  5-minute ticket (`POST /api/auth/ws-ticket` → `/ws?wsTicket=`); redemption
  re-validates the session against the database, so revoking a bearer also
  kills pre-minted tickets.
- `agentique auth sessions` / `agentique auth revoke <id>` manage sessions.
- CORS: allowlisted (RP) origins keep credentialed CORS; every other origin
  gets `Access-Control-Allow-Origin: *` **without** `Allow-Credentials` —
  cookies can never ride cross-site, bearer headers can. WS upgrades carrying
  a `wsTicket` skip the origin allowlist (the ticket is the proof).
- A server running `--disable-auth` advertises `pairing: false` and is
  addable token-less; clients then omit Authorization and tickets entirely.

### Machine catalog

Paired machines are **account state, not device state**: the catalog lives in
the primary's `machines` table (`GET/PUT/DELETE /api/machines`,
auth-guarded), including each remote's bearer token — peer material to the
auth sessions already stored in that database. Any client that signs into the
primary inherits every paired machine; the browser's localStorage copy is
only an offline cache.

### Cross-machine project identity

`projects.remote_url` holds the canonical key of the checkout's primary git
remote, computed by `gitops.CanonicalRemoteURL` at project create and
re-resolved by a startup refresh:

- remote preference: `upstream` > `origin` > alphabetically first;
- normalization makes SSH and HTTPS clones of one repo equal
  (`git@github.com:Org/Repo.git` ≡ `https://github.com/org/repo`), stripping
  credentials, ports, case, and `.git` (GitHub semantics are the deliberate
  bias);
- a project rooted in a repo subdirectory gets the relative path appended
  (`host/org/repo::packages/ui`), so monorepo-subdir projects never merge
  with each other while the same subdir on another machine still matches;
- `''` (no usable remote) never groups.

### Tailnet peer discovery

`GET /api/machines/discover` enumerates online tailnet peers (`tailscale
status --json` Peer map) and probes each concurrently for the well-known
descriptor (https-then-http on the server's own port plus the 9201/19201
defaults; TLS unverified — discovery only reads the public descriptor).
Discovery is a **hint layer**: it feeds Add-machine suggestions and grants
nothing; pairing still authorizes.

## Client side

### Connection architecture

- `lib/machines/registry.ts` owns one `WsClient` per machine, created once
  and reconnected **in place**, never replaced — components hold clients via
  `useRef`, so a swapped instance would strand them on a dead socket. Remote
  sockets resolve their URL asynchronously per attempt (fresh one-time
  ticket); a resolution failure schedules ordinary backoff (500ms doubling to
  a 30s cap — a sleeping laptop costs ~2 probes/minute).
- `useWebSocket()` returns the `RoutingWsClient` facade
  (`lib/machines/router.ts`): every **request** dispatches to the machine
  that owns the entity in its payload (`projectId` directly, or `sessionId` →
  its session's `projectId` → that project's machine; anything else → the
  primary); **subscriptions** fan in from every machine's socket, including
  machines paired later; **lifecycle** (connectionState, onConnect, reconnect)
  delegates to the primary only.
- Remote machine lifecycle is per-machine (`useMachineConnections`): each
  (re)connect re-syncs only that machine's projects and sessions. A flaky
  remote must never trigger the primary's reconnect-and-refetch path or reset
  primary streaming state.
- REST resolves the same way through `lib/machines/api.ts` (`apiFetch` +
  `machineIdForProject`/`machineIdForSession`). Content that loads via
  element `src` (image previews) is fetched as a Blob into an object URL,
  because an `<img>` cannot carry the bearer header a remote machine needs.

### Data model

Only `Project` carries a client-side `machineId` tag, applied at ingest;
sessions derive their machine through `meta.projectId`. Entity ids are UUIDs
and globally unique, so store maps need no re-keying. Remote projects' slugs
get a stable machine suffix at ingest (`agentique~ab12cd34`) so
slug-addressed routes stay unambiguous; the primary's slugs are never
rewritten and all pre-existing URLs remain valid.

### Grouping and machine selection

`groupProjects` (`lib/machines/grouping.ts`) merges same-`remote_url`
projects across machines into one logical project. The grouping is
**display-only, never routing** — every command targets one physical
(machine, project, session). The primary machine's copy, when present, is
the representative and drives the group's name, color, icon, folder, and
navigation slug. Sessions from every member interleave under the one row,
each carrying its machine glyph; the new-session page shows a "Run on"
control when the group spans machines (primary is the default), and the New
Project dialog offers a machine picker whose path input, directory browser,
and validation all run against the chosen machine.

### Offline behavior

Machines come and go (a laptop suspends mid-session). Each machine's
last-known projects and session metadata persist to a per-machine
localStorage cache (`lib/machines/cache.ts`), snapshotted after every
successful sync and again at disconnect, and hydrated merge-only at startup —
the machine's half of the sidebar stays visible and navigable while it is
unreachable, with its connection state shown on badges and chips. Snapshots
sanitize live-ness (`running` → `idle`, `connected: false`, pending
approvals stripped) so cached rows never fake a live pulse. The live re-sync
on reconnect is authoritative and replaces everything cached. Removing a
machine clears its cache and drops its sessions from the stores.

## Invariants a change here must keep

- **Verify `machineId` on pair and connect**; refuse mismatches.
- **The server is the authorization boundary** — endpoint reachability
  (tailnet or otherwise) never substitutes for auth, and discovery never
  grants access.
- **Bearer tokens never appear in URLs**; sockets use one-time tickets whose
  redemption re-checks the database.
- **An explicit credential never falls back to another** (bad bearer ≠ try
  the cookie).
- **Primary lifecycle isolation**: remote reconnects re-sync only their own
  machine.
- **Grouping is a view**: commands always target a physical entity.
- **Identity/secret files are created from `serve.go`**, never in
  constructors.
- **Cached data never impersonates live data.**

## Out of scope (deliberate)

Teams/channels are per-server (`messages` is a per-server source of truth);
cross-machine channels would be a federation feature. The brain is
per-machine; sharing memory across machines per canonical repo is deferred
until the core is proven.
