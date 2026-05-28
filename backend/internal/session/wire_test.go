package session

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/allbin/agentkit/runtime"
	claudecli "github.com/allbin/claudecli-go"
)

func TestToWireEvent_ToolResultTextOnly(t *testing.T) {
	event := runtime.ToolResultEvent{
		ToolUseID: "tu_123",
		Content: []runtime.ToolContent{
			{Type: "text", Text: "file contents here"},
		},
	}

	wire := ToWireEvent(event, "")
	tr, ok := wire.(WireToolResultEvent)
	if !ok {
		t.Fatalf("expected WireToolResultEvent, got %T", wire)
	}

	if tr.ToolID != "tu_123" {
		t.Errorf("ToolID = %q, want tu_123", tr.ToolID)
	}
	if len(tr.Content) != 1 {
		t.Fatalf("Content has %d blocks, want 1", len(tr.Content))
	}
	if tr.Content[0].Type != "text" || tr.Content[0].Text != "file contents here" {
		t.Errorf("block = %+v, want text block", tr.Content[0])
	}
}

func TestToWireEvent_ToolResultWithImage(t *testing.T) {
	event := runtime.ToolResultEvent{
		ToolUseID: "tu_456",
		Content: []runtime.ToolContent{
			{Type: "text", Text: "Screenshot saved"},
			{Type: "image", MediaType: "image/png", Data: "iVBORw0KGgo="},
		},
	}

	wire := ToWireEvent(event, "")
	tr, ok := wire.(WireToolResultEvent)
	if !ok {
		t.Fatalf("expected WireToolResultEvent, got %T", wire)
	}

	if len(tr.Content) != 2 {
		t.Fatalf("Content has %d blocks, want 2", len(tr.Content))
	}

	// Text block
	if tr.Content[0].Type != "text" || tr.Content[0].Text != "Screenshot saved" {
		t.Errorf("block[0] = %+v, want text", tr.Content[0])
	}

	// Image block — should be a data URL
	img := tr.Content[1]
	if img.Type != "image" {
		t.Errorf("block[1].Type = %q, want image", img.Type)
	}
	if img.MediaType != "image/png" {
		t.Errorf("block[1].MediaType = %q, want image/png", img.MediaType)
	}
	wantPrefix := "data:image/png;base64,"
	if !strings.HasPrefix(img.URL, wantPrefix) {
		t.Errorf("block[1].URL doesn't start with %q: %q", wantPrefix, img.URL[:min(len(img.URL), 40)])
	}
}

func TestToWireEvent_ToolResultJSON(t *testing.T) {
	event := runtime.ToolResultEvent{
		ToolUseID: "tu_789",
		Content: []runtime.ToolContent{
			{Type: "text", Text: "ok"},
			{Type: "image", MediaType: "image/png", Data: "AAAA"},
		},
	}

	wire := ToWireEvent(event, "")
	data, err := json.Marshal(wire)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	// Verify the JSON structure matches what the frontend expects.
	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if parsed["type"] != "tool_result" {
		t.Errorf("type = %v", parsed["type"])
	}
	if parsed["toolId"] != "tu_789" {
		t.Errorf("toolId = %v", parsed["toolId"])
	}

	blocks, ok := parsed["content"].([]any)
	if !ok || len(blocks) != 2 {
		t.Fatalf("content = %v, want 2-element array", parsed["content"])
	}

	b0 := blocks[0].(map[string]any)
	if b0["type"] != "text" || b0["text"] != "ok" {
		t.Errorf("block[0] = %v", b0)
	}

	b1 := blocks[1].(map[string]any)
	if b1["type"] != "image" {
		t.Errorf("block[1].type = %v", b1["type"])
	}
	if b1["url"] != "data:image/png;base64,AAAA" {
		t.Errorf("block[1].url = %v", b1["url"])
	}
}

func TestToWireEvent_ErrorClassification(t *testing.T) {
	tests := []struct {
		name           string
		err            error
		wantErrorType  string
		wantRetryAfter int
	}{
		{
			name:           "rate limit with retry",
			err:            &claudecli.RateLimitError{RetryAfter: 30 * time.Second, Message: "slow down"},
			wantErrorType:  "rate_limit",
			wantRetryAfter: 30,
		},
		{
			name:          "rate limit without retry",
			err:           &claudecli.RateLimitError{Message: "slow down"},
			wantErrorType: "rate_limit",
		},
		{
			name:          "auth error",
			err:           claudecli.ErrAuth,
			wantErrorType: "auth",
		},
		{
			name:          "overloaded",
			err:           claudecli.ErrOverloaded,
			wantErrorType: "overloaded",
		},
		{
			name:          "ErrAPI wrapped",
			err:           fmt.Errorf("%w: billing_error: payment required", claudecli.ErrAPI),
			wantErrorType: "api_error",
		},
		{
			name:          "non-sentinel error",
			err:           errors.New("connection reset"),
			wantErrorType: "api_error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			event := runtime.ErrorEvent{Err: tt.err, Fatal: false}
			wire := ToWireEvent(event, "")
			we, ok := wire.(WireErrorEvent)
			if !ok {
				t.Fatalf("expected WireErrorEvent, got %T", wire)
			}
			if we.ErrorType != tt.wantErrorType {
				t.Errorf("ErrorType = %q, want %q", we.ErrorType, tt.wantErrorType)
			}
			if we.RetryAfterSecs != tt.wantRetryAfter {
				t.Errorf("RetryAfterSecs = %d, want %d", we.RetryAfterSecs, tt.wantRetryAfter)
			}
		})
	}
}

