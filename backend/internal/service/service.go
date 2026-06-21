// Package service manages platform-specific service installation (systemd, launchd).
package service

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

// Status represents the current state of the installed service.
type Status struct {
	Installed bool
	Running   bool
	PID       int
	UnitPath  string
}

// Install registers and starts the service for the current platform.
func Install() error {
	bin, err := binaryPath()
	if err != nil {
		return fmt.Errorf("resolve binary path: %w", err)
	}

	switch runtime.GOOS {
	case "linux":
		return installSystemd(bin)
	case "darwin":
		return installLaunchd(bin)
	case "windows":
		return installWindows(bin)
	default:
		return unsupportedError()
	}
}

// Uninstall stops and removes the service.
func Uninstall() error {
	switch runtime.GOOS {
	case "linux":
		return uninstallSystemd()
	case "darwin":
		return uninstallLaunchd()
	case "windows":
		return uninstallWindows()
	default:
		return unsupportedError()
	}
}

// GetStatus returns the current service status.
func GetStatus() (Status, error) {
	switch runtime.GOOS {
	case "linux":
		return statusSystemd()
	case "darwin":
		return statusLaunchd()
	case "windows":
		return statusWindows()
	default:
		return Status{}, unsupportedError()
	}
}

// Restart stops and starts the service.
func Restart() error {
	switch runtime.GOOS {
	case "linux":
		return restartSystemd()
	case "darwin":
		return restartLaunchd()
	case "windows":
		return restartWindows()
	default:
		return unsupportedError()
	}
}

// Start starts the service.
func Start() error {
	switch runtime.GOOS {
	case "linux":
		return startSystemd()
	case "darwin":
		return startLaunchd()
	case "windows":
		return startWindows()
	default:
		return unsupportedError()
	}
}

// Stop stops the service.
func Stop() error {
	switch runtime.GOOS {
	case "linux":
		return stopSystemd()
	case "darwin":
		return stopLaunchd()
	case "windows":
		return stopWindows()
	default:
		return unsupportedError()
	}
}

// LogsCmd returns an exec.Cmd that streams service logs.
// The caller is responsible for running it (e.g. cmd.Run() with stdout/stderr wired up).
func LogsCmd() (*exec.Cmd, error) {
	switch runtime.GOOS {
	case "linux":
		return logsSystemd(), nil
	case "darwin":
		return logsLaunchd(), nil
	case "windows":
		return logsWindows(), nil
	default:
		return nil, unsupportedError()
	}
}

func unsupportedError() error {
	return fmt.Errorf("service management not supported on %s (supported: linux, macOS, windows)", runtime.GOOS)
}

func binaryPath() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(exe)
}
