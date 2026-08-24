package session

import (
	"context"
	"testing"
	"time"

	"github.com/allbin/agentkit/runtime"
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
	s.mgr = NewManager(s.DB, s.Queries, s.Broadcaster, connectorAdapter{s.Connector})
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

func (s *ServiceSuite) TestCreateSession_CapabilitiesClaudeDefault() {
	result, err := s.svc.CreateSession(context.Background(), CreateSessionParams{
		ProjectID: s.Project.ID,
		Name:      "claude-default",
	})
	s.Require().NoError(err)
	s.Require().NotNil(result.Capabilities)
	s.Equal("claude", result.Capabilities.Provider)
	s.True(result.Capabilities.PlanMode)
	s.True(result.Capabilities.Resume)
	s.True(result.Capabilities.MidTurnSendMessage)
	s.True(result.Capabilities.Attachments)
}

func (s *ServiceSuite) TestCreateSession_CapabilitiesCodex() {
	result, err := s.svc.CreateSession(context.Background(), CreateSessionParams{
		ProjectID: s.Project.ID,
		Name:      "codex-test",
		Provider:  "codex",
	})
	s.Require().NoError(err)
	s.Equal("codex", result.Provider)
	s.Require().NotNil(result.Capabilities)
	s.Equal("codex", result.Capabilities.Provider)
	s.False(result.Capabilities.PlanMode)
	s.True(result.Capabilities.Resume)
	// Emulated by agentique (buffer + replay on idle), not native to codex.
	s.True(result.Capabilities.MidTurnSendMessage)
	s.False(result.Capabilities.Attachments)
	s.True(result.Capabilities.AskUserQuestion)

	// And the same caps surface on the list / persisted snapshot path.
	list, err := s.svc.ListSessions(context.Background(), s.Project.ID)
	s.Require().NoError(err)
	var found bool
	for _, info := range list.Sessions {
		if info.ID == result.SessionID {
			found = true
			s.Require().NotNil(info.Capabilities)
			s.Equal("codex", info.Capabilities.Provider)
			s.False(info.Capabilities.PlanMode)
		}
	}
	s.True(found, "newly created codex session must be in ListSessions")
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

	history, err := s.svc.GetHistory(context.Background(), sessionID, 0)
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

// Archiving is a placement decision: it stamps archived_at and never claims a
// lifecycle state of its own. The state that follows comes from releasing the
// idle CLI — a stop that actually happened.
func (s *ServiceSuite) TestArchiveSession() {
	sessionID, _ := s.createLiveSession()

	s.Require().NoError(s.svc.ArchiveSession(context.Background(), sessionID))

	dbSess, err := s.Queries.GetSession(context.Background(), sessionID)
	s.Require().NoError(err)
	s.True(dbSess.ArchivedAt.Valid, "archive must stamp archived_at")
	s.NotEqual(string(StateDone), dbSess.State, "archive must not fabricate a done state")
	s.Equal(string(StateStopped), dbSess.State, "archiving an idle session releases its CLI")
}

// Unarchive is the exact inverse — one field cleared, no state residue. The old
// mark-done/unmark-done pair left a session stranded in StateDone here.
func (s *ServiceSuite) TestUnarchiveSessionIsTheInverse() {
	sessionID, _ := s.createLiveSession()
	ctx := context.Background()

	before, err := s.Queries.GetSession(ctx, sessionID)
	s.Require().NoError(err)

	s.Require().NoError(s.svc.ArchiveSession(ctx, sessionID))
	s.Require().NoError(s.svc.UnarchiveSession(ctx, sessionID))

	after, err := s.Queries.GetSession(ctx, sessionID)
	s.Require().NoError(err)
	s.False(after.ArchivedAt.Valid, "unarchive must clear archived_at")
	s.NotEqual(string(StateDone), after.State, "unarchive must not leave a done state behind")
	s.Equal(before.WorktreeMerged, after.WorktreeMerged)
}

// A turn in flight is refused: the turn would keep running behind a row the user
// believes is filed away.
func (s *ServiceSuite) TestArchiveSessionRefusedMidTurn() {
	sessionID, _ := s.createLiveSession()
	ctx := context.Background()

	s.Require().NoError(s.svc.QuerySession(ctx, sessionID, "hello", nil))

	err := s.svc.ArchiveSession(ctx, sessionID)
	s.Require().Error(err)
	s.ErrorIs(err, ErrBusy)

	dbSess, err := s.Queries.GetSession(ctx, sessionID)
	s.Require().NoError(err)
	s.False(dbSess.ArchivedAt.Valid, "a refused archive must not stamp archived_at")
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

// A clean CLI exit is a process fact, not user intent. It must reach a terminal
// state without archiving — the seam that once let a subprocess exiting hide a
// session inside the collapsed Archived section.
func (s *ServiceSuite) TestCleanCLIExitDoesNotArchive() {
	sessionID, _ := s.createLiveSession()
	ctx := context.Background()

	sess := s.mgr.Get(sessionID)
	s.Require().NotNil(sess)
	handleRuntimeStateChange(sess, runtime.StateChangeEvent{To: runtime.StateDone})

	dbSess, err := s.Queries.GetSession(ctx, sessionID)
	s.Require().NoError(err)
	s.Equal(string(StateDone), dbSess.State, "the runtime still owns the state")
	s.False(dbSess.ArchivedAt.Valid, "a clean CLI exit must never archive")
	s.Empty(sess.archivedAt, "in-memory archivedAt must stay empty too")
}

// Sending a message to an archived session brings it back: a turn is the
// clearest possible statement that the user is not finished with it. The
// session must leave the Archived section on the way in, not once the turn
// ends — so archived_at is cleared as the turn commits, and the running
// snapshot that follows already says so.
func (s *ServiceSuite) TestQueryUnarchivesTheSession() {
	sessionID, _ := s.createLiveSession()
	ctx := context.Background()

	s.Require().NoError(s.svc.ArchiveSession(ctx, sessionID))
	archived, err := s.Queries.GetSession(ctx, sessionID)
	s.Require().NoError(err)
	s.Require().True(archived.ArchivedAt.Valid, "precondition: the session is archived")
	s.Require().Equal(string(StateStopped), archived.State, "precondition: its CLI was released")

	// Sending lazy-resumes the released CLI and starts a turn.
	s.Require().NoError(s.svc.QuerySession(ctx, sessionID, "actually, one more thing", nil))

	after, err := s.Queries.GetSession(ctx, sessionID)
	s.Require().NoError(err)
	s.False(after.ArchivedAt.Valid, "sending must un-archive the session")
	s.NotEqual(string(StateStopped), after.State, "and bring it back to life")

	// The live session agrees, so the snapshot broadcast with the running
	// transition carries the cleared marker rather than a stale one.
	live := s.mgr.Get(sessionID)
	s.Require().NotNil(live)
	_, _, _, archivedAt, _ := live.liveState()
	s.Empty(archivedAt, "the broadcast snapshot must report it un-archived")
}

// Pinned means "keep this at the top" and archived means "stow this away" — a
// session cannot claim both, and leaving the pin set kept filed-away work
// sitting in the sidebar's priority section. Archiving releases the pin.
func (s *ServiceSuite) TestArchiveReleasesThePin() {
	sessionID, _ := s.createLiveSession()
	ctx := context.Background()

	s.Require().NoError(s.svc.SetSessionPinned(ctx, sessionID, true, 3))
	pinned, err := s.Queries.GetSession(ctx, sessionID)
	s.Require().NoError(err)
	s.Require().EqualValues(1, pinned.Pinned, "precondition: the session is pinned")

	s.Require().NoError(s.svc.ArchiveSession(ctx, sessionID))

	after, err := s.Queries.GetSession(ctx, sessionID)
	s.Require().NoError(err)
	s.True(after.ArchivedAt.Valid, "still archived")
	s.EqualValues(0, after.Pinned, "archiving must release the pin")
	s.EqualValues(0, after.PinOrder, "and drop its place in the pin order")
}

// Un-archiving does not resurrect the pin. Archive released it deliberately;
// guessing that the user still wants this at the top would re-create exactly
// the contradiction the release exists to prevent.
func (s *ServiceSuite) TestUnarchiveDoesNotRestoreThePin() {
	sessionID, _ := s.createLiveSession()
	ctx := context.Background()

	s.Require().NoError(s.svc.SetSessionPinned(ctx, sessionID, true, 2))
	s.Require().NoError(s.svc.ArchiveSession(ctx, sessionID))
	s.Require().NoError(s.svc.UnarchiveSession(ctx, sessionID))

	after, err := s.Queries.GetSession(ctx, sessionID)
	s.Require().NoError(err)
	s.False(after.ArchivedAt.Valid)
	s.EqualValues(0, after.Pinned, "the pin stays released")
}
