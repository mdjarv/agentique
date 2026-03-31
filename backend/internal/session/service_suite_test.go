package session

import (
	"context"
	"testing"
	"time"

	"github.com/mdjarv/agentique/backend/internal/testutil"
	"github.com/stretchr/testify/suite"
)

type ServiceSuite struct {
	testutil.DBSuite
	svc *Service
	mgr *Manager
}

func TestServiceSuite(t *testing.T) {
	suite.Run(t, new(ServiceSuite))
}

func (s *ServiceSuite) SetupTest() {
	s.DBSuite.SetupTest()
	s.mgr = NewManager(s.Queries, s.Broadcaster, connectorAdapter{s.Connector})
	s.svc = NewService(s.mgr, s.Queries, s.Broadcaster, testutil.NewMockBlockingRunner())
}

func (s *ServiceSuite) createLiveSession() (string, *testutil.MockCLISession) {
	result, err := s.svc.CreateSession(context.Background(), CreateSessionParams{
		ProjectID: s.Project.ID,
		Name:      "svc-test",
		Model:     "opus",
	})
	s.Require().NoError(err)
	mock := s.Connector.Last()
	return result.SessionID, mock
}

// --- Tests ---

func (s *ServiceSuite) TestCreateSession_InvalidProject() {
	_, err := s.svc.CreateSession(context.Background(), CreateSessionParams{
		ProjectID: "nonexistent-id",
		Name:      "test",
	})
	s.Error(err)
	s.Contains(err.Error(), "project not found")
}

func (s *ServiceSuite) TestCreateSession_GeneratesID() {
	result, err := s.svc.CreateSession(context.Background(), CreateSessionParams{
		ProjectID: s.Project.ID,
		Name:      "test",
	})
	s.Require().NoError(err)
	s.NotEmpty(result.SessionID)
	s.Equal("idle", result.State)
	s.True(result.Connected)
}

func (s *ServiceSuite) TestGetHistory_ReconstructsTurns() {
	sessionID, mock := s.createLiveSession()
	sess := s.mgr.Get(sessionID)

	// Turn 0
	s.Require().NoError(sess.Query(context.Background(), "question one", nil))
	s.Require().NoError(mock.Inject(testutil.TextEvent("answer one")))
	s.Require().NoError(mock.Inject(testutil.ResultEvent(0.01)))
	waitForState(s.T(), sess, StateIdle)

	// Turn 1
	s.Require().NoError(sess.Query(context.Background(), "question two", nil))
	s.Require().NoError(mock.Inject(testutil.TextEvent("answer two")))
	s.Require().NoError(mock.Inject(testutil.ResultEvent(0.02)))
	waitForState(s.T(), sess, StateIdle)

	history, err := s.svc.GetHistory(context.Background(), sessionID)
	s.Require().NoError(err)
	s.Len(history.Turns, 2)
	s.Equal("question one", history.Turns[0].Prompt)
	s.Equal("question two", history.Turns[1].Prompt)
	s.NotEmpty(history.Turns[0].Events)
	s.NotEmpty(history.Turns[1].Events)
}

func (s *ServiceSuite) TestRenameSession() {
	sessionID, _ := s.createLiveSession()

	s.Require().NoError(s.svc.RenameSession(context.Background(), sessionID, "new-name"))

	dbSess, err := s.Queries.GetSession(context.Background(), sessionID)
	s.Require().NoError(err)
	s.Equal("new-name", dbSess.Name)

	msgs := s.Broadcaster.MessagesOfType("session.renamed")
	s.NotEmpty(msgs)
}

func (s *ServiceSuite) TestSetPermissionMode() {
	sessionID, _ := s.createLiveSession()

	s.Require().NoError(s.svc.SetPermissionMode(sessionID, "plan"))

	dbSess, err := s.Queries.GetSession(context.Background(), sessionID)
	s.Require().NoError(err)
	s.Equal("plan", dbSess.PermissionMode)
}

func (s *ServiceSuite) TestSetAutoApproveMode() {
	sessionID, _ := s.createLiveSession()

	s.Require().NoError(s.svc.SetAutoApproveMode(sessionID, "fullAuto"))

	dbSess, err := s.Queries.GetSession(context.Background(), sessionID)
	s.Require().NoError(err)
	s.Equal("fullAuto", dbSess.AutoApproveMode)
}

func (s *ServiceSuite) TestListSessions() {
	for i := 0; i < 3; i++ {
		s.createLiveSession()
	}

	result, err := s.svc.ListSessions(context.Background(), s.Project.ID)
	s.Require().NoError(err)
	s.Len(result.Sessions, 3)

	for _, info := range result.Sessions {
		s.Equal(s.Project.ID, info.ProjectID)
		s.True(info.Connected)
	}
}

func (s *ServiceSuite) TestMarkSessionDone() {
	sessionID, _ := s.createLiveSession()

	s.Require().NoError(s.svc.MarkSessionDone(context.Background(), sessionID))

	dbSess, err := s.Queries.GetSession(context.Background(), sessionID)
	s.Require().NoError(err)
	s.Equal("done", dbSess.State)
}

func (s *ServiceSuite) TestQuerySession() {
	sessionID, mock := s.createLiveSession()

	s.Require().NoError(s.svc.QuerySession(context.Background(), sessionID, "hello", nil))

	// Session should be running.
	sess := s.mgr.Get(sessionID)
	s.Equal(StateRunning, sess.State())

	// Complete the query.
	s.Require().NoError(mock.Inject(testutil.TextEvent("hi")))
	s.Require().NoError(mock.Inject(testutil.ResultEvent(0.01)))
	waitForState(s.T(), sess, StateIdle)
}

func (s *ServiceSuite) TestQuerySession_NotLive() {
	// Create a DB-only session with a Claude session ID (required for resume).
	dbSess := testutil.SeedSessionWithClaude(s.T(), s.Queries, s.Project.ID, "stopped", "claude-123")

	// QuerySession should resume the session (lazy resume).
	err := s.svc.QuerySession(context.Background(), dbSess.ID, "hello", nil)
	s.Require().NoError(err)

	// Should now be live and running.
	sess := s.mgr.Get(dbSess.ID)
	s.Require().NotNil(sess)
	s.Equal(StateRunning, sess.State())

	// Clean up.
	mock := s.Connector.Last()
	s.Require().NoError(mock.Inject(testutil.ResultEvent(0.01)))
	waitForState(s.T(), sess, StateIdle)
}

// waitForState polls until session reaches target state.
func waitForState(t *testing.T, sess *Session, target State) {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for sess.State() != target {
		select {
		case <-deadline:
			t.Fatalf("timeout waiting for %s, got %s", target, sess.State())
		default:
			time.Sleep(5 * time.Millisecond)
		}
	}
}
