package testmode

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/allbin/agentkit/runtime"
	claudecli "github.com/allbin/claudecli-go"
)

// --- Connector ---

func TestConnector_ConnectAndAssociate(t *testing.T) {
	c := NewConnector()
	sess, err := c.Connect(context.Background(), runtime.ConnectParams{})
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if sess == nil {
		t.Fatal("Connect returned nil session")
	}

	mock := c.Associate("sess-1")
	if mock == nil {
		t.Fatal("Associate returned nil")
	}

	got := c.Get("sess-1")
	if got != mock {
		t.Error("Get returned different session than Associate")
	}

	if c.Get("nonexistent") != nil {
		t.Error("Get for unknown ID should return nil")
	}
}

func TestConnector_AssociateEmpty(t *testing.T) {
	c := NewConnector()
	if c.Associate("sess-1") != nil {
		t.Error("Associate with no pending should return nil")
	}
}

func TestConnector_AssociateFIFO(t *testing.T) {
	c := NewConnector()
	s1, _ := c.Connect(context.Background(), runtime.ConnectParams{})
	s2, _ := c.Connect(context.Background(), runtime.ConnectParams{})

	got1 := c.Associate("a")
	got2 := c.Associate("b")

	if got1 != s1.(*Session) {
		t.Error("first Associate should return first Connect'd session")
	}
	if got2 != s2.(*Session) {
		t.Error("second Associate should return second Connect'd session")
	}
}

func TestConnector_SetBehaviorBeforeAssociate(t *testing.T) {
	c := NewConnector()
	scenarios := []Scenario{{Events: []ScriptedEvent{{Event: json.RawMessage(`{"type":"text","content":"hi"}`)}}}}
	c.SetBehavior("sess-1", scenarios)

	c.Connect(context.Background(), runtime.ConnectParams{})
	mock := c.Associate("sess-1")

	mock.mu.Lock()
	got := len(mock.scenarios)
	mock.mu.Unlock()

	if got != 1 {
		t.Errorf("expected 1 scenario, got %d", got)
	}
}

func TestConnector_SetBehaviorAfterAssociate(t *testing.T) {
	c := NewConnector()
	c.Connect(context.Background(), runtime.ConnectParams{})
	mock := c.Associate("sess-1")

	scenarios := []Scenario{{Events: []ScriptedEvent{{Event: json.RawMessage(`{"type":"text","content":"hi"}`)}}}}
	c.SetBehavior("sess-1", scenarios)

	mock.mu.Lock()
	got := len(mock.scenarios)
	mock.mu.Unlock()

	if got != 1 {
		t.Errorf("expected 1 scenario, got %d", got)
	}
}

func TestConnector_Reset(t *testing.T) {
	c := NewConnector()
	c.Connect(context.Background(), runtime.ConnectParams{})
	c.Connect(context.Background(), runtime.ConnectParams{})
	s1 := c.Associate("a")
	s2 := c.Associate("b")

	c.Reset()

	if c.Get("a") != nil || c.Get("b") != nil {
		t.Error("Get should return nil after Reset")
	}
	if c.Associate("c") != nil {
		t.Error("pending should be empty after Reset")
	}

	// Both sessions should be closed.
	s1.mu.Lock()
	closed1 := s1.closed
	s1.mu.Unlock()
	s2.mu.Lock()
	closed2 := s2.closed
	s2.mu.Unlock()

	if !closed1 || !closed2 {
		t.Error("sessions should be closed after Reset")
	}
}

// --- Session ---

func TestSession_InjectEvent(t *testing.T) {
	s := NewSession()
	defer s.Close()

	want := runtime.AssistantTextEvent{Content: "hello"}
	if err := s.InjectEvent(want); err != nil {
		t.Fatalf("InjectEvent: %v", err)
	}

	select {
	case got := <-s.Events():
		te, ok := got.(runtime.AssistantTextEvent)
		if !ok {
			t.Fatalf("expected AssistantTextEvent, got %T", got)
		}
		if te.Content != "hello" {
			t.Errorf("content = %q, want %q", te.Content, "hello")
		}
	case <-time.After(time.Second):
		t.Fatal("timeout reading event")
	}
}

