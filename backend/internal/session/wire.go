package session

import (
	"encoding/json"

	claudecli "github.com/allbin/claudecli-go"
)

// Wire event types for JSON serialization to the frontend.

type WireTextEvent struct {
	Type    string `json:"type"`
	Content string `json:"content"`
}

type WireThinkingEvent struct {
	Type    string `json:"type"`
	Content string `json:"content"`
}

type WireToolUseEvent struct {
	Type  string          `json:"type"`
	ID    string          `json:"id"`
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"`
}

type WireToolResultEvent struct {
	Type      string `json:"type"`
	ToolUseID string `json:"toolUseId"`
	Content   string `json:"content"`
}

type WireResultEvent struct {
	Type       string  `json:"type"`
	CostUSD    float64 `json:"costUsd"`
	Duration   int64   `json:"duration"`
	Usage      any     `json:"usage"`
	StopReason string  `json:"stopReason"`
}

type WireErrorEvent struct {
	Type    string `json:"type"`
	Message string `json:"message"`
	Fatal   bool   `json:"fatal"`
}

// ToWireEvent converts a claudecli-go event to a JSON-friendly wire format.
// Returns nil for event types we don't forward to the frontend.
func ToWireEvent(event claudecli.Event) any {
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
	default:
		return nil
	}
}
