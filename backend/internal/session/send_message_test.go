package session

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestParseSendMessageInput(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantTo   string
		wantBody string
		wantErr  bool
	}{
		{
			name:     "content field with string (preamble format)",
			input:    `{"to":"@spawn","content":"{\"teamName\":\"workers\",\"workers\":[]}"}`,
			wantTo:   "@spawn",
			wantBody: `{"teamName":"workers","workers":[]}`,
		},
		{
			name:     "message field with string",
			input:    `{"to":"peer","message":"hello"}`,
			wantTo:   "peer",
			wantBody: "hello",
		},
		{
			name:     "message field with JSON object (production format)",
			input:    `{"to":"@spawn","summary":"Spawn workers","message":{"teamName":"Architecture Deepening","workers":[{"name":"W1","prompt":"do stuff"}]}}`,
			wantTo:   "@spawn",
			wantBody: `{"teamName":"Architecture Deepening","workers":[{"name":"W1","prompt":"do stuff"}]}`,
		},
		{
			name:     "content takes precedence over message",
			input:    `{"to":"peer","content":"from-content","message":"from-message"}`,
			wantTo:   "peer",
			wantBody: "from-content",
		},
		{
			name:     "message field with stringified JSON",
			input:    `{"to":"@spawn","message":"{\"teamName\":\"t\",\"workers\":[]}"}`,
			wantTo:   "@spawn",
			wantBody: `{"teamName":"t","workers":[]}`,
		},
		{
			name:     "both empty",
			input:    `{"to":"peer"}`,
			wantTo:   "peer",
			wantBody: "",
		},
		{
			name:     "extra fields ignored",
			input:    `{"to":"peer","message":"hi","summary":"greet","recipient":"peer"}`,
			wantTo:   "peer",
			wantBody: "hi",
		},
		{
			name:    "invalid JSON",
			input:   `not json`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			to, body, err := parseSendMessageInput(json.RawMessage(tt.input))
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if to != tt.wantTo {
				t.Errorf("to = %q, want %q", to, tt.wantTo)
			}
			if body != tt.wantBody {
				t.Errorf("body = %q, want %q", body, tt.wantBody)
			}
		})
	}
}

// TestParseSendMessageInput_SpawnPayloadRoundtrip verifies that the body
// produced by parseSendMessageInput can be unmarshalled as a SpawnWorkersRequest
// for all common input shapes.
func TestParseSendMessageInput_SpawnPayloadRoundtrip(t *testing.T) {
	spawnPayload := `{"teamName":"team","workers":[{"name":"W1","role":"expert","prompt":"do X"}]}`

	inputs := []struct {
		name  string
		input string
	}{
		{
			"content with stringified JSON",
			`{"to":"@spawn","content":` + mustJSON(spawnPayload) + `}`,
		},
		{
			"message with stringified JSON",
			`{"to":"@spawn","message":` + mustJSON(spawnPayload) + `}`,
		},
		{
			"message with raw JSON object",
			`{"to":"@spawn","message":` + spawnPayload + `}`,
		},
	}

	for _, tt := range inputs {
		t.Run(tt.name, func(t *testing.T) {
			_, body, err := parseSendMessageInput(json.RawMessage(tt.input))
			if err != nil {
				t.Fatalf("parse error: %v", err)
			}

			var req SpawnWorkersRequest
			if err := json.Unmarshal([]byte(body), &req); err != nil {
				t.Fatalf("body is not valid SpawnWorkersRequest: %v\nbody: %s", err, body)
			}
			if req.TeamName != "team" {
				t.Errorf("teamName = %q, want %q", req.TeamName, "team")
			}
			if len(req.Workers) != 1 {
				t.Fatalf("workers count = %d, want 1", len(req.Workers))
			}
			if req.Workers[0].Name != "W1" {
				t.Errorf("worker name = %q, want %q", req.Workers[0].Name, "W1")
			}
			if req.Workers[0].Role != "expert" {
				t.Errorf("worker role = %q, want %q", req.Workers[0].Role, "expert")
			}
		})
	}
}

