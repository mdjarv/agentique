//go:build !windows

package procctl

import (
	"bytes"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
)

// configureDetached starts the child in a new session so it is not killed when
// the caller (e.g. the tray) exits.
func configureDetached(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}

// Alive reports whether a process exists. Signal 0 probes existence without
// affecting the process.
func Alive(pid int) bool {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return proc.Signal(syscall.Signal(0)) == nil
}

// Terminate requests a graceful shutdown (SIGTERM).
func Terminate(pid int) error {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return proc.Signal(syscall.SIGTERM)
}

// Kill forces termination (SIGKILL).
func Kill(pid int) error {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return proc.Signal(syscall.SIGKILL)
}

// terminateGroup sends SIGTERM to the whole process group pgid (kill(-pgid)),
// reaping a CLI subprocess together with its own children (e.g. the Playwright
// MCP node/chromium subtree, which inherit the group). Refuses to signal the
// caller's own group or the init/undefined groups.
func terminateGroup(pgid int) error { return signalGroup(pgid, syscall.SIGTERM) }

// killGroup is terminateGroup with SIGKILL.
func killGroup(pgid int) error { return signalGroup(pgid, syscall.SIGKILL) }

func signalGroup(pgid int, sig syscall.Signal) error {
	if pgid <= 1 || pgid == syscall.Getpgrp() {
		return errRefuseSelfGroup
	}
	return syscall.Kill(-pgid, sig)
}

// findCLIProcesses scans /proc for agentique-spawned CLI subprocesses, returning
// their pid, parent pid, and process-group id. Unreadable or vanished entries are
// skipped (best-effort). A process qualifies only when BOTH hold:
//
//   - its command line passes isAgentiqueCLICmdline — the marker appears as the
//     value of the --append-system-prompt flag, not merely somewhere on the line,
//     so a user's interactive `claude` whose prompt happens to mention Agentique
//     is never matched; and
//   - it is its own process-group leader (pgid == pid) — every session CLI is
//     spawned with Setpgid, so this holds for all real targets and excludes
//     incidental shell commands that inherit their shell's group; and
//   - when owner is non-empty, its environment carries OwnerEnvVar=owner, so a
//     CLI spawned by a *different* agentique instance (a sandboxed verify run
//     with its own AGENTIQUE_HOME) is not ours to signal.
//
// These together identify agentique CLIs precisely enough that the reapers can
// rely on parentage (see ReapOrphanedCLIProcesses / KillCLIChildrenOf) rather
// than a fragile substring match.
func findCLIProcesses(owner string) []CLIProcess {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil
	}
	var out []CLIProcess
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue // not a pid dir
		}
		cmdline, err := os.ReadFile("/proc/" + e.Name() + "/cmdline")
		if err != nil || !isAgentiqueCLICmdline(cmdline) {
			continue
		}
		if owner != "" && !hasOwner("/proc/"+e.Name()+"/environ", owner) {
			continue // spawned by a different agentique instance — not ours
		}
		ppid, pgid, ok := readParentAndGroup("/proc/" + e.Name() + "/stat")
		if !ok || pgid != pid {
			continue // only own-group leaders are real spawned CLIs
		}
		out = append(out, CLIProcess{PID: pid, PPID: ppid, PGID: pgid})
	}
	return out
}

// hasOwner reports whether the process whose environment lives at environPath
// was spawned by the agentique instance owning the data dir owner, i.e. its
// environment contains exactly OwnerEnvVar=owner.
//
// Fails CLOSED: an unreadable environ (a process of another user, or one that
// exited mid-scan) yields false, so a candidate we cannot attribute is never
// signaled. /proc/<pid>/environ is the process's *initial* environment, which is
// what we want — it cannot be rewritten by the child after exec.
func hasOwner(environPath, owner string) bool {
	data, err := os.ReadFile(environPath)
	if err != nil {
		return false
	}
	want := []byte(OwnerEnvVar + "=" + owner)
	for _, kv := range bytes.Split(data, []byte{0}) {
		if bytes.Equal(kv, want) {
			return true
		}
	}
	return false
}

// isAgentiqueCLICmdline reports whether a NUL-separated /proc cmdline is an
// agentique-spawned provider CLI: it must carry the CLIProcessMarker as the
// value of the --append-system-prompt flag (how agentique injects the session
// preamble). Requiring the flag/value adjacency — not a bare substring — means
// a process that merely mentions the marker text in a prompt or argument does
// not qualify.
func isAgentiqueCLICmdline(cmdline []byte) bool {
	args := bytes.Split(cmdline, []byte{0})
	marker := []byte(CLIProcessMarker)
	for i := 0; i+1 < len(args); i++ {
		if string(args[i]) == appendSystemPromptFlag && bytes.Contains(args[i+1], marker) {
			return true
		}
	}
	return false
}

// readParentAndGroup parses ppid (field 4) and pgrp (field 5) from a
// /proc/<pid>/stat file. The comm field (2) is wrapped in parentheses and may
// itself contain spaces and parens, so fields are read after the final ')'.
func readParentAndGroup(statPath string) (ppid, pgid int, ok bool) {
	data, err := os.ReadFile(statPath)
	if err != nil {
		return 0, 0, false
	}
	s := string(data)
	rparen := strings.LastIndexByte(s, ')')
	if rparen < 0 || rparen+2 >= len(s) {
		return 0, 0, false
	}
	// Fields after "(comm) ": state ppid pgrp ...
	fields := strings.Fields(s[rparen+2:])
	if len(fields) < 3 {
		return 0, 0, false
	}
	ppid, err = strconv.Atoi(fields[1])
	if err != nil {
		return 0, 0, false
	}
	pgid, err = strconv.Atoi(fields[2])
	if err != nil {
		return 0, 0, false
	}
	return ppid, pgid, true
}
