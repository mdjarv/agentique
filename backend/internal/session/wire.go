package session

import (
	"encoding/json"
	"errors"
	"strings"

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
	Category  string          `json:"category"`
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
	Type           string `json:"type"`
	Message        string `json:"message"`
	Fatal          bool   `json:"fatal"`
	ErrorType      string `json:"errorType,omitempty"`
	RetryAfterSecs int    `json:"retryAfterSecs,omitempty"`
}

type WireRateLimitEvent struct {
	Type        string  `json:"type"`
	Status      string  `json:"status"`
	Utilization float64 `json:"utilization"`
}

type WireStreamEvent struct {
	Type  string          `json:"type"`
	Event json.RawMessage `json:"event"`
}

func (e WireTextEvent) WireType() string       { return e.Type }
func (e WireThinkingEvent) WireType() string   { return e.Type }
func (e WireToolUseEvent) WireType() string    { return e.Type }
func (e WireToolResultEvent) WireType() string { return e.Type }
func (e WireResultEvent) WireType() string     { return e.Type }
func (e WireErrorEvent) WireType() string      { return e.Type }
func (e WireRateLimitEvent) WireType() string  { return e.Type }
func (e WireStreamEvent) WireType() string     { return e.Type }

// ToWireEvent converts a claudecli-go event to a JSON-friendly wire format.
// Returns nil for event types we don't forward to the frontend.
func ToWireEvent(event claudecli.Event) any {
	switch e := event.(type) {
	case *claudecli.TextEvent:
		return WireTextEvent{Type: "text", Content: e.Content}
	case *claudecli.ThinkingEvent:
		return WireThinkingEvent{Type: "thinking", Content: e.Content}
	case *claudecli.ToolUseEvent:
		return WireToolUseEvent{
			Type:      "tool_use",
			ToolID:    e.ID,
			ToolName:  e.Name,
			ToolInput: e.Input,
			Category:  classifyTool(e.Name),
		}
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
		we := WireErrorEvent{Type: "error", Message: e.Error(), Fatal: e.Fatal}
		var cliErr *claudecli.Error
		if errors.As(e.Err, &cliErr) && cliErr.Details != nil {
			we.ErrorType = cliErr.Details.Type
			if cliErr.Details.RetryAfter > 0 {
				we.RetryAfterSecs = int(cliErr.Details.RetryAfter.Seconds())
			}
		}
		return we
	case *claudecli.RateLimitEvent:
		return WireRateLimitEvent{
			Type:        "rate_limit",
			Status:      e.Status,
			Utilization: e.Utilization,
		}
	case *claudecli.StreamEvent:
		return WireStreamEvent{
			Type:  "stream",
			Event: e.Event,
		}
	default:
		return nil
	}
}

func classifyTool(name string) string {
	switch name {
	case "Bash":
		return "command"
	case "Edit", "Write", "NotebookEdit", "MultiEdit":
		return "file_write"
	case "Read", "Glob", "Grep":
		return "file_read"
	case "WebSearch", "WebFetch":
		return "web"
	case "Agent":
		return "agent"
	case "TodoWrite", "TodoRead":
		return "task"
	}
	if strings.HasPrefix(name, "mcp__") {
		return "mcp"
	}
	return "other"
}
