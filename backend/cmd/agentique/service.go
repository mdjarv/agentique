package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/spf13/cobra"

	"github.com/mdjarv/agentique/backend/internal/service"
)

func init() {
	serviceCmd.AddCommand(serviceInstallCmd)
	serviceCmd.AddCommand(serviceUninstallCmd)
	serviceCmd.AddCommand(serviceStartCmd)
	serviceCmd.AddCommand(serviceStopCmd)
	serviceCmd.AddCommand(serviceRestartCmd)
	serviceCmd.AddCommand(serviceStatusCmd)
	serviceCmd.AddCommand(serviceLogsCmd)
	serviceInstallCmd.Flags().BoolVar(&serviceInstallTray, "tray", false, "also autostart the system-tray controller on login")
	rootCmd.AddCommand(serviceCmd)
}

var serviceInstallTray bool

var serviceCmd = &cobra.Command{
	Use:   "service",
	Short: "Manage system service (systemd/launchd)",
}

var serviceInstallCmd = &cobra.Command{
	Use:   "install",
	Short: "Install and start Agentique as a system service",
	RunE:  runServiceInstall,
}

var serviceUninstallCmd = &cobra.Command{
	Use:   "uninstall",
	Short: "Stop and remove the Agentique service",
	RunE:  runServiceUninstall,
}

var serviceStartCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the Agentique service",
	RunE:  runServiceStart,
}

var serviceStopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop the Agentique service",
	RunE:  runServiceStop,
}

var serviceRestartCmd = &cobra.Command{
	Use:   "restart",
	Short: "Restart the Agentique service",
	RunE:  runServiceRestart,
}

var serviceStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show service status",
	RunE:  runServiceStatus,
}

var serviceLogsCmd = &cobra.Command{
	Use:   "logs",
	Short: "Stream service logs",
	RunE:  runServiceLogs,
}

func runServiceInstall(cmd *cobra.Command, args []string) error {
	before, _ := service.GetStatus()

	exe, _ := os.Executable()
	if !before.Installed && !isStandardBinPath(exe) {
		fmt.Printf("Warning: binary is at %s\n", exe)
		fmt.Println("  The service unit will reference this path.")
		fmt.Println("  Consider moving it first:")
		if runtime.GOOS == "windows" {
			fmt.Println(`    to %LOCALAPPDATA%\Programs\agentique\agentique.exe (where install.ps1 puts it)`)
		} else {
			fmt.Println("    sudo cp " + exe + " /usr/local/bin/agentique")
			fmt.Println("  Or for user-local install:")
			fmt.Println("    mkdir -p ~/.local/bin && cp " + exe + " ~/.local/bin/agentique")
		}
		fmt.Println()
		fmt.Printf("Install from current location anyway? [y/N] ")
		reader := bufio.NewReader(os.Stdin)
		answer, _ := reader.ReadString('\n')
		answer = strings.TrimSpace(strings.ToLower(answer))
		if answer != "y" && answer != "yes" {
			fmt.Println("cancelled — move the binary and try again")
			return nil
		}
	}

	if err := service.Install(); err != nil {
		return fmt.Errorf("install: %w", err)
	}

	after, _ := service.GetStatus()
	fmt.Printf("  Unit: %s\n", after.UnitPath)

	if before.Running {
		fmt.Println("Service unit updated")
		fmt.Println("  Restart when ready: agentique service restart")
	} else {
		fmt.Println("Service installed and started")
		if after.Running {
			fmt.Printf("  PID:  %d\n", after.PID)
		}
		fmt.Println("\nUseful commands:")
		fmt.Println("  agentique service status    — check status")
		fmt.Println("  agentique service start     — start service")
		fmt.Println("  agentique service stop      — stop service")
		fmt.Println("  agentique service restart   — restart after upgrade")
		fmt.Println("  agentique service logs      — stream logs")
		fmt.Println("  agentique service uninstall — remove service")
	}

	if serviceInstallTray {
		if err := service.InstallTray(exe); err != nil {
			fmt.Printf("  Tray autostart: failed (%v)\n", err)
		} else {
			fmt.Println("  Tray autostart: enabled ('agentique tray' on login)")
		}
	}
	return nil
}

