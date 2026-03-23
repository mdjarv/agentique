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
	Type      string          `json:"type"`
	ToolID    string          `json:"toolId"`
	ToolName  string          `json:"toolName"`
	ToolInput json.RawMessage `json:"toolInput"`
}

type WireToolResultEvent struct {
	Type    string `json:"type"`
	ToolID  string `json:"toolId"`
	Content string `json:"content"`
}

type WireResultEvent struct {
	Type       string  `json:"type"`
	Cost       float64 `json:"cost"`
	Duration   int64   `json:"duration"`
	Usage      any     `json:"usage"`
	StopReason string  `json:"stopReason"`
}

type WireErrorEvent struct {
	Type    string `json:"type"`
	Message string `json:"message"`
	Fatal   bool   `json:"fatal"`
}

func (e WireTextEvent) WireType() string       { return e.Type }
func (e WireThinkingEvent) WireType() string   { return e.Type }
func (e WireToolUseEvent) WireType() string    { return e.Type }
func (e WireToolResultEvent) WireType() string { return e.Type }
func (e WireResultEvent) WireType() string     { return e.Type }
func (e WireErrorEvent) WireType() string      { return e.Type }

// ToWireEvent converts a claudecli-go event to a JSON-friendly wire format.
// Returns nil for event types we don't forward to the frontend.
func ToWireEvent(event claudecli.Event) any {
	switch e := event.(type) {
	case *claudecli.TextEvent:
		return WireTextEvent{Type: "text", Content: e.Content}
	case *claudecli.ThinkingEvent:
		return WireThinkingEvent{Type: "thinking", Content: e.Content}
	case *claudecli.ToolUseEvent:
		return WireToolUseEvent{Type: "tool_use", ToolID: e.ID, ToolName: e.Name, ToolInput: e.Input}
	case *claudecli.ToolResultEvent:
		return WireToolResultEvent{Type: "tool_result", ToolID: e.ToolUseID, Content: e.Content}
	case *claudecli.ResultEvent:
		return WireResultEvent{
			Type:       "result",
			Cost:       e.CostUSD,
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
