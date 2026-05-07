package session

import (
	"context"
	"fmt"
	"testing"
	"time"

	claudecli "github.com/allbin/claudecli-go"
	"github.com/mdjarv/agentique/backend/internal/testutil"
)

// Most lifecycle / event-loop / approval-pump tests live in
// lifecycle_suite_test.go (using testutil.RecordingBroadcaster). This file
// keeps a few integration tests using the local mockBroadcaster +
// mockConnector helpers; the watchdog and approval pump itself moved to
// agentkit/runtime and are tested there.

func TestEventLoop_QueryThenResult(t *testing.T) {
	db, q := testutil.SetupDB(t)
	proj := testutil.SeedProject(t, q, "test", t.TempDir())
	bc := &mockBroadcaster{}
	conn := newMockConnector()
	mgr := NewManager(db, q, bc, conn)

	sess, err := mgr.Create(context.Background(), CreateParams{
		ProjectID: proj.ID,
		Name:      "test",
		WorkDir:   t.TempDir(),
		Model:     "opus",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	if err := sess.Query(context.Background(), "hello", nil); err != nil {
		t.Fatalf("query: %v", err)
	}
	if sess.State() != StateRunning {
		t.Fatalf("expected running, got %s", sess.State())
	}

	mock := conn.last()
	mock.events <- testTextEvent("response text")
	mock.events <- testResultEvent(0.02)

	deadline := time.After(2 * time.Second)
	for sess.State() != StateIdle {
		select {
		case <-deadline:
			t.Fatalf("timeout waiting for idle, state is %s", sess.State())
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}

	events, err := q.ListEventsBySession(context.Background(), sess.ID)
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	if len(events) == 0 {
		t.Fatal("expected persisted events")
	}
	if events[0].Type != "prompt" {
		t.Errorf("expected prompt event, got %s", events[0].Type)
	}
}

func TestEventLoop_FatalError(t *testing.T) {
	db, q := testutil.SetupDB(t)
	proj := testutil.SeedProject(t, q, "test", t.TempDir())
	bc := &mockBroadcaster{}
	conn := newMockConnector()
	mgr := NewManager(db, q, bc, conn)

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
	db, q := testutil.SetupDB(t)
	proj := testutil.SeedProject(t, q, "test", t.TempDir())
	bc := &mockBroadcaster{}
	conn := newMockConnector()
	mgr := NewManager(db, q, bc, conn)

	sess, err := mgr.Create(context.Background(), CreateParams{
		ProjectID: proj.ID,
		Name:      "test",
		WorkDir:   t.TempDir(),
		Model:     "opus",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	sess.Close()

	// Idle sessions stay in their last-known state — runtime translates
	// Idle→Stopped only when actively running.
	state := sess.State()
	if state != StateIdle && state != StateStopped {
		t.Errorf("unexpected state after close from idle: %s", state)
	}
}

func TestManager_CreateAndStop(t *testing.T) {
	db, q := testutil.SetupDB(t)
	proj := testutil.SeedProject(t, q, "test", t.TempDir())
	bc := &mockBroadcaster{}
	conn := newMockConnector()
	mgr := NewManager(db, q, bc, conn)

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

	dbSess, err := q.GetSession(context.Background(), sess.ID)
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	if dbSess.State != "stopped" {
		t.Errorf("expected stopped in DB, got %s", dbSess.State)
	}
}

func TestManager_CloseAll(t *testing.T) {
	db, q := testutil.SetupDB(t)
	proj := testutil.SeedProject(t, q, "test", t.TempDir())
	bc := &mockBroadcaster{}
	conn := newMockConnector()
	mgr := NewManager(db, q, bc, conn)

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
