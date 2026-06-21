package main

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/mdjarv/agentique/backend/internal/procctl"
	"github.com/mdjarv/agentique/backend/internal/service"
)

var trayCmd = &cobra.Command{
	Use:   "tray",
	Short: "Run a system-tray controller for the agentique server",
	Long: `Run a system-tray icon that shows server status and can start, stop,
restart, and open agentique. The tray is a thin controller — it does not host
the server itself. When a background service is installed the tray drives that
(so it never fights the service's restart-on-failure); otherwise it launches a
detached 'agentique serve' that keeps running after the tray quits.

On a headless Linux session (no display) there is no tray — use 'agentique serve'
or 'agentique service install' instead.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if headlessLinux() {
			return fmt.Errorf("no desktop session detected (no DISPLAY/WAYLAND_DISPLAY) — use 'agentique serve' or 'agentique service install' instead")
		}
		return runTray()
	},
}

func init() {
	rootCmd.AddCommand(trayCmd)
}

// headlessLinux reports whether we're on Linux with no graphical session, where
// a tray icon cannot be shown.
func headlessLinux() bool {
	if runtime.GOOS != "linux" {
		return false
	}
	return os.Getenv("DISPLAY") == "" && os.Getenv("WAYLAND_DISPLAY") == ""
}

// serviceOwnsServer reports whether a background service is installed to manage
// the server, so start/stop should route through it rather than fighting its
// restart-on-failure.
func serviceOwnsServer() bool {
	st, err := service.GetStatus()
	return err == nil && st.Installed
}

// startServer brings the server up — via the installed service if present,
// otherwise by launching a detached 'serve' that outlives the tray.
func startServer() error {
	if serviceOwnsServer() {
		return service.Start()
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	_, err = procctl.StartDetached(exe, "serve")
	return err
}

// stopServer takes the server down. If a service owns it, stop via the service
// so it is not auto-restarted; otherwise terminate the recorded PID.
func stopServer() error {
	if serviceOwnsServer() {
		return service.Stop()
	}
	pid, alive := readServerPID()
	if !alive {
		return nil // already down, or never ours to stop
	}
	return procctl.Terminate(pid)
}

// restartServer restarts via the service when present, else stop+start with a
// short wait for the port to free.
func restartServer() error {
	if serviceOwnsServer() {
		return service.Restart()
	}
	if err := stopServer(); err != nil {
		return err
	}
	for range 50 {
		if !isServerRunning() {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	return startServer()
}

// openUI opens the agentique web UI in the default browser.
func openUI() error {
	url := baseURL()
	switch runtime.GOOS {
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	case "darwin":
		return exec.Command("open", url).Start()
	default:
		return exec.Command("xdg-open", url).Start()
	}
}

// displayAddr is the host:port shown in the tray menu (scheme stripped).
func displayAddr() string {
	u := baseURL()
	u = strings.TrimPrefix(u, "https://")
	u = strings.TrimPrefix(u, "http://")
	return u
}
