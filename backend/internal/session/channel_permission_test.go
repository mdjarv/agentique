package session

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// sendMessageInput returns a valid SendMessage tool input JSON.
func sendMessageInput(to, msg string) json.RawMessage {
	b, _ := json.Marshal(map[string]string{"to": to, "message": msg})
	return b
}

// newPermTestSession creates a minimal Session suitable for handleToolPermission tests.
func newPermTestSession(autoApprove, permMode string) *Session {
	ctx, cancel := context.WithCancel(context.Background())
	s := &Session{
		ID:        "perm-test",
		ctx:       ctx,
		cancelCtx: cancel,
		approvalState: approvalState{
			autoApproveMode:  autoApprove,
			permissionMode:   permMode,
			pendingApprovals: make(map[string]*pendingApproval),
		},
		broadcast: func(string, any) {},
	}
	s.toolInterceptors = map[string]toolInterceptor{
		ChannelSendMessageTool: s.interceptSendMessage,
		"AskTeammate": s.interceptAskTeammate,
	}
	return s
}

// TestHandleToolPermission_SendMessage_DeniesWithSuccess verifies that
// handleToolPermission intercepts SendMessage and returns deny-with-"delivered"
// regardless of session configuration.
func TestHandleToolPermission_SendMessage_DeniesWithSuccess(t *testing.T) {
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

			resp, err := sess.handleToolPermission(ChannelSendMessageTool, sendMessageInput("Worker", "hello"))
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

// TestHandleToolPermission_SendMessage_NeverBlocks verifies that SendMessage
// interception returns immediately even in manual mode (strictest), without
// entering the pending-approval flow or broadcasting session.tool-permission.
func TestHandleToolPermission_SendMessage_NeverBlocks(t *testing.T) {
	var broadcasts []string
	sess := newPermTestSession("manual", "default")
	sess.broadcast = func(pushType string, _ any) {
		broadcasts = append(broadcasts, pushType)
	}
	defer sess.cancelCtx()

	done := make(chan struct{})
	go func() {
		resp, err := sess.handleToolPermission(ChannelSendMessageTool, sendMessageInput("peer", "hi"))
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if resp.Allow {
			t.Error("should deny")
		}
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("handleToolPermission blocked — SendMessage should return immediately")
	}

	for _, b := range broadcasts {
		if b == "session.tool-permission" {
			t.Error("SendMessage should not trigger tool-permission broadcast")
		}
	}
}

// TestHandleToolPermission_SendMessage_FullAutoDoesNotBypass verifies that
// fullAuto mode bypasses normal tools (like Bash) but NOT SendMessage.
// SendMessage is intercepted at line 860 before the shouldBypassPermission
// check at line 884 — this is the core of the "can_use_tool mystery".
func TestHandleToolPermission_SendMessage_FullAutoDoesNotBypass(t *testing.T) {
	sess := newPermTestSession("fullAuto", "default")
	defer sess.cancelCtx()

	// Bash should be auto-approved in fullAuto.
	bashResp, err := sess.handleToolPermission("Bash", json.RawMessage(`{"command":"ls"}`))
	if err != nil {
		t.Fatalf("Bash: unexpected error: %v", err)
	}
	if !bashResp.Allow {
		t.Error("Bash should be allowed in fullAuto mode")
	}

	// SendMessage should still be denied (intercepted before bypass check).
	smResp, err := sess.handleToolPermission(ChannelSendMessageTool, sendMessageInput("peer", "hi"))
	if err != nil {
		t.Fatalf("SendMessage: unexpected error: %v", err)
	}
	if smResp.Allow {
		t.Error("SendMessage should be denied even in fullAuto mode")
	}
	if !strings.Contains(smResp.DenyMessage, "delivered") {
		t.Errorf("deny message should indicate success, got: %s", smResp.DenyMessage)
	}
}

// TestHandleToolPermission_SendMessage_LeadVsWorker proves that SendMessage
// interception is identical for lead-like and worker-like session configurations.
// This directly addresses the reported "mystery" of can_use_tool not firing
// for workers — the code path is the same regardless of auto-approve mode.
func TestHandleToolPermission_SendMessage_LeadVsWorker(t *testing.T) {
	configs := []struct {
		name string
		auto string
		perm string
	}{
		{"lead-auto-default", "auto", "default"},
		{"lead-auto-plan", "auto", "plan"},
		{"worker-fullAuto-default", "fullAuto", "default"},
		{"worker-auto-default", "auto", "default"},
	}

	for _, cfg := range configs {
		t.Run(cfg.name, func(t *testing.T) {
			sess := newPermTestSession(cfg.auto, cfg.perm)
			defer sess.cancelCtx()

			resp, err := sess.handleToolPermission(ChannelSendMessageTool, sendMessageInput("Target", "message"))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if resp.Allow {
				t.Errorf("[%s] SendMessage should be denied", cfg.name)
			}
			if !strings.Contains(resp.DenyMessage, "delivered") {
				t.Errorf("[%s] deny message should say delivered, got: %s", cfg.name, resp.DenyMessage)
			}
		})
	}
}

// TestHandleToolPermission_SendMessage_SpawnTarget verifies that @spawn
// targets take the interceptSpawnWorkers path (not the regular deny path).
func TestHandleToolPermission_SendMessage_SpawnTarget(t *testing.T) {
	sess := newPermTestSession("auto", "default")
	defer sess.cancelCtx()

	// No onSpawnWorkers callback set — should get error about no callback.
	input := sendMessageInput("@spawn", `{"channelName":"test","workers":[{"name":"W1","prompt":"do stuff"}]}`)
	resp, err := sess.handleToolPermission(ChannelSendMessageTool, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Without a spawn callback, the response should deny (not crash).
	if resp.Allow {
		t.Error("@spawn without callback should not allow")
	}
	// Should NOT contain "delivered" — it's a spawn error, not a routing success.
	if strings.Contains(resp.DenyMessage, "delivered") {
		t.Errorf("@spawn deny should not say 'delivered', got: %s", resp.DenyMessage)
	}
}
