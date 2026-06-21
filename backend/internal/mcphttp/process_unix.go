//go:build !windows

package mcphttp

import (
	"os"
	"syscall"
)

// terminateProcess asks a process to exit gracefully (SIGTERM).
func terminateProcess(proc *os.Process) error {
	return proc.Signal(syscall.SIGTERM)
}

// forceKillProcess terminates a process immediately (SIGKILL).
func forceKillProcess(proc *os.Process) error {
	return proc.Signal(syscall.SIGKILL)
}

// pidAlive reports whether a process exists. Signal 0 probes existence without
// affecting the process.
func pidAlive(pid int) bool {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return proc.Signal(syscall.Signal(0)) == nil
}