func runServiceStart(cmd *cobra.Command, args []string) error {
	st, err := service.GetStatus()
	if err != nil {
		return err
	}
	if !st.Installed {
		fmt.Println("Service not installed")
		return nil
	}
	if st.Running {
		fmt.Printf("Service already running (PID %d)\n", st.PID)
		return nil
	}
	if err := service.Start(); err != nil {
		return fmt.Errorf("start: %w", err)
	}
	st, _ = service.GetStatus()
	fmt.Println("Service started")
	if st.Running {
		fmt.Printf("  PID: %d\n", st.PID)
	}
	return nil
}

func runServiceStop(cmd *cobra.Command, args []string) error {
	st, err := service.GetStatus()
	if err != nil {
		return err
	}
	if !st.Installed {
		fmt.Println("Service not installed")
		return nil
	}
	if !st.Running {
		fmt.Println("Service not running")
		return nil
	}
	if err := service.Stop(); err != nil {
		return fmt.Errorf("stop: %w", err)
	}
	fmt.Println("Service stopped")
	return nil
}

func runServiceRestart(cmd *cobra.Command, args []string) error {
	st, err := service.GetStatus()
	if err != nil {
		return err
	}
	if !st.Installed {
		fmt.Println("Service not installed")
		return nil
	}

	if err := service.Restart(); err != nil {
		return fmt.Errorf("restart: %w", err)
	}

	st, _ = service.GetStatus()
	fmt.Println("Service restarted")
	if st.Running {
		fmt.Printf("  PID: %d\n", st.PID)
	}
	return nil
}

func runServiceUninstall(cmd *cobra.Command, args []string) error {
	st, err := service.GetStatus()
	if err != nil {
		return err
	}
	if !st.Installed {
		fmt.Println("Service not installed")
		return nil
	}

	fmt.Printf("Remove Agentique service? [y/N] ")
	reader := bufio.NewReader(os.Stdin)
	answer, _ := reader.ReadString('\n')
	answer = strings.TrimSpace(strings.ToLower(answer))
	if answer != "y" && answer != "yes" {
		fmt.Println("cancelled")
		return nil
	}

	if err := service.Uninstall(); err != nil {
		return fmt.Errorf("uninstall: %w", err)
	}
	_ = service.UninstallTray() // best effort; tray autostart may not exist

	fmt.Println("Service removed")
	return nil
}

func runServiceStatus(cmd *cobra.Command, args []string) error {
	st, err := service.GetStatus()
	if err != nil {
		return err
	}

	if !st.Installed {
		fmt.Println("Not installed")
		fmt.Println("\nInstall with: agentique service install")
		return nil
	}

	if st.Running {
		fmt.Printf("Running (PID %d)\n", st.PID)
	} else {
		fmt.Println("Installed but not running")
	}
	fmt.Printf("  Unit: %s\n", st.UnitPath)
	return nil
}

// isStandardBinPath reports whether exe sits in a directory installs normally
// land in, so the "consider moving it" nag stays quiet there. Compared as
// cleaned directories (case-insensitively on Windows), not string prefixes —
// a Windows exe under home\.local\bin never matches a "/"-joined prefix.
func isStandardBinPath(exe string) bool {
	dir := filepath.Clean(filepath.Dir(exe))
	var standard []string
	if runtime.GOOS == "windows" {
		if lad := os.Getenv("LOCALAPPDATA"); lad != "" {
			standard = append(standard, filepath.Join(lad, "Programs", "agentique"))
		}
	} else {
		standard = append(standard, "/usr/local/bin", "/usr/bin")
	}
	// just install's home on every platform.
	if home, _ := os.UserHomeDir(); home != "" {
		standard = append(standard, filepath.Join(home, ".local", "bin"))
	}
	for _, s := range standard {
		s = filepath.Clean(s)
		if dir == s || (runtime.GOOS == "windows" && strings.EqualFold(dir, s)) {
			return true
		}
	}
	return false
}

func runServiceLogs(cmd *cobra.Command, args []string) error {
	logsCmd, err := service.LogsCmd()
	if err != nil {
		return err
	}

	logsCmd.Stdout = os.Stdout
	logsCmd.Stderr = os.Stderr
	return logsCmd.Run()
}
