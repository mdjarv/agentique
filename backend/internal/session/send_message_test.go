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

func TestInterceptSendMessage_TeamMessage(t *testing.T) {
	sess := &Session{
		ID:               "lead-1",
		pendingApprovals: make(map[string]*pendingApproval),
	}

	var gotSender, gotTarget, gotContent string
	sess.SetAgentMessageCallback(func(senderID, targetName, content string) error {
		gotSender = senderID
		gotTarget = targetName
		gotContent = content
		return nil
	})

	input := json.RawMessage(`{"to":"Backend Worker","message":"looks good, proceed"}`)
	resp, err := sess.interceptSendMessage(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Allow {
		t.Error("should deny (message routed externally)")
	}
	if !strings.Contains(resp.DenyMessage, "delivered") {
		t.Errorf("deny message should indicate success, got: %s", resp.DenyMessage)
	}
	if gotSender != "lead-1" {
		t.Errorf("sender = %q, want %q", gotSender, "lead-1")
	}
	if gotTarget != "Backend Worker" {
		t.Errorf("target = %q, want %q", gotTarget, "Backend Worker")
	}
	if gotContent != "looks good, proceed" {
		t.Errorf("content = %q, want %q", gotContent, "looks good, proceed")
	}
}

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
	if !strings.Contains(resp.DenyMessage, "not part of a team") {
		t.Errorf("should indicate no team, got: %s", resp.DenyMessage)
	}
}

// mustJSON returns s as a JSON-encoded string (with quotes and escapes).
func mustJSON(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
