package session

import (
	"context"
	"testing"

	"github.com/mdjarv/agentique/backend/internal/testutil"
	"github.com/stretchr/testify/suite"
)

type StateSuite struct {
	testutil.DBSuite
	mgr *Manager
}

func TestStateSuite(t *testing.T) {
	suite.Run(t, new(StateSuite))
}

func (s *StateSuite) SetupTest() {
	s.DBSuite.SetupTest()
	s.mgr = NewManager(s.Queries, s.Broadcaster, connectorAdapter{s.Connector})
}

func (s *StateSuite) createSessionInState(state State) *Session {
	sess, err := s.mgr.Create(context.Background(), CreateParams{
		ProjectID: s.Project.ID,
		Name:      "state-test",
		WorkDir:   s.T().TempDir(),
		Model:     "opus",
	})
	s.Require().NoError(err)

	// Force the session into the desired starting state for table-driven tests.
	if state != StateIdle {
		sess.mu.Lock()
		sess.state = state
		sess.mu.Unlock()
		sess.persistState(state)
	}
	return sess
}

func (s *StateSuite) TestAllValidTransitions_PersistToDB() {
	for from, targets := range validTransitions {
		for to := range targets {
			s.Run(string(from)+"->"+string(to), func() {
				sess := s.createSessionInState(from)

				err := sess.setState(to)
				s.NoError(err, "%s -> %s should succeed", from, to)

				// Verify persisted to DB.
				dbSess, err := s.Queries.GetSession(context.Background(), sess.ID)
				s.Require().NoError(err)
				s.Equal(string(to), dbSess.State)
			})
		}
	}
}

func (s *StateSuite) TestInvalidTransitions_Rejected() {
	invalid := []struct{ from, to State }{
		{StateRunning, StateRunning},
		{StateRunning, StateStopped},
		{StateRunning, StateMerging},
		{StateFailed, StateRunning},
		{StateFailed, StateMerging},
		{StateDone, StateRunning},
		{StateDone, StateFailed},
		{StateDone, StateMerging},
		{StateStopped, StateRunning},
		{StateStopped, StateFailed},
		{StateStopped, StateMerging},
	}

	for _, tc := range invalid {
		s.Run(string(tc.from)+"->"+string(tc.to), func() {
			sess := s.createSessionInState(tc.from)

			err := sess.setState(tc.to)
			s.Error(err, "%s -> %s should be rejected", tc.from, tc.to)

			// DB should still show the original state.
			dbSess, err := s.Queries.GetSession(context.Background(), sess.ID)
			s.Require().NoError(err)
			s.Equal(string(tc.from), dbSess.State)
		})
	}
}

func (s *StateSuite) TestTryLockForGitOp() {
	sess := s.createSessionInState(StateIdle)

	s.Require().NoError(sess.TryLockForGitOp("merging"))
	s.Equal(StateMerging, sess.State())
}

func (s *StateSuite) TestTryLockForGitOp_BlocksWhileRunning() {
	sess := s.createSessionInState(StateRunning)

	err := sess.TryLockForGitOp("merging")
	s.Error(err)
	s.Contains(err.Error(), "running")
}

func (s *StateSuite) TestUnlockGitOp() {
	sess := s.createSessionInState(StateIdle)
	s.Require().NoError(sess.TryLockForGitOp("rebasing"))
	s.Equal(StateMerging, sess.State())

	s.Require().NoError(sess.UnlockGitOp(StateIdle))
	s.Equal(StateIdle, sess.State())
}

func (s *StateSuite) TestUnlockGitOp_ToFailed() {
	sess := s.createSessionInState(StateIdle)
	s.Require().NoError(sess.TryLockForGitOp("merging"))

	s.Require().NoError(sess.UnlockGitOp(StateFailed))
	s.Equal(StateFailed, sess.State())
}
