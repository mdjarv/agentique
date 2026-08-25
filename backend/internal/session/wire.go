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
	Type       string  `json:"type"`
	Cost       float64 `json:"cost"`
	Duration   int64   `json:"duration"`
	Usage      any     `json:"usage"`
	StopReason string  `json:"stopReason"`
	// ContextWindow describes the last API call, so it does not shrink when the
	// provider compacts. WireContextUsageEvent carries the live measurement and
	// supersedes it whenever the provider can answer one.
	ContextWindow int   `json:"contextWindow,omitempty"`
	InputTokens   int   `json:"inputTokens,omitempty"`
	OutputTokens  int   `json:"outputTokens,omitempty"`
	Timestamp     int64 `json:"timestamp"` // epoch ms — set by pipeline
	// WorkflowPending marks the placeholder "running in the background" result a
	// dynamic workflow emits when it launches. It is NOT the final answer — a
	// later result with WorkflowPending=false carries that. The frontend must not
	// render a pending result as the turn's assistant message or treat it as a
	// turn boundary. See WireWorkflowLaunchedEvent / WireTaskEvent workflow fields.
	WorkflowPending bool `json:"workflowPending,omitempty"`
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

// WireContextUsageEvent carries a live context-window measurement taken against
// the provider's current transcript.
//
// It exists because WireResultEvent.ContextWindow describes the *last API call*:
// it does not shrink when the provider compacts, and drifts upward until the
// next turn — so the meter is simply wrong after a compaction. This event is
// measured on demand (see contextMeter) and stays correct across one.
//
// Transient: broadcast-only, never persisted. It is a point-in-time measurement
// of the session, not part of the conversation, so replaying history must not
// resurrect a stale one.
type WireContextUsageEvent struct {
	Type string `json:"type"` // "context_usage"
	// ContextWindow is the window usage is reported against — runtime's
	// MaxTokens, which is what ContextUsage.Remaining() tracks. Render
	// against this one, not RawContextWindow.
	ContextWindow int `json:"contextWindow"`
	// UsedTokens is unclamped and can exceed ContextWindow when the session is
	// over the limit.
	UsedTokens int     `json:"usedTokens"`
	Percentage float64 `json:"percentage"`
	// RawContextWindow is the model's believed hard limit. Larger than
	// ContextWindow when a narrower compaction-policy window applies.
	RawContextWindow     int  `json:"rawContextWindow,omitempty"`
	AutoCompactEnabled   bool `json:"autoCompactEnabled,omitempty"`
	AutoCompactThreshold int  `json:"autoCompactThreshold,omitempty"`
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
	// Queued marks a message buffered for delivery as a fresh turn (providers
	// without native mid-turn injection, e.g. codex). The UI renders it as a
	// pending "queued" bubble that is cleared when the replayed turn starts.
	// Omitted (false) for native mid-turn messages, which the model picks up
	// within the current turn.
	Queued bool `json:"queued,omitempty"`
}

// WireMessageDeliveryEvent resolves the fate of a user message sent via
// SendMessage or buffered by QueuePendingMessage. Transient — broadcast only,
// not persisted.
//
// Without it a queued message's UI bubble stays pending forever, so every path
// that removes a message from a queue must emit one.
type WireMessageDeliveryEvent struct {
	Type string `json:"type"`
	// Status is "delivered" (the CLI read it) or "cancelled" (a stop dropped
	// it before it ran — see Session.Interrupt).
	Status    string `json:"status"`
	MessageID string `json:"messageId"`
}

