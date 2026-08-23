# In-app upgrades — one click per machine, no dead turns

Status: **designed, not implemented.** Decisions settled 2026-08-23 after
review (proposal artifact `6fd17232-9ccc-4aad-9e1e-1d51206bd499`); phases
V1–V5 below, none started. This document is the working contract — a session
picking up any phase should need nothing else.

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
| Asking GitHub for the latest tag            | —                                           | **new**      |
| Per-machine version client-side             | descriptor probe keeps only `machineId`     | **new** (small) |
| An endpoint that performs the upgrade       | —                                           | **new**      |
| Knowing whether it is safe to restart       | turn registry exists; nothing consults it   | **new**      |

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
  "checkedAt": "2026-08-23T12:04:11Z",
  "busy":      false,          // a turn is running here right now
  "armed":     false,          // waiting for idle to upgrade itself
  "progress":  null,           // or the live phase
  "notes":     "…release notes, truncated…"
}

POST   /api/update/apply      body {"expect": "v0.5.0"}
         → 202, progress events, then the socket drops
DELETE /api/update/apply      cancel: disarm, or abort before replacing
```

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

Busy is answered by the turn registry (the same source of truth scheduled
loops use — never state polling):

- **idle** → upgrade now.
- **busy** → offer *upgrade when idle*: arm a one-shot that fires on the next
  idle transition.
- **override** → allowed, but the button states the cost ("2 turns will be
  terminated") and is a deliberate second click, never the default.

Armed state carries a **deadline** (default a few hours) after which it
disarms and says so, and is **in-memory only** — if the server restarts for
any other reason the arming is forgotten. That is the fail-safe direction: an
upgrade armed on Tuesday must not fire on Thursday because a lid closed at
the wrong moment.

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

Deferred to V5. `internal/doctor` already gives the installed version;
`npm view <pkg> version` gives the published one on the same hourly tick.

**Detect how each CLI was installed — never assume.** Resolve the real binary
(`exec.LookPath` + symlink resolution) and classify by where it lands: an npm
global prefix, a version-manager shim (fnm/nvm/volta), or a native install
dir; prefer whatever the tool reports about itself over our inference. The
install method decides which command we show. Showing the wrong one does not
fail cleanly — it half-installs a second copy and the version we probe stops
describing the one that runs.

Surface the fact and offer the tool's own updater. Do not reimplement it.

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
- **Version numbers never gate behaviour**; capabilities do.
- **A dev build never nags.**

## Phases

- **V1 — Know.** `/api/update/status`, hourly ETag-cached check, per-machine
  version kept client-side, versions listed in Settings › About. No chip, no
  button, no restart path. Zero risk, and with machines drifting
  independently, seeing the versions side by side is already most of the
  value. Worth shipping alone.
- **V2 — Tell.** Footer chip and the dialog, fanned out across machines.
  Still no button.
- **V3 — Apply, narrated.** Verification, `.prev` retention, restart,
  reconnect-and-confirm, per-phase progress as state *and* events, cancel
  through verification. Narration is not a follow-up: an unnarrated 30-second
  binary swap is the version nobody trusts twice. Verified on throwaway
  servers (isolated `AGENTIQUE_HOME`) before it goes near a real one; gated
  to `linux/amd64`.
- **V4 — Wait for idle.** Drain gate, armed one-shot with deadline and
  cancel, override with its honest warning. V3 simply refuses while busy
  until this lands.
- **V5 — CLIs.** Claude and Codex rows: installed version, published version,
  install-method detection, the right command for the method found.

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
