# Multi-Machine: Research (t3code) + Incorporation Proposal

Status: research + design sketch, 2026-08-20. No implementation yet.

Goal: one Agentique UI connected to Agentique servers on several machines at
once, with cross-machine project detection ("this repo exists on laptop and
workstation — pick where to run"). Premise: all machines share a tailnet, so
connectivity is direct and NAT traversal is a non-problem.

Primary source: the t3code repo (`~/git/t3code`), which ships exactly this
feature ("environments"). This doc records how t3code does it, what Agentique
looks like today, and a proposed shape for the feature.

---

## Part 1 — How t3code does it

### 1.1 Core concept: the environment

An **environment** is one machine running a t3 server. Its identity is a
server-generated UUID persisted to a file in the server's state dir
(`apps/server/src/environment/ServerEnvironment.ts`) — stable across restarts,
ports, IPs, and access methods, which is why the same machine reached via LAN,
Tailscale, or SSH is *one* environment, not three. The friendly label comes
from a fallback chain (`scutil --get ComputerName` → `/etc/machine-info`
`PRETTY_HOSTNAME` → `os.hostname()` → cwd basename).

The descriptor `{ environmentId, label, platform, serverVersion, capabilities }`
is served **unauthenticated** at `GET /.well-known/t3/environment`. That one
endpoint is load-bearing everywhere: it's the "is a t3 server here, and which
one" probe, the version-skew signal, and the capability negotiation surface.
Capabilities are optional booleans; a missing key means unsupported, so old
servers degrade without version comparisons ever gating features.

Every client-side connect **asserts descriptor.environmentId equals the saved
target's id** and fails on mismatch — a saved environment can never silently
reattach to a different machine that reused the same URL.

### 1.2 Client connection architecture

There is no multiplexer. The design is **N independent single-connection state
machines indexed by a registry**:

- A **connection catalog** persisted client-side (IndexedDB on web, OS secure
  storage on desktop): targets, connection profiles (URLs), and credentials as
  normalized arrays joined by `environmentId`/`connectionId`.
- An **EnvironmentRegistry** owning one supervisor + one closeable scope per
  environment, with a per-environment semaphore so concurrent operations on
  the same machine serialize while different machines run fully in parallel.
  Closing a scope tears down exactly that machine's socket and fibers.
- An **EnvironmentSupervisor** (per machine) is the *only* place retry policy
  lives. The dial layer makes exactly one attempt; the supervisor owns backoff
  (3s/4s/8s/16s ladder, reset after 30s of stable connection), offline gating,
  and app-foreground probes. "Connected" means the initial config RPC answered,
  not that a socket opened.
- **Two-way error taxonomy** drives the state machine: *transient*
  (network/timeout/unavailable → retry forever with backoff) vs. *blocked*
  (auth/config/permission → park with no timer until an external input
  changes). This beats a long error enum.
- A monotonically increasing **generation counter** per successfully
  established session is the cache-invalidation key: every per-environment
  query atom revalidates on generation change and parks (no error spam) while
  the machine is down.

### 1.3 State scoping and merged lists

One runtime for the whole app; scoping is by key, not by instance:

- No bare id crosses a UI boundary — everything is a scoped ref
  `{ environmentId, projectId | threadId }`, and atom families key on
  `` `${environmentId}\0${id}` ``. Routes carry the machine:
  `/$environmentId/$threadId`.
- Cross-machine lists use a **refs-then-entities two-level merge**: a refs
  atom iterates the catalog (not the connected set) and only changes identity
  when the *set* of entities changes; entity atoms re-derive per ref. One
  machine's update re-renders one row, not the whole merged list.
- **Cache-first offline.** Each environment's shell snapshot persists locally;
  status ladder is `empty → cached → synchronizing → live`, and disconnect
  keeps the snapshot, only downgrading status to `cached`. An offline
  machine's projects and sessions stay fully visible; only **writes** are
  gated (disabled rows with the connection status as description, composer
  banners like "Reconnecting to {machine}…").
- Connection health and data-sync status are **separate values** —
  "connected but sync failed" is a real, displayable state.
- Stale-data guards: events apply only when `sequence > snapshotSequence`;
  resume cursors are only sent when the snapshot came from *this* session; a
  history epoch counter discards in-flight pagination captured before a
  snapshot replacement.

### 1.4 Cross-machine project identity

**The matching key is the normalized git remote URL** — fetch URL of
`upstream`, else `origin`, else the alphabetically-first remote. Normalization
lowercases, strips trailing `/` and `.git`, and collapses ssh/https forms to
`host/path` (`git@github.com:Org/Repo.git` → `github.com/org/repo`). No path
match, no name match, no content hash. Projects with no remote never group.

Mechanics worth copying exactly:

- The identity is **computed at read time, never persisted** (server shells
  out to git, caches 1 minute keyed by repo root, and attaches
  `RepositoryIdentity` to every project row in the snapshot).
- Grouping is **client-side, display-only, never routing** — commands always
  target one physical `(environmentId, projectId)`. The group is a view.
- Grouping is a **user preference with per-checkout overrides**: three modes —
  `repository` (default; every clone/worktree of a repo on every machine is
  one logical project), `repository_path` (repo + subdirectory), `separate`
  (no grouping) — plus an override map keyed by physical project so one
  checkout can opt out.
- Physical dedupe first (same machine+path from stale rows → freshest wins),
  and a member that hasn't reported an identity yet *borrows a sibling's* so
  it isn't temporarily kicked out of its group while indexing.

### 1.5 The UX pattern

**Pick the logical project first (machine-agnostic), then pick the machine as
a second, separate control. Never a flat list of `project @ machine` rows.**

- New chat: the project picker shows one entry per logical project. The
  machine choice is a small **"Run on"** selector in the composer context
  strip (next to branch/worktree), monitor icon for the local machine, cloud
  icon for remotes, primary sorted first.
- Machine choice **locks once the thread has messages or a live session**.
- With one machine the picker **degrades to a static label rather than
  disappearing** (location stays legible; layout doesn't jump when a second
  machine connects). Multi-machine chrome generally hides until it's actually
  multi-machine.
- Switching machines pre-send just re-points the draft at the sibling
  physical project — the typed prompt follows you.
- Sidebar: the merged thread list is sectioned by *lifecycle*
  (pinned/active/snoozed/settled), **not by machine**. Machine shows as a
  small server icon on rows not on the current machine, with the full context
  (machine label, branch, model) in the hover tooltip. No per-machine colors
  or avatars; the only color is the connection-status dot.
- Add Project: palette flow is Environments → Sources; disconnected machines
  are listed but disabled with status text. Adding the same repo on a second
  machine merges into the existing group *emergently* — there's no explicit
  "link projects" action.
- Command palette search terms union every member's title, path, and machine
  label, so typing a machine name finds the project.
- Project settings: one page per logical group with a "checkout" sub-selector;
  group-level fields fan out to every member (with partial-failure messages
  naming the machine that failed), checkout-level fields stay scoped. New-
  thread defaults deliberately read only the *primary* machine's settings.

### 1.6 Tailscale integration and pairing

- **Tailscale is a ~400-line endpoint-provider add-on, not an architecture**
  (`packages/tailscale/src/tailscale.ts`). It shells out to exactly two CLI
  subcommands: `tailscale status --json` (reads only `Self.DNSName` +
  `Self.TailscaleIPs`, CGNAT-checked) and `tailscale serve` (to publish an
  HTTPS MagicDNS endpoint). No LocalAPI socket, no tsnet, no peer reading.
- **There is no automatic peer discovery.** Adding a machine is always
  explicit: pairing URL/QR, host + 12-char code, or desktop-managed SSH
  launch. Discovery means self-enumeration for display plus the well-known
  probe.
- Endpoints are modeled as **hints, not proof** — a normalized
  `AdvertisedEndpoint` list (loopback/LAN/tailnet/custom, with reachability
  and status); the connection attempt decides. Tailscale is deliberately not
  a connection-target *kind*: a Tailscale URL pairs through the ordinary
  bearer path.
- **The tailnet is treated as a dumb pipe.** Zero reliance on tailnet
  identity (no WhoIs, no headers). Stated principle: *"The backend remains
  the authorization boundary. Endpoint discovery never disables backend
  auth."* Rationale that survives scrutiny: a tailnet is a network boundary,
  not an authorization boundary (any tailnet device can reach the port);
  per-client revocation and scopes are finer than device identity; and the
  moment one non-tailnet path exists, transport can't be the auth model.
- Pairing: one-time 12-char token (Crockford-ish alphabet, 60 bits, 5-minute
  TTL, consumed atomically in SQL) exchanged via RFC 8693 token exchange for
  a **30-day bearer session** — an HMAC-signed opaque token that is *also*
  checked against the DB on every request, so revocation is real. Scopes are
  granted at pairing and exchange enforces subset, so a paired client can
  never escalate. WebSockets never carry the long-lived token: the client
  POSTs it for a **5-minute WS ticket** that goes in the URL, and per-RPC
  scope enforcement still applies on the socket. Tokens ride URL *fragments*,
  never query strings.

### 1.7 What t3code's hosted piece adds (and that we don't need)

"T3 Connect" is optional: an account-scoped machine registry, a credential
broker, and Cloudflare-tunnel provisioning for NAT traversal — traffic never
flows through it. Our tailnet premise removes the NAT problem, so the entire
relay/hosted-pairing layer (and DPoP, whose purpose is broker-transited
credentials) can be skipped.

---

## Part 2 — Agentique today (the gap analysis)

1. **Same-origin is a hard assumption.** Every REST call is a root-relative
   path (per-module `const BASE = "/api"` in `frontend/src/lib/api.ts:10`,
   `brain-api.ts`, `auth-api.ts`, `template-api.ts`, plus inline fetches);
   the single module-level WebSocket derives its URL from
   `window.location.host` (`frontend/src/hooks/useWebSocket.ts:4-14`). In
   production the SPA is embedded in and served by the backend it talks to.
   Tailwind: the `ws` handle is already threaded as the first argument
   through every RPC call site (`frontend/src/lib/ws-rpc.ts`), so a
   `getClient(machineId)` registry is tractable. Trap: `reconnectWebSocket`
   reconnects *in place* because consumers hold the client via `useRef`.
2. **Auth is cookie-only WebAuthn** (`backend/internal/auth/service.go:224`,
   `SameSite=Lax`, RP ID per host). No bearer/header path exists, so
   cross-origin API/WS calls have no way to authenticate today. CORS is an
   RP-origin allowlist; WS upgrades check Origin exact-match.
3. **Stores are flat and keyed by server-local UUIDs** (21 Zustand stores; no
   machine dimension). UUIDs make cross-machine maps accidentally
   collision-free, but `activeSessionId` is a single global, session-shortid
   resolution is a prefix scan over all sessions, and two persisted stores +
   two raw localStorage keys would collide across machines.
4. **Routes carry no machine**: `/project/$projectSlug/session/$shortId`, and
   slugs are unique per-server only (two machines can both have
   `project/agentique`).
5. **Project identity is the local filesystem path** (the only UNIQUE
   constraint, migration 001). The git remote URL is stored nowhere; gitops
   has the shell-out plumbing but no `remote get-url` call.
6. **No server identity on the wire.** No `os.Hostname()` anywhere;
   `/api/health` returns `{status, features}` only. The data dir + instance
   lock + owner stamp are the de-facto identity, none of it exposed.
7. **Brain scope is `project:<uuid>`** — two machines' checkouts of the same
   repo would have disjoint memory scopes without a stable cross-machine key.
8. Already pointing the right way: `--addr 0.0.0.0`, TLS support, the README's
   LAN/Tailscale row, `[[dev-urls]]` multi-origin precedent, the PWA/mobile
   work, and the fact that `session.Manager`/worktrees/CLI subprocesses are
   already fully owned per-server (nothing server-side needs to federate).

---

## Part 3 — Proposed shape for Agentique

Direction: adopt t3code's model — **client-side fan-out to N sovereign
servers**, no server-to-server federation. Each machine's Agentique server
keeps owning its DB, worktrees, CLI subprocesses, reaper, and scheduler
untouched. "Multi-machine" is a frontend + auth + identity feature. The SPA
(served by whichever machine you opened) connects same-origin to its own
server (the *primary*) and cross-origin, bearer-authenticated, to the others.

### 3.1 Server identity + probe (backend, small)

- `machine_id` UUID read-or-generated in `<datadir>/machine-id`; label from
  `PRETTY_HOSTNAME` → `os.Hostname()` fallback chain, overridable via
  `[server] machine-label` in config.toml.
- `GET /.well-known/agentique/environment` (unauthenticated, like t3):
  `{ machineId, label, platform, version, capabilities }`. Also fold
  machineId/label into `/api/health` (feature-store already fetches it).
- Capabilities as optional booleans from day one — we already have the
  pattern (`features.browser/teams`); this is where provider support, brain,
  teams, schedules get advertised so version skew degrades per-feature.

### 3.2 Auth: pairing → bearer session (backend, the real work)

Keep WebAuthn cookies for the same-origin primary. Add alongside it:

- **Pairing UX (decided): smooth and CLI-first, like t3code.** On the machine
  to be added, `agentique pair` mints a one-time token (short, human-typeable,
  ~5 min TTL, consumed atomically) and prints the token, a full pairing URL,
  and a QR code. In the client of choice, "Add machine" accepts a pasted
  pairing URL *or* host + token, exchanges it, and saves the machine — no
  restart, no config editing. `agentique pair --tailscale` variants can print
  the MagicDNS URL. A settings-UI "mint pairing token" on an
  already-authenticated session can come later; the CLI path is the v1
  contract.
- Exchange endpoint → long-lived bearer session: opaque signed token, stored
  server-side so revocation is real; `Authorization: Bearer` accepted by the
  auth middleware as a peer of the cookie path.
- WS ticket endpoint: POST bearer → 5-minute single-purpose ticket →
  `?wsTicket=` on the upgrade. Never the bearer in a URL.
- Sessions listable/revocable (settings UI + `agentique auth` CLI).
- CORS: bearer-authenticated requests don't rest on ambient authority, so
  the allowlist can be relaxed for them (t3 ships `allow-origin: *`); keep
  the strict RP-origin list for cookie-authed routes.
- Skip: DPoP, relay, hosted pairing, scopes (single-user product; add scopes
  only if non-owner clients ever appear). Do **not** skip revocation or the
  DB check per request.
- Tailnet stays transport-only. Even on a trusted tailnet, the server is the
  authorization boundary (t3's reasoning, §1.6, holds for us).

### 3.3 Tailscale niceties (optional, cheap)

- Port t3's `tailscale status --json` self-enumeration (~a few hundred lines,
  CLI-only) to print/pair with the MagicDNS URL.
- **Possible differentiator t3 doesn't have:** since our premise is
  tailnet-everywhere, we *can* do peer discovery — read the `Peer` map from
  `tailscale status --json`, probe `https?://<peer>:9201/.well-known/...`,
  and pre-fill "Add machine" with discovered Agentique servers. Pairing is
  still required for auth; discovery only removes URL typing.

### 3.4 Frontend connection layer

- Replace the WS singleton with a **registry keyed by machineId**: one
  `WsClient` + reconnect state machine per machine (our existing
  backoff/heartbeat/visibility logic is already per-client — it just needs N
  instances). Adopt t3's separation: dial does one attempt; a per-machine
  supervisor owns retry, with the transient-vs-blocked split and a
  `generation` counter driving refetch (`useGlobalSubscriptions`' reconnect
  refetch becomes per-machine instead of global).
- A client-side **machine catalog** (machineId, label, URL, bearer token) in
  persisted storage; the primary (same-origin) machine is implicit and
  unremovable.
- Verify `machineId` from the well-known descriptor on every connect; refuse
  mismatch.

### 3.5 Frontend state + routes

- Introduce scoped refs `(machineId, id)` at store boundaries. Cheapest
  concrete path given our stores: key maps by a composite key, keep the
  primary machine's data unprefixed... **no** — half-measures here are the
  trap; follow t3: no bare session/project id crosses a component boundary
  that can span machines. Realistically this lands as a `machineId` field on
  `SessionData`/`Project` plus composite keys in the few maps that merge
  across machines, while per-machine slices stay keyed by plain UUID.
- Routes: add a machine segment for non-primary machines —
  `/m/$machineSlug/project/$projectSlug/...` with the bare
  `/project/$projectSlug` meaning the primary. Keeps every existing URL
  valid and every existing deep link working.
- Namespace the persisted UI stores (`agentique:ui` etc.) per primary origin
  only — drafts/expansion state for remote machines key by
  `machineId:entityId` inside the same document.
- Offline: keep last-known projects/sessions per machine in a local cache and
  render them with a `cached` status; gate writes only (t3 §1.3). This can
  ship *after* the live path works.

### 3.6 Cross-machine project matching

- New nullable `projects.remote_url` column holding the **normalized**
  canonical key (`host/org/repo`), refreshed at project create + a cheap
  re-resolve on project open (read-time-ish; never trusted stale). Reuse
  t3's normalization rules exactly (upstream > origin > alpha; lowercase;
  strip `.git`).
