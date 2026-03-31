package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/allbin/agentique/backend/internal/service"
	"github.com/allbin/agentique/backend/internal/update"
)

var (
	upgradeCheck bool
	upgradeYes   bool
)

func init() {
	upgradeCmd.Flags().BoolVar(&upgradeCheck, "check", false, "check for updates without installing")
	upgradeCmd.Flags().BoolVarP(&upgradeYes, "yes", "y", false, "skip confirmation prompt")
	rootCmd.AddCommand(upgradeCmd)
}

var upgradeCmd = &cobra.Command{
	Use:   "upgrade",
	Short: "Check for and install updates",
	RunE:  runUpgrade,
}

func runUpgrade(cmd *cobra.Command, args []string) error {
	if version == "dev" {
		fmt.Println("Development build — upgrade not available.")
		return nil
	}

	fmt.Printf("Current version: %s\n", version)
	fmt.Println("Checking for updates...")

	result, err := update.Check(version)
	if err != nil {
		return fmt.Errorf("check for updates: %w", err)
	}

	if !result.Available {
		fmt.Println("Already up to date.")
		return nil
	}

	fmt.Printf("Update available: %s → %s\n", result.Current, result.Latest)

	if upgradeCheck {
		return nil
	}

	if !upgradeYes {
		fmt.Print("Install update? [Y/n] ")
		reader := bufio.NewReader(os.Stdin)
		line, _ := reader.ReadString('\n')
		line = strings.TrimSpace(strings.ToLower(line))
		if line != "" && line != "y" && line != "yes" {
			fmt.Println("Aborted.")
			return nil
		}
	}

	fmt.Println("Downloading...")
	downloaded, err := update.Download(result.Release)
	if err != nil {
		return fmt.Errorf("download: %w", err)
	}
	defer update.Cleanup(downloaded)

	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve current binary: %w", err)
	}

	fmt.Printf("Replacing %s...\n", exe)
	if err := update.Replace(downloaded, exe); err != nil {
		return fmt.Errorf("replace binary: %w", err)
	}

	fmt.Printf("Updated to %s\n", result.Latest)

	// Offer to restart the service if running.
	st, err := service.GetStatus()
	if err != nil || !st.Running {
		return nil
	}

	if !upgradeYes {
		fmt.Printf("Service is running (PID %d). Restart now? [Y/n] ", st.PID)
		reader := bufio.NewReader(os.Stdin)
		line, _ := reader.ReadString('\n')
		line = strings.TrimSpace(strings.ToLower(line))
		if line != "" && line != "y" && line != "yes" {
			fmt.Println("Run 'agentique service restart' to apply the update.")
			return nil
		}
	}

	fmt.Println("Restarting service...")
	if err := service.Restart(); err != nil {
		return fmt.Errorf("restart service: %w", err)
	}
	fmt.Println("Service restarted.")
	return nil
}
