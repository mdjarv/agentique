//go:build windows

package procctl

import (
	"os"
	"os/exec"
	"syscall"
)

const (
	// detachedProcess runs the child without a console (background server).
	detachedProcess = 0x00000008
	// createNewProcessGroup isolates the child from the caller's Ctrl signals.
	createNewProcessGroup = 0x00000200
)

func configureDetached(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: detachedProcess | createNewProcessGroup,
	}
}

const (
	// synchronizeAccess is the SYNCHRONIZE right needed to wait on a process.
	synchronizeAccess = 0x00100000
	// waitTimeout is returned by WaitForSingleObject when the process is still
	// running; WAIT_OBJECT_0 (0) means it has exited.
	waitTimeout = 0x00000102
)

// Alive reports whether a process is still running. A zero-timeout wait on the
// process handle distinguishes running (timeout) from exited (signaled); a
// failed open means it is gone.
func Alive(pid int) bool {
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

// Terminate and Kill both map to TerminateProcess on Windows — there is no
// graceful signal equivalent to SIGTERM.
func Terminate(pid int) error { return killByPID(pid) }

func Kill(pid int) error { return killByPID(pid) }

func killByPID(pid int) error {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return proc.Kill()
}
