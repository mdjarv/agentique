package session

import (
	"encoding/json"
	"errors"
	"strings"

	"github.com/allbin/agentkit/runtime"
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
	Signature       string `json:"signature,omitempty"`
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
	Text      string `json:"text,omitempty"`      // populated for text blocks
	MediaType string `json:"mediaType,omitempty"` // e.g. "image/png"; image blocks only
	URL       string `json:"url,omitempty"`       // data: URL; image blocks only
}

type WireToolResultEvent struct {
	Type            string             `json:"type"`
	ToolID          string             `json:"toolId"`
	Content         []WireContentBlock `json:"content"`
	ParentToolUseID string             `json:"parentToolUseId,omitempty"`
}

type WireResultEvent struct {
	Type          string  `json:"type"`
	Cost          float64 `json:"cost"`
	Duration      int64   `json:"duration"`
	Usage         any     `json:"usage"`
	StopReason    string  `json:"stopReason"`
	ContextWindow int     `json:"contextWindow,omitempty"`
	InputTokens   int     `json:"inputTokens,omitempty"`
	OutputTokens  int     `json:"outputTokens,omitempty"`
	Timestamp     int64   `json:"timestamp"` // epoch ms — set by pipeline
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

type WireToolOutputDeltaEvent struct {
	Type     string `json:"type"`
	ItemID   string `json:"itemId"`
	ToolName string `json:"toolName,omitempty"`
	Delta    string `json:"delta"`
}

type WireReasoningDeltaEvent struct {
	Type   string `json:"type"`
	ItemID string `json:"itemId"`
	Delta  string `json:"delta"`
}

type WireTurnDiffEvent struct {
	Type   string          `json:"type"`
	TurnID string          `json:"turnId,omitempty"`
	Raw    json.RawMessage `json:"raw"`
}

type WireToolProgressEvent struct {
	Type      string `json:"type"`
	ToolUseID string `json:"toolUseId"`
	ToolName  string `json:"toolName,omitempty"`
	ElapsedMs int64  `json:"elapsedMs"`
}

// Agent message direction constants.
const (
	DirectionSent     = "sent"
	DirectionReceived = "received"
)

// WireAgentMessageEvent represents a message between peer sessions in a channel.
type WireAgentMessageEvent struct {
	Type            string `json:"type"`      // "agent_message"
	Direction       string `json:"direction"` // DirectionSent or DirectionReceived
	ChannelID       string `json:"channelId,omitempty"`
	SenderSessionID string `json:"senderSessionId"`
	SenderName      string `json:"senderName"`
	TargetSessionID string `json:"targetSessionId"`
	TargetName      string `json:"targetName"`
	Content         string `json:"content"`
	MessageType     string `json:"messageType,omitempty"`
	FromUser        bool   `json:"fromUser,omitempty"`
}

// WireChannelMessage is the unified wire format for channel timeline messages.
// Replaces WireAgentMessageEvent for timeline reads — no direction/dedup needed.
type WireChannelMessage struct {
	ID          string          `json:"id"`
	ChannelID   string          `json:"channelId"`
	SenderType  string          `json:"senderType"` // "session" or "user"
	SenderID    string          `json:"senderId"`
	SenderName  string          `json:"senderName"`
	Content     string          `json:"content"`
	MessageType string          `json:"messageType,omitempty"`
	Metadata    json.RawMessage `json:"metadata,omitempty"`
	CreatedAt   string          `json:"createdAt"`
}

func (e WireChannelMessage) WireType() string { return "channel_message" }

// WireUserMessageEvent represents a user message injected mid-turn via SendMessage.
type WireUserMessageEvent struct {
	Type        string            `json:"type"`
	Content     string            `json:"content"`
	MessageID   string            `json:"messageId,omitempty"`
	Attachments []QueryAttachment `json:"attachments,omitempty"`
}

// WireMessageDeliveryEvent confirms that the CLI has read a user message
// sent via SendMessage. Transient — broadcast only, not persisted.
type WireMessageDeliveryEvent struct {
	Type      string `json:"type"`   // "message_delivery"
	Status    string `json:"status"` // "delivered"
	MessageID string `json:"messageId"`
}

// WireTaskEvent represents a subagent lifecycle event.
type WireTaskEvent struct {
	Type         string `json:"type"`    // "task"
	Subtype      string `json:"subtype"` // "task_started", "task_progress", "task_notification"
	TaskID       string `json:"taskId"`
	ToolUseID    string `json:"toolUseId"` // parent Agent ToolUseEvent.ID
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
	Type              string             `json:"type"` // "agent_result"
	ParentToolUseID   string             `json:"parentToolUseId"`
	Status            string             `json:"status"`
	AgentID           string             `json:"agentId,omitempty"`
	AgentType         string             `json:"agentType,omitempty"`
	Content           []WireContentBlock `json:"content"`
	TotalDurationMs   int                `json:"totalDurationMs,omitempty"`
	TotalTokens       int                `json:"totalTokens,omitempty"`
	TotalToolUseCount int                `json:"totalToolUseCount,omitempty"`
}

func (e WireTextEvent) WireType() string              { return e.Type }
func (e WireThinkingEvent) WireType() string          { return e.Type }
func (e WireToolUseEvent) WireType() string           { return e.Type }
func (e WireToolResultEvent) WireType() string        { return e.Type }
func (e WireResultEvent) WireType() string            { return e.Type }
func (e WireErrorEvent) WireType() string             { return e.Type }
func (e WireRateLimitEvent) WireType() string         { return e.Type }
func (e WireStreamEvent) WireType() string            { return e.Type }
func (e WireCompactStatusEvent) WireType() string     { return e.Type }
func (e WireCompactBoundaryEvent) WireType() string   { return e.Type }
func (e WireContextManagementEvent) WireType() string { return e.Type }
func (e WireAgentMessageEvent) WireType() string      { return e.Type }
func (e WireUserMessageEvent) WireType() string       { return e.Type }
func (e WireMessageDeliveryEvent) WireType() string   { return e.Type }
func (e WireTaskEvent) WireType() string              { return e.Type }
func (e WireAgentResultEvent) WireType() string       { return e.Type }
func (e WireToolOutputDeltaEvent) WireType() string   { return e.Type }
func (e WireReasoningDeltaEvent) WireType() string    { return e.Type }
func (e WireTurnDiffEvent) WireType() string          { return e.Type }
func (e WireToolProgressEvent) WireType() string      { return e.Type }

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

// defaultContextWindow returns a sensible fallback context window size for a model
// before the CLI reports the actual value.
func defaultContextWindow(model string) int {
	if strings.HasSuffix(model, "[1m]") {
		return 1_000_000
	}
	return 200_000
}

func rawJSONOrString(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return json.RawMessage(`null`)
	}
	if json.Valid(raw) {
		return append(json.RawMessage(nil), raw...)
	}
	encoded, err := json.Marshal(string(raw))
	if err != nil {
		return json.RawMessage(`null`)
	}
	return encoded
}