- Grouping is client-side and display-only: group projects across connected
  machines by canonical key; default `repository` mode; per-project opt-out
  later if needed. Commands always target one physical project.
- **Brain stays per-machine. Sharing memories across machines is explicitly
  deferred** (decision 2026-08-20: premature — too much else has to be right
  first). The canonical key merely makes a future design *possible*; nothing
  in this feature may depend on or reach for cross-machine brain state.

### 3.7 UX / left panel

Recommendation: **a user-selectable grouping axis — group by project
(default) OR by machine** — rather than a fixed machine level in the tree.

- **Group by project (default, t3's pattern).** Sidebar stays organized by
  folder → project → session. Projects that exist on several machines merge
  into one logical project row (grouped by canonical key); sessions from all
  machines interleave under it, sorted by the existing priority rules.
  Sessions on a non-primary machine get a small server icon; full context
  (machine, branch, state) in the tooltip. No per-machine colors.
- **Group by machine (the ops view).** Top-level sections are machines
  (label + connection dot), each containing that machine's physical projects
  and sessions. No logical merging in this mode — each checkout appears under
  its owner. Useful for "what is running where", which matters more for
  long-running agent sessions than for t3's chat threads. Implementation-wise
  this is a second bucketing pass in the existing grouping engine
  (`use-folder-groups.ts` already builds `ProjectEntry[]`; machine is one
  more grouping key), with the toggle persisted alongside focus mode. The
  toggle only renders once >1 machine is paired.
- **Machine picker at session-creation time, not in the tree.** The
  new-session view gets a "Run on" control next to model/permission-mode,
  visible only when the selected project's group spans >1 machine (degrading
  to a static label when the project is remote-only). Locked once the
  session exists.
