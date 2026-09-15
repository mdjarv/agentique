package session

import (
	"context"
	"testing"

	"github.com/mdjarv/agentique/backend/internal/testutil"
	"github.com/stretchr/testify/suite"
)

type IdempotentCreateSuite struct {
	testutil.DBSuite
	svc *Service
	mgr *Manager
}

func TestIdempotentCreateSuite(t *testing.T) {
	t.Parallel()
	suite.Run(t, new(IdempotentCreateSuite))
}

func (s *IdempotentCreateSuite) SetupTest() {
	s.DBSuite.SetupTest()
	s.mgr = NewManager(s.DB, s.Queries, s.Broadcaster, connectorAdapter{s.Connector})
	s.svc = NewService(s.mgr, s.Queries, s.Broadcaster, testutil.NewMockBlockingRunner())
}

func (s *IdempotentCreateSuite) TearDownTest() { s.mgr.CloseAll() }

// A create repeated under the same key, once the first has finished, answers
// the first session rather than making a second. It is what a paired machine's
// retried create relies on (the peer handler keys it on credential and request
// id) — and it is only that: a repeat arriving while the first is still being
// made is not in the cache yet, and a new key is a new session.
func (s *IdempotentCreateSuite) TestARepeatedKeyAnswersTheFirstSession() {
	ctx := context.Background()
	params := CreateSessionParams{ProjectID: s.Project.ID, Model: "opus", IdempotencyKey: "peer:cred:request-1"}

	first, err := s.svc.CreateSession(ctx, params)
	s.Require().NoError(err)
	again, err := s.svc.CreateSession(ctx, params)
	s.Require().NoError(err)
	s.Equal(first.SessionID, again.SessionID, "a repeated key made a second session")

	params.IdempotencyKey = "peer:cred:request-2"
	other, err := s.svc.CreateSession(ctx, params)
	s.Require().NoError(err)
	s.NotEqual(first.SessionID, other.SessionID)

	all, err := s.Queries.ListAllSessions(ctx)
	s.Require().NoError(err)
	s.Len(all, 2, "two keys, two sessions")
}
