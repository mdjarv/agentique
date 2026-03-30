package session

import (
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	claudecli "github.com/allbin/claudecli-go"
)

// testSink collects persist and broadcast calls for assertions.
type testSink struct {
	mu         sync.Mutex
	persisted  []persistedEvent
	broadcasts []broadcastEvent
}

type persistedEvent struct {
	TurnIndex int
	Seq       int
	WireType  string
	Data      []byte
}

type broadcastEvent struct {
	PushType string
	Payload  any
}

func newTestSink() *testSink {
	return &testSink{}
}

func (ts *testSink) eventSink() EventSink {
	return EventSink{
		Persist: func(turnIndex, seq int, wireType string, data []byte) {
			ts.mu.Lock()
			defer ts.mu.Unlock()
			ts.persisted = append(ts.persisted, persistedEvent{
				TurnIndex: turnIndex,
				Seq:       seq,
				WireType:  wireType,
				Data:      data,
			})
		},
		Broadcast: func(pushType string, payload any) {
			ts.mu.Lock()
			defer ts.mu.Unlock()
			ts.broadcasts = append(ts.broadcasts, broadcastEvent{
				PushType: pushType,
				Payload:  payload,
			})
		},
	}
}

func newTestPipeline(sink *testSink, opts ...func(*PipelineConfig)) *EventPipeline {
	cfg := PipelineConfig{
		SessionID:        "test-session",
		Sink:             sink.eventSink(),
		InitialTurnIndex: 0,
	}
	for _, opt := range opts {
		opt(&cfg)
	}
	return NewEventPipeline(cfg)
}

func TestPipeline_TransientEventsSkipDB(t *testing.T) {
	sink := newTestSink()
	p := newTestPipeline(sink)

	p.ProcessEvent(&claudecli.RateLimitEvent{Status: "normal", Utilization: 0.5})
	p.ProcessEvent(&claudecli.StreamEvent{Event: json.RawMessage(`{}`)})

	if len(sink.persisted) != 0 {
		t.Errorf("expected 0 persisted events, got %d", len(sink.persisted))
	}
	if len(sink.broadcasts) != 2 {
		t.Errorf("expected 2 broadcasts, got %d", len(sink.broadcasts))
	}
}

func TestPipeline_PersistentEventsGetBoth(t *testing.T) {
	sink := newTestSink()
	p := newTestPipeline(sink)
	// Advance turn so seq numbering works as expected.
	p.AdvanceTurn()

	p.ProcessEvent(&claudecli.TextEvent{Content: "hello"})

	if len(sink.persisted) != 1 {
		t.Fatalf("expected 1 persisted event, got %d", len(sink.persisted))
	}
	if sink.persisted[0].WireType != "text" {
		t.Errorf("expected wire type 'text', got %q", sink.persisted[0].WireType)
	}
	if sink.persisted[0].TurnIndex != 1 {
		t.Errorf("expected turn index 1, got %d", sink.persisted[0].TurnIndex)
	}
	if sink.persisted[0].Seq != 0 {
		t.Errorf("expected seq 0, got %d", sink.persisted[0].Seq)
	}
	// One broadcast for session.event.
	if len(sink.broadcasts) != 1 {
		t.Errorf("expected 1 broadcast, got %d", len(sink.broadcasts))
	}
}

func TestPipeline_SequenceNumbering(t *testing.T) {
	sink := newTestSink()
	p := newTestPipeline(sink)
	p.AdvanceTurn()

	p.ProcessEvent(&claudecli.TextEvent{Content: "a"})
	p.ProcessEvent(&claudecli.ThinkingEvent{Content: "b"})
	p.ProcessEvent(&claudecli.TextEvent{Content: "c"})

	if len(sink.persisted) != 3 {
		t.Fatalf("expected 3 persisted events, got %d", len(sink.persisted))
	}
	for i, pe := range sink.persisted {
		if pe.Seq != i {
			t.Errorf("event %d: expected seq %d, got %d", i, i, pe.Seq)
		}
		if pe.TurnIndex != 1 {
			t.Errorf("event %d: expected turn 1, got %d", i, pe.TurnIndex)
		}
	}
}