// WireTaskEvent represents a subagent lifecycle event. A dynamic workflow
// surfaces through this same shape as a single synthetic task with
// TaskType == "local_workflow"; the Workflow* fields are populated only then and
// carry per-phase / per-agent progress for the workflow panel.
type WireTaskEvent struct {
	Type         string `json:"type"`    // "task"
	Subtype      string `json:"subtype"` // "task_started", "task_progress", "task_notification"
	TaskID       string `json:"taskId"`
	ToolUseID    string `json:"toolUseId"` // parent Agent ToolUseEvent.ID
	Description  string `json:"description,omitempty"`
	TaskType     string `json:"taskType,omitempty"` // "local_agent" | "local_workflow"
	Prompt       string `json:"prompt,omitempty"`
	LastToolName string `json:"lastToolName,omitempty"`
	Status       string `json:"status,omitempty"`
	Summary      string `json:"summary,omitempty"`
	TotalTokens  int    `json:"totalTokens,omitempty"`
	ToolUses     int    `json:"toolUses,omitempty"`
	DurationMs   int    `json:"durationMs,omitempty"`

	// Workflow fields — set only when TaskType == "local_workflow".
	WorkflowName     string                 `json:"workflowName,omitempty"`
	OutputFile       string                 `json:"outputFile,omitempty"`
	EndTime          int64                  `json:"endTime,omitempty"`
	WorkflowProgress []WireWorkflowProgress `json:"workflowProgress,omitempty"`
}

// WireWorkflowProgress is one entry in a workflow's progress list: a phase
// boundary (Type == "workflow_phase", carrying Index/Title) or a single
// subagent's state (Type == "workflow_agent"). Mirrors
// runtime.WorkflowAgentProgress; rides on task_progress WireTaskEvents.
type WireWorkflowProgress struct {
	Type  string `json:"type"` // "workflow_agent" | "workflow_phase"
	Index int    `json:"index"`

	// workflow_phase
	Title string `json:"title,omitempty"`

	// workflow_agent
	Label           string `json:"label,omitempty"`
	PhaseIndex      int    `json:"phaseIndex,omitempty"`
	PhaseTitle      string `json:"phaseTitle,omitempty"`
	AgentID         string `json:"agentId,omitempty"`
	Model           string `json:"model,omitempty"`
	State           string `json:"state,omitempty"` // queued|start|progress|done|error
	Attempt         int    `json:"attempt,omitempty"`
	LastToolName    string `json:"lastToolName,omitempty"`
	LastToolSummary string `json:"lastToolSummary,omitempty"`
	PromptPreview   string `json:"promptPreview,omitempty"`
	ResultPreview   string `json:"resultPreview,omitempty"`
	Tokens          int    `json:"tokens,omitempty"`
	ToolCalls       int    `json:"toolCalls,omitempty"`
	DurationMs      int    `json:"durationMs,omitempty"`
}