func TestToWireEvent_ResultTokens(t *testing.T) {
	event := runtime.TurnCompletedEvent{
		Status:     runtime.TurnStatusCompleted,
		CostUSD:    0.01,
		Duration:   time.Second,
		StopReason: "end_turn",
		Usage: runtime.TokenUsage{
			InputTokens:       100,
			CacheReadTokens:   50_000,
			CacheCreateTokens: 5_000,
			OutputTokens:      2_000,
		},
		ContextWindow: 200_000,
	}

	wire := ToWireEvent(event, "")
	r, ok := wire.(WireResultEvent)
	if !ok {
		t.Fatalf("expected WireResultEvent, got %T", wire)
	}

	wantInput := 100 + 50_000 + 5_000
	if r.InputTokens != wantInput {
		t.Errorf("InputTokens = %d, want %d", r.InputTokens, wantInput)
	}
	if r.OutputTokens != 2_000 {
		t.Errorf("OutputTokens = %d, want 2000", r.OutputTokens)
	}
	if r.ContextWindow != 200_000 {
		t.Errorf("ContextWindow = %d, want 200000", r.ContextWindow)
	}
}

func TestToWireEvent_ResultDefaultContextWindow(t *testing.T) {
	event := runtime.TurnCompletedEvent{
		Status:     runtime.TurnStatusCompleted,
		CostUSD:    0.01,
		Duration:   time.Second,
		StopReason: "end_turn",
	}

	wire := ToWireEvent(event, "opus")
	r, ok := wire.(WireResultEvent)
	if !ok {
		t.Fatalf("expected WireResultEvent, got %T", wire)
	}
	if r.ContextWindow != 200_000 {
		t.Errorf("ContextWindow = %d, want 200000 (default fallback)", r.ContextWindow)
	}

	wire1m := ToWireEvent(event, "opus[1m]")
	r1m, ok := wire1m.(WireResultEvent)
	if !ok {
		t.Fatalf("expected WireResultEvent, got %T", wire1m)
	}
	if r1m.ContextWindow != 1_000_000 {
		t.Errorf("ContextWindow = %d, want 1000000 (1M model fallback)", r1m.ContextWindow)
	}
}