func TestPipeline_AdvanceTurnResetsSeq(t *testing.T) {
	sink := newTestSink()
	p := newTestPipeline(sink)

	p.AdvanceTurn() // turn 1
	p.ProcessEvent(&claudecli.TextEvent{Content: "a"})
	p.ProcessEvent(&claudecli.TextEvent{Content: "b"})

	p.AdvanceTurn() // turn 2
	p.ProcessEvent(&claudecli.TextEvent{Content: "c"})

	if len(sink.persisted) != 3 {
		t.Fatalf("expected 3, got %d", len(sink.persisted))
	}
	if sink.persisted[0].TurnIndex != 1 || sink.persisted[0].Seq != 0 {
		t.Errorf("event 0: want turn=1 seq=0, got turn=%d seq=%d", sink.persisted[0].TurnIndex, sink.persisted[0].Seq)
	}
	if sink.persisted[1].TurnIndex != 1 || sink.persisted[1].Seq != 1 {
		t.Errorf("event 1: want turn=1 seq=1, got turn=%d seq=%d", sink.persisted[1].TurnIndex, sink.persisted[1].Seq)
	}
	if sink.persisted[2].TurnIndex != 2 || sink.persisted[2].Seq != 0 {
		t.Errorf("event 2: want turn=2 seq=0, got turn=%d seq=%d", sink.persisted[2].TurnIndex, sink.persisted[2].Seq)
	}
}

func TestPipeline_AllocSeq(t *testing.T) {
	sink := newTestSink()
	p := newTestPipeline(sink)
	p.AdvanceTurn()
	p.SetSeq(5) // simulate 5 events already processed

	turn, seq := p.AllocSeq()
	if turn != 1 || seq != 5 {
		t.Errorf("want turn=1 seq=5, got turn=%d seq=%d", turn, seq)
	}
	turn2, seq2 := p.AllocSeq()
	if turn2 != 1 || seq2 != 6 {
		t.Errorf("want turn=1 seq=6, got turn=%d seq=%d", turn2, seq2)
	}
}

func TestPipeline_InitCapture(t *testing.T) {
	sink := newTestSink()
	var capturedID string
	p := newTestPipeline(sink, func(cfg *PipelineConfig) {
		cfg.OnClaudeSessionID = func(id string) { capturedID = id }
	})

	p.ProcessEvent(&claudecli.InitEvent{SessionID: "claude-123"})

	if p.ClaudeSessionID() != "claude-123" {
		t.Errorf("expected claude-123, got %q", p.ClaudeSessionID())
	}
	if capturedID != "claude-123" {
		t.Errorf("callback expected claude-123, got %q", capturedID)
	}
	// Init events should not be persisted or broadcast.
	if len(sink.persisted) != 0 || len(sink.broadcasts) != 0 {
		t.Error("init event should not persist or broadcast")
	}
}

func TestPipeline_InitCapture_OnlyFirst(t *testing.T) {
	sink := newTestSink()
	callCount := 0
	p := newTestPipeline(sink, func(cfg *PipelineConfig) {
		cfg.OnClaudeSessionID = func(_ string) { callCount++ }
	})

	p.ProcessEvent(&claudecli.InitEvent{SessionID: "first"})
	p.ProcessEvent(&claudecli.InitEvent{SessionID: "second"})

	if p.ClaudeSessionID() != "first" {
		t.Errorf("expected first, got %q", p.ClaudeSessionID())
	}
	if callCount != 1 {
		t.Errorf("expected 1 callback call, got %d", callCount)
	}
}

func TestPipeline_ResultTriggersTurnComplete(t *testing.T) {
	sink := newTestSink()
	turnCompleted := false
	p := newTestPipeline(sink, func(cfg *PipelineConfig) {
		cfg.OnTurnComplete = func() { turnCompleted = true }
	})
	p.AdvanceTurn()

	p.ProcessEvent(testResultEvent(0.01))

	if !turnCompleted {
		t.Error("expected OnTurnComplete callback")
	}
}

func TestPipeline_FatalErrorTriggersCallback(t *testing.T) {
	sink := newTestSink()
	var fatalErr error
	p := newTestPipeline(sink, func(cfg *PipelineConfig) {
		cfg.OnFatalError = func(err error) { fatalErr = err }
	})
	p.AdvanceTurn()

	p.ProcessEvent(&claudecli.ErrorEvent{
		Err:   errors.New("boom"),
		Fatal: true,
	})

	if fatalErr == nil || fatalErr.Error() != "boom" {
		t.Errorf("expected fatal error 'boom', got %v", fatalErr)
	}
}

