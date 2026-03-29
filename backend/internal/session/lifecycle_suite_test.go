package session

import (
	"context"
	"testing"
	"time"

	claudecli "github.com/allbin/claudecli-go"
	"github.com/allbin/agentique/backend/internal/testutil"
	"github.com/stretchr/testify/suite"
)

// connectorAdapter wraps testutil.RecordingConnector to satisfy CLIConnector.
type connectorAdapter struct{ rc *testutil.RecordingConnector }

func (a connectorAdapter) Connect(_ context.Context, _ ...claudecli.Option) (CLISession, error) {
	return a.rc.NextSession()
}

type LifecycleSuite struct {
	testutil.DBSuite
	mgr *Manager
	svc *Service
}

func TestLifecycleSuite(t *testing.T) {
	suite.Run(t, new(LifecycleSuite))
}

func (s *LifecycleSuite) SetupTest() {
	s.DBSuite.SetupTest()
	s.mgr = NewManager(s.Queries, s.Broadcaster, connectorAdapter{s.Connector})
	s.svc = NewService(s.mgr, s.Queries, s.Broadcaster, testutil.NewMockBlockingRunner())
}

func (s *LifecycleSuite) createSession() *Session {
	sess, err := s.mgr.Create(context.Background(), CreateParams{
		ProjectID: s.Project.ID,
		Name:      "test-session",
		WorkDir:   s.T().TempDir(),
		Model:     "opus",
	})
	s.Require().NoError(err)
	return sess
}

func (s *LifecycleSuite) waitForState(sess *Session, target State, timeout time.Duration) {
	s.T().Helper()
	deadline := time.After(timeout)
	for sess.State() != target {
		select {
		case <-deadline:
			s.Failf("timeout", "waiting for %s, got %s", target, sess.State())
			return
		default:
			time.Sleep(5 * time.Millisecond)
		}
	}
}

// --- Tests ---

func (s *LifecycleSuite) TestCreateAndQuery() {
	sess := s.createSession()
	s.Equal(StateIdle, sess.State())

	// Query transitions to running.
	s.Require().NoError(sess.Query(context.Background(), "hello", nil))
	s.Equal(StateRunning, sess.State())

	mock := s.Connector.Last()

	// Simulate response.
	s.Require().NoError(mock.Inject(testutil.TextEvent("response")))
	s.Require().NoError(mock.Inject(testutil.ResultEvent(0.01)))

	s.waitForState(sess, StateIdle, 2*time.Second)

	// Verify events persisted in DB.
	events, err := s.Queries.ListEventsBySession(context.Background(), sess.ID)
	s.Require().NoError(err)
	s.GreaterOrEqual(len(events), 3) // prompt + text + result

	// First event should be the prompt.
	s.Equal("prompt", events[0].Type)

	// Verify DB state matches in-memory state.
	dbSess, err := s.Queries.GetSession(context.Background(), sess.ID)
	s.Require().NoError(err)
	s.Equal("idle", dbSess.State)
}

func (s *LifecycleSuite) TestFatalError() {
	sess := s.createSession()
	s.Require().NoError(sess.Query(context.Background(), "test", nil))

	mock := s.Connector.Last()
	s.Require().NoError(mock.Inject(testutil.ErrorEvent("something broke", true)))

	s.waitForState(sess, StateFailed, 2*time.Second)

	// Verify DB reflects failed state.
	dbSess, err := s.Queries.GetSession(context.Background(), sess.ID)
	s.Require().NoError(err)
	s.Equal("failed", dbSess.State)
}

func (s *LifecycleSuite) TestMultiTurn() {
	sess := s.createSession()

	// Turn 0
	s.Require().NoError(sess.Query(context.Background(), "first question", nil))
	mock := s.Connector.Last()
	s.Require().NoError(mock.Inject(testutil.TextEvent("answer 1")))
	s.Require().NoError(mock.Inject(testutil.ResultEvent(0.01)))
	s.waitForState(sess, StateIdle, 2*time.Second)

	// Turn 1
	s.Require().NoError(sess.Query(context.Background(), "second question", nil))
	s.Require().NoError(mock.Inject(testutil.TextEvent("answer 2")))
	s.Require().NoError(mock.Inject(testutil.ResultEvent(0.02)))
	s.waitForState(sess, StateIdle, 2*time.Second)

	// Verify two turns with incrementing turn_index.
	events, err := s.Queries.ListEventsBySession(context.Background(), sess.ID)
	s.Require().NoError(err)

	turnIndices := map[int64]bool{}
	promptCount := 0
	for _, e := range events {
		turnIndices[e.TurnIndex] = true
		if e.Type == "prompt" {
			promptCount++
		}
	}
	s.Equal(2, len(turnIndices), "should have 2 distinct turns")
	s.Equal(2, promptCount, "should have 2 prompt events")
}

