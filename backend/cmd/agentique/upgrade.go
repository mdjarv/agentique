package main

import (
	"fmt"

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
	fmt.Println("To upgrade, run the install script:")
	fmt.Println()
	fmt.Println("  curl -fsSL https://raw.githubusercontent.com/mdjarv/agentique/master/install.sh | bash")
	fmt.Println()
	fmt.Println("This downloads the latest release, updates the service unit,")
	fmt.Println("and prints a reminder to restart when ready.")
	return nil
}