// TestInterceptSendMessage_DeniesWithSuccess verifies that interceptSendMessage
// denies the tool with a "delivered" message (actual routing happens in the
// EventPipeline's OnSendMessage callback, not here).
func TestInterceptSendMessage_DeniesWithSuccess(t *testing.T) {
	sess := &Session{
		ID:               "lead-1",
		pendingApprovals: make(map[string]*pendingApproval),
	}

	input := json.RawMessage(`{"to":"Backend Worker","message":"looks good, proceed"}`)
	resp, err := sess.interceptSendMessage(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Allow {
		t.Error("should deny (routing happens in pipeline)")
	}
	if !strings.Contains(resp.DenyMessage, "delivered") {
		t.Errorf("deny message should indicate success, got: %s", resp.DenyMessage)
	}
}

// TestInterceptSendMessage_NoTeam verifies the deny message when no team
// callback is set. Even without a team, interceptSendMessage still denies
// with a success message — the pipeline handles the "no team" case.
func TestInterceptSendMessage_NoTeam(t *testing.T) {
	sess := &Session{
		ID:               "solo-1",
		pendingApprovals: make(map[string]*pendingApproval),
	}

	input := json.RawMessage(`{"to":"peer","message":"hello"}`)
	resp, err := sess.interceptSendMessage(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Allow {
		t.Error("should deny")
	}
	if !strings.Contains(resp.DenyMessage, "delivered") {
		t.Errorf("deny message should indicate success, got: %s", resp.DenyMessage)
	}
}

// TestPipelineSendMessageRouting verifies that the EventPipeline's OnSendMessage
// callback fires when a SendMessage ToolUseEvent is processed.
func TestPipelineSendMessageRouting(t *testing.T) {
	var gotToolID, gotTarget, gotContent string
	ch := make(chan struct{}, 1)

	pipeline := NewEventPipeline(PipelineConfig{
		SessionID:        "test-session",
		InitialTurnIndex: 0,
		Sink: EventSink{
			Persist:   func(int, int, string, []byte) {},
			Broadcast: func(string, any) {},
		},
		OnSendMessage: func(toolUseID, targetName, content string) {
			gotToolID = toolUseID
			gotTarget = targetName
			gotContent = content
			ch <- struct{}{}
		},
	})

	input, _ := json.Marshal(map[string]string{
		"to":      "Backend Worker",
		"message": "looks good",
	})
	pipeline.trackToolUse(WireToolUseEvent{
		Type:     "tool_use",
		ToolID:   "tu_123",
		ToolName: "SendMessage",
		ToolInput: input,
	})

	<-ch // wait for goroutine
	if gotToolID != "tu_123" {
		t.Errorf("toolID = %q, want %q", gotToolID, "tu_123")
	}
	if gotTarget != "Backend Worker" {
		t.Errorf("target = %q, want %q", gotTarget, "Backend Worker")
	}
	if gotContent != "looks good" {
		t.Errorf("content = %q, want %q", gotContent, "looks good")
	}
}

// TestPipelineSendMessageSkipsSpawn verifies @spawn targets are not routed
// through the pipeline (handled by handleToolPermission instead).
func TestPipelineSendMessageSkipsSpawn(t *testing.T) {
	called := false
	pipeline := NewEventPipeline(PipelineConfig{
		SessionID:        "test-session",
		InitialTurnIndex: 0,
		Sink: EventSink{
			Persist:   func(int, int, string, []byte) {},
			Broadcast: func(string, any) {},
		},
		OnSendMessage: func(_, _, _ string) {
			called = true
		},
	})

	input, _ := json.Marshal(map[string]string{
		"to":      "@spawn",
		"message": `{"teamName":"t","workers":[]}`,
	})
	pipeline.trackToolUse(WireToolUseEvent{
		Type:      "tool_use",
		ToolID:    "tu_456",
		ToolName:  "SendMessage",
		ToolInput: input,
	})

	if called {
		t.Error("OnSendMessage should not fire for @spawn targets")
	}
}

// mustJSON returns s as a JSON-encoded string (with quotes and escapes).
func mustJSON(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
