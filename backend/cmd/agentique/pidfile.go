package main

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/mdjarv/agentique/backend/internal/paths"
	"github.com/mdjarv/agentique/backend/internal/procctl"
)

// pidFilePath is where a running server records its PID so the tray / CLI can
// find and stop a server they did not start.
func pidFilePath() string {
	return filepath.Join(paths.DataDir(), "agentique.pid")
}

// writePIDFile records the current process PID atomically (temp + rename) so a
// concurrent reader never sees a partial write.
func writePIDFile() error {
	path := pidFilePath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(strconv.Itoa(os.Getpid())), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// removePIDFile clears the pid file; missing is not an error.
func removePIDFile() {
	_ = os.Remove(pidFilePath())
}

// readServerPID returns the PID recorded in the pid file and whether that
// process is currently alive. A stale file (process gone) reports alive=false.
func readServerPID() (pid int, alive bool) {
	data, err := os.ReadFile(pidFilePath())
	if err != nil {
		return 0, false
	}
	pid, err = strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 0 {
		return 0, false
	}
	return pid, procctl.Alive(pid)
}
