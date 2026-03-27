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

// WireContentBlock represents a single block of tool result content.
type WireContentBlock struct {
	Type      string `json:"type"`                // "text" or "image"
	Text      string `json:"text,omitempty"`       // populated for text blocks
	MediaType string `json:"mediaType,omitempty"`  // e.g. "image/png"; image blocks only
	URL       string `json:"url,omitempty"`        // data: URL; image blocks only
}

type WireToolResultEvent struct {
	Type    string             `json:"type"`
	ToolID  string             `json:"toolId"`
	Content []WireContentBlock `json:"content"`
}

type WireResultEvent struct {
	Type          string `json:"type"`
	Cost          float64 `json:"cost"`
	Duration      int64   `json:"duration"`
	Usage         any     `json:"usage"`
	StopReason    string  `json:"stopReason"`
	ContextWindow int     `json:"contextWindow,omitempty"`
	InputTokens   int     `json:"inputTokens,omitempty"`
	OutputTokens  int     `json:"outputTokens,omitempty"`
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
	ResetsAt    int64   `json:"resetsAt,omitempty"`
}

type WireStreamEvent struct {
	Type  string          `json:"type"`
	Event json.RawMessage `json:"event"`
}

type WireCompactStatusEvent struct {
	Type   string `json:"type"`
	Status string `json:"status"`
}

type WireCompactBoundaryEvent struct {
	Type      string `json:"type"`
	Trigger   string `json:"trigger"`
	PreTokens int    `json:"preTokens"`
}

type WireContextManagementEvent struct {
	Type string          `json:"type"`
	Raw  json.RawMessage `json:"raw"`
}

func (e WireTextEvent) WireType() string       { return e.Type }
func (e WireThinkingEvent) WireType() string   { return e.Type }
func (e WireToolUseEvent) WireType() string    { return e.Type }
func (e WireToolResultEvent) WireType() string { return e.Type }
func (e WireResultEvent) WireType() string     { return e.Type }
func (e WireErrorEvent) WireType() string      { return e.Type }
func (e WireRateLimitEvent) WireType() string  { return e.Type }
func (e WireStreamEvent) WireType() string          { return e.Type }
func (e WireCompactStatusEvent) WireType() string   { return e.Type }
func (e WireCompactBoundaryEvent) WireType() string    { return e.Type }
func (e WireContextManagementEvent) WireType() string  { return e.Type }

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
		return WireToolResultEvent{
			Type:    "tool_result",
			ToolID:  e.ToolUseID,
			Content: convertToolContent(e.Content),
		}
	case *claudecli.ResultEvent:
		wire := WireResultEvent{
			Type:       "result",
			Cost:       e.CostUSD,
			Duration:   e.Duration.Milliseconds(),
			Usage:      e.Usage,
			StopReason: e.StopReason,
		}
		for _, mu := range e.ModelUsage {
			wire.InputTokens += mu.InputTokens
			wire.OutputTokens += mu.OutputTokens
			if mu.ContextWindow > wire.ContextWindow {
				wire.ContextWindow = mu.ContextWindow
			}
		}
		return wire
	case *claudecli.ErrorEvent:
		we := WireErrorEvent{Type: "error", Message: e.Error(), Fatal: e.Fatal}
		// Classify by sentinel — most specific first.
		// TODO: add ErrBilling, ErrPermission, ErrNotFound, ErrRequestTooLarge,
		// ErrInvalidRequest checks here when claudecli-go adds those sentinels.
		if errors.Is(e.Err, claudecli.ErrRateLimit) {
			we.ErrorType = "rate_limit"
			var rlErr *claudecli.RateLimitError
			if errors.As(e.Err, &rlErr) && rlErr.RetryAfter > 0 {
				we.RetryAfterSecs = int(rlErr.RetryAfter.Seconds())
			}
		} else if errors.Is(e.Err, claudecli.ErrAuth) {
			we.ErrorType = "auth"
		} else if errors.Is(e.Err, claudecli.ErrOverloaded) {
			we.ErrorType = "overloaded"
		} else {
			we.ErrorType = "api_error"
		}
		return we
	case *claudecli.RateLimitEvent:
		return WireRateLimitEvent{
			Type:        "rate_limit",
			Status:      e.Status,
			Utilization: e.Utilization,
			ResetsAt:    e.ResetsAt,
		}
	case *claudecli.StreamEvent:
		return WireStreamEvent{
			Type:  "stream",
			Event: e.Event,
		}
	case *claudecli.CompactStatusEvent:
		return WireCompactStatusEvent{
			Type:   "compact_status",
			Status: e.Status,
		}
	case *claudecli.CompactBoundaryEvent:
		return WireCompactBoundaryEvent{
			Type:      "compact_boundary",
			Trigger:   e.Trigger,
			PreTokens: e.PreTokens,
		}
	case *claudecli.ContextManagementEvent:
		return WireContextManagementEvent{
			Type: "context_management",
			Raw:  e.Raw,
		}
	default:
		return nil
	}
}

// convertToolContent converts claudecli-go content blocks to wire format.
// Image blocks are encoded as data: URLs so the frontend can render them directly.
func convertToolContent(blocks []claudecli.ToolContent) []WireContentBlock {
	out := make([]WireContentBlock, 0, len(blocks))
	for _, b := range blocks {
		switch b.Type {
		case "text":
			out = append(out, WireContentBlock{Type: "text", Text: b.Text})
		case "image":
			out = append(out, WireContentBlock{
				Type:      "image",
				MediaType: b.MediaType,
				URL:       "data:" + b.MediaType + ";base64," + b.Data,
			})
		}
	}
	return out
}

// toolResultText concatenates all text blocks from a tool result.
func toolResultText(blocks []WireContentBlock) string {
	var parts []string
	for _, b := range blocks {
		if b.Type == "text" && b.Text != "" {
			parts = append(parts, b.Text)
		}
	}
	return strings.Join(parts, "")
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
	case "EnterPlanMode", "ExitPlanMode":
		return "plan"
	}
	if strings.HasPrefix(name, "mcp__") {
		return "mcp"
	}
	return "other"
}
