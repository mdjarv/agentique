//go:build windows

package mcphttp

import (
	"os"
	"syscall"
)

// Windows has no SIGTERM/SIGKILL distinction; both graceful and forceful
// termination map to TerminateProcess via (*os.Process).Kill.

func terminateProcess(proc *os.Process) error { return proc.Kill() }

func forceKillProcess(proc *os.Process) error { return proc.Kill() }

const (
	// synchronizeAccess is the SYNCHRONIZE right needed to wait on a process.
	synchronizeAccess = 0x00100000
	// waitTimeout is returned by WaitForSingleObject when the process has not
	// exited; WAIT_OBJECT_0 (0) means it has.
	waitTimeout = 0x00000102
)

// pidAlive reports whether a process is still running. We open the process for
// SYNCHRONIZE and probe with a zero-timeout wait: a timeout means it is still
// alive, a signaled handle means it has exited. A failed open (e.g. the PID is
// gone or access-denied because it already terminated) is treated as dead.
func pidAlive(pid int) bool {
	h, err := syscall.OpenProcess(synchronizeAccess, false, uint32(pid))
	if err != nil {
		return false
	}
	defer syscall.CloseHandle(h)
	event, err := syscall.WaitForSingleObject(h, 0)
	if err != nil {
		return false
	}
	return event == waitTimeout
}