// ToWireEvent converts a runtime CLIEvent to a JSON-friendly wire format.
// Returns nil for event types we don't forward to the frontend.
// The model parameter is used to pick a sensible default context window before
// the CLI reports one.
func ToWireEvent(event runtime.CLIEvent, model string) any {
	switch e := event.(type) {
	case runtime.AssistantTextEvent:
		return WireTextEvent{Type: "text", Content: e.Content, ParentToolUseID: e.ParentToolUseID}
	case runtime.AssistantTextDeltaEvent:
		raw, _ := json.Marshal(map[string]string{"itemId": e.ItemID, "delta": e.Delta})
		return WireStreamEvent{Type: "stream", Event: raw}
	case runtime.ToolOutputDeltaEvent:
		return WireToolOutputDeltaEvent{Type: "tool_output_delta", ItemID: e.ItemID, ToolName: e.ToolName, Delta: e.Delta}
	case runtime.ReasoningDeltaEvent:
		return WireReasoningDeltaEvent{Type: "reasoning_delta", ItemID: e.ItemID, Delta: e.Delta}
	case runtime.TurnDiffEvent:
		return WireTurnDiffEvent{Type: "turn_diff", TurnID: e.TurnID, Raw: rawJSONOrString(e.Raw)}
	case runtime.ToolProgressEvent:
		return WireToolProgressEvent{Type: "tool_progress", ToolUseID: e.ToolUseID, ToolName: e.ToolName, ElapsedMs: e.Elapsed.Milliseconds()}
	case runtime.ThinkingEvent:
		return WireThinkingEvent{Type: "thinking", Content: e.Content, Signature: e.Signature, ParentToolUseID: e.ParentToolUseID}
	case runtime.ToolUseEvent:
		return WireToolUseEvent{
			Type:            "tool_use",
			ToolID:          e.ID,
			ToolName:        e.Name,
			ToolInput:       rawJSONOrString(e.Input),
			Category:        classifyTool(e.Name),
			ParentToolUseID: e.ParentToolUseID,
		}
	case runtime.ToolResultEvent:
		return WireToolResultEvent{
			Type:            "tool_result",
			ToolID:          e.ToolUseID,
			Content:         convertToolContent(e.Content),
			ParentToolUseID: e.ParentToolUseID,
		}
	case runtime.TurnCompletedEvent:
		wire := WireResultEvent{
			Type:          "result",
			Cost:          e.CostUSD,
			Duration:      e.Duration.Milliseconds(),
			Usage:         e.Usage,
			StopReason:    e.StopReason,
			ContextWindow: e.ContextWindow,
			InputTokens:   e.Usage.InputTokens + e.Usage.CacheReadTokens + e.Usage.CacheCreateTokens,
			OutputTokens:  e.Usage.OutputTokens,
		}
		if wire.ContextWindow == 0 {
			wire.ContextWindow = defaultContextWindow(model)
		}
		return wire
	case runtime.ErrorEvent:
		return wireErrorEvent(e)
	case runtime.RateLimitEvent:
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
	case runtime.StreamEvent:
		return WireStreamEvent{Type: "stream", Event: rawJSONOrString(e.Raw)}
	case runtime.CompactStatusEvent:
		return WireCompactStatusEvent{Type: "compact_status", Status: e.Status}
	case runtime.CompactBoundaryEvent:
		return WireCompactBoundaryEvent{Type: "compact_boundary", Trigger: e.Trigger, PreTokens: e.PreTokens}
	case runtime.ContextManagementEvent:
		return WireContextManagementEvent{Type: "context_management", Raw: rawJSONOrString(e.Raw)}
	case runtime.SubagentEvent:
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
	case runtime.AgentResultEvent:
		return WireAgentResultEvent{
			Type:              "agent_result",
			ParentToolUseID:   e.ParentToolUseID,
			Status:            e.Status,
			AgentID:           e.AgentID,
			AgentType:         e.AgentType,
			Content:           convertToolContent(e.Content),
			TotalDurationMs:   e.TotalDurationMs,
			TotalTokens:       e.TotalTokens,
			TotalToolUseCount: e.TotalToolUseCount,
		}
	// SessionInitEvent is consumed by EventPipeline.handleInit before reaching
	// ToWireEvent. UserEcho is handled by processUserEvent (can produce
	// multiple wire events per CLI event).
	default:
		return nil
	}
}