func TestPipeline_NonFatalErrorNoCallback(t *testing.T) {
	sink := newTestSink()
	fatalCalled := false
	p := newTestPipeline(sink, func(cfg *PipelineConfig) {
		cfg.OnFatalError = func(_ error) { fatalCalled = true }
	})
	p.AdvanceTurn()

	p.ProcessEvent(&claudecli.ErrorEvent{
		Err:   errors.New("warn"),
		Fatal: false,
	})

	if fatalCalled {
		t.Error("OnFatalError should not fire for non-fatal errors")
	}
	// Non-fatal errors are still persisted and broadcast.
	if len(sink.persisted) != 1 {
		t.Errorf("expected 1 persisted, got %d", len(sink.persisted))
	}
}

func TestPipeline_ToolCategoryTracking_GitRefresh(t *testing.T) {
	sink := newTestSink()
	gitRefreshCalled := false
	p := newTestPipeline(sink, func(cfg *PipelineConfig) {
		cfg.OnWriteToolResult = func() { gitRefreshCalled = true }
	})
	p.AdvanceTurn()

	// Write tool use + result.
	p.ProcessEvent(&claudecli.ToolUseEvent{ID: "t1", Name: "Write", Input: json.RawMessage(`{}`)})
	p.ProcessEvent(&claudecli.ToolResultEvent{
		ToolUseID: "t1",
		Content:   []claudecli.ToolContent{{Type: "text", Text: "ok"}},
	})

	if !gitRefreshCalled {
		t.Error("expected OnWriteToolResult after file_write tool result")
	}
}

func TestPipeline_ToolCategoryTracking_ReadNoRefresh(t *testing.T) {
	sink := newTestSink()
	gitRefreshCalled := false
	p := newTestPipeline(sink, func(cfg *PipelineConfig) {
		cfg.OnWriteToolResult = func() { gitRefreshCalled = true }
	})
	p.AdvanceTurn()

	// Read tool use + result — should NOT trigger git refresh.
	p.ProcessEvent(&claudecli.ToolUseEvent{ID: "t1", Name: "Read", Input: json.RawMessage(`{}`)})
	p.ProcessEvent(&claudecli.ToolResultEvent{
		ToolUseID: "t1",
		Content:   []claudecli.ToolContent{{Type: "text", Text: "content"}},
	})

	if gitRefreshCalled {
		t.Error("OnWriteToolResult should not fire for read tools")
	}
}

func TestPipeline_ToolCategoryTracking_BashRefresh(t *testing.T) {
	sink := newTestSink()
	gitRefreshCalled := false
	p := newTestPipeline(sink, func(cfg *PipelineConfig) {
		cfg.OnWriteToolResult = func() { gitRefreshCalled = true }
	})
	p.AdvanceTurn()

	p.ProcessEvent(&claudecli.ToolUseEvent{ID: "t1", Name: "Bash", Input: json.RawMessage(`{}`)})
	p.ProcessEvent(&claudecli.ToolResultEvent{
		ToolUseID: "t1",
		Content:   []claudecli.ToolContent{{Type: "text", Text: "output"}},
	})

	if !gitRefreshCalled {
		t.Error("expected OnWriteToolResult after command tool result")
	}
}

func TestPipeline_PlanModeTransitions(t *testing.T) {
	sink := newTestSink()
	var transitions []string
	var exitInput json.RawMessage
	p := newTestPipeline(sink, func(cfg *PipelineConfig) {
		cfg.OnPlanTransition = func(mode string) { transitions = append(transitions, mode) }
		cfg.OnExitPlanMode = func(input json.RawMessage) { exitInput = input }
	})
	p.AdvanceTurn()

	p.ProcessEvent(&claudecli.ToolUseEvent{ID: "t1", Name: "EnterPlanMode", Input: json.RawMessage(`{}`)})
	p.ProcessEvent(&claudecli.ToolUseEvent{ID: "t2", Name: "ExitPlanMode", Input: json.RawMessage(`{"plan":"test"}`)})

	if len(transitions) != 1 {
		t.Fatalf("expected 1 transition (EnterPlanMode only), got %d: %v", len(transitions), transitions)
	}
	if transitions[0] != "plan" {
		t.Errorf("expected 'plan', got %q", transitions[0])
	}
	if exitInput == nil {
		t.Fatal("expected OnExitPlanMode to be called")
	}
	if string(exitInput) != `{"plan":"test"}` {
		t.Errorf("expected exit input %q, got %q", `{"plan":"test"}`, string(exitInput))
	}
}

