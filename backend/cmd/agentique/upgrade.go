package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(upgradeCmd)
}

var upgradeCmd = &cobra.Command{
	Use:   "upgrade",
	Short: "Check for and install updates",
	RunE:  runUpgrade,
}

func runUpgrade(cmd *cobra.Command, args []string) error {
	fmt.Printf("Current version: %s\n", version)
	fmt.Println()
	fmt.Println("Automatic upgrades are not yet available.")
	fmt.Println()
	fmt.Println("To upgrade manually:")

	exe, err := os.Executable()
	if err != nil {
		exe = "/path/to/agentique"
	}

	fmt.Printf("  1. Download the new binary\n")
	fmt.Printf("  2. Replace the current binary:\n")
	fmt.Printf("       cp agentique-linux-amd64 %s\n", exe)
	fmt.Printf("  3. Restart the service:\n")
	fmt.Printf("       agentique service restart\n")
	return nil
}
