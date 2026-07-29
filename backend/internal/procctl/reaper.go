package procctl

import (
	"errors"
	"log/slog"
	"os"
)

// appendSystemPromptFlag is the provider-CLI flag agentique uses to inject the
// session preamble (which carries CLIProcessMarker). The reaper requires the
// marker to appear as this flag's value, not merely anywhere on the command
// line, so a user's interactive CLI whose prompt mentions Agentique never
// matches.
const appendSystemPromptFlag = "--append-system-prompt"

// errRefuseSelfGroup guards the group-signal helpers from ever targeting the
// caller's own process group or the init/undefined groups.
var errRefuseSelfGroup = errors.New("procctl: refusing to signal own or invalid process group")

// ErrNoOwner is returned by the reapers when called without an owner data dir.
// An empty authority must ABORT, never widen to "everything is mine" — the same
// rule that governs the worktree sweep.
var ErrNoOwner = errors.New("procctl: refusing to reap without an owner data dir")

// OwnerEnvVar names the environment variable agentique stamps onto every CLI
// subprocess it spawns, carrying the absolute data directory of the server that
// owns it. It scopes a reaper's authority to its own instance: a sandbox server
// (AGENTIQUE_HOME=/tmp/... for a local verify run) resolves a different data dir
// and therefore cannot match — let alone signal — the production server's CLIs.
//
// Marker + own-group leadership identify "an agentique CLI"; this identifies
// "an agentique CLI that is MINE". Process isolation does not follow from an
// isolated DB, port, or AGENTIQUE_HOME — those isolate state, not the process
// table — so without this stamp a second instance's startup reap kills every
// live session of the first (which is exactly what happened on 2026-07-29).
//
// It is set with os.Setenv, so it propagates by ordinary environment
// inheritance to the provider CLI and its whole subtree — no per-provider
// plumbing, and it covers claude, codex, and any future adapter alike.
const OwnerEnvVar = "AGENTIQUE_OWNER_DATADIR"

// StampOwner records dataDir as this process's owner identity so every CLI
// subprocess spawned later inherits it. Call once, early in server startup,
// before any session is created.
func StampOwner(dataDir string) error {
	if dataDir == "" {
		return ErrNoOwner
	}
	return os.Setenv(OwnerEnvVar, dataDir)
}

// CLIProcessMarker is a substring present in the command line of every provider
// CLI subprocess agentique spawns for a session — it is injected into the
// session system prompt via the CLI's --append-system-prompt (see
// session.preambleIdentity, which is the prefix of both buildPreamble and
// buildPersonaPreamble). The reaper uses it to recognize agentique-owned CLI
// processes without needing the OS PID from the provider library.
//
// This value must stay a substring of session.preambleIdentity; a unit test in
// the session package asserts that invariant so a preamble reword can't silently
// blind the reaper.
const CLIProcessMarker = "running inside Agentique"

// CLIProcess is a running CLI subprocess matched by the reaper.
type CLIProcess struct {
	PID  int // process ID
	PPID int // parent process ID (a reaper process — init or a subreaper — once orphaned)
	PGID int // process group ID; equals PID (own-group leader) for every match
}

// FindCLIProcesses returns every running agentique CLI process owned by owner
// (its OwnerEnvVar equals owner). Best-effort: enumeration errors yield a
// partial list rather than failing. Platform-specific — proc_unix.go scans
// /proc; proc_windows.go does not enumerate (returns nil) as the orphan model
// differs there.
//
// An empty owner disables the ownership filter and returns every agentique CLI
// regardless of which instance spawned it. That is for diagnostics only — never
// pass "" on a path that signals, which is why the reapers reject it outright.
func FindCLIProcesses(owner string) []CLIProcess { return findCLIProcesses(owner) }

// ReapOrphanedCLIProcesses terminates the process group of every agentique CLI
// process orphaned by a prior server that exited without cleaning up its
// children. Returns the number of process groups signaled.
//
// An orphan is defined as "not a child of the current process" (PPID !=
// os.Getpid()), NOT "reparented to init (PPID == 1)": on a systemd user session
// a dead parent's children reparent to the systemd --user *subreaper*, not pid 1,
// so a PPID==1 test would miss every orphan. The current server just started and
// (per the caller) has spawned no sessions yet, so any process findCLIProcesses
// returns is by construction not our child.
//
// SAFETY, in layers — every one of them must hold:
//   - owner (this instance's data dir) is required and matched against each
//     candidate's OwnerEnvVar, so only CLIs THIS instance spawned are eligible;
//     an empty owner returns ErrNoOwner rather than reaping everything.
//   - the caller holds this data dir's instance lock, so no other server owns
//     the matched processes.
//   - the caller has not resumed any session yet, so nothing matched can be live.
//
// findCLIProcesses (marker as --append-system-prompt value + own-group leader +
// owner match) therefore identifies exactly our own leaked CLIs. An unrelated
// `claude` (SSH/interactive, reviewbot, a nested agent-run claude) carries no
// agentique preamble via --append-system-prompt and is never matched.
func ReapOrphanedCLIProcesses(owner string) (int, error) {
	if owner == "" {
		return 0, ErrNoOwner
	}
	self := os.Getpid()
	n := 0
	for _, p := range findCLIProcesses(owner) {
		if p.PPID == self {
			continue // our own child (none at startup) — never reap
		}
		if err := terminateGroup(p.PGID); err != nil {
			slog.Warn("orphan CLI reap: SIGTERM failed", "pid", p.PID, "pgid", p.PGID, "error", err)
			continue
		}
		slog.Info("reaped orphaned CLI process group", "pid", p.PID, "ppid", p.PPID, "pgid", p.PGID)
		n++
	}
	return n, nil
}

// KillCLIChildrenOf force-kills (SIGKILL) the process group of every agentique
// CLI process that is a direct child of parentPID. It is a shutdown backstop:
// after graceful Close, any session subprocess still alive (e.g. because a
// cooperative Close raced a shutdown timeout while a turn was in flight) would
// otherwise be orphaned when the server exits. Returns the number of groups
// signaled. Pass os.Getpid().
//
// owner is required for the same reason as in ReapOrphanedCLIProcesses. Direct
// parentage already scopes this to our own children, so the owner match is
// belt-and-braces — but a backstop that can only ever be as safe as its weakest
// caller should not have a mode where the scope is "every agentique CLI".
func KillCLIChildrenOf(parentPID int, owner string) (int, error) {
	if owner == "" {
		return 0, ErrNoOwner
	}
	n := 0
	for _, p := range findCLIProcesses(owner) {
		if p.PPID != parentPID {
			continue
		}
		if err := killGroup(p.PGID); err != nil {
			slog.Warn("shutdown backstop: SIGKILL failed", "pid", p.PID, "pgid", p.PGID, "error", err)
			continue
		}
		slog.Info("shutdown backstop killed surviving CLI process group", "pid", p.PID, "pgid", p.PGID)
		n++
	}
	return n, nil
}