func TestToWireEvent_ParentToolUseID(t *testing.T) {
	tests := []struct {
		name  string
		event runtime.CLIEvent
		check func(t *testing.T, wire any)
	}{
		{
			name:  "AssistantTextEvent with ParentToolUseID",
			event: runtime.AssistantTextEvent{Content: "hello", ParentToolUseID: "tu_parent"},
			check: func(t *testing.T, wire any) {
				e := wire.(WireTextEvent)
				if e.ParentToolUseID != "tu_parent" {
					t.Errorf("ParentToolUseID = %q, want tu_parent", e.ParentToolUseID)
				}
			},
		},
		{
			name:  "AssistantTextEvent without ParentToolUseID omits field",
			event: runtime.AssistantTextEvent{Content: "hello"},
			check: func(t *testing.T, wire any) {
				data, _ := json.Marshal(wire)
				if strings.Contains(string(data), "parentToolUseId") {
					t.Error("parentToolUseId should be omitted when empty")
				}
			},
		},
		{
			name:  "ThinkingEvent with ParentToolUseID",
			event: runtime.ThinkingEvent{Content: "think", ParentToolUseID: "tu_parent"},
			check: func(t *testing.T, wire any) {
				e := wire.(WireThinkingEvent)
				if e.ParentToolUseID != "tu_parent" {
					t.Errorf("ParentToolUseID = %q, want tu_parent", e.ParentToolUseID)
				}
			},
		},
		{
			name: "ToolUseEvent with ParentToolUseID",
			event: runtime.ToolUseEvent{
				ID: "t1", Name: "Read", Input: json.RawMessage(`{}`),
				ParentToolUseID: "tu_parent",
			},
			check: func(t *testing.T, wire any) {
				e := wire.(WireToolUseEvent)
				if e.ParentToolUseID != "tu_parent" {
					t.Errorf("ParentToolUseID = %q, want tu_parent", e.ParentToolUseID)
				}
			},
		},
		{
			name: "ToolResultEvent with ParentToolUseID",
			event: runtime.ToolResultEvent{
				ToolUseID:       "t1",
				Content:         []runtime.ToolContent{{Type: "text", Text: "ok"}},
				ParentToolUseID: "tu_parent",
			},
			check: func(t *testing.T, wire any) {
				e := wire.(WireToolResultEvent)
				if e.ParentToolUseID != "tu_parent" {
					t.Errorf("ParentToolUseID = %q, want tu_parent", e.ParentToolUseID)
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wire := ToWireEvent(tt.event, "")
			if wire == nil {
				t.Fatal("ToWireEvent returned nil")
			}
			tt.check(t, wire)
		})
	}
}

func TestToWireEvent_SubagentEvent(t *testing.T) {
	tests := []struct {
		name    string
		event   runtime.SubagentEvent
		wantSub string
	}{
		{
			name: "task_started",
			event: runtime.SubagentEvent{
				Subtype: "task_started", TaskID: "task-1", ToolUseID: "tu_agent",
				Description: "Explore codebase", TaskType: "local_agent",
			},
			wantSub: "task_started",
		},
		{
			name: "task_progress",
			event: runtime.SubagentEvent{
				Subtype: "task_progress", TaskID: "task-1", ToolUseID: "tu_agent",
				LastToolName: "Read", TotalTokens: 5000, ToolUses: 3,
			},
			wantSub: "task_progress",
		},
		{
			name: "task_notification",
			event: runtime.SubagentEvent{
				Subtype: "task_notification", TaskID: "task-1", ToolUseID: "tu_agent",
				Status: "completed", Summary: "Done exploring",
				TotalTokens: 10000, ToolUses: 8, DurationMs: 5000,
			},
			wantSub: "task_notification",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wire := ToWireEvent(tt.event, "")
			te, ok := wire.(WireTaskEvent)
			if !ok {
				t.Fatalf("expected WireTaskEvent, got %T", wire)
			}
			if te.Type != "task" {
				t.Errorf("Type = %q, want task", te.Type)
			}
			if te.Subtype != tt.wantSub {
				t.Errorf("Subtype = %q, want %q", te.Subtype, tt.wantSub)
			}
			if te.TaskID != tt.event.TaskID {
				t.Errorf("TaskID = %q, want %q", te.TaskID, tt.event.TaskID)
			}
			if te.ToolUseID != tt.event.ToolUseID {
				t.Errorf("ToolUseID = %q, want %q", te.ToolUseID, tt.event.ToolUseID)
			}
		})
	}
}

func TestToWireEvent_UserEchoReturnsNil(t *testing.T) {
	// UserEcho is handled by EventPipeline.processUserEcho, not ToWireEvent.
	event := runtime.UserEcho{MessageID: "msg_1"}
	wire := ToWireEvent(event, "")
	if wire != nil {
		t.Errorf("expected nil for UserEcho, got %T", wire)
	}
}

func TestToWireEvent_NonJSONRawPayloadsMarshal(t *testing.T) {
	tests := []struct {
		name  string
		event runtime.CLIEvent
	}{
		{
			name:  "turn diff",
			event: runtime.TurnDiffEvent{TurnID: "turn_1", Raw: json.RawMessage(`diff --git a/file b/file`)},
		},
		{
			name:  "tool input",
			event: runtime.ToolUseEvent{ID: "tool_1", Name: "fileChange", Input: json.RawMessage(`done`)},
		},
		{
			name:  "stream",
			event: runtime.StreamEvent{Raw: json.RawMessage(`partial text`)},
		},
		{
			name:  "context management",
			event: runtime.ContextManagementEvent{Raw: json.RawMessage(`summary`)},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wire := ToWireEvent(tt.event, "")
			data, err := json.Marshal(wire)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			if !json.Valid(data) {
				t.Fatalf("invalid JSON: %s", data)
			}
		})
	}
}

func TestRawJSONOrStringPreservesValidJSON(t *testing.T) {
	got := rawJSONOrString(json.RawMessage(`{"ok":true}`))
	if string(got) != `{"ok":true}` {
		t.Fatalf("rawJSONOrString valid JSON = %s", got)
	}

	got = rawJSONOrString(json.RawMessage(`diff --git a/file b/file`))
	if string(got) != `"diff --git a/file b/file"` {
		t.Fatalf("rawJSONOrString invalid JSON = %s", got)
	}
}

func TestToWireEvent_UnknownProviderEvent(t *testing.T) {
	event := runtime.UnknownProviderEvent{Provider: "claude", Type: "future_type", Raw: json.RawMessage(`{}`)}
	wire := ToWireEvent(event, "")
	if wire != nil {
		t.Errorf("expected nil for UnknownProviderEvent, got %T", wire)
	}
}

func TestToolResultText(t *testing.T) {
	blocks := []WireContentBlock{
		{Type: "text", Text: "line1"},
		{Type: "image", URL: "data:image/png;base64,AAAA"},
		{Type: "text", Text: "line2"},
	}
	got := toolResultText(blocks)
	if got != "line1line2" {
		t.Errorf("toolResultText = %q, want line1line2", got)
	}
}
