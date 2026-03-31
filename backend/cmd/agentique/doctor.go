package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/allbin/agentique/backend/internal/doctor"
)

var doctorJSON bool

func init() {
	doctorCmd.Flags().BoolVar(&doctorJSON, "json", false, "output in JSON format")
	rootCmd.AddCommand(doctorCmd)
}

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Check runtime dependencies and system health",
	RunE:  runDoctor,
}

func runDoctor(cmd *cobra.Command, args []string) error {
	checks := doctor.RunAll()

	if doctorJSON {
		return printDoctorJSON(checks)
	}

	const colName = 14

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

func printDoctorJSON(checks []doctor.Check) error {
	type jsonCheck struct {
		Name     string `json:"name"`
		Status   string `json:"status"`
		Message  string `json:"message"`
		Fix      string `json:"fix,omitempty"`
		Required bool   `json:"required"`
	}

	out := struct {
		OK     bool        `json:"ok"`
		Checks []jsonCheck `json:"checks"`
	}{
		OK:     !doctor.HasFailures(checks),
		Checks: make([]jsonCheck, len(checks)),
	}

	for i, c := range checks {
		out.Checks[i] = jsonCheck{
			Name:     c.Name,
			Status:   c.Status.String(),
			Message:  c.Message,
			Fix:      c.Fix,
			Required: c.Required,
		}
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(out); err != nil {
		return err
	}

	if !out.OK {
		os.Exit(1)
	}
	return nil
}
