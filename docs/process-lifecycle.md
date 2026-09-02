# CLI subprocess lifecycle & leak prevention

Every session drives a provider CLI subprocess (`claude` / `codex`) spawned via
`agentkit/runtime` → the claudecli/codexcli adapters. Each child is placed in its
**own process group** (`Setpgid: true` in claudecli's `executor_unix.go`) as a
direct child of the agentique server, and it may spawn its own children — most
notably the Playwright MCP node/Chromium subtree, which inherit the group. The
child is only ever terminated cooperatively: `Session.Close()` closes stdin,
waits a 5s grace period for a clean exit, then SIGTERMs the **group**
(`kill(-pgid)`), reaping the subtree.

agentique intentionally spawns with `context.Background()` (see
`manager.go` `rt.Create`), so a child's lifetime is independent of the request
that created it. That is correct — but it means the child's *only* death paths
are `Session.Close()` (via Stop / Delete / Evict / server Shutdown) and the
mechanisms below. There is **no ambient-context safety net**.

## Failure modes this addresses

1. **Orphans on an ungraceful server exit.** On SIGKILL / crash / OOM the server
   dies without running Shutdown. Because each child is in its own group and only
   a child of the server (never in the server's group), nothing signals it — it
   reparents to init (or the systemd `--user` subreaper) and survives until
   reboot, leaking a `claude` process plus its Playwright/Chromium subtree.
   Restarts do not clean it up (`RecoverStale
   Sessions` only rewrites DB rows), so orphans accumulate across restarts.
   On a swapless box this feeds a vicious cycle: warm processes exhaust memory →
   OOM-kill → orphan storm → restart → repeat.
2. **Shutdown race.** Even on a clean SIGTERM, `runtime.Manager.CloseAll` bounds
   each session to 5s while `claudecli.Close` alone can spend 5s in its stdin-EOF
   grace before it SIGTERMs. A busy session can be abandoned mid-kill.
3. **Steady-state warm-process bloat.** A completed turn leaves the session
   *idle* with its CLI (and browser subtree) still resident for the whole server
   lifetime. Many started-but-not-deleted sessions ⇒ many warm processes.

## Mechanisms

### Orphan reaper — `internal/procctl` (Layer A)
- `ReapOrphanedCLIProcesses()` runs at startup (in `serve.go`, in the
  production-only `!testMode` block next to `SweepOrphans` — deliberately kept
  out of `server.New`, since a constructor must have no destructive side
  effects). `findCLIProcesses` scans `/proc` and matches a process only when
  **both** hold: (a) `CLIProcessMarker` (`"running inside Agentique"`, from
  `preamble.go` `preambleIdentity`) appears as the **value of the
  `--append-system-prompt` flag** — not merely somewhere on the line, so a
  user's interactive `claude` whose *prompt* mentions Agentique is never matched;
  and (b) the process is its **own process-group leader** (`pgid == pid`, true for
  every Setpgid-spawned CLI). It then SIGTERMs the process group of each match
  that is **not a child of the current server** (`PPID != os.Getpid()`).
  - **Orphan = "not our child", not "PPID == 1".** On a systemd *user* session a
    dead parent's children reparent to the systemd `--user` **subreaper**, not
    pid 1, so a `PPID == 1` test misses every orphan (verified empirically). Since
    the single-instance guard ran and startup has spawned no sessions, any match
    is by construction not the current server's child, hence an orphan of a dead
    prior server.
  - Safe for unrelated CLIs: an SSH/interactive `claude`, reviewbot's claude, or a
    nested agent-run `claude` carries no agentique preamble via
    `--append-system-prompt`, so it fails the match regardless of parentage.
- `KillCLIChildrenOf(os.Getpid())` runs as a **shutdown backstop** after
  `srv.Shutdown()` in `serve.go`, SIGKILLing the group of any marker-matching
  process still a direct child of the server — catching the shutdown race (#2).
- A session-package test (`preamble_marker_test.go`) asserts the marker stays a
  substring of `preambleIdentity`, so a preamble reword can't silently blind the
  reaper. Windows does not enumerate (`findCLIProcesses` returns nil) — orphans
  are prevented there rather than reaped; see "Job-object containment" below.

### Idle eviction — `internal/session/idle_evict.go` (Layer C)
Opt-in via `[session] idle-evict-timeout` (env
`AGENTIQUE_SESSION_IDLE_EVICT_TIMEOUT`; default off). A background sweep stops
sessions idle at least the TTL, reusing `StopSession` (browser cleanup +
git-version seed + `stopped` DB state). The session lazy-resumes on the next
message via `Service.ensureLive`. The eviction claim (`Session.beginIdleEvict`)
and a turn start (`validateAndPrepareQuery`) are mutually exclusive under
`s.mu`: whichever wins first either refreshes `lastActiveAt` (claim skipped) or
sets `evicting` (turn refused), so a turn can never start on a session being
torn down. Verified by a `-race` mutual-exclusion test.

**A reclaim says so, because reusing `StopSession` makes it indistinguishable
from one.** Right for the mechanism, wrong for the story: the row an eviction
left was byte-identical to the one a person's stop button leaves, so every
surface read it as a deliberate stop — the chat pane announced "Session
interrupted" and offered Resume for a session nothing had interrupted, on every
session left alone past the TTL. `sessions.evicted_at` is the difference. It is
written by the sweep and by nothing else (a restart's reap and a person's stop
both really did end something and stay unmarked), it rides `GitSnapshot` and
`SessionInfo` as the optional `evictedAt`, and it is stamped **before** the
stop — the mirror on `Session` exists only so the `stopped` push that announces
the eviction already carries the reason, since a client that learned it one
refresh later would have drawn the banner and taken it back. Resuming clears it,
so the mark always describes the most recent stop. Downstream, `deriveRestToken`
turns it into the `evicted` token and `ChatPanel` suppresses the resume banner,
on the same argument that suppresses it for a schedule-parked session.

A suggested TTL is hours rather than minutes. The sweep costs the next message a
lazy resume, which is cheap but not free, and a TTL shorter than a lunch break
reclaims sessions their operator is still in the middle of using.

### cgroup containment — systemd unit (the OS-level guarantee)
When agentique runs as its systemd unit (`internal/service/systemd.go`), every
session CLI and its Playwright/Chromium subtree lives in the unit's cgroup, so the
kernel/systemd govern the whole tree as one:
- **`KillMode=control-group`** — a stop/restart SIGKILLs the entire cgroup after
  the stop timeout, so a restart can never orphan a subprocess regardless of
  process groups. This is the *primary* teardown guarantee; the in-process reaper
  above is a backstop for non-systemd launches and mid-life stuck sessions.
- **`OOMPolicy=kill`** — if any member is OOM-killed, the whole unit is torn down
  and restarted (via `Restart=on-failure`) rather than left half-dead.
- **`MemoryHigh` / `MemoryMax`** (percent of host RAM, e.g. 70% / 85%) — bound
  agentique's total footprint so a burst of warm sessions can't exhaust host
  memory and force a reboot. The cgroup OOMs *within its own limit* instead of
  taking the box down; combined with idle eviction, steady-state footprint stays
  bounded. Tune via the unit or a drop-in.

This is what makes DB-persisted PIDs unnecessary for crash-safety: the kernel
enforces subtree cleanup, no bookkeeping that PID reuse could corrupt.

### Job-object containment — Windows (the OS-level guarantee there)
Windows has neither POSIX process groups the reaper could signal nor a `/proc`
to scan, so containment inverts: instead of reaping orphans, it makes them
impossible. `procctl.ConfineProcessTree()` (called from `serve.go` right after
`StampOwner`) puts the server itself in a job object with
`JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE`; every descendant — the provider CLI, its
MCP servers, node, Chromium — inherits membership. The server holds the only
job handle and never closes it, so *any* exit (graceful, crash,
`schtasks /End`, TerminateProcess) closes the handle table and the kernel kills
the whole tree. Both reapers stay no-ops on Windows because there is nothing
left for them to find; the one gap is a boot where confinement itself failed
(nested-job support needs Windows 8+), which is logged at startup and degrades
to the old leak, never to a refused boot.

**Graceful stop is a named kernel event, because Windows has no SIGTERM.** A
Scheduled Task's `/End` is a hard TerminateProcess, which used to mean the
serve loop's shutdown block (HTTP drain, cooperative session close, pid-file
removal) never ran under service management. `procctl.NotifyStopRequests`
creates `Global\agentique-stop-<sha256(datadir)[:8]>` — keyed by data dir like
the instance lock, `Global\` so an SSH session can stop the interactive
session's server, default DACL so another local user cannot — and the serve
loop selects on it beside SIGTERM. `agentique service stop` and the tray call
`procctl.RequestStop` first and fall back to the hard path only when nothing
listens or the grace window (30s) expires; with the job object, even that
fallback no longer leaks — it only skips the drain. On unix both functions are
no-ops (`RequestStop` reports `ErrNoStopListener`) and `Terminate`'s SIGTERM
remains the graceful path.

What job confinement does **not** give: per-session tree-kill. Stopping one
session still goes through the connector's cooperative close, and on Windows
claudecli-go's kill reaches only `claude.exe` itself — its MCP children exit on
stdin EOF, best-effort. The exact fix is a per-spawn job object inside
claudecli-go/codexcli-go, which own the `exec.Cmd`; until then a wedged MCP
child survives its session but never the server.

## Known gap — PID not exposed
The reaper matches by command-line marker because `claudecli.ProcessInfo` (and
therefore `runtime.ProcessInfo`) does not expose the child's OS PID. Exposing it
(pending changes in claudecli-go + agentkit) would let agentique target exact
PIDs for the shutdown backstop and force-kill a stuck session mid-life, replacing
the heuristic. See `docs/tech-debt.md`.
