package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(queryCmd)
}

var queryCmd = &cobra.Command{
	Use:   "query <session-id> <prompt>",
	Short: "Send a prompt to a session",
	Args:  cobra.ExactArgs(2),
	RunE:  runQuery,
}

func runQuery(cmd *cobra.Command, args []string) error {
	client := apiClient()
	target, err := resolveSession(client, args[0])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return nil
	}

	base := baseURL()
	body := map[string]string{"prompt": args[1]}
	if err := postJSON(client, base+"/api/sessions/"+target.ID+"/query", body); err != nil {
		fmt.Fprintf(os.Stderr, "failed to send query: %v\n", err)
		return nil
	}

	fmt.Printf("query sent to %s (%s)\n", target.Name, shortID(target.ID))
	fmt.Printf("use: agentique sessions follow %s\n", shortID(target.ID))
	return nil
}
