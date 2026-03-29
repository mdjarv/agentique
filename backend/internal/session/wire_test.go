package session

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	claudecli "github.com/allbin/claudecli-go"
)

func TestToWireEvent_ToolResultTextOnly(t *testing.T) {
	event := &claudecli.ToolResultEvent{
		ToolUseID: "tu_123",
		Content: []claudecli.ToolContent{
			{Type: "text", Text: "file contents here"},
		},
	}

	wire := ToWireEvent(event)
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
	event := &claudecli.ToolResultEvent{
		ToolUseID: "tu_456",
		Content: []claudecli.ToolContent{
			{Type: "text", Text: "Screenshot saved"},
			{Type: "image", MediaType: "image/png", Data: "iVBORw0KGgo="},
		},
	}

	wire := ToWireEvent(event)
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
	event := &claudecli.ToolResultEvent{
		ToolUseID: "tu_789",
		Content: []claudecli.ToolContent{
			{Type: "text", Text: "ok"},
			{Type: "image", MediaType: "image/png", Data: "AAAA"},
		},
	}

	wire := ToWireEvent(event)
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
			name:          "rate limit with retry",
			err:           &claudecli.RateLimitError{RetryAfter: 30 * time.Second, Message: "slow down"},
			wantErrorType: "rate_limit",
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
			event := &claudecli.ErrorEvent{Err: tt.err, Fatal: false}
			wire := ToWireEvent(event)
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

func TestToWireEvent_ResultUsesContextSnapshot(t *testing.T) {
	event := &claudecli.ResultEvent{
		CostUSD:    0.01,
		Duration:   time.Second,
		StopReason: "end_turn",
		ContextSnapshot: &claudecli.ContextSnapshot{
			InputTokens:              100,
			CacheReadInputTokens:     50_000,
			CacheCreationInputTokens: 5_000,
			OutputTokens:             2_000,
			ContextWindow:            200_000,
		},
		// ModelUsage has large cumulative values — should be ignored for tokens.
		ModelUsage: map[string]claudecli.ModelUsage{
			"claude-opus-4-6": {
				InputTokens:       500_000,
				OutputTokens:      100_000,
				CacheReadTokens:   9_000_000,
				CacheCreateTokens: 500_000,
				ContextWindow:     200_000,
			},
		},
	}

	wire := ToWireEvent(event)
	r, ok := wire.(WireResultEvent)
	if !ok {
		t.Fatalf("expected WireResultEvent, got %T", wire)
	}

	wantInput := 100 + 50_000 + 5_000
	if r.InputTokens != wantInput {
		t.Errorf("InputTokens = %d, want %d (from ContextSnapshot, not cumulative ModelUsage)", r.InputTokens, wantInput)
	}
	if r.OutputTokens != 2_000 {
		t.Errorf("OutputTokens = %d, want 2000", r.OutputTokens)
	}
	if r.ContextWindow != 200_000 {
		t.Errorf("ContextWindow = %d, want 200000", r.ContextWindow)
	}
}

func TestToWireEvent_ResultFallbackWithoutSnapshot(t *testing.T) {
	event := &claudecli.ResultEvent{
		CostUSD:    0.01,
		Duration:   time.Second,
		StopReason: "end_turn",
		// No ContextSnapshot — fallback to ModelUsage for ContextWindow only.
		ModelUsage: map[string]claudecli.ModelUsage{
			"claude-opus-4-6": {
				ContextWindow: 200_000,
			},
		},
	}

	wire := ToWireEvent(event)
	r, ok := wire.(WireResultEvent)
	if !ok {
		t.Fatalf("expected WireResultEvent, got %T", wire)
	}

	if r.InputTokens != 0 {
		t.Errorf("InputTokens = %d, want 0 (no snapshot)", r.InputTokens)
	}
	if r.ContextWindow != 200_000 {
		t.Errorf("ContextWindow = %d, want 200000", r.ContextWindow)
	}
}

func TestToWireEvent_ResultDefaultContextWindow(t *testing.T) {
	event := &claudecli.ResultEvent{
		CostUSD:    0.01,
		Duration:   time.Second,
		StopReason: "end_turn",
	}

	wire := ToWireEvent(event)
	r, ok := wire.(WireResultEvent)
	if !ok {
		t.Fatalf("expected WireResultEvent, got %T", wire)
	}

	if r.ContextWindow != 200_000 {
		t.Errorf("ContextWindow = %d, want 200000 (default fallback)", r.ContextWindow)
	}
}

func TestToWireEvent_ParentToolUseID(t *testing.T) {
	tests := []struct {
		name  string
		event claudecli.Event
		check func(t *testing.T, wire any)
	}{
		{
			name:  "TextEvent with ParentToolUseID",
			event: &claudecli.TextEvent{Content: "hello", ParentToolUseID: "tu_parent"},
			check: func(t *testing.T, wire any) {
				e := wire.(WireTextEvent)
				if e.ParentToolUseID != "tu_parent" {
					t.Errorf("ParentToolUseID = %q, want tu_parent", e.ParentToolUseID)
				}
			},
		},
		{
			name:  "TextEvent without ParentToolUseID omits field",
			event: &claudecli.TextEvent{Content: "hello"},
			check: func(t *testing.T, wire any) {
				data, _ := json.Marshal(wire)
				if strings.Contains(string(data), "parentToolUseId") {
					t.Error("parentToolUseId should be omitted when empty")
				}
			},
		},
		{
			name:  "ThinkingEvent with ParentToolUseID",
			event: &claudecli.ThinkingEvent{Content: "think", ParentToolUseID: "tu_parent"},
			check: func(t *testing.T, wire any) {
				e := wire.(WireThinkingEvent)
				if e.ParentToolUseID != "tu_parent" {
					t.Errorf("ParentToolUseID = %q, want tu_parent", e.ParentToolUseID)
				}
			},
		},
		{
			name: "ToolUseEvent with ParentToolUseID",
			event: &claudecli.ToolUseEvent{
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
			event: &claudecli.ToolResultEvent{
				ToolUseID: "t1",
				Content:   []claudecli.ToolContent{{Type: "text", Text: "ok"}},
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
			wire := ToWireEvent(tt.event)
			if wire == nil {
				t.Fatal("ToWireEvent returned nil")
			}
			tt.check(t, wire)
		})
	}
}

func TestToWireEvent_TaskEvent(t *testing.T) {
	tests := []struct {
		name    string
		event   *claudecli.TaskEvent
		wantSub string
	}{
		{
			name: "task_started",
			event: &claudecli.TaskEvent{
				Subtype: "task_started", TaskID: "task-1", ToolUseID: "tu_agent",
				Description: "Explore codebase", TaskType: "local_agent",
			},
			wantSub: "task_started",
		},
		{
			name: "task_progress",
			event: &claudecli.TaskEvent{
				Subtype: "task_progress", TaskID: "task-1", ToolUseID: "tu_agent",
				LastToolName: "Read", TotalTokens: 5000, ToolUses: 3,
			},
			wantSub: "task_progress",
		},
		{
			name: "task_notification",
			event: &claudecli.TaskEvent{
				Subtype: "task_notification", TaskID: "task-1", ToolUseID: "tu_agent",
				Status: "completed", Summary: "Done exploring",
				TotalTokens: 10000, ToolUses: 8, DurationMs: 5000,
			},
			wantSub: "task_notification",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wire := ToWireEvent(tt.event)
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

func TestToWireEvent_UserEventWithAgentResult(t *testing.T) {
	event := &claudecli.UserEvent{
		ParentToolUseID: "tu_agent",
		AgentResult: &claudecli.AgentResult{
			Status:            "completed",
			AgentID:           "explorer",
			AgentType:         "Explore",
			Content:           []claudecli.ToolContent{{Type: "text", Text: "Found it"}},
			TotalDurationMs:   3000,
			TotalTokens:       8000,
			TotalToolUseCount: 5,
		},
	}
	wire := ToWireEvent(event)
	ar, ok := wire.(WireAgentResultEvent)
	if !ok {
		t.Fatalf("expected WireAgentResultEvent, got %T", wire)
	}
	if ar.Type != "agent_result" {
		t.Errorf("Type = %q, want agent_result", ar.Type)
	}
	if ar.ParentToolUseID != "tu_agent" {
		t.Errorf("ParentToolUseID = %q, want tu_agent", ar.ParentToolUseID)
	}
	if ar.Status != "completed" {
		t.Errorf("Status = %q, want completed", ar.Status)
	}
	if ar.TotalTokens != 8000 {
		t.Errorf("TotalTokens = %d, want 8000", ar.TotalTokens)
	}
	if len(ar.Content) != 1 || ar.Content[0].Text != "Found it" {
		t.Errorf("Content = %+v, want single text block", ar.Content)
	}
}

func TestToWireEvent_UserEventWithoutAgentResult(t *testing.T) {
	event := &claudecli.UserEvent{ParentToolUseID: "tu_agent"}
	wire := ToWireEvent(event)
	if wire != nil {
		t.Errorf("expected nil for UserEvent without AgentResult, got %T", wire)
	}
}

func TestToWireEvent_UnknownEvent(t *testing.T) {
	event := &claudecli.UnknownEvent{Type: "future_type", Raw: json.RawMessage(`{}`)}
	wire := ToWireEvent(event)
	if wire != nil {
		t.Errorf("expected nil for UnknownEvent, got %T", wire)
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
