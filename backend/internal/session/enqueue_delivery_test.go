package session

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/allbin/agentkit/runtime"
	"github.com/mdjarv/agentique/backend/internal/testutil"
	"github.com/stretchr/testify/suite"
)

// A client cannot tell which of the three deliveries its message got — the
// session state it reads is a push that may be a round trip behind this
// decision. EnqueueMessage therefore reports what it did, and these tests pin
// each branch to its answer: get one wrong and the UI draws the message twice
// (its own optimistic turn plus the echo of the same message).

// midTurnCLISession advertises native mid-turn injection. The plain mock
// advertises none, so the two together cover both running branches.
type midTurnCLISession struct {
	*testutil.MockCLISession

	mu       sync.Mutex
	sendHook func(prompt string) error // optional; overrides SendMessage
}

func (c *midTurnCLISession) Capabilities() runtime.Capabilities {
	return runtime.Capabilities{Provider: "mock", MidTurnSendMessage: true}
}

func (c *midTurnCLISession) SendMessage(ctx context.Context, prompt string, atts ...runtime.Attachment) error {
	c.mu.Lock()
	hook := c.sendHook
	c.mu.Unlock()
	if hook != nil {
		return hook(prompt)
	}
	return c.MockCLISession.SendMessage(ctx, prompt, atts...)
}

func (c *midTurnCLISession) setSendHook(hook func(prompt string) error) {
	c.mu.Lock()
	c.sendHook = hook
	c.mu.Unlock()
}

type midTurnConnector struct {
	rc *testutil.RecordingConnector

	mu   sync.Mutex
	last *midTurnCLISession
}

func (a *midTurnConnector) Connect(_ context.Context, _ runtime.ConnectParams) (runtime.CLISession, error) {
	mock, err := a.rc.NextSession()
	if err != nil {
		return nil, err
	}
	wrapped := &midTurnCLISession{MockCLISession: mock}
	a.mu.Lock()
	a.last = wrapped
	a.mu.Unlock()
	return wrapped, nil
}

type EnqueueDeliverySuite struct {
	testutil.DBSuite
	mgr *Manager
	svc *Service
}

func TestEnqueueDeliverySuite(t *testing.T) {
	suite.Run(t, new(EnqueueDeliverySuite))
}

func (s *EnqueueDeliverySuite) SetupTest() {
	s.DBSuite.SetupTest()
	s.useConnector(connectorAdapter{s.Connector})
}

func (s *EnqueueDeliverySuite) useConnector(c runtime.CLIConnector) {
	s.mgr = NewManager(s.DB, s.Queries, s.Broadcaster, c)
	s.svc = NewService(s.mgr, s.Queries, s.Broadcaster, testutil.NewMockBlockingRunner())
}

func (s *EnqueueDeliverySuite) newSession() *Session {
	s.T().Helper()
	sess, err := s.mgr.Create(context.Background(), CreateParams{
		ProjectID: s.Project.ID,
		Name:      "enqueue-test",
		WorkDir:   s.T().TempDir(),
		Model:     "opus",
	})
	s.Require().NoError(err)
	return sess
}

func (s *EnqueueDeliverySuite) TestIdleSessionOpensATurn() {
	sess := s.newSession()
	s.Require().Equal(StateIdle, sess.State())

	delivery, err := s.svc.EnqueueMessage(context.Background(), sess.ID, "start", nil)
	s.Require().NoError(err)
	s.Equal(DeliveryTurn, delivery)
	s.Equal(StateRunning, sess.State())
}

// No native mid-turn channel: the message is buffered and replayed at the next
// idle, so nothing about the running turn changes.
func (s *EnqueueDeliverySuite) TestRunningWithoutNativeMidTurnQueues() {
	sess := s.newSession()
	s.Require().NoError(sess.Query(context.Background(), "work", nil))
	s.Require().Equal(StateRunning, sess.State())

	delivery, err := s.svc.EnqueueMessage(context.Background(), sess.ID, "follow-up", nil)
	s.Require().NoError(err)
	s.Equal(DeliveryQueued, delivery)
	s.Require().Len(sess.pendingMessages, 1)
	s.Equal("follow-up", sess.pendingMessages[0].prompt)
}

func (s *EnqueueDeliverySuite) TestRunningWithNativeMidTurnInjects() {
	s.useConnector(&midTurnConnector{rc: s.Connector})
	sess := s.newSession()
	s.Require().NoError(sess.Query(context.Background(), "work", nil))
	s.Require().Equal(StateRunning, sess.State())

	delivery, err := s.svc.EnqueueMessage(context.Background(), sess.ID, "follow-up", nil)
	s.Require().NoError(err)
	s.Equal(DeliveryMidTurn, delivery)
	s.Empty(sess.pendingMessages, "a natively injected message is not buffered")
}

// The turn can complete between EnqueueMessage's in-flight check and the
// runtime SendMessage, which then refuses with its state error. That race
// must fall through to a fresh turn (the codex branch always did), not
// surface "send message failed" for a message sent to an idle session.
func (s *EnqueueDeliverySuite) TestMidTurnSendRacingCompletionFallsThroughToTurn() {
	conn := &midTurnConnector{rc: s.Connector}
	s.useConnector(conn)
	sess := s.newSession()
	s.Require().NoError(sess.Query(context.Background(), "work", nil))

	conn.mu.Lock()
	cli := conn.last
	conn.mu.Unlock()
	cli.setSendHook(func(string) error {
		// Simulate the race: the turn completes underneath the send, and the
		// send comes back with the runtime's state refusal only after the
		// runtime has settled the turn (the order the real race produces).
		s.Require().NoError(cli.Inject(testutil.ResultEvent(0)))
		s.Require().Eventually(func() bool { return !sess.TurnInFlight() },
			2*time.Second, 5*time.Millisecond, "turn never settled after completion")
		return errors.New("runtime: cannot SendMessage in state idle")
	})

	delivery, err := s.svc.EnqueueMessage(context.Background(), sess.ID, "follow-up", nil)
	s.Require().NoError(err)
	s.Equal(DeliveryTurn, delivery)
	s.Contains(cli.Queries(), "follow-up", "the message opens a fresh turn")
}

// A send refusal while the turn is still in flight is a real failure —
// falling through would hand a genuinely running session a duplicate fresh
// turn, so the error surfaces instead.
func (s *EnqueueDeliverySuite) TestMidTurnSendFailureWhileRunningSurfaces() {
	conn := &midTurnConnector{rc: s.Connector}
	s.useConnector(conn)
	sess := s.newSession()
	s.Require().NoError(sess.Query(context.Background(), "work", nil))

	conn.mu.Lock()
	cli := conn.last
	conn.mu.Unlock()
	cli.setSendHook(func(string) error {
		return errors.New("transport broke")
	})

	_, err := s.svc.EnqueueMessage(context.Background(), sess.ID, "follow-up", nil)
	s.Require().ErrorContains(err, "transport broke")
	s.NotContains(cli.Queries(), "follow-up", "a failed mid-turn send must not open a duplicate turn")
}