func TestSession_InjectEventClosed(t *testing.T) {
	s := NewSession()
	s.Close()

	err := s.InjectEvent(runtime.AssistantTextEvent{Content: "hi"})
	if err == nil {
		t.Fatal("expected error injecting into closed session")
	}
	if !strings.Contains(err.Error(), "session is closed") {
		t.Errorf("error = %q, want containing 'session is closed'", err)
	}
}

func TestSession_CloseIdempotent(t *testing.T) {
	s := NewSession()
	if err := s.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

func TestSession_QueryRecordsPrompt(t *testing.T) {
	s := NewSession()
	defer s.Close()

	ctx := context.Background()
	if err := s.Query(ctx, "hello"); err != nil {
		t.Fatalf("Query: %v", err)
	}
	if err := s.Query(ctx, "world"); err != nil {
		t.Fatalf("Query: %v", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.queries) != 2 {
		t.Fatalf("expected 2 queries, got %d", len(s.queries))
	}
	if s.queries[0] != "hello" || s.queries[1] != "world" {
		t.Errorf("queries = %v", s.queries)
	}
}

func TestSession_QueryReplaysScenario(t *testing.T) {
	s := NewSession()
	defer s.Close()

	s.mu.Lock()
	s.scenarios = []Scenario{{
		Events: []ScriptedEvent{
			{Event: json.RawMessage(`{"type":"text","content":"one"}`)},
			{Event: json.RawMessage(`{"type":"result","stopReason":"end_turn","cost":0,"duration":0}`)},
		},
	}}
	s.mu.Unlock()

	if err := s.Query(context.Background(), "go"); err != nil {
		t.Fatalf("Query: %v", err)
	}

	// Read text event.
	select {
	case e := <-s.Events():
		if te, ok := e.(runtime.AssistantTextEvent); !ok || te.Content != "one" {
			t.Errorf("first event: got %T %v", e, e)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for text event")
	}

	// Read result event.
	select {
	case e := <-s.Events():
		if _, ok := e.(runtime.TurnCompletedEvent); !ok {
			t.Errorf("second event: got %T, want TurnCompletedEvent", e)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for result event")
	}
}

func TestSession_SetPlanMode(t *testing.T) {
	s := NewSession()
	defer s.Close()
	if err := s.SetPlanMode(context.Background(), runtime.PlanModePlan); err != nil {
		t.Fatalf("SetPlanMode: %v", err)
	}
}

func TestSession_SetModel(t *testing.T) {
	s := NewSession()
	defer s.Close()
	if err := s.SetModel(context.Background(), "claude-sonnet"); err != nil {
		t.Fatalf("SetModel: %v", err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.model != "claude-sonnet" {
		t.Errorf("model = %v, want claude-sonnet", s.model)
	}
}

func TestSession_Interrupt(t *testing.T) {
	s := NewSession()
	defer s.Close()
	if err := s.Interrupt(context.Background()); err != nil {
		t.Fatalf("Interrupt: %v", err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.interrupted {
		t.Error("interrupted should be true")
	}
}

// --- parseWireToRuntimeEvent ---

func TestParseWireToRuntimeEvent(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		check   func(t *testing.T, e runtime.CLIEvent)
		wantErr string
	}{
		{
			name:  "text",
			input: `{"type":"text","content":"hi"}`,
			check: func(t *testing.T, e runtime.CLIEvent) {
				te, ok := e.(runtime.AssistantTextEvent)
				if !ok {
					t.Fatalf("type = %T, want AssistantTextEvent", e)
				}
				if te.Content != "hi" {
					t.Errorf("Content = %q, want %q", te.Content, "hi")
				}
			},
		},
		{
			name:  "thinking",
			input: `{"type":"thinking","content":"hmm"}`,
			check: func(t *testing.T, e runtime.CLIEvent) {
				te, ok := e.(runtime.ThinkingEvent)
				if !ok {
					t.Fatalf("type = %T, want ThinkingEvent", e)
				}
				if te.Content != "hmm" {
					t.Errorf("Content = %q, want %q", te.Content, "hmm")
				}
			},
		},
		{
			name:  "tool_use",
			input: `{"type":"tool_use","toolId":"t1","toolName":"bash","toolInput":{"cmd":"ls"}}`,
			check: func(t *testing.T, e runtime.CLIEvent) {
				te, ok := e.(runtime.ToolUseEvent)
				if !ok {
					t.Fatalf("type = %T, want ToolUseEvent", e)
				}
				if te.ID != "t1" {
					t.Errorf("ID = %q, want %q", te.ID, "t1")
				}
				if te.Name != "bash" {
					t.Errorf("Name = %q, want %q", te.Name, "bash")
				}
				if string(te.Input) != `{"cmd":"ls"}` {
					t.Errorf("Input = %s", te.Input)
				}
			},
		},
		{
			name:  "tool_result",
			input: `{"type":"tool_result","toolId":"t1","content":[{"type":"text","text":"ok"}]}`,
			check: func(t *testing.T, e runtime.CLIEvent) {
				te, ok := e.(runtime.ToolResultEvent)
				if !ok {
					t.Fatalf("type = %T, want ToolResultEvent", e)
				}
				if te.ToolUseID != "t1" {
					t.Errorf("ToolUseID = %q, want %q", te.ToolUseID, "t1")
				}
				if len(te.Content) != 1 {
					t.Fatalf("Content len = %d, want 1", len(te.Content))
				}
				if te.Content[0].Type != "text" || te.Content[0].Text != "ok" {
					t.Errorf("Content[0] = %+v", te.Content[0])
				}
			},
		},
		{
			name:  "result",
			input: `{"type":"result","stopReason":"end_turn","cost":0.01,"duration":500}`,
			check: func(t *testing.T, e runtime.CLIEvent) {
				te, ok := e.(runtime.TurnCompletedEvent)
				if !ok {
					t.Fatalf("type = %T, want TurnCompletedEvent", e)
				}
				if te.StopReason != "end_turn" {
					t.Errorf("StopReason = %q, want %q", te.StopReason, "end_turn")
				}
				if te.CostUSD != 0.01 {
					t.Errorf("CostUSD = %f, want 0.01", te.CostUSD)
				}
				if te.Duration != 500*time.Millisecond {
					t.Errorf("Duration = %v, want 500ms", te.Duration)
				}
			},
		},
		{
			name:  "error",
			input: `{"type":"error","message":"boom","fatal":true}`,
			check: func(t *testing.T, e runtime.CLIEvent) {
				te, ok := e.(runtime.ErrorEvent)
				if !ok {
					t.Fatalf("type = %T, want ErrorEvent", e)
				}
				if te.Err.Error() != "boom" {
					t.Errorf("Err = %q, want %q", te.Err, "boom")
				}
				if !te.Fatal {
					t.Error("Fatal should be true")
				}
			},
		},
		{
			name:    "unknown type",
			input:   `{"type":"banana"}`,
			wantErr: "unknown event type",
		},
		{
			name:    "invalid JSON",
			input:   `not json`,
			wantErr: "invalid character",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e, err := parseWireToRuntimeEvent(json.RawMessage(tt.input))
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("error = %q, want containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			tt.check(t, e)
		})
	}
}

// --- BlockingRunner ---

func TestBlockingRunner_Default(t *testing.T) {
	r := NewBlockingRunner()
	res, err := r.RunBlocking(context.Background(), "test")
	if err != nil {
		t.Fatalf("RunBlocking: %v", err)
	}
	if !strings.Contains(res.Text, "test commit") {
		t.Errorf("default result text = %q, want containing 'test commit'", res.Text)
	}
}

func TestBlockingRunner_SetResult(t *testing.T) {
	r := NewBlockingRunner()
	r.SetResult(&claudecli.BlockingResult{Text: "custom"}, nil)

	res, err := r.RunBlocking(context.Background(), "test")
	if err != nil {
		t.Fatalf("RunBlocking: %v", err)
	}
	if res.Text != "custom" {
		t.Errorf("text = %q, want %q", res.Text, "custom")
	}
}

func TestBlockingRunner_SetResultError(t *testing.T) {
	r := NewBlockingRunner()
	r.SetResult(nil, fmt.Errorf("fail"))

	res, err := r.RunBlocking(context.Background(), "test")
	if err == nil {
		t.Fatal("expected error")
	}
	if res != nil {
		t.Errorf("expected nil result, got %v", res)
	}
	if err.Error() != "fail" {
		t.Errorf("error = %q, want %q", err, "fail")
	}
}
