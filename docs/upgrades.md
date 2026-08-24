# In-app upgrades — one click per machine, no dead turns

Status: **V1–V4 shipped; V5 designed, not built.** Decisions settled
2026-08-23 after review (proposal artifact
`6fd17232-9ccc-4aad-9e1e-1d51206bd499`); V5's own fifteen settled 2026-08-24
(artifact `39f2b8b8-9d98-4dbc-a1b6-0b4be9883620`). This document is the working
contract — a session picking up any phase should need nothing else. Per-phase
status is on each phase below.

## Goal

A tagged release lands; every client says so, names which machines are
behind, and upgrades them one at a time on request — without ending a turn
that is mid-flight, and without pretending to work on a platform we have
never run.

## This is mostly wiring

Almost every part already exists and is trusted in production. The new work
is connecting them and deciding when it is safe to pull the trigger.

| Piece                                       | Where                                       | State        |
| ------------------------------------------- | ------------------------------------------- | ------------ |
| Tagged release → binaries + `checksums.txt` | `.github/workflows/release.yml`             | exists       |
| Version stamped into the binary             | `-X main.version`; `git describe` locally   | exists       |
| Download → sha256 → atomic replace          | `install.sh`, `install.ps1`                 | exists (sh)  |
| Service control, three platforms            | `internal/service` (systemd/launchd/schtasks) | exists     |
| Version on the wire, per machine            | `/api/health`, `/.well-known/agentique/environment` | exists |
| CLI version probing                         | `internal/doctor` (`claude --version`, `parseVersion`) | exists |
| Version shown to the user                   | Settings › About                            | exists       |
| Asking GitHub for the latest tag            | `internal/update`                           | V1 ✓         |
| Per-machine version client-side             | `machine-store.versions`, from the probe    | V1 ✓         |
| An endpoint that performs the upgrade       | `internal/update` (`Applier`)               | V3 ✓         |
| Knowing whether it is safe to restart       | `Manager.BusyTurns` ← `EventPipeline.TurnOpen` | V3 ✓      |

## The contract

Each server answers two questions about itself — what am I running, what is
published — and exposes both. The client does no version arithmetic beyond
comparing strings it was handed.

```
GET /api/update/status        (authenticated, per machine)
{
  "current":   "v0.4.1",       // main.version, as stamped
  "latest":    "v0.5.0",       // cached tag from the GitHub releases API
  "behind":    true,
  "channel":   "release",      // or "dev" — a git-describe build never nags
  "asset":     "agentique-linux-amd64",
  "supported": true,           // false: no asset, or platform not yet verified
  "platform":  "linux/amd64",  // so a row can explain itself
  "checkedAt": "2026-08-23T12:04:11Z",
  "checkError": "",            // last check's failure; the cached answer stands
  "releaseUrl": "https://github.com/…/releases/tag/v0.5.0",
  "busy":      false,          // a turn is running here right now (V3)
  "armed":     false,          // waiting for idle to upgrade itself (V4)
  "progress":  null,           // or the live phase (V3)
  "notes":     "…release notes, truncated…"
}

POST   /api/update/apply      body {"expect": "v0.5.0"}   (full access)
         → 202, progress events, then the socket drops
DELETE /api/update/apply      cancel: disarm, or abort before replacing
```

Reading the status only needs a session — every client shows the chip. Applying
needs **full access**: it replaces this machine's binary and restarts its
service, and `force` ends every turn in flight. That is at least as privileged
as reading the machine catalog, so it carries the same guard.

The asset and checksum URLs are taken from the release document and must be
HTTPS (loopback excepted). The checksum proves the download matches what that
document said, so the transport carrying both is the part that has to be
trustworthy — releases are not signed, which is the remaining gap here.

`?refresh=1` forces a check instead of reading the hourly cache; without it
the request never touches the network. Fields land with the phase that makes
them real (marked above) rather than being stubbed early. `checkedAt` is
stamped on failure too, so a stale answer can be dated: "as of 2h ago" is
information, "unknown" is not.

The endpoint is off entirely when `[update] disabled` is set
(`AGENTIQUE_UPDATE_DISABLED`); `[update] api-url`
(`AGENTIQUE_UPDATE_API_URL`) repoints the check at a fork's repo — or at a
stub, which is how the apply path is verified without touching a real
release.

