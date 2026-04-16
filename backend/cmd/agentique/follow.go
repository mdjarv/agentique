package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"strings"

	"github.com/spf13/cobra"
)

func init() {
	sessionsCmd.AddCommand(followCmd)
}

var followCmd = &cobra.Command{
	Use:   "follow <session-id>",
	Short: "Stream live events for a session",
	Args:  cobra.ExactArgs(1),
	RunE:  runFollow,
}

func runFollow(cmd *cobra.Command, args []string) error {
	client := apiClient()
	target, err := resolveSession(client, args[0])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return nil
	}

	fmt.Printf("following %s (%s) [%s]\n\n", target.Name, shortID(target.ID), target.State)

	// Connect to SSE.
	base := baseURL()
	resp, err := client.Get(base + "/api/sessions/events?project=" + target.ProjectID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to connect to event stream: %v\n", err)
		return nil
	}
	defer resp.Body.Close()

	ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt)
	defer stop()

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 256*1024), 1024*1024)
	var eventType string

	for {
		select {
		case <-ctx.Done():
			fmt.Println("\nstopped")
			return nil
		default:
		}

		if !scanner.Scan() {
			break
		}
		line := scanner.Text()

		if v, ok := strings.CutPrefix(line, "event: "); ok {
			eventType = v
			continue
		}
		if v, ok := strings.CutPrefix(line, "data: "); ok {
			printSSEEvent(eventType, v, target.ID)
			eventType = ""
			continue
		}
	}

	if err := scanner.Err(); err != nil {
		fmt.Fprintf(os.Stderr, "stream error: %v\n", err)
	}
	return nil
}

func printSSEEvent(eventType, data, sessionID string) {
	var envelope struct {
		Payload json.RawMessage `json:"payload"`
	}
	if err := json.Unmarshal([]byte(data), &envelope); err != nil {
		return
	}

	var header struct {
		SessionID string `json:"sessionId"`
	}
	if err := json.Unmarshal(envelope.Payload, &header); err != nil {
		return
	}
	if header.SessionID != sessionID {
		return
	}

	switch eventType {
	case "session.state":
		var p struct {
			State     string `json:"state"`
			Connected bool   `json:"connected"`
		}
		json.Unmarshal(envelope.Payload, &p)
		fmt.Printf("[state] %s (connected=%v)\n", p.State, p.Connected)

	case "session.event":
		var p struct {
			Event json.RawMessage `json:"event"`
		}
		if err := json.Unmarshal(envelope.Payload, &p); err != nil {
			return
		}
		renderEvent(p.Event)

	case "session.renamed":
		var p struct {
			Name string `json:"name"`
		}
		json.Unmarshal(envelope.Payload, &p)
		fmt.Printf("[renamed] %s\n", p.Name)

	case "session.deleted":
		fmt.Println("[deleted]")

	default:
		fmt.Printf("[%s]\n", eventType)
	}
}
