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

// findCLIProcesses scans /proc for processes whose command line contains
// CLIProcessMarker, returning their pid, parent pid, and process-group id.
// Unreadable or vanished entries are skipped (best-effort).
func findCLIProcesses() []CLIProcess {
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
		if err != nil || !bytes.Contains(cmdline, []byte(CLIProcessMarker)) {
			continue
		}
		ppid, pgid, ok := readParentAndGroup("/proc/" + e.Name() + "/stat")
		if !ok {
			continue
		}
		out = append(out, CLIProcess{PID: pid, PPID: ppid, PGID: pgid})
	}
	return out
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
