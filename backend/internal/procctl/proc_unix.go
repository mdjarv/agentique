//go:build !windows

package procctl

import (
	"os"
	"os/exec"
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