The check polls hourly per server, cached against the response `ETag`, and
refreshable on demand. Unauthenticated GitHub allows 60 requests/hour/IP; one
per hour per machine is nowhere near it. A check that fails keeps the last
cached answer and its age — a version check never blocks the UI.

Apply is `install.sh` in Go, against the machine's own platform:

1. Resolve the asset for `GOOS/GOARCH`; refuse if none, or if the platform is
   not on the verified allowlist.
2. Download to a temp file **beside the install dir** — same filesystem, so
   the rename is atomic.
3. Verify sha256 against `checksums.txt`. **Mismatch aborts.** This step is
   what makes the feature safe to have at all.
4. Keep the current binary as `agentique.prev`, then `rename(2)` the new one
   over the target. On Windows, rename the busy target aside first — the
   trick `install.ps1` already uses.
5. Reply `202`, flush, **then** `service.Restart()`.

Success looks like a disconnect: the process serving the reply is the process
being replaced. The client treats the drop as expected and confirms by
re-reading the version, which is also how it verifies the upgrade worked.

## Progress is state, not just events

An upgrade runs for tens of seconds and must narrate itself. Each phase is
published on the WS global topic **and** held as server state in `progress`.
Events alone strand anyone who reloads mid-upgrade or opens a second client;
state alone makes the bar lurch on a poll interval. Both; first to arrive
wins.

```
queued → downloading (bytes/total) → verifying → replacing → restarting
                                                   │
                         ─────── socket drops here ┘
                                                   ↓
                      reconnecting → confirmed (version re-read) | failed
```

After `restarting` nobody is left to report, so the client polls the
unauthenticated descriptor until the version changes or a deadline passes,
then shows the version it **actually found**. `reconnecting` renders as
progress, not error — on this one command a dropped socket means it worked.