func TestPipeline_ExitPlanModeFallback(t *testing.T) {
	sink := newTestSink()
	var transitions []string
	p := newTestPipeline(sink, func(cfg *PipelineConfig) {
		cfg.OnPlanTransition = func(mode string) { transitions = append(transitions, mode) }
		// No OnExitPlanMode — should fall back to OnPlanTransition("default").
	})
	p.AdvanceTurn()

	p.ProcessEvent(&claudecli.ToolUseEvent{ID: "t1", Name: "ExitPlanMode", Input: json.RawMessage(`{}`)})

	if len(transitions) != 1 || transitions[0] != "default" {
		t.Errorf("expected fallback to OnPlanTransition('default'), got %v", transitions)
	}
}

func TestPipeline_ResultClearsToolCategories(t *testing.T) {
	sink := newTestSink()
	gitRefreshCalled := false
	turnCompleted := false
	p := newTestPipeline(sink, func(cfg *PipelineConfig) {
		cfg.OnWriteToolResult = func() { gitRefreshCalled = true }
		cfg.OnTurnComplete = func() { turnCompleted = true }
	})
	p.AdvanceTurn()

	// Register a write tool, then receive ResultEvent before the tool result.
	p.ProcessEvent(&claudecli.ToolUseEvent{ID: "t1", Name: "Write", Input: json.RawMessage(`{}`)})
	p.ProcessEvent(testResultEvent(0.01))

	if !turnCompleted {
		t.Error("expected OnTurnComplete")
	}

	// Now process a ToolResultEvent — the category should have been cleared.
	gitRefreshCalled = false
	p.AdvanceTurn()
	p.ProcessEvent(&claudecli.ToolResultEvent{
		ToolUseID: "t1",
		Content:   []claudecli.ToolContent{{Type: "text", Text: "ok"}},
	})

	if gitRefreshCalled {
		t.Error("OnWriteToolResult should not fire — categories cleared by ResultEvent")
	}
}

func TestTruncateToolResult_Small(t *testing.T) {
	tr := WireToolResultEvent{
		Type:   "tool_result",
		ToolID: "t1",
		Content: []WireContentBlock{
			{Type: "text", Text: "short"},
		},
	}
	result := truncateToolResult(tr)
	if result.Content[0].Text != "short" {
		t.Error("small content should not be truncated")
	}
}

func TestTruncateToolResult_Large(t *testing.T) {
	largeText := strings.Repeat("x", maxToolResultDBSize+1000)
	tr := WireToolResultEvent{
		Type:   "tool_result",
		ToolID: "t1",
		Content: []WireContentBlock{
			{Type: "text", Text: largeText},
		},
	}
	result := truncateToolResult(tr)
	if len(result.Content[0].Text) >= len(largeText) {
		t.Error("large content should be truncated")
	}
	if !strings.Contains(result.Content[0].Text, "...[truncated]...") {
		t.Error("truncated text should contain marker")
	}
}

func TestTruncateToolResult_PreservesImages(t *testing.T) {
	tr := WireToolResultEvent{
		Type:   "tool_result",
		ToolID: "t1",
		Content: []WireContentBlock{
			{Type: "text", Text: strings.Repeat("x", maxToolResultDBSize+1000)},
			{Type: "image", URL: "data:image/png;base64,abc"},
		},
	}
	result := truncateToolResult(tr)
	if len(result.Content) != 2 {
		t.Fatalf("expected 2 blocks, got %d", len(result.Content))
	}
	if result.Content[1].Type != "image" {
		t.Error("image block should be preserved")
	}
}

func TestIsTransient(t *testing.T) {
	tests := []struct {
		name      string
		event     any
		transient bool
	}{
		{"rate_limit", WireRateLimitEvent{Type: "rate_limit"}, true},
		{"stream", WireStreamEvent{Type: "stream"}, true},
		{"compact_status", WireCompactStatusEvent{Type: "compact_status"}, true},
		{"context_management", WireContextManagementEvent{Type: "context_management"}, true},
		{"text", WireTextEvent{Type: "text"}, false},
		{"tool_use", WireToolUseEvent{Type: "tool_use"}, false},
		{"result", WireResultEvent{Type: "result"}, false},
		{"error", WireErrorEvent{Type: "error"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isTransient(tt.event); got != tt.transient {
				t.Errorf("isTransient(%s) = %v, want %v", tt.name, got, tt.transient)
			}
		})
	}
}

