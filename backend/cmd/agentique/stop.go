package main

import (
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(stopCmd)
}

var stopCmd = &cobra.Command{
	Use:   "stop <session-id>",
	Short: "Stop a running session",
	Args:  cobra.ExactArgs(1),
	RunE:  runStop,
}

func runStop(cmd *cobra.Command, args []string) error {
	client := &http.Client{Timeout: 10 * time.Second}
	target, err := resolveSession(client, args[0])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return nil
	}

	base := baseURL()
	if err := postJSON(client, base+"/api/sessions/"+target.ID+"/stop", nil); err != nil {
		fmt.Fprintf(os.Stderr, "failed to stop: %v\n", err)
		return nil
	}

	fmt.Printf("stopped %s (%s)\n", target.Name, shortID(target.ID))
	return nil
}
