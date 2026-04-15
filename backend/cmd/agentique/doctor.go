package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/mdjarv/agentique/backend/internal/doctor"
)

func init() {
	rootCmd.AddCommand(doctorCmd)
}

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Check runtime dependencies and system health",
	RunE:  runDoctor,
}

func runDoctor(cmd *cobra.Command, args []string) error {
	checks := doctor.RunAll()

	// Column widths.
	const (
		colStatus  = 4
		colName    = 14
	)

	for _, c := range checks {
		icon := "\033[32m✓\033[0m" // green check
		if c.Status == doctor.Warn {
			icon = "\033[33m!\033[0m" // yellow warning
		} else if c.Status == doctor.Fail {
			icon = "\033[31m✗\033[0m" // red cross
		}

		req := ""
		if !c.Required {
			req = " (optional)"
		}

		fmt.Printf("  %s  %-*s %s%s\n", icon, colName, c.Name, c.Message, req)

		if c.Fix != "" && c.Status != doctor.OK {
			fmt.Printf("     %-*s %s\n", colName, "", c.Fix)
		}
	}

	if doctor.HasFailures(checks) {
		fmt.Fprintln(os.Stderr, "\nSome required checks failed.")
		os.Exit(1)
	}

	fmt.Println("\nAll required checks passed.")
	return nil
}