The byte counter belongs only to `downloading` (the one phase where "is it
hung?" arises) and should stay hidden under a size/duration threshold: 33 MB
over a fast link finishes before a bar means anything.

## The drain gate

**A restart is not a pause.** On startup the server reaps orphaned CLI
process groups (`procctl.ReapOrphanedCLIProcesses`, see
`docs/process-lifecycle.md`) — the guard that stops a crashed server leaking
`claude` and its Playwright subtree. So restarting mid-turn does not suspend
that turn: the new process comes up and kills it.

Sessions survive — worktrees, history and metadata are on disk. The cost of a
badly-timed restart is the **current turn**, not the session, and the UI must
say exactly that; "will this lose my work" is the question that stops someone
clicking.

Busy is answered by the runtime's turn lifecycle — `Manager.BusyTurns()` over
`runtime.Session.TurnInFlight`, never session state, which reports Idle for one
dispatch before the completion that caused it is broadcast:

- **idle** → upgrade now.
- **busy** → offer *upgrade when idle*: arm a one-shot that fires on the next
  idle transition.
- **override** → allowed, but the button states the cost ("2 turns will be
  terminated") and is a deliberate second click, never the default.

Armed state carries a **deadline** (default 4h, `[update] arm-deadline`)
after which it disarms and says so, and is **in-memory only** — if the server
restarts for any other reason the arming is forgotten. That is the fail-safe
direction: an upgrade armed on Tuesday must not fire on Thursday because a
lid closed at the wrong moment.

**Where the gate listens matters more than it looks.** The obvious hook is
the idle transition, and it is the wrong one: agentkit flips the runtime to
Idle from inside the completion's own dispatch, *before* the
`TurnCompletedEvent` is broadcast. An observer woken at that moment still sees
the turn it is waiting on as in flight, so a gate wired there never fires.

That is now agentkit's own contract rather than something agentique
reconstructs: `runtime.WithOnTurnEnd` fires strictly after the completion
broadcast and after `runtime.Session.TurnInFlight` has cleared, for every way a
turn can stop — completed, died with the CLI, or closed mid-flight.
`Manager.AddTurnEndListener` just fans that one hook out to agentique's
consumers.

## Cancelling

Two different things wear the word. An **armed** upgrade is cancellable for
as long as it is armed. An **in-flight** one is cancellable up to a line:

| Phase                      | Cancellable | Why                                              |
| -------------------------- | ----------- | ------------------------------------------------ |
| `queued`, `downloading`    | yes         | Nothing installed; delete the temp file.         |
| `verifying`                | yes         | Installed binary still untouched.                |
| `replacing`                | no          | A single `rename(2)` — over before a cancel lands. |
| `restarting`               | no          | New binary installed; "cancel" now means rollback. |

The Cancel button is real through verification and then **disappears**,
replaced by "no going back" — more honest than a control that stays visible
and quietly stops working. It covers the long phase anyway: download is where
the seconds go.

## Across machines

Every machine checks for itself, because only it knows its platform, its
install method and whether it is busy. The client fans status calls out
through the existing routing facade (`lib/machines/router.ts`) and merges the
answers into one dialog, one row per machine.

- **An offline machine is not a problem to solve.** Last-known version,
  greyed, no action. It gets offered the upgrade when it returns.
- **Only a client may trigger an upgrade.** Never a peer machine, never as a
  side effect of anything else. If presentation sync ships
  (`docs/multi-machine-sync.md`), its scoped credential is excluded from this
  route by construction.
- **Mixed versions stay legal.** The descriptor carries `capabilities` and
  clients treat a missing key as unsupported. An upgrade feature makes skew
  routine rather than exceptional, so nothing may start comparing version
  numbers to decide behaviour.

A local build reports `channel: "dev"` — `git describe --tags --always
--dirty` yields e.g. `v0.4.1-7-gab12cd3-dirty`, and a machine you are
actively developing on must never be told it is behind.

## Release matrix: build wide, enable narrow

`release.yml` currently builds `linux-amd64` and `windows-amd64` only.
Cross-compiling is free (`CGO_ENABLED=0` already), and not having the
hardware does not stop us publishing assets — it stops us promising they
self-upgrade. So:

- **Publish** `linux-amd64`, `linux-arm64`, `windows-amd64`, `darwin-arm64`,
  so a manual `install.sh` works anywhere.
- **Enable in-app apply** only on an explicit allowlist of verified
  platforms, starting with `linux/amd64`. Everything else reports
  `supported: false` and the row says "manual upgrade".
- A platform graduates when someone actually runs it, not when it compiles.

## Claude and Codex CLIs

**Nobody in this repo runs a CLI.** Each provider's Go library owns its own
command entirely — agentique never constructs, execs or shells out to `claude`
or `codex`, not to read a version, not to run `doctor`, not to update. It asks
agentkit's `runtime.InstallInspectable`; agentkit asks the adapter; the adapter
asks the library. Anything the product needs from a CLI is a gap in that
library, and the fix is to add it there rather than to route around it.

**The target is the binary agentique itself would spawn**, resolved by the
connector — never by a PATH lookup in the product. Those agree today only
because nothing overrides the binary path. The connector owns the client
options, so it is the only thing that stays right the moment something does,
and it is why the capability hangs off `CLIConnector` rather than being a
helper anyone can call.

**Detect how each CLI was installed — never assume.** Showing the wrong update
command does not fail cleanly: `npm install -g` against a native install writes
a second complete copy into an npm prefix, whichever copy PATH reaches first
answers `--version` from then on, and the copy actually in use stays stale. An
empty update command means *tell the user to update manually*; it never means
*fall back to npm*.

**The install method never gates behaviour.** `Method` is a label to display.
`InstallNative` means the standalone layout only in codexcli-go but includes a
bare executable in claudecli-go; codex updates its own npm-global installs while
claude's hand back a command. Branch on the library's verdict — `SelfManaged`, a
non-empty `UpdateCmd`, a passing preflight — never on a method name. Same rule
as the model catalog, one level down: versions and enums never gate behaviour,
capabilities do.

**Knowing and acting are separate questions.** Report that an update exists
whenever a trustworthy source for *that* install can be named; offer to perform
it only where the library manages that install itself and its preflight passes.
Never let "we cannot act" suppress "you should know" — an npm-global install
into a root-owned prefix is knowable and untouchable at once, and that is a
common case, not an edge one. Where no source can be named (brew, winget, mise,
asdf, unknown) the row says so; it never borrows another channel's number,
because the channels disagree — npm and the native `latest` channel tracked
2.1.241 on the day the native `stable` channel was ten patches behind at
2.1.231.

**Preflight is the library's, not ours.** The directory that must be writable is
not the one holding the binary on PATH: for an npm install it is the managed
package root, for a codex standalone install it is `$CODEX_HOME/packages/
standalone`. Neither is derivable from the resolved path, so a check in the
product would test the wrong directory and offer a button that cannot work.

**Exit codes from CLI updaters are not evidence.** `codex update` was observed
exiting 0 and printing success after its updater command was missing entirely.
An update is verified by re-reading the version, exactly as V3 verifies
agentique's own upgrade by re-reading the descriptor rather than trusting the
response.

**A CLI update is not a restart**, so the drain gate does not apply and V5 does
not use it. The server keeps running, running turns keep their already-exec'd
binary, and the new version applies to the next session — which is what the UI
says.

That is observed, not reasoned. A real `claude update` ran on the dev box at
07:40 on 2026-08-24 while three CLI sessions were mid-turn (started 06:35,
06:37 and 07:06). All three continued, and all three still had
`/proc/<pid>/exe` pointing at `versions/2.1.239` afterwards while the symlink
had moved to `2.1.241`. The native layout keeps every version as its own file
and repoints a symlink, so a running turn is not reading anything the update
touches. A gate would have suspended those turns to prevent nothing.

The claude CLI also self-updates on the same mechanism — four versions in the
thirteen days to 2026-08-24, installed without anyone asking — so this happens
whether or not agentique offers a button. The gate returns as a question only
if a library starts calling a shared-tree rewrite self-managed.

## Settled decisions

| #  | Decision                        | Why                                                                 |
| -- | ------------------------------- | ------------------------------------------------------------------- |
| U1 | Each server checks for itself   | A machine that cannot reach GitHub cannot upgrade anyway.            |
| U2 | Chip; dismissal dies on reload  | Deliberate pressure to update. Nothing about it persists to storage. |
| U3 | Per-row action, no bulk         | One machine, one button, one visible outcome.                        |
| U4 | Arm when idle; override on a second click | See the drain gate; cancel semantics above.                |
| U5 | Build wide, enable narrow       | No Mac/ARM hardware to verify against.                               |
| U6 | CLI updates deferred to V5      | Must detect install method first.                                    |
| U7 | Auto-upgrade per machine, default off | Ships as a setting; stays off until apply is exercised by hand. |
| U8 | No pre-release channel          | Everything goes to master and out; `releases/latest` is all of it.   |

### V5, the CLIs

| #   | Decision                              | Why                                                                     |
| --- | ------------------------------------- | ----------------------------------------------------------------------- |
| C1  | Target is the binary agentique spawns | Anything else describes a binary nobody here executes.                   |
| C2  | claudecli-go owns the claude command  | Detection already exists there, read-only and network-free.              |
| C3  | codexcli-go owns the codex command    | Its own report beats our inference; the product must not shell out.      |
| C4  | Knowing and acting are separate       | Root-owned installs are knowable and untouchable at the same time.       |
| C5  | Only the tools' own updaters, run by their own libraries | The server has no npm prefix, and never should. |
| C6  | No drain gate for CLI updates         | Not a restart; the CLI already self-updates under live sessions.         |
| C7  | CLIs never drive the footer chip      | They ship most days; a permanently lit chip is one nobody reads.         |
| C8  | `clis` rides `/api/update/status`     | Detection is offline and cheap; a second endpoint buys nothing.          |
| C9  | Shadowing is reported, symmetrically  | A warning that works for one CLI and not the other teaches false trust.  |
| C10 | `internal/doctor` stops running the CLI | Two answers to "how do I update this" must not differ.                 |
| C11 | Run-it button ships off               | Mirrors U7: capability ships, trigger waits for a hand-run.              |
| C12 | Show the version a session reported   | The only field derived from what happened rather than from inspection.   |
| C13 | The connector answers, not the PATH   | Keeps detection and execution from drifting apart.                       |
| C14 | The install method never gates behaviour | The two libraries' enums deliberately disagree.                       |
| C15 | V5a ships without a "behind" verdict  | Nothing in the stack can compute one yet; a stub would be wrong, not small. |

## Invariants a change here must keep

- **A restart is not a pause.** Anything that restarts the server must
  consult the turn registry first.
- **Checksum before replace, always.** No path installs an unverified binary.
- **The previous binary is kept** (`agentique.prev`) and rollback stays a
  deliberate command — nothing auto-reverts, because an automatic rollback
  that also fails is a worse place to be.
- **Never offer a button that cannot work.** Unsupported platform,
  unwritable install dir, and no-service-installed are all detected before
  the row offers an action.
- **Only a client triggers an upgrade** — never a peer, never a schedule
  (until U7 is explicitly enabled on that machine).
- **Version numbers never gate behaviour**; capabilities do. For the CLIs the
  same holds of install-method enums: they are labels, and the two provider
  libraries define them differently on purpose.
- **A dev build never nags.**
- **agentique never runs a provider CLI.** No `exec` of `claude` or `codex`
  anywhere in this repo — versions, install methods and updates all come
  through `runtime.InstallInspectable`. A missing fact is a gap in the provider
  library, not a reason to shell out.
- **An empty update command means "manually", never "use npm."**

## Phases

- **V1 — Know. Shipped.** `/api/update/status`, hourly ETag-cached check,
  per-machine version kept client-side, versions listed in Settings › About.
  No chip, no button, no restart path. Zero risk, and with machines drifting
  independently, seeing the versions side by side is already most of the
  value. Worth shipping alone.
  - Backend: `internal/update` (`Checker` + `Handler`), constructed in
    `server.New`, poll loop started from serve.go's production block — same
    precedent as the scheduler, so a unit test never reaches the network.
  - Client: the descriptor probe in `lib/machines/health.ts` now returns the
    descriptor it already fetched instead of discarding everything but
    `machineId`; `machine-store.versions` persists each machine's last-known
    version, so an away machine still says what it was running.
  - `release.yml` builds `linux-arm64` and `darwin-arm64` too, and
    `install.sh` accepts them (portable sha256, name-anchored checksum
    lookup). Apply stays gated to `linux/amd64` in
    `internal/update/platform.go`.
- **V2 — Tell. Shipped.** Footer chip and the dialog, fanned out across
  machines. Still no button.
  - `useUpdateChecks` (mounted at the root) re-reads every machine's cached
    answer on a 15-minute beat and immediately when the catalog changes. The
    servers do the hourly GitHub check; the client only re-reads.
  - The chip lives in `SidebarFooter`; `dismissed` is a field on the
    (unpersisted) update store, so a reload brings it back — verified in the
    browser, and nothing lands in localStorage.
  - `UpdateDialog` is one row per machine. An away machine renders greyed,
    keeps its last-known version and verdict, and never reads as wanting
    attention.
- **V3 — Apply, narrated. Shipped.** Verification, `.prev` retention,
  restart, reconnect-and-confirm, per-phase progress as state *and* events,
  cancel through verification. Narration is not a follow-up: an unnarrated
  30-second binary swap is the version nobody trusts twice. Verified on
  throwaway servers (isolated `AGENTIQUE_HOME`, a stub releases endpoint, and
  a `systemctl` shim that can only ever signal the throwaway) before it went
  near a real one; gated to `linux/amd64`.
  - `internal/update`: `Applier` (preflight → download → verify → replace →
    restart), `install.go` (checksums, atomic install, `Rollback`),
    `POST`/`DELETE /api/update/apply`, and `agentique rollback`.
  - Busy comes from `EventPipeline.TurnOpen` via `Manager.BusyTurns()` — the
    turn lifecycle, not session state. V3 refuses while busy unless `force`
    is passed; V4 turns that into the drain gate.
  - `status.installable` is the full preflight (verified platform + published
    asset + writable install dir + a service to restart) and is what the UI
    keys its button on; `blocker` says why not. `supported` stays the
    platform/asset fact the contract above defines.
  - Cancel and the point of no return contend on one mutex (`Applier.commit`),
    so an accepted cancel can never be silently ignored by an install that was
    already under way.
- **V4 — Wait for idle. Shipped.** Drain gate, armed one-shot with deadline
  and cancel, override with its honest warning.
  - `POST /api/update/apply` with `{"whenIdle": true}` arms; `DELETE` disarms
    (it tries disarm before cancel — an armed upgrade has no progress to
    abort). `status.armed` carries the target and the deadline.
  - **The gate fires on turn END, not on the idle transition.** The runtime
    flips Idle *before* the completion is broadcast, so an idle-time check
    still sees that very turn in flight and the gate would never fire. Driven
    by `runtime.WithOnTurnEnd`, fanned out via `Manager.AddTurnEndListener`.
  - A 30s ticker enforces the deadline, which has no event of its own. It is
    a safety net, not the mechanism.
  - Losing the race back to busy re-arms rather than dropping the request.
  - "Upgrade when idle" is the default offer on a busy machine; "now" is the
    secondary that then states its cost in turns and needs a second click.
- **V5 — CLIs.** Claude and Codex rows in the same dialog, nested under the
  machine that runs them. Dependencies live outside this repo and are pinned,
  not vendored: `agentkit v0.1.0` ships `runtime.InstallInspectable` with both
  adapters implemented, delegating to `claudecli-go v0.5.0` and
  `codexcli-go v0.1.0`.
  - **V5a — what is installed. Shipped.** `internal/update/cli.go` asks the capability
    per provider, caches on the existing hourly tick, and adds `clis[]` to
    `/api/update/status`: tool, installed version, path and real path, method,
    source, self-managed, update command, version manager, package manager,
    warnings. `internal/doctor` converts to the same source and gains its
    missing codex check. No verdict, no buttons (C15).
  - **V5b — the rows.** One expandable machine row per UI shape A, local
    expanded by default. Seven row states, of which *update available, nothing
    to click* is first-class, not an edge case. Plus `lastRan` from
    `runtime.SessionInitEvent.CLIVersion` (C12) — today it is dropped on the
    floor.
  - **V5c — the button.** Needs a perform-the-update method in both provider
    libraries and on the agentkit capability first; ships behind
    `[update] cli-updates`, default off, and verified against a throwaway
    server before it goes near a real one. The neutral outcome must separate
    five terminal states, because they are five different rows: **updated**
    (with before and after), **already current**, **manual** — not ours to
    update, carrying the command to show and where to run it, a normal result
    rather than an error — **blocked**, where it would be ours but preflight
    refuses, and **failed**. Manual and blocked are both "no button" and are
    not interchangeable: one is about ownership, the other about permission,
    and the user's next action differs. "Reported success but the version did
    not change" must be reachable as its own state and must be impossible to
    render as success — that is the observed `codex update` failure, and the
    one a naive implementation calls a win. Its twin is **unknown**: both
    libraries report an empty version when the probe fails, so `"" == ""` is a
    probe that could not see, not a binary that did not move. Equal-and-known
    is a failure; unknown is an update we cannot confirm, rendered as success
    with different words. Collapsing the two would make the honest case wear
    the accusation meant for the dishonest one.
  - **Outside this repo, in dependency order:** `claudecli-go` needs an
    `Update`, a published-version lookup (only it knows whether an install
    tracks `latest` or `stable`), and a PATH-entries report so C9 can be
    symmetric; `codexcli-go` needs its `Update` (queued for v0.2.0);
    `agentkit` needs the capability extended to perform, not just report.

When V3 lands, fold the drain-gate rule into the process-lifecycle block in
CLAUDE.md — a restart is not a pause, and anything that restarts the server
has to know it.

## Known risks and unverified claims

- **Windows is the least-exercised path** (replacing a running executable,
  restarting a scheduled task) and the Windows port has never been verified
  on real hardware. Ship V3 for Linux; Windows reports "manual" until it can
  be tested on the machine itself.
- **The restart hand-off** — reply `202`, flush, close listeners, let the
  service manager take over — is easy to get subtly wrong and wants a real
  test on throwaway servers, not a unit test.
- **macOS quarantine**: a binary fetched by our own Go code should carry no
  `com.apple.quarantine` attribute (that is applied by browsers, not plain
  HTTP clients), which would make darwin self-upgrade viable. That is
  inference, not something anyone has run — which is exactly why apply is
  gated behind verified platforms.
- **Release notes** are auto-generated from commits and may be noise rather
  than signal; a link may beat rendering them.
- **No CLI updater has been run by the service**, as opposed to by a person in
  a shell. That difference is what V3 learned to respect with throwaway servers,
  and it is why V5c's button ships off.
- **Updating an npm-global CLI under a live session is untested** — a running
  node process may lazily load from the tree being rewritten. Currently
  unreachable, because codex reports `SelfManaged: false` for npm installs and
  claude's npm-global installs hand back a command. It becomes live the moment
  any library calls such an install self-managed, which is also when C6's "no
  gate" needs revisiting.
- **`codex doctor --json` is a stringly-keyed `details` map** at schemaVersion
  1. Absorbed inside codexcli-go rather than in this repo, but an upstream
  rename still degrades a row to "unknown" — which is the correct failure, and
  the one to keep.