func TestPipeline_TaskProgressIsTransient(t *testing.T) {
	sink := newTestSink()
	p := newTestPipeline(sink)
	p.AdvanceTurn()

	p.ProcessEvent(&claudecli.TaskEvent{
		Subtype: "task_progress", TaskID: "task-1", ToolUseID: "tu_agent",
		LastToolName: "Read", TotalTokens: 5000,
	})

	if len(sink.persisted) != 0 {
		t.Errorf("task_progress should not be persisted, got %d", len(sink.persisted))
	}
	if len(sink.broadcasts) != 1 {
		t.Errorf("task_progress should be broadcast, got %d", len(sink.broadcasts))
	}
}

func TestPipeline_TaskStartedAndNotificationPersist(t *testing.T) {
	sink := newTestSink()
	p := newTestPipeline(sink)
	p.AdvanceTurn()

	p.ProcessEvent(&claudecli.TaskEvent{
		Subtype: "task_started", TaskID: "task-1", ToolUseID: "tu_agent",
		Description: "Explore", TaskType: "local_agent",
	})
	p.ProcessEvent(&claudecli.TaskEvent{
		Subtype: "task_notification", TaskID: "task-1", ToolUseID: "tu_agent",
		Status: "completed", Summary: "Done",
	})

	if len(sink.persisted) != 2 {
		t.Fatalf("expected 2 persisted events, got %d", len(sink.persisted))
	}
	if sink.persisted[0].WireType != "task" || sink.persisted[1].WireType != "task" {
		t.Errorf("expected wire type 'task', got %q and %q", sink.persisted[0].WireType, sink.persisted[1].WireType)
	}
	if len(sink.broadcasts) != 2 {
		t.Errorf("expected 2 broadcasts, got %d", len(sink.broadcasts))
	}
}

func TestPipeline_UnknownEventDropped(t *testing.T) {
	sink := newTestSink()
	p := newTestPipeline(sink)
	p.AdvanceTurn()

	p.ProcessEvent(&claudecli.UnknownEvent{Type: "future_type", Raw: json.RawMessage(`{}`)})

	if len(sink.persisted) != 0 {
		t.Errorf("UnknownEvent should not be persisted, got %d", len(sink.persisted))
	}
	if len(sink.broadcasts) != 0 {
		t.Errorf("UnknownEvent should not be broadcast, got %d", len(sink.broadcasts))
	}
}

func TestPipeline_SubagentPlanModeIsolation(t *testing.T) {
	sink := newTestSink()
	planTransitioned := false
	p := newTestPipeline(sink, func(cfg *PipelineConfig) {
		cfg.OnPlanTransition = func(_ string) { planTransitioned = true }
	})
	p.AdvanceTurn()

	// Subagent EnterPlanMode should NOT trigger parent plan transition.
	p.ProcessEvent(&claudecli.ToolUseEvent{
		ID: "t1", Name: "EnterPlanMode", Input: json.RawMessage(`{}`),
		ParentToolUseID: "tu_agent",
	})

	if planTransitioned {
		t.Error("subagent EnterPlanMode should not trigger parent plan transition")
	}
}

func TestPipeline_SubagentWriteToolTriggersGitRefresh(t *testing.T) {
	sink := newTestSink()
	gitRefreshCalled := false
	p := newTestPipeline(sink, func(cfg *PipelineConfig) {
		cfg.OnWriteToolResult = func() { gitRefreshCalled = true }
	})
	p.AdvanceTurn()

	p.ProcessEvent(&claudecli.ToolUseEvent{
		ID: "t1", Name: "Write", Input: json.RawMessage(`{}`),
		ParentToolUseID: "tu_agent",
	})
	p.ProcessEvent(&claudecli.ToolResultEvent{
		ToolUseID: "t1",
		Content:   []claudecli.ToolContent{{Type: "text", Text: "ok"}},
		ParentToolUseID: "tu_agent",
	})

	if !gitRefreshCalled {
		t.Error("subagent Write tool should trigger git refresh")
	}
}

func TestPipeline_AgentResultPersisted(t *testing.T) {
	sink := newTestSink()
	p := newTestPipeline(sink)
	p.AdvanceTurn()

	p.ProcessEvent(&claudecli.UserEvent{
		ParentToolUseID: "tu_agent",
		AgentResult: &claudecli.AgentResult{
			Status:    "completed",
			AgentID:   "explorer",
			AgentType: "Explore",
			Content:   []claudecli.ToolContent{{Type: "text", Text: "result"}},
		},
	})

	if len(sink.persisted) != 1 {
		t.Fatalf("expected 1 persisted event, got %d", len(sink.persisted))
	}
	if sink.persisted[0].WireType != "agent_result" {
		t.Errorf("expected wire type 'agent_result', got %q", sink.persisted[0].WireType)
	}
}