func (s *LifecycleSuite) TestStopWhileRunning() {
	sess := s.createSession()
	s.Require().NoError(sess.Query(context.Background(), "work", nil))
	s.Equal(StateRunning, sess.State())

	sess.Close()
	s.Equal(StateStopped, sess.State())

	dbSess, err := s.Queries.GetSession(context.Background(), sess.ID)
	s.Require().NoError(err)
	s.Equal("stopped", dbSess.State)
}

func (s *LifecycleSuite) TestDeleteCleansUp() {
	sess := s.createSession()
	sessionID := sess.ID

	s.Require().NoError(s.svc.DeleteSession(context.Background(), sessionID))

	// DB row should be gone.
	_, err := s.Queries.GetSession(context.Background(), sessionID)
	s.Error(err)

	// Broadcast should include deletion.
	msgs := s.Broadcaster.MessagesOfType("session.deleted")
	s.NotEmpty(msgs)
}

func (s *LifecycleSuite) TestConcurrentQueryRejected() {
	sess := s.createSession()
	s.Require().NoError(sess.Query(context.Background(), "first", nil))
	s.Equal(StateRunning, sess.State())

	// Second query while running should fail.
	err := sess.Query(context.Background(), "second", nil)
	s.Error(err, "concurrent query should be rejected")
}

func (s *LifecycleSuite) TestManagerStop() {
	sess := s.createSession()
	s.True(s.mgr.IsLive(sess.ID))

	s.Require().NoError(s.mgr.Stop(context.Background(), sess.ID))
	s.False(s.mgr.IsLive(sess.ID))

	dbSess, err := s.Queries.GetSession(context.Background(), sess.ID)
	s.Require().NoError(err)
	s.Equal("stopped", dbSess.State)
}

func (s *LifecycleSuite) TestCloseAllMultipleSessions() {
	for i := 0; i < 3; i++ {
		s.createSession()
	}
	s.Equal(3, s.Connector.SessionCount())

	s.mgr.CloseAll()

	sessions, err := s.mgr.ListAll(context.Background())
	s.Require().NoError(err)
	for _, sess := range sessions {
		s.False(s.mgr.IsLive(sess.ID))
	}
}

func (s *LifecycleSuite) TestApprovalFlow() {
	sess := s.createSession()

	done := make(chan *claudecli.PermissionResponse, 1)
	go func() {
		resp, _ := sess.handleToolPermission("Write", []byte(`{"path":"test.go"}`))
		done <- resp
	}()

	// Wait for the approval broadcast.
	msg, ok := s.Broadcaster.WaitForType("session.tool-permission", 2*time.Second)
	s.Require().True(ok, "expected tool-permission broadcast")

	payload := msg.Payload.(map[string]any)
	approvalID := payload["approvalId"].(string)
	s.Require().NoError(sess.ResolveApproval(approvalID, true, ""))

	select {
	case resp := <-done:
		s.True(resp.Allow)
	case <-time.After(2 * time.Second):
		s.Fail("timeout waiting for approval response")
	}
}

func (s *LifecycleSuite) TestToolUseEventsPersisted() {
	sess := s.createSession()
	s.Require().NoError(sess.Query(context.Background(), "read a file", nil))

	mock := s.Connector.Last()
	s.Require().NoError(mock.Inject(testutil.ToolUseEvent("t1", "Read", map[string]string{"path": "/tmp/test"})))
	s.Require().NoError(mock.Inject(testutil.ToolResultEvent("t1", "file contents")))
	s.Require().NoError(mock.Inject(testutil.TextEvent("done")))
	s.Require().NoError(mock.Inject(testutil.ResultEvent(0.01)))
	s.waitForState(sess, StateIdle, 2*time.Second)

	events, err := s.Queries.ListEventsBySession(context.Background(), sess.ID)
	s.Require().NoError(err)

	types := make([]string, len(events))
	for i, e := range events {
		types[i] = e.Type
	}
	s.Contains(types, "tool_use")
	s.Contains(types, "tool_result")
}