// WireWorkflowLaunchedEvent reports that a dynamic workflow launched in the
// background. It is the run's identity/handle (RunID keys a run; ScriptPath
// locates the generated script). The lifecycle then streams through
// WireTaskEvents (TaskType == "local_workflow") and the final answer arrives as
// a later non-pending WireResultEvent.
//
// NOTE: agentkit's WorkflowLaunchedEvent carries no TaskID/ToolUseID, so this
// event cannot be correlated to the task stream client-side — the workflow panel
// keys on the task events' ToolUseID instead. See docs/workflows.md.
type WireWorkflowLaunchedEvent struct {
	Type          string `json:"type"` // "workflow_launched"
	RunID         string `json:"runId"`
	WorkflowName  string `json:"workflowName,omitempty"`
	ScriptPath    string `json:"scriptPath,omitempty"`
	TranscriptDir string `json:"transcriptDir,omitempty"`
	Summary       string `json:"summary,omitempty"`
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
func (e WireContextUsageEvent) WireType() string      { return e.Type }
func (e WireAgentMessageEvent) WireType() string      { return e.Type }
func (e WireUserMessageEvent) WireType() string       { return e.Type }
func (e WireMessageDeliveryEvent) WireType() string   { return e.Type }
func (e WireTaskEvent) WireType() string              { return e.Type }
func (e WireWorkflowLaunchedEvent) WireType() string  { return e.Type }
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
		wire.WorkflowPending = e.WorkflowPending
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
			Type:             "task",
			Subtype:          e.Subtype,
			TaskID:           e.TaskID,
			ToolUseID:        e.ToolUseID,
			Description:      e.Description,
			TaskType:         e.TaskType,
			Prompt:           e.Prompt,
			LastToolName:     e.LastToolName,
			Status:           e.Status,
			Summary:          e.Summary,
			TotalTokens:      e.TotalTokens,
			ToolUses:         e.ToolUses,
			DurationMs:       e.DurationMs,
			WorkflowName:     e.WorkflowName,
			OutputFile:       e.OutputFile,
			EndTime:          e.EndTime,
			WorkflowProgress: convertWorkflowProgress(e.WorkflowProgress),
		}
	case runtime.WorkflowLaunchedEvent:
		return WireWorkflowLaunchedEvent{
			Type:          "workflow_launched",
			RunID:         e.RunID,
			WorkflowName:  e.WorkflowName,
			ScriptPath:    e.ScriptPath,
			TranscriptDir: e.TranscriptDir,
			Summary:       e.Summary,
		}
	case runtime.AgentResultEvent:
		// Not every AgentResultEvent describes an agent. The claude adapter
		// derives one from any user event carrying a tool_use_result
		// (agentkit runtime/cli/claude/event_map.go, mapUserEvent), and
		// claudecli's parseAgentResult returns a non-nil result for any JSON
		// object — so an ordinary Bash/Read/Edit result unmarshals into an
		// all-zero AgentResult and arrives here as an event with no status, no
		// agent and no report. Dropping it is deliberate rather than filtering
		// at persist time: it is not news to anyone, so there is nothing to
		// broadcast either. Real ones always carry a Status ("completed",
		// "async_launched").
		if emptyAgentResult(e) {
			return nil
		}
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

// emptyAgentResult reports whether an AgentResultEvent carries nothing at all:
// no outcome, no agent identity, no report, no totals.
//
// ParentToolUseID is excluded from the test on purpose. It is the only field an
// ordinary *nested* tool result inside a subagent populates, and such an event
// is exactly as empty as a top-level one — keeping it would leave the same
// per-tool-call noise, one level down.
func emptyAgentResult(e runtime.AgentResultEvent) bool {
	return e.Status == "" && e.AgentID == "" && e.AgentType == "" &&
		len(e.Content) == 0 && e.TotalDurationMs == 0 && e.TotalTokens == 0 &&
		e.TotalToolUseCount == 0
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

// convertWorkflowProgress maps neutral runtime workflow-progress entries to
// their wire shape. Returns nil for ordinary subagent tasks (nil input), so the
// omitempty tag drops the field for non-workflow task events.
func convertWorkflowProgress(in []runtime.WorkflowAgentProgress) []WireWorkflowProgress {
	if len(in) == 0 {
		return nil
	}
	out := make([]WireWorkflowProgress, 0, len(in))
	for _, p := range in {
		out = append(out, WireWorkflowProgress{
			Type:            p.Type,
			Index:           p.Index,
			Title:           p.Title,
			Label:           p.Label,
			PhaseIndex:      p.PhaseIndex,
			PhaseTitle:      p.PhaseTitle,
			AgentID:         p.AgentID,
			Model:           p.Model,
			State:           p.State,
			Attempt:         p.Attempt,
			LastToolName:    p.LastToolName,
			LastToolSummary: p.LastToolSummary,
			PromptPreview:   p.PromptPreview,
			ResultPreview:   p.ResultPreview,
			Tokens:          p.Tokens,
			ToolCalls:       p.ToolCalls,
			DurationMs:      p.DurationMs,
		})
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
	case "TodoWrite", "TodoRead",
		// Claude Code's task-list family superseded TodoWrite; TaskOutput/TaskStop
		// are background-process control (a different "task") and stay default.
		"TaskCreate", "TaskUpdate", "TaskList", "TaskGet":
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
