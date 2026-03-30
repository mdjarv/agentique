package session

import (
	"encoding/json"
	"errors"
	"strings"

	claudecli "github.com/allbin/claudecli-go"
)

// Wire event types for JSON serialization to the frontend.

type WireTextEvent struct {
	Type            string `json:"type"`
	Content         string `json:"content"`
	ParentToolUseID string `json:"parentToolUseId,omitempty"`
}

type WireThinkingEvent struct {
	Type            string `json:"type"`
	Content         string `json:"content"`
	ParentToolUseID string `json:"parentToolUseId,omitempty"`
}

type WireToolUseEvent struct {
	Type            string          `json:"type"`
	ToolID          string          `json:"toolId"`
	ToolName        string          `json:"toolName"`
	ToolInput       json.RawMessage `json:"toolInput"`
	Category        string          `json:"category"`
	ParentToolUseID string          `json:"parentToolUseId,omitempty"`
}

// WireContentBlock represents a single block of tool result content.
type WireContentBlock struct {
	Type      string `json:"type"`                // "text" or "image"
	Text      string `json:"text,omitempty"`       // populated for text blocks
	MediaType string `json:"mediaType,omitempty"`  // e.g. "image/png"; image blocks only
	URL       string `json:"url,omitempty"`        // data: URL; image blocks only
}

type WireToolResultEvent struct {
	Type            string             `json:"type"`
	ToolID          string             `json:"toolId"`
	Content         []WireContentBlock `json:"content"`
	ParentToolUseID string             `json:"parentToolUseId,omitempty"`
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
	Content        string `json:"content"`
	Fatal          bool   `json:"fatal"`
	ErrorType      string `json:"errorType,omitempty"`
	RetryAfterSecs int    `json:"retryAfterSecs,omitempty"`
}