- **Connection strip in `SidebarFooter`**: one status dot + label per
  connected machine (the existing `ConnectionIndicator` generalizes from
  singleton to list), popover with connect/disconnect/remove/pair-new. All
  multi-machine chrome hidden until a second machine is paired.
- Teams tab, discussions, schedules, brain pages: **primary-machine-only in
  v1** (they're deeply server-owned); the page header states which machine
  you're viewing once >1 is connected.

### 3.8 Phasing

- **M0 — identity + auth** (**SHIPPED 2026-08-20**, see §4): machine-id/label
  + well-known descriptor; pairing → bearer sessions + WS ticket +
  revocation; `agentique pair` CLI. Independently useful (proper tokens for
  the PWA on the phone, replacing per-host passkey friction for secondary
  devices).
- **M1 — client fan-out** (**SHIPPED 2026-08-20**, f10689a + 759f52a +
  1e02281): RoutingWsClient facade behind useWebSocket() (requests route by
  payload entity → owning machine; subscriptions fan in; lifecycle stays
  primary-only), per-machine ticket-authenticated sockets, machine
  indicators everywhere a session appears, Run-on picker, token-less
  no-auth machines, mock-server e2e path.
- **M2 — full remote panes** (**SHIPPED 2026-08-20**, 1056e1f + 08ae4b7):
  machine-aware REST (lib/machines/api.ts) — project files/content, image
  previews via blob object URLs, session-file links, filesystem
  browse/validate, project mutations; add-project machine picker with
  remote directory browsing. Git/changes/browser panes were WS-routed
  already.
- **M3 — project grouping + Run-on** (**SHIPPED 2026-08-20**, c535d41):
  `projects.remote_url` canonical key (SSH/HTTPS-equivalent, GitHub-biased,
  `::subpath` qualifier for monorepo subdirs) + display-only client-side
  merging; primary copy drives name/color/icon; Run-on in 1e02281.
- **M4 — polish** (**mostly SHIPPED 2026-08-20**): per-machine offline
  cache + status-colored badges (b0d049c); tailnet peer discovery
  suggestions in Add-machine (660d752); **server-mastered machine catalog**
  (3b7d08e — pairing is account state; localStorage is only an offline
  cache, so every device logging into the primary sees the same machines).
  Still open: per-machine usage/disk in the footer, palette/sidebar search
  matching machine labels.

Validated in real use 2026-08-20: remote sessions driven from the phone
PWA, and the same paired machines/sessions visible from desktop and phone
after the catalog-sync fix.

### 3.9 Decisions so far + open questions

Decided (2026-08-20):

- **Brain sharing across machines: deferred, not designed.** Per-machine
  brain is the standing behavior; revisit only after the core feature is
  solid.
- **Pairing UX: CLI-first** — `agentique pair` mints a temporary token;
  paste token/URL (or scan QR) in the client of choice (§3.2).
- **Left panel: grouping axis toggle** — group by project (merged, default)
  or by machine (§3.7).
- **Primary semantics: the SPA-serving machine is the primary** (t3's model,
  chosen for least effort/simplest). Its settings drive new-session defaults
  and app-level preferences; remote machines contribute projects and
  sessions, not configuration. This also matches t3's hard-won insight that
  reading a *remote's* settings for defaults silently resets them. Symmetric
  or user-designated primary can be revisited later without breaking
  anything — it's a defaults-resolution rule, not a data-model property.

- **Remote-pane scope: files/git/etc. land as their own phase (M2) between
  client fan-out (M1) and project grouping (M3)** — remote sessions are
  chat-first briefly, then get full pane parity before the grouping work.

Open:

1. **Teams/channels spanning machines** — out of scope v1 per the additive
   principle, but the messages table is per-server; cross-machine channels
   would be a federation feature, much bigger.

---

## Part 4 — M0 implementation notes + testing stepping stones

Shipped 2026-08-20. What landed:

- `internal/machine`: `machine-id` UUID read-or-created in the data dir
  (created from `serve.go`, never in constructors); label from
  `[server] machine-label` / `AGENTIQUE_MACHINE_LABEL` →
  `/etc/machine-info` `PRETTY_HOSTNAME` → hostname.
- `GET /.well-known/agentique/environment` (unauthenticated):
  `{machineId, label, version, platform, capabilities}`. `machineId`/
  `machineLabel`/`version` also on `/api/health`.
- Pairing (migration 041, `internal/auth/pairing.go`): one-time 12-char
  tokens (Crockford-ish alphabet, 60 bits, default 5 min TTL, consumed
  atomically in SQL) → `POST /api/auth/pair` → 30-day bearer session in the
  existing `auth_sessions` table (new `id`/`label`/`kind` columns). Bearer
  accepted via `Authorization: Bearer` as a peer of the cookie; an invalid
  bearer never falls back to the cookie.
- WS tickets: `POST /api/auth/ws-ticket` (any authenticated credential) →
  one-time 5-min ticket → `/ws?wsTicket=`; redeeming re-validates the
  session against the DB, so revocation kills pre-minted tickets.
- Mint/list/revoke authorization: an admin session **or** the data-dir
  `admin-secret` file (0600, created at serve startup) presented as
  `X-Agentique-Admin-Secret`. That is how the CLI proves data-dir access —
  deliberately *not* t3code's direct-write-to-the-live-DB approach.
- CLI: `agentique pair [--ttl]`, `agentique auth sessions`,
  `agentique auth revoke <id>`. (`baseURL()` also fixed: an explicit
  `--addr` flag now beats the config file, matching serve's precedence.)
- CORS: allowlisted (RP) origins keep credentialed CORS; every other origin
  now gets `Access-Control-Allow-Origin: *` **without** Allow-Credentials
  instead of a 403 — cookies can never ride cross-site, bearer headers can.
  WS upgrades carrying a `wsTicket` skip the origin allowlist (the one-time
  ticket, validated by the middleware before upgrade, is the proof).

### Testing stepping stones

1. **Unit** (done, in CI path): `internal/auth/pairing_test.go` — mint auth
   fail-closed, exchange once-only + expiry + normalization, bearer
   no-cookie-fallback, ws-ticket once-only + revoked-session rejection,
   revoke-by-id; `internal/machine/machine_test.go` — id stability +
   corrupt-file regeneration.
2. **Single-machine, isolated server** (done, 2026-08-20): full curl
   walkthrough against `AGENTIQUE_HOME=<tmp>` + scratch `--db` on a spare
   port — descriptor, pair, exchange (lowercased/dashed token), token reuse
   → 401, bearer + foreign `Origin` → 200 with `ACAO: *`, ws upgrade with
   ticket → 101 / reuse → 401 / bare → 401, CLI list + revoke → bearer 401.
3. **Two real machines over the tailnet** (next, needs the user): deploy
   this build on both. On machine B: `agentique pair` → from machine A:
   `curl -X POST http://<B-tailnet>:9201/api/auth/pair -d '{"token":"…","label":"machine-a"}'`
   then a bearer `GET /api/sessions` and a ws-ticket upgrade. Verifies the
   real network path, TLS/plain-HTTP behavior, and that the descriptor
   labels look right in practice.
4. **M1 client fan-out** will then consume exactly these endpoints; the
   pairing UX can be exercised end-to-end from the browser only after M1's
   "Add machine" UI exists.
