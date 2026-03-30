package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/allbin/agentique/backend/internal/service"
)

func init() {
	serviceCmd.AddCommand(serviceInstallCmd)
	serviceCmd.AddCommand(serviceUninstallCmd)
	serviceCmd.AddCommand(serviceRestartCmd)
	serviceCmd.AddCommand(serviceStatusCmd)
	serviceCmd.AddCommand(serviceLogsCmd)
	rootCmd.AddCommand(serviceCmd)
}

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
	// Check if already installed.
	st, err := service.GetStatus()
	if err != nil {
		return err
	}
	if st.Installed {
		if st.Running {
			fmt.Printf("Service already installed and running (PID %d)\n", st.PID)
		} else {
			fmt.Println("Service already installed but not running")
		}
		fmt.Printf("  Unit: %s\n", st.UnitPath)
		return nil
	}

	exe, _ := os.Executable()
	if !isStandardBinPath(exe) {
		fmt.Printf("Warning: binary is at %s\n", exe)
		fmt.Println("  The service unit will reference this path.")
		fmt.Println("  Consider moving it first:")
		fmt.Println("    sudo cp " + exe + " /usr/local/bin/agentique")
		fmt.Println("  Or for user-local install:")
		fmt.Println("    mkdir -p ~/.local/bin && cp " + exe + " ~/.local/bin/agentique")
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

	st, _ = service.GetStatus()
	fmt.Println("Service installed and started")
	fmt.Printf("  Unit: %s\n", st.UnitPath)
	if st.Running {
		fmt.Printf("  PID:  %d\n", st.PID)
	}
	fmt.Println("\nUseful commands:")
	fmt.Println("  agentique service status    — check status")
	fmt.Println("  agentique service restart   — restart after upgrade")
	fmt.Println("  agentique service logs      — stream logs")
	fmt.Println("  agentique service uninstall — remove service")
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

func isStandardBinPath(exe string) bool {
	standardPrefixes := []string{
		"/usr/local/bin/",
		"/usr/bin/",
	}
	home, _ := os.UserHomeDir()
	if home != "" {
		standardPrefixes = append(standardPrefixes, home+"/.local/bin/")
	}
	for _, prefix := range standardPrefixes {
		if strings.HasPrefix(exe, prefix) {
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