type WireRateLimitEvent struct {
	Type          string  `json:"type"`
	Status        string  `json:"status"`
	Utilization   float64 `json:"utilization"`
	ResetsAt      int64   `json:"resetsAt,omitempty"`
	RateLimitType string  `json:"rateLimitType,omitempty"`
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

// Agent message direction constants.
const (
	DirectionSent     = "sent"
	DirectionReceived = "received"
)

// WireAgentMessageEvent represents a message between peer sessions in a team.
type WireAgentMessageEvent struct {
	Type            string `json:"type"`            // "agent_message"
	Direction       string `json:"direction"`       // DirectionSent or DirectionReceived
	SenderSessionID string `json:"senderSessionId"`
	SenderName      string `json:"senderName"`
	TargetSessionID string `json:"targetSessionId"`
	TargetName      string `json:"targetName"`
	Content         string `json:"content"`
}

// WireUserMessageEvent represents a user message injected mid-turn via SendMessage.
type WireUserMessageEvent struct {
	Type        string            `json:"type"`
	Content     string            `json:"content"`
	Attachments []QueryAttachment `json:"attachments,omitempty"`
}

// WireTaskEvent represents a subagent lifecycle event.
type WireTaskEvent struct {
	Type         string `json:"type"`                    // "task"
	Subtype      string `json:"subtype"`                 // "task_started", "task_progress", "task_notification"
	TaskID       string `json:"taskId"`
	ToolUseID    string `json:"toolUseId"`               // parent Agent ToolUseEvent.ID
	Description  string `json:"description,omitempty"`
	TaskType     string `json:"taskType,omitempty"`
	Prompt       string `json:"prompt,omitempty"`
	LastToolName string `json:"lastToolName,omitempty"`
	Status       string `json:"status,omitempty"`
	Summary      string `json:"summary,omitempty"`
	TotalTokens  int    `json:"totalTokens,omitempty"`
	ToolUses     int    `json:"toolUses,omitempty"`
	DurationMs   int    `json:"durationMs,omitempty"`
}

// WireAgentResultEvent represents a completed subagent execution.
type WireAgentResultEvent struct {
	Type              string             `json:"type"`              // "agent_result"
	ParentToolUseID   string             `json:"parentToolUseId"`
	Status            string             `json:"status"`
	AgentID           string             `json:"agentId,omitempty"`
	AgentType         string             `json:"agentType,omitempty"`
	Content           []WireContentBlock `json:"content"`
	TotalDurationMs   int                `json:"totalDurationMs,omitempty"`
	TotalTokens       int                `json:"totalTokens,omitempty"`
	TotalToolUseCount int                `json:"totalToolUseCount,omitempty"`
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
func (e WireAgentMessageEvent) WireType() string       { return e.Type }
func (e WireUserMessageEvent) WireType() string        { return e.Type }
func (e WireTaskEvent) WireType() string               { return e.Type }
func (e WireAgentResultEvent) WireType() string        { return e.Type }

// errorDetail extracts a clean human-readable message from a claudecli error,
// stripping redundant sentinel prefixes (e.g. "permission denied: Your API key..."
// becomes just "Your API key...").
func errorDetail(err error) string {
	var rlErr *claudecli.RateLimitError
	if errors.As(err, &rlErr) {
		return rlErr.Message
	}
	var cliErr *claudecli.Error
	if errors.As(err, &cliErr) && cliErr.Message != "" {
		return cliErr.Message
	}
	return err.Error()
}

// ToWireEvent converts a claudecli-go event to a JSON-friendly wire format.
// Returns nil for event types we don't forward to the frontend.
func ToWireEvent(event claudecli.Event) any {
	switch e := event.(type) {
	case *claudecli.TextEvent:
		return WireTextEvent{Type: "text", Content: e.Content, ParentToolUseID: e.ParentToolUseID}
	case *claudecli.ThinkingEvent:
		return WireThinkingEvent{Type: "thinking", Content: e.Content, ParentToolUseID: e.ParentToolUseID}
	case *claudecli.ToolUseEvent:
		return WireToolUseEvent{
			Type:            "tool_use",
			ToolID:          e.ID,
			ToolName:        e.Name,
			ToolInput:       e.Input,
			Category:        classifyTool(e.Name),
			ParentToolUseID: e.ParentToolUseID,
		}
	case *claudecli.ToolResultEvent:
		return WireToolResultEvent{
			Type:            "tool_result",
			ToolID:          e.ToolUseID,
			Content:         convertToolContent(e.Content),
			ParentToolUseID: e.ParentToolUseID,
		}
	case *claudecli.ResultEvent:
		wire := WireResultEvent{
			Type:       "result",
			Cost:       e.CostUSD,
			Duration:   e.Duration.Milliseconds(),
			Usage:      e.Usage,
			StopReason: e.StopReason,
		}
		// Use ContextSnapshot (per-API-call usage from the last stream event pair)
		// instead of ModelUsage (cumulative across all API calls in the run).
		if cs := e.ContextSnapshot; cs != nil {
			wire.InputTokens = cs.InputTokens + cs.CacheReadInputTokens + cs.CacheCreationInputTokens
			wire.OutputTokens = cs.OutputTokens
			wire.ContextWindow = cs.ContextWindow
		}
		// ContextWindow from ModelUsage as fallback (snapshot may lack it on model mismatch).
		if wire.ContextWindow == 0 {
			for _, mu := range e.ModelUsage {
				if mu.ContextWindow > wire.ContextWindow {
					wire.ContextWindow = mu.ContextWindow
				}
			}
		}
		if wire.ContextWindow == 0 {
			wire.ContextWindow = 200_000
		}
		return wire
	case *claudecli.ErrorEvent:
		we := WireErrorEvent{Type: "error", Content: errorDetail(e.Err), Fatal: e.Fatal}
		switch {
		case errors.Is(e.Err, claudecli.ErrRateLimit):
			we.ErrorType = "rate_limit"
			var rlErr *claudecli.RateLimitError
			if errors.As(e.Err, &rlErr) && rlErr.RetryAfter > 0 {
				we.RetryAfterSecs = int(rlErr.RetryAfter.Seconds())
			}
		case errors.Is(e.Err, claudecli.ErrAuth):
			we.ErrorType = "auth"
		case errors.Is(e.Err, claudecli.ErrOverloaded):
			we.ErrorType = "overloaded"
		case errors.Is(e.Err, claudecli.ErrBilling):
			we.ErrorType = "billing"
		case errors.Is(e.Err, claudecli.ErrPermission):
			we.ErrorType = "permission"
		case errors.Is(e.Err, claudecli.ErrInvalidRequest):
			we.ErrorType = "invalid_request"
		case errors.Is(e.Err, claudecli.ErrNotFound):
			we.ErrorType = "not_found"
		case errors.Is(e.Err, claudecli.ErrRequestTooLarge):
			we.ErrorType = "request_too_large"
		case errors.Is(e.Err, claudecli.ErrAPI):
			we.ErrorType = "api_error"
		default:
			we.ErrorType = "api_error"
		}
		return we
	case *claudecli.RateLimitEvent:
		rlType := e.RateLimitType
		if rlType == "seven_day_opus" {
			rlType = "seven_day"
		}
		return WireRateLimitEvent{
			Type:          "rate_limit",
			Status:        e.Status,
			Utilization:   e.Utilization,
			ResetsAt:      e.ResetsAt,
			RateLimitType: rlType,
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
	case *claudecli.TaskEvent:
		return WireTaskEvent{
			Type:         "task",
			Subtype:      e.Subtype,
			TaskID:       e.TaskID,
			ToolUseID:    e.ToolUseID,
			Description:  e.Description,
			TaskType:     e.TaskType,
			Prompt:       e.Prompt,
			LastToolName: e.LastToolName,
			Status:       e.Status,
			Summary:      e.Summary,
			TotalTokens:  e.TotalTokens,
			ToolUses:     e.ToolUses,
			DurationMs:   e.DurationMs,
		}
	case *claudecli.UserEvent:
		if e.AgentResult == nil {
			return nil
		}
		return WireAgentResultEvent{
			Type:              "agent_result",
			ParentToolUseID:   e.ParentToolUseID,
			Status:            e.AgentResult.Status,
			AgentID:           e.AgentResult.AgentID,
			AgentType:         e.AgentResult.AgentType,
			Content:           convertToolContent(e.AgentResult.Content),
			TotalDurationMs:   e.AgentResult.TotalDurationMs,
			TotalTokens:       e.AgentResult.TotalTokens,
			TotalToolUseCount: e.AgentResult.TotalToolUseCount,
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
	case "ToolSearch", "Skill":
		return "meta"
	case "AskUserQuestion":
		return "question"
	case "ExitWorktree":
		return "agent"
	}
	if strings.HasPrefix(name, "mcp__") {
		return "mcp"
	}
	return "other"
}