func TestPipeline_UserEventToolResultPersisted(t *testing.T) {
	sink := newTestSink()
	p := newTestPipeline(sink)
	p.AdvanceTurn()

	// Register a tool_use so trackToolResult can look up the category.
	p.ProcessEvent(&claudecli.ToolUseEvent{
		ID: "tu_bash", Name: "Bash", Input: json.RawMessage(`{"command":"ls"}`),
	})

	// UserEvent with a tool_result content block.
	p.ProcessEvent(&claudecli.UserEvent{
		Content: []claudecli.UserContent{
			{Type: "tool_result", ToolUseID: "tu_bash", Content: []claudecli.ToolContent{{Type: "text", Text: "file1\nfile2"}}},
		},
	})

	// Expect tool_use + tool_result persisted.
	if len(sink.persisted) != 2 {
		t.Fatalf("expected 2 persisted events, got %d", len(sink.persisted))
	}
	if sink.persisted[1].WireType != "tool_result" {
		t.Errorf("expected wire type 'tool_result', got %q", sink.persisted[1].WireType)
	}
}

func TestPipeline_UserEventToolResultTriggersGitRefresh(t *testing.T) {
	sink := newTestSink()
	gitRefreshCalled := false
	p := newTestPipeline(sink, func(cfg *PipelineConfig) {
		cfg.OnWriteToolResult = func() { gitRefreshCalled = true }
	})
	p.AdvanceTurn()

	// Register Write tool_use.
	p.ProcessEvent(&claudecli.ToolUseEvent{
		ID: "tu_write", Name: "Write", Input: json.RawMessage(`{}`),
	})

	// Tool result via UserEvent.
	p.ProcessEvent(&claudecli.UserEvent{
		Content: []claudecli.UserContent{
			{Type: "tool_result", ToolUseID: "tu_write", Content: []claudecli.ToolContent{{Type: "text", Text: "ok"}}},
		},
	})

	if !gitRefreshCalled {
		t.Error("Write tool result via UserEvent should trigger git refresh")
	}
}

func TestPipeline_UserEventWithBothToolResultAndAgentResult(t *testing.T) {
	sink := newTestSink()
	p := newTestPipeline(sink)
	p.AdvanceTurn()

	// Register Agent tool_use.
	p.ProcessEvent(&claudecli.ToolUseEvent{
		ID: "tu_agent", Name: "Agent", Input: json.RawMessage(`{}`),
	})

	// UserEvent with tool_result AND agent_result (normal for Agent completion).
	p.ProcessEvent(&claudecli.UserEvent{
		ParentToolUseID: "",
		Content: []claudecli.UserContent{
			{Type: "tool_result", ToolUseID: "tu_agent", Content: []claudecli.ToolContent{{Type: "text", Text: "agent output"}}},
		},
		AgentResult: &claudecli.AgentResult{
			Status:  "completed",
			AgentID: "explorer",
			Content: []claudecli.ToolContent{{Type: "text", Text: "agent output"}},
		},
	})

	// Expect tool_use + tool_result + agent_result = 3 persisted events.
	if len(sink.persisted) != 3 {
		t.Fatalf("expected 3 persisted events, got %d", len(sink.persisted))
	}
	if sink.persisted[1].WireType != "tool_result" {
		t.Errorf("persisted[1] type = %q, want tool_result", sink.persisted[1].WireType)
	}
	if sink.persisted[2].WireType != "agent_result" {
		t.Errorf("persisted[2] type = %q, want agent_result", sink.persisted[2].WireType)
	}
}

func TestPipeline_SetClaudeSessionID(t *testing.T) {
	sink := newTestSink()
	p := newTestPipeline(sink)

	p.SetClaudeSessionID("restored-id")
	if p.ClaudeSessionID() != "restored-id" {
		t.Errorf("expected restored-id, got %q", p.ClaudeSessionID())
	}

	// Init event should NOT overwrite since ID is already set.
	p.ProcessEvent(&claudecli.InitEvent{SessionID: "new-id"})
	if p.ClaudeSessionID() != "restored-id" {
		t.Errorf("expected restored-id to be preserved, got %q", p.ClaudeSessionID())
	}
}
