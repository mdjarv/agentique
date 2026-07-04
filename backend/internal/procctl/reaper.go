package procctl

import (
	"errors"
	"log/slog"
)

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
	PPID int // parent process ID (1 when reparented to init, i.e. orphaned)
	PGID int // process group ID — the whole session subtree shares it
}

// FindCLIProcesses returns every running process whose command line contains
// CLIProcessMarker. Best-effort: enumeration errors yield a partial list rather
// than failing. Platform-specific — proc_unix.go scans /proc; proc_windows.go
// does not enumerate (returns nil) as the orphan model differs there.
func FindCLIProcesses() []CLIProcess { return findCLIProcesses() }

// ReapOrphanedCLIProcesses terminates the process group of every agentique CLI
// process that has been reparented to init (PPID == 1) — i.e. orphaned by a
// prior server that exited without cleaning up its children. Returns the number
// of process groups signaled.
//
// SAFETY: only call this when no other agentique server is running (the caller
// guarantees single-instance before serving). Under that guarantee, a
// marker-matching process reparented to init can only belong to a dead server,
// never to a live one, so terminating it is unambiguous. A still-running server
// keeps its children as direct descendants (PPID == that server), so they are
// never matched here.
func ReapOrphanedCLIProcesses() int {
	n := 0
	for _, p := range findCLIProcesses() {
		if p.PPID != 1 {
			continue
		}
		if err := terminateGroup(p.PGID); err != nil {
			slog.Warn("orphan CLI reap: SIGTERM failed", "pid", p.PID, "pgid", p.PGID, "error", err)
			continue
		}
		slog.Info("reaped orphaned CLI process group", "pid", p.PID, "pgid", p.PGID)
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
