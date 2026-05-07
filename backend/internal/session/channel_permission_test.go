package session

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// sendMessageInput returns a valid SendMessage tool input JSON.
func sendMessageInput(to, msg string) json.RawMessage {
	b, _ := json.Marshal(map[string]string{"to": to, "message": msg})
	return b
}

// newPermTestSession creates a minimal Session suitable for SendMessage
// interceptor tests. The runtime tool-permission callback now lives in
// agentkit/runtime; interception logic is unchanged and is exercised here
// by calling the interceptor directly.
func newPermTestSession(autoApprove, permMode string) *Session {
	ctx, cancel := context.WithCancel(context.Background())
	return &Session{
		ID:                 "perm-test",
		ctx:                ctx,
		cancelCtx:          cancel,
		autoApproveMode:    autoApprove,
		permissionMode:     permMode,
		syntheticApprovals: make(map[string]*syntheticApproval),
		broadcast:          func(string, any) {},
	}
}

// TestInterceptSendMessage_DeniesWithSuccess verifies the SendMessage
// interceptor returns deny-with-"delivered" regardless of session
// configuration. The runtime never sees these calls.
func TestInterceptSendMessage_DeniesWithSuccess(t *testing.T) {
	modes := []struct {
		auto string
		perm string
	}{
		{"manual", "default"},
		{"auto", "default"},
		{"auto", "plan"},
		{"fullAuto", "default"},
	}

	for _, m := range modes {
		t.Run(m.auto+"/"+m.perm, func(t *testing.T) {
			sess := newPermTestSession(m.auto, m.perm)
			defer sess.cancelCtx()

			resp, err := sess.interceptSendMessage(sendMessageInput("Worker", "hello"))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if resp.Allow {
				t.Error("SendMessage should be denied (routing happens in pipeline)")
			}
			if !strings.Contains(resp.DenyMessage, "delivered") {
				t.Errorf("deny message should indicate success, got: %s", resp.DenyMessage)
			}
		})
	}
}

// TestInterceptSendMessage_SpawnTarget verifies that @spawn targets take the
// interceptSpawnWorkers path (not the regular deny path).
func TestInterceptSendMessage_SpawnTarget(t *testing.T) {
	sess := newPermTestSession("auto", "default")
	defer sess.cancelCtx()

	input := sendMessageInput("@spawn", `{"channelName":"test","workers":[{"name":"W1","prompt":"do stuff"}]}`)
	resp, err := sess.interceptSendMessage(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Allow {
		t.Error("@spawn without callback should not allow")
	}
	if strings.Contains(resp.DenyMessage, "delivered") {
		t.Errorf("@spawn deny should not say 'delivered', got: %s", resp.DenyMessage)
	}
}
