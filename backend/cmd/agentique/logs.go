package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(logsCmd)
}

var logsCmd = &cobra.Command{
	Use:   "logs <session-id>",
	Short: "Show turn history for a session",
	Args:  cobra.ExactArgs(1),
	RunE:  runLogs,
}

type historyResult struct {
	Turns []historyTurn `json:"turns"`
}

type historyTurn struct {
	Prompt string            `json:"prompt"`
	Events []json.RawMessage `json:"events"`
}

func runLogs(cmd *cobra.Command, args []string) error {
	client := apiClient()
	target, err := resolveSession(client, args[0])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return nil
	}

	base := baseURL()
	result, err := fetchJSON[historyResult](client, base+"/api/sessions/"+target.ID+"/history")
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to fetch history: %v\n", err)
		return nil
	}

	if len(result.Turns) == 0 {
		fmt.Printf("%s (%s): no turns\n", target.Name, shortID(target.ID))
		return nil
	}

	fmt.Printf("%s (%s) — %d turns\n\n", target.Name, shortID(target.ID), len(result.Turns))

	for i, turn := range result.Turns {
		fmt.Printf("--- Turn %d ---\n", i+1)
		if turn.Prompt != "" {
			fmt.Printf("> %s\n\n", turn.Prompt)
		}
		for _, event := range turn.Events {
			renderEvent(event)
		}
		fmt.Println()
	}

	return nil
}
