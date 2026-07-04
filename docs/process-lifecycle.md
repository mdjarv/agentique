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
   reparents to init and survives until reboot, leaking a `claude` process plus
   its Playwright/Chromium subtree. Restarts do not clean it up (`RecoverStale
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
- `ReapOrphanedCLIProcesses()` runs at startup (in `server.New`, right after
  `RecoverStaleSessions`, gated on `!TestMode`). It scans `/proc` for processes
  whose command line contains `CLIProcessMarker` (`"running inside Agentique"`,
  injected into every session's system prompt via `--append-system-prompt`;
  `preamble.go` `preambleIdentity`) **and** that are reparented to init
  (`PPID == 1`), then SIGTERMs their process group. Safe because the
  single-instance guard (`isServerRunning()`) runs before `server.New` and
  startup does not auto-resume, so any such orphan can only belong to a dead
  server. A live session (child of the running server, `PPID != 1`) is never
  matched.
- `KillCLIChildrenOf(os.Getpid())` runs as a **shutdown backstop** after
  `srv.Shutdown()` in `serve.go`, SIGKILLing the group of any marker-matching
  process still a direct child of the server — catching the shutdown race (#2).
- A session-package test (`preamble_marker_test.go`) asserts the marker stays a
  substring of `preambleIdentity`, so a preamble reword can't silently blind the
  reaper. Windows does not enumerate (`findCLIProcesses` returns nil) — its
  orphan model differs; tracked separately with the Windows port.

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

## Known gap — PID not exposed
The reaper matches by command-line marker because `claudecli.ProcessInfo` (and
therefore `runtime.ProcessInfo`) does not expose the child's OS PID. Exposing it
(pending changes in claudecli-go + agentkit) would let agentique target exact
PIDs for the shutdown backstop and force-kill a stuck session mid-life, replacing
the heuristic. See `docs/tech-debt.md`.