// wireErrorEvent maps a runtime.ErrorEvent to a WireErrorEvent, classifying the
// error via claudecli sentinels when the underlying error originates from the
// claude adapter. Codex-emitted errors fall through to generic api_error
// classification — codex's wire shape lacks comparable sentinels today.
func wireErrorEvent(e runtime.ErrorEvent) WireErrorEvent {
	we := WireErrorEvent{Type: "error", Content: errorDetail(e.Err), Fatal: e.Fatal}
	switch e.Kind {
	case runtime.ErrorKindRateLimit:
		we.ErrorType = "rate_limit"
	case runtime.ErrorKindAuth:
		we.ErrorType = "auth"
	case runtime.ErrorKindBilling:
		we.ErrorType = "billing"
	case runtime.ErrorKindOverloaded:
		we.ErrorType = "overloaded"
	case runtime.ErrorKindPermission:
		we.ErrorType = "permission"
	case runtime.ErrorKindInvalidRequest:
		we.ErrorType = "invalid_request"
	case runtime.ErrorKindMaxTurns:
		we.ErrorType = "max_turns"
	}
	if we.ErrorType == "" {
		// Fall back to claudecli error sentinels for claude-originated errors
		// that the runtime didn't classify upstream.
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
		case errors.Is(e.Err, claudecli.ErrContextWindowExceeded):
			we.ErrorType = "context_window_exceeded"
		case errors.Is(e.Err, claudecli.ErrAPI):
			we.ErrorType = "api_error"
		default:
			we.ErrorType = "api_error"
		}
	}
	if we.RetryAfterSecs == 0 {
		var rlErr *claudecli.RateLimitError
		if errors.As(e.Err, &rlErr) && rlErr.RetryAfter > 0 {
			we.RetryAfterSecs = int(rlErr.RetryAfter.Seconds())
		}
	}
	return we
}

// convertToolContent converts runtime ToolContent blocks to wire format.
// Image blocks are encoded as data: URLs so the frontend can render them directly.
func convertToolContent(blocks []runtime.ToolContent) []WireContentBlock {
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
