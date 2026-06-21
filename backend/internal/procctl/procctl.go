// Package procctl provides cross-platform process control: liveness checks,
// graceful/forceful termination by PID, and launching detached background
// processes that outlive the caller. Platform specifics live in the build-tagged
// proc_unix.go / proc_windows.go files; this file holds the shared surface.
package procctl

import "os/exec"

// StartDetached launches name+args as a background process that outlives the
// caller — it has no controlling terminal (unix) / no console window (windows)
// and is not killed when the caller exits. Returns the child PID. The child's
// stdio is not inherited.
func StartDetached(name string, args ...string) (int, error) {
	cmd := exec.Command(name, args...)
	configureDetached(cmd)
	if err := cmd.Start(); err != nil {
		return 0, err
	}
	pid := cmd.Process.Pid
	// Release the handle — the process is independent now; we track it by PID.
	_ = cmd.Process.Release()
	return pid, nil
}
