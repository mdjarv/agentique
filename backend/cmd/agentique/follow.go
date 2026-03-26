package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/http"
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
	base := baseURL()
	prefix := args[0]

	// Resolve short ID to full ID + get project ID.
	client := &http.Client{}
	sessions, err := fetchJSON[[]sessionBrief](client, base+"/api/sessions")
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to fetch sessions: %v\n", err)
		return nil
	}

	var target sessionBrief
	var matches []sessionBrief
	for _, s := range sessions {
		if strings.HasPrefix(s.ID, prefix) {
			matches = append(matches, s)
		}
	}
	switch len(matches) {
	case 0:
		fmt.Fprintf(os.Stderr, "no session matching %q\n", prefix)
		return nil
	case 1:
		target = matches[0]
	default:
		fmt.Fprintf(os.Stderr, "ambiguous prefix %q matches %d sessions:\n", prefix, len(matches))
		for _, m := range matches {
			fmt.Fprintf(os.Stderr, "  %s  %s\n", m.ID[:8], m.Name)
		}
		return nil
	}

	fmt.Printf("following %s (%s) [%s]\n\n", target.Name, target.ID[:8], target.State)

	// Connect to SSE.
	endpoint := base + "/api/sessions/events?project=" + target.ProjectID
	resp, err := client.Get(endpoint)
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

	// All payloads have sessionId — filter.
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
		printSessionEvent(p.Event)

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

func printSessionEvent(raw json.RawMessage) {
	var typed struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(raw, &typed); err != nil {
		return
	}

	switch typed.Type {
	case "text":
		var e struct {
			Content string `json:"content"`
		}
		json.Unmarshal(raw, &e)
		fmt.Print(e.Content)

	case "thinking":
		// Skip thinking blocks — too verbose for follow.

	case "tool_use":
		var e struct {
			ToolName string `json:"toolName"`
			Category string `json:"category"`
		}
		json.Unmarshal(raw, &e)
		fmt.Printf("\n[tool] %s\n", e.ToolName)

	case "tool_result":
		// Skip tool results — too verbose.

	case "result":
		var e struct {
			StopReason string `json:"stopReason"`
		}
		json.Unmarshal(raw, &e)
		fmt.Printf("\n[result] stop=%s\n", e.StopReason)

	case "error":
		var e struct {
			Message string `json:"message"`
			Fatal   bool   `json:"fatal"`
		}
		json.Unmarshal(raw, &e)
		label := "error"
		if e.Fatal {
			label = "FATAL"
		}
		fmt.Printf("[%s] %s\n", label, e.Message)

	case "rate_limit":
		// Skip rate limit events.

	case "stream":
		// Skip raw stream events.
	}
}
