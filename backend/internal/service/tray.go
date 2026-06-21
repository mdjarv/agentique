package service

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// InstallTray registers an autostart entry that launches "agentique tray" on
// login (in addition to, not instead of, the server service). On Linux this is
// an XDG autostart .desktop entry; on Windows a second logon Scheduled Task.
func InstallTray(binaryPath string) error {
	switch runtime.GOOS {
	case "linux":
		return installTrayLinux(binaryPath)
	case "windows":
		return installTrayWindows(binaryPath)
	default:
		return fmt.Errorf("tray autostart not supported on %s", runtime.GOOS)
	}
}

// UninstallTray removes the tray autostart entry. Missing is not an error.
func UninstallTray() error {
	switch runtime.GOOS {
	case "linux":
		return uninstallTrayLinux()
	case "windows":
		return uninstallTrayWindows()
	default:
		return nil
	}
}

// --- Linux: XDG autostart ---

func trayAutostartPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "autostart", "agentique-tray.desktop")
}

func installTrayLinux(binaryPath string) error {
	path := trayAutostartPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create autostart directory: %w", err)
	}
	content := "[Desktop Entry]\n" +
		"Type=Application\n" +
		"Name=Agentique Tray\n" +
		"Comment=System-tray controller for the Agentique server\n" +
		"Exec=" + binaryPath + " tray\n" +
		"Terminal=false\n" +
		"Categories=Development;\n" +
		"X-GNOME-Autostart-enabled=true\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return fmt.Errorf("write autostart entry: %w", err)
	}
	return nil
}

func uninstallTrayLinux() error {
	if err := os.Remove(trayAutostartPath()); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// --- Windows: a second logon Scheduled Task ---

const trayTaskName = "agentique-tray"

func installTrayWindows(binaryPath string) error {
	if err := createTask(trayTaskName, binaryPath, "tray", "Agentique system-tray controller"); err != nil {
		return err
	}
	// Launch now so the icon appears without waiting for the next login.
	exec.Command("schtasks", "/Run", "/TN", trayTaskName).CombinedOutput()
	return nil
}

func uninstallTrayWindows() error {
	exec.Command("schtasks", "/End", "/TN", trayTaskName).CombinedOutput()
	out, err := exec.Command("schtasks", "/Delete", "/TN", trayTaskName, "/F").CombinedOutput()
	if err != nil && !strings.Contains(strings.ToLower(string(out)), "cannot find") {
		return fmt.Errorf("schtasks /Delete %s: %w\n%s", trayTaskName, err, out)
	}
	return nil
}
