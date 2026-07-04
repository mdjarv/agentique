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

// FindCLIProcesses returns every running process whose command line contains
// CLIProcessMarker. Best-effort: enumeration errors yield a partial list rather
// than failing. Platform-specific — proc_unix.go scans /proc; proc_windows.go
// does not enumerate (returns nil) as the orphan model differs there.
func FindCLIProcesses() []CLIProcess { return findCLIProcesses() }

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
// SAFETY: only call this when no other agentique server is running (the caller
// guarantees single-instance before serving) and before spawning any sessions.
// Under that guarantee, findCLIProcesses (marker as --append-system-prompt value
// + own-group leader) matches only agentique-spawned CLIs, and none can belong to
// a live server — so terminating them is unambiguous. An unrelated `claude`
// (SSH/interactive, reviewbot, a nested agent-run claude) carries no
// agentique preamble via --append-system-prompt and is never matched.
func ReapOrphanedCLIProcesses() int {
	self := os.Getpid()
	n := 0
	for _, p := range findCLIProcesses() {
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
	return n
}

// KillCLIChildrenOf force-kills (SIGKILL) the process group of every agentique
// CLI process that is a direct child of parentPID. It is a shutdown backstop:
// after graceful Close, any session subprocess still alive (e.g. because a
// cooperative Close raced a shutdown timeout while a turn was in flight) would
// otherwise be orphaned when the server exits. Returns the number of groups
// signaled. Pass os.Getpid().
func KillCLIChildrenOf(parentPID int) int {
	n := 0
	for _, p := range findCLIProcesses() {
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
	return n
}
