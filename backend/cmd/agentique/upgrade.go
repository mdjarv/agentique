package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/allbin/agentique/backend/internal/upgrade"
)

var upgradeCheck bool

func init() {
	upgradeCmd.Flags().BoolVar(&upgradeCheck, "check", false, "check for updates without installing")
	rootCmd.AddCommand(upgradeCmd)
}

var upgradeCmd = &cobra.Command{
	Use:   "upgrade",
	Short: "Check for and install updates",
	RunE:  runUpgrade,
}

func runUpgrade(cmd *cobra.Command, args []string) error {
	if !isRelease() {
		fmt.Println("Upgrade not available for dev builds")
		fmt.Printf("  version: %s\n", version)
		return nil
	}

	fmt.Printf("Current version: %s\n", version)
	fmt.Println("Checking for updates...")

	rel, err := upgrade.Check(version)
	if err != nil {
		return fmt.Errorf("check failed: %w", err)
	}

	if rel == nil {
		fmt.Println("Already up to date")
		return nil
	}

	fmt.Printf("New version available: %s\n", rel.Version)
	if rel.Notes != "" {
		fmt.Printf("\n%s\n", rel.Notes)
	}

	if upgradeCheck {
		return nil
	}

	fmt.Printf("\nUpgrade to %s? [y/N] ", rel.Version)
	reader := bufio.NewReader(os.Stdin)
	answer, _ := reader.ReadString('\n')
	answer = strings.TrimSpace(strings.ToLower(answer))
	if answer != "y" && answer != "yes" {
		fmt.Println("cancelled")
		return nil
	}

	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve binary path: %w", err)
	}

	fmt.Println("Downloading...")
	tmpPath := exe + ".new"
	if err := upgrade.Download(rel, tmpPath); err != nil {
		return fmt.Errorf("download: %w", err)
	}

	fmt.Println("Applying update...")
	if err := upgrade.Apply(tmpPath, exe); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("apply: %w", err)
	}

	fmt.Printf("Upgraded to %s\n", rel.Version)
	fmt.Println("  Old binary backed up to " + exe + ".bak")
	fmt.Println("\nRestart the server to use the new version:")
	fmt.Println("  agentique service logs  (if running as service)")
	fmt.Println("  Or restart manually")
	return nil
}
