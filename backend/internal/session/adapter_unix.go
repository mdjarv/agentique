//go:build !windows

package session

import (
	"context"
	"encoding/json"

	claudecli "github.com/allbin/claudecli-go"
)

// claudecliAdapter wraps a real claudecli-go Session.
type claudecliAdapter struct {
	sess *claudecli.Session
}

func (a *claudecliAdapter) Query(prompt string) error {
	return a.sess.Query(prompt)
}

func (a *claudecliAdapter) Events() <-chan CLIEvent {
	raw := a.sess.Events()
	out := make(chan CLIEvent, 64)
	go func() {
		defer close(out)
		for event := range raw {
			out <- event
		}
	}()
	return out
}

func (a *claudecliAdapter) Wait() (CLIResult, error) {
	result, err := a.sess.Wait()
	if err != nil {
		return CLIResult{}, err
	}
	if result == nil {
		return CLIResult{}, nil
	}
	return CLIResult{
		CostUSD:    result.CostUSD,
		DurationMS: result.Duration.Milliseconds(),
		StopReason: result.StopReason,
		Usage:      result.Usage,
	}, nil
}

func (a *claudecliAdapter) Close() error {
	return a.sess.Close()
}

// realConnector creates sessions via the real claudecli-go client.
type realConnector struct{}

func (c *realConnector) Connect(workDir string) (CLISession, error) {
	client := claudecli.New()
	sess, err := client.Connect(context.Background(),
		claudecli.WithWorkDir(workDir),
		claudecli.WithModel(claudecli.ModelOpus),
		claudecli.WithPermissionMode(claudecli.PermissionBypass),
	)
	if err != nil {
		return nil, err
	}
	return &claudecliAdapter{sess: sess}, nil
}

// NewRealConnector returns a CLIConnector backed by the real Claude CLI.
func NewRealConnector() CLIConnector {
	return &realConnector{}
}

// ToWireEvent converts a claudecli-go event to a JSON-friendly wire format.
// Returns nil for event types we don't forward to the frontend.
func ToWireEvent(event CLIEvent) any {
	switch e := event.(type) {
	case *claudecli.TextEvent:
		return WireTextEvent{Type: "text", Content: e.Content}
	case *claudecli.ThinkingEvent:
		return WireThinkingEvent{Type: "thinking", Content: e.Content}
	case *claudecli.ToolUseEvent:
		return WireToolUseEvent{Type: "tool_use", ID: e.ID, Name: e.Name, Input: e.Input}
	case *claudecli.ToolResultEvent:
		return WireToolResultEvent{Type: "tool_result", ToolUseID: e.ToolUseID, Content: e.Content}
	case *claudecli.ResultEvent:
		return WireResultEvent{
			Type:       "result",
			CostUSD:    e.CostUSD,
			Duration:   e.Duration.Milliseconds(),
			Usage:      e.Usage,
			StopReason: e.StopReason,
		}
	case *claudecli.ErrorEvent:
		return WireErrorEvent{Type: "error", Message: e.Error(), Fatal: e.Fatal}
	case *claudecli.InitEvent:
		// Don't forward init events to the frontend.
		return nil
	default:
		// Check for JSON-marshalable custom events
		if data, err := json.Marshal(e); err == nil {
			var m map[string]any
			if json.Unmarshal(data, &m) == nil {
				return m
			}
		}
		return nil
	}
}
