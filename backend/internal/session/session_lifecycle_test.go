package session

import (
	"context"
	"fmt"
	"testing"
	"time"

	claudecli "github.com/allbin/claudecli-go"
	"github.com/allbin/agentique/backend/internal/testutil"
)

func TestEventLoop_TextAndResult(t *testing.T) {
	q := testutil.SetupDB(t)
	proj := testutil.SeedProject(t, q, "test", t.TempDir())
	bc := &mockBroadcaster{}
	conn := newMockConnector()
	mgr := NewManager(q, bc, conn)

	sess, err := mgr.Create(context.Background(), CreateParams{
		ProjectID: proj.ID,
		Name:      "test",
		WorkDir:   t.TempDir(),
		Model:     "opus",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	mock := conn.last()

	// Send text + result, then close
	mock.events <- testTextEvent("hello")
	mock.events <- testResultEvent(0.01)
	mock.Close()

	// Wait for event loop to finish
	select {
	case <-sess.eventLoopDone:
	case <-time.After(2 * time.Second):
		t.Fatal("event loop did not stop")
	}

	// Session should be idle (result event transitions running->idle, but
	// we never called Query so it was idle->idle which is invalid).
	// Actually: after Create, session is idle. ResultEvent only transitions
	// if state is running. Since we didn't call Query, it stays idle.
	// The final state after channel close should be done (clean close).
	if sess.State() != StateDone {
		t.Errorf("expected done, got %s", sess.State())
	}

	// Verify events were broadcast
	events := bc.messagesOfType("session.event")
	if len(events) == 0 {
		t.Error("expected broadcast events")
	}
}

func TestEventLoop_QueryThenResult(t *testing.T) {
	q := testutil.SetupDB(t)
	proj := testutil.SeedProject(t, q, "test", t.TempDir())
	bc := &mockBroadcaster{}
	conn := newMockConnector()
	mgr := NewManager(q, bc, conn)

	sess, err := mgr.Create(context.Background(), CreateParams{
		ProjectID: proj.ID,
		Name:      "test",
		WorkDir:   t.TempDir(),
		Model:     "opus",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	// Query transitions to running
	if err := sess.Query(context.Background(), "hello", nil); err != nil {
		t.Fatalf("query: %v", err)
	}
	if sess.State() != StateRunning {
		t.Fatalf("expected running, got %s", sess.State())
	}

	mock := conn.last()

	// Simulate response
	mock.events <- testTextEvent("response text")
	mock.events <- testResultEvent(0.02)

	// Wait for state to return to idle (result event causes running->idle)
	deadline := time.After(2 * time.Second)
	for sess.State() != StateIdle {
		select {
		case <-deadline:
			t.Fatalf("timeout waiting for idle, state is %s", sess.State())
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}

	// Verify prompt was persisted
	events, err := q.ListEventsBySession(context.Background(), sess.ID)
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	if len(events) == 0 {
		t.Fatal("expected persisted events")
	}
	// First event should be the prompt
	if events[0].Type != "prompt" {
		t.Errorf("expected prompt event, got %s", events[0].Type)
	}
}

func TestEventLoop_FatalError(t *testing.T) {
	q := testutil.SetupDB(t)
	proj := testutil.SeedProject(t, q, "test", t.TempDir())
	bc := &mockBroadcaster{}
	conn := newMockConnector()
	mgr := NewManager(q, bc, conn)

	sess, err := mgr.Create(context.Background(), CreateParams{
		ProjectID: proj.ID,
		Name:      "test",
		WorkDir:   t.TempDir(),
		Model:     "opus",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	// Need to be in running state for fatal error to transition to failed
	if err := sess.Query(context.Background(), "test", nil); err != nil {
		t.Fatalf("query: %v", err)
	}

	mock := conn.last()
	mock.events <- &claudecli.ErrorEvent{
		Err:   fmt.Errorf("test error"),
		Fatal: true,
	}

	deadline := time.After(2 * time.Second)
	for sess.State() != StateFailed {
		select {
		case <-deadline:
			t.Fatalf("timeout waiting for failed, state is %s", sess.State())
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}
}

func TestSession_Close(t *testing.T) {
	q := testutil.SetupDB(t)
	proj := testutil.SeedProject(t, q, "test", t.TempDir())
	bc := &mockBroadcaster{}
	conn := newMockConnector()
	mgr := NewManager(q, bc, conn)

	sess, err := mgr.Create(context.Background(), CreateParams{
		ProjectID: proj.ID,
		Name:      "test",
		WorkDir:   t.TempDir(),
		Model:     "opus",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	// Close from idle should stay idle
	sess.Close()

	if sess.State() != StateIdle {
		t.Errorf("expected idle after close from idle, got %s", sess.State())
	}
}

func TestSession_CloseWhileRunning(t *testing.T) {
	q := testutil.SetupDB(t)
	proj := testutil.SeedProject(t, q, "test", t.TempDir())
	bc := &mockBroadcaster{}
	conn := newMockConnector()
	mgr := NewManager(q, bc, conn)

	sess, err := mgr.Create(context.Background(), CreateParams{
		ProjectID: proj.ID,
		Name:      "test",
		WorkDir:   t.TempDir(),
		Model:     "opus",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	if err := sess.Query(context.Background(), "test", nil); err != nil {
		t.Fatalf("query: %v", err)
	}

	sess.Close()

	if sess.State() != StateStopped {
		t.Errorf("expected stopped after close from running, got %s", sess.State())
	}
}

func TestSession_ApprovalFlow(t *testing.T) {
	q := testutil.SetupDB(t)
	proj := testutil.SeedProject(t, q, "test", t.TempDir())
	bc := &mockBroadcaster{}
	conn := newMockConnector()
	mgr := NewManager(q, bc, conn)

	sess, err := mgr.Create(context.Background(), CreateParams{
		ProjectID: proj.ID,
		Name:      "test",
		WorkDir:   t.TempDir(),
		Model:     "opus",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	// Simulate approval flow in a goroutine (handleToolPermission blocks)
	done := make(chan *claudecli.PermissionResponse, 1)
	go func() {
		resp, _ := sess.handleToolPermission("Write", []byte(`{"path":"test.go"}`))
		done <- resp
	}()

	// Wait for broadcast of the approval request
	deadline := time.After(2 * time.Second)
	for {
		msgs := bc.messagesOfType("session.tool-permission")
		if len(msgs) > 0 {
			payload := msgs[0].Payload.(map[string]any)
			approvalID := payload["approvalId"].(string)

			// Resolve the approval
			if err := sess.ResolveApproval(approvalID, true, ""); err != nil {
				t.Fatalf("resolve: %v", err)
			}
			break
		}
		select {
		case <-deadline:
			t.Fatal("timeout waiting for approval broadcast")
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}

	select {
	case resp := <-done:
		if !resp.Allow {
			t.Error("expected approval to be allowed")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for approval response")
	}
}

func TestManager_CreateAndStop(t *testing.T) {
	q := testutil.SetupDB(t)
	proj := testutil.SeedProject(t, q, "test", t.TempDir())
	bc := &mockBroadcaster{}
	conn := newMockConnector()
	mgr := NewManager(q, bc, conn)

	sess, err := mgr.Create(context.Background(), CreateParams{
		ProjectID: proj.ID,
		Name:      "test",
		WorkDir:   t.TempDir(),
		Model:     "opus",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	if !mgr.IsLive(sess.ID) {
		t.Error("session should be live after create")
	}

	if err := mgr.Stop(context.Background(), sess.ID); err != nil {
		t.Fatalf("stop: %v", err)
	}

	if mgr.IsLive(sess.ID) {
		t.Error("session should not be live after stop")
	}

	// DB should show stopped
	dbSess, err := q.GetSession(context.Background(), sess.ID)
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	if dbSess.State != "stopped" {
		t.Errorf("expected stopped in DB, got %s", dbSess.State)
	}
}

func TestManager_CloseAll(t *testing.T) {
	q := testutil.SetupDB(t)
	proj := testutil.SeedProject(t, q, "test", t.TempDir())
	bc := &mockBroadcaster{}
	conn := newMockConnector()
	mgr := NewManager(q, bc, conn)

	for i := 0; i < 3; i++ {
		_, err := mgr.Create(context.Background(), CreateParams{
			ProjectID: proj.ID,
			Name:      "test",
			WorkDir:   t.TempDir(),
			Model:     "opus",
		})
		if err != nil {
			t.Fatalf("create %d: %v", i, err)
		}
	}

	mgr.CloseAll()

	// All sessions should be evicted
	sessions, err := mgr.ListAll(context.Background())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	for _, s := range sessions {
		if mgr.IsLive(s.ID) {
			t.Errorf("session %s should not be live after CloseAll", s.ID)
		}
	}
}
