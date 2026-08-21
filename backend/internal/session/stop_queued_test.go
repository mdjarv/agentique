package session

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/allbin/agentkit/runtime"
	claudecli "github.com/allbin/claudecli-go"
	"github.com/mdjarv/agentique/backend/internal/testutil"
	"github.com/stretchr/testify/suite"
)

// capableCLISession decorates the plain mock with the two optional adapter
// interfaces agentkit reaches through — the ones a real claude CLI advertises
// and testutil.MockCLISession deliberately does not, so both the supported and
// the ErrNotSupported paths are reachable from tests.
type capableCLISession struct {
	*testutil.MockCLISession

	mu sync.Mutex
	// receipt/receiptErr are what InterruptWithQueued answers.
	receipt    *runtime.InterruptReceipt
	receiptErr error
	// cancelQueuedCalls records the flag every InterruptWithQueued saw.
	cancelQueuedCalls []bool

	usage    *runtime.ContextUsage
	usageErr error
	// usageQueries counts live measurements, so a test can prove the meter
	// coalesces instead of querying per event.
	usageQueries int
}

func (c *capableCLISession) InterruptWithQueued(ctx context.Context, cancelQueued bool) (*runtime.InterruptReceipt, error) {
	c.mu.Lock()
	c.cancelQueuedCalls = append(c.cancelQueuedCalls, cancelQueued)
	receipt, err := c.receipt, c.receiptErr
	c.mu.Unlock()
	if err != nil {
		return nil, err
	}
	// A real adapter still aborts the turn; mirror that so the session settles.
	if ierr := c.MockCLISession.Interrupt(ctx); ierr != nil {
		return nil, ierr
	}
	return receipt, nil
}

func (c *capableCLISession) QueryContextUsage(_ context.Context) (*runtime.ContextUsage, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.usageQueries++
	if c.usageErr != nil {
		return nil, c.usageErr
	}
	return c.usage, nil
}

func (c *capableCLISession) queuedInterrupts() []bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]bool(nil), c.cancelQueuedCalls...)
}

func (c *capableCLISession) usageQueryCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.usageQueries
}

// capableConnector hands out capableCLISession wrappers around the recording
// connector's mocks, so the suite keeps testutil's inject/assert helpers.
type capableConnector struct {
	rc *testutil.RecordingConnector

	mu   sync.Mutex
	last *capableCLISession
}

func (a *capableConnector) Connect(_ context.Context, _ runtime.ConnectParams) (runtime.CLISession, error) {
	mock, err := a.rc.NextSession()
	if err != nil {
		return nil, err
	}
	wrapped := &capableCLISession{MockCLISession: mock}
	a.mu.Lock()
	a.last = wrapped
	a.mu.Unlock()
	return wrapped, nil
}

func (a *capableConnector) Last() *capableCLISession {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.last
}

// StopQueuedSuite drives a real runtime.Session so Interrupt exercises the
// actual agentkit dispatch, not a hand-rolled double.
type StopQueuedSuite struct {
	testutil.DBSuite
	capable *capableConnector
	mgr     *Manager
	svc     *Service
}

func TestStopQueuedSuite(t *testing.T) {
	suite.Run(t, new(StopQueuedSuite))
}

func (s *StopQueuedSuite) SetupTest() {
	s.DBSuite.SetupTest()
	s.capable = &capableConnector{rc: s.Connector}
	s.mgr = NewManager(s.DB, s.Queries, s.Broadcaster, s.capable)
	s.svc = NewService(s.mgr, s.Queries, s.Broadcaster, testutil.NewMockBlockingRunner())
}

// plainSetup swaps in the undecorated mock, which implements neither optional
// interface — the ErrNotSupported path.
func (s *StopQueuedSuite) plainSetup() {
	s.mgr = NewManager(s.DB, s.Queries, s.Broadcaster, connectorAdapter{s.Connector})
	s.svc = NewService(s.mgr, s.Queries, s.Broadcaster, testutil.NewMockBlockingRunner())
}

func (s *StopQueuedSuite) startRunningSession() *Session {
	s.T().Helper()
	sess, err := s.mgr.Create(context.Background(), CreateParams{
		ProjectID: s.Project.ID,
		Name:      "stop-test",
		WorkDir:   s.T().TempDir(),
		Model:     "opus",
	})
	s.Require().NoError(err)
	s.Require().NoError(sess.Query(context.Background(), "work", nil))
	s.Require().Equal(StateRunning, sess.State())
	return sess
}

// advertiseCapability replays the init event the CLI sends on connect, which is
// the only place protocol capabilities are reported.
func (s *StopQueuedSuite) advertiseCapability(caps ...string) {
	s.T().Helper()
	s.Require().NoError(s.Connector.Last().Inject(runtime.SessionInitEvent{
		SessionID:    "cli-session",
		Model:        "claude-opus-5",
		Capabilities: caps,
	}))
}

func (s *StopQueuedSuite) waitFor(cond func() bool, msg string) {
	s.T().Helper()
	deadline := time.After(2 * time.Second)
	for !cond() {
		select {
		case <-deadline:
			s.Fail("timeout", msg)
			return
		default:
			time.Sleep(5 * time.Millisecond)
		}
	}
}

// --- Stop cancels the provider's queue -------------------------------------

// A user stop must ask the provider to drop queued work, not just abort the
// running turn — otherwise the next queued command starts the instant the turn
// aborts and the user watches work continue after pressing stop.
func (s *StopQueuedSuite) TestStopCancelsProviderQueue() {
	sess := s.startRunningSession()
	s.advertiseCapability(claudecli.CapabilityInterruptCancelQueued)
	s.waitFor(
		func() bool { return sess.pipeline.HasCLICapability(claudecli.CapabilityInterruptCancelQueued) },
		"init event never reached the pipeline",
	)

	cli := s.capable.Last()
	cli.mu.Lock()
	cli.receipt = &runtime.InterruptReceipt{Cancelled: []string{"cli-uuid-1", "cli-uuid-2"}}
	cli.mu.Unlock()

	s.Require().NoError(s.svc.InterruptSession(sess.ID))
	s.Equal([]bool{true}, cli.queuedInterrupts(), "stop must request cancelQueued")
}

// Without the capability the flag would be silently ignored, so agentique must
// not pass it — and must not then tell the UI anything was cancelled.
func (s *StopQueuedSuite) TestStopDoesNotClaimCancellationWithoutCapability() {
	sess := s.startRunningSession()
	s.advertiseCapability() // an older CLI: advertises nothing
	s.waitFor(func() bool { return sess.pipeline.ClaudeSessionID() != "" }, "init event never arrived")

	// A mid-turn message the CLI has queued but not yet echoed back.
	s.Require().NoError(sess.SendMessage("follow-up", nil))

	cli := s.capable.Last()
	s.Require().NoError(s.svc.InterruptSession(sess.ID))

	s.Equal([]bool{false}, cli.queuedInterrupts(),
		"a CLI that never advertised the capability must not be asked to cancel")
	s.Empty(s.cancelledMessageIDs(), "no cancellation may be claimed when the queue survives")
}

// The receipt's own report is a diagnostic, not something agentique reconciles
// against: it can name ids agentique never sent (cron triggers, auto-resume
// continuations). Unknown ids must not fail the stop.
func (s *StopQueuedSuite) TestStopIgnoresUnknownReceiptIDs() {
	sess := s.startRunningSession()
	s.advertiseCapability(claudecli.CapabilityInterruptCancelQueued)
	s.waitFor(
		func() bool { return sess.pipeline.HasCLICapability(claudecli.CapabilityInterruptCancelQueued) },
		"init event never reached the pipeline",
	)

	cli := s.capable.Last()
	cli.mu.Lock()
	cli.receipt = &runtime.InterruptReceipt{
		Cancelled:   []string{"a-cron-trigger-agentique-never-sent"},
		StillQueued: []string{"another-stranger"},
	}
	cli.mu.Unlock()

	s.Require().NoError(s.svc.InterruptSession(sess.ID))
}

// A mid-turn message the CLI accepted is waiting on a delivery confirmation
// that will now never come. Cancelling the queue must resolve its UI bubble
// instead of leaving it pending forever.
func (s *StopQueuedSuite) TestStopResolvesInFlightMessageBubbles() {
	sess := s.startRunningSession()
	s.advertiseCapability(claudecli.CapabilityInterruptCancelQueued)
	s.waitFor(
		func() bool { return sess.pipeline.HasCLICapability(claudecli.CapabilityInterruptCancelQueued) },
		"init event never reached the pipeline",
	)

	s.Require().NoError(sess.SendMessage("follow-up", nil))
	s.Require().Len(sess.pipeline.pendingMessageIDs, 1)
	queuedID := sess.pipeline.pendingMessageIDs[0]

	s.Require().NoError(s.svc.InterruptSession(sess.ID))
	s.Equal([]string{queuedID}, s.cancelledMessageIDs())
	s.Empty(sess.pipeline.pendingMessageIDs, "the replay-confirmation queue must be drained")
}

// --- Stop cancels agentique's own emulated queue ---------------------------

// Providers without native mid-turn send (codex) have their queue inside
// agentique: flushPendingMessages replays it on the very Idle transition the
// interrupt causes. Cancelling only the provider's queue would still let that
// one drain, which is the same bug one layer up.
func (s *StopQueuedSuite) TestStopDropsEmulatedQueue() {
	sess := s.startRunningSession()
	s.Require().True(sess.QueuePendingMessage("queued for next turn", nil))
	s.Require().Len(sess.pendingMessages, 1)
	queuedID := sess.pendingMessages[0].id

	s.Require().NoError(s.svc.InterruptSession(sess.ID))

	s.Empty(sess.pendingMessages, "stop must drop the emulated queue")
	s.Contains(s.cancelledMessageIDs(), queuedID)

	// Nothing replays it once the turn settles: the only Query is the original
	// prompt, never the message the stop dropped.
	s.Require().NoError(s.Connector.Last().Inject(testutil.ResultEvent(0)))
	s.waitFor(func() bool { return sess.State() == StateIdle }, "session never went idle")
	time.Sleep(50 * time.Millisecond)
	s.Equal([]string{"work"}, s.Connector.Last().Queries(),
		"no queued message may be replayed after a stop")
}

// A refused interrupt leaves the turn running, so the queue it would have
// flushed must survive — the user loses nothing to a failed stop.
func (s *StopQueuedSuite) TestFailedInterruptRestoresEmulatedQueue() {
	sess := s.startRunningSession()
	s.advertiseCapability(claudecli.CapabilityInterruptCancelQueued)
	s.waitFor(
		func() bool { return sess.pipeline.HasCLICapability(claudecli.CapabilityInterruptCancelQueued) },
		"init event never reached the pipeline",
	)
	s.Require().True(sess.QueuePendingMessage("survive me", nil))

	cli := s.capable.Last()
	cli.mu.Lock()
	cli.receiptErr = context.DeadlineExceeded
	cli.mu.Unlock()

	s.Error(s.svc.InterruptSession(sess.ID))
	s.Require().Len(sess.pendingMessages, 1, "a failed stop must not eat the queue")
	s.Equal("survive me", sess.pendingMessages[0].prompt)
	s.Empty(s.cancelledMessageIDs(), "nothing was cancelled, so nothing may be reported as such")
}

// --- Fallback --------------------------------------------------------------

// An adapter with no queued-interrupt support at all answers ErrNotSupported.
// The stop must still stop the turn, via a plain interrupt.
func (s *StopQueuedSuite) TestStopFallsBackWhenAdapterCannotCancelQueued() {
	s.plainSetup()
	sess := s.startRunningSession()

	mock := s.Connector.Last()
	s.Require().NoError(s.svc.InterruptSession(sess.ID))
	s.True(mock.Interrupted(), "fallback must still interrupt the running turn")
	s.Empty(s.cancelledMessageIDs(), "an unsupported adapter cancels nothing to report")
}

// The emulated queue is agentique's own, so it is dropped even when the
// provider cannot cancel its side — that half of the stop always works.
func (s *StopQueuedSuite) TestFallbackStillDropsEmulatedQueue() {
	s.plainSetup()
	sess := s.startRunningSession()
	s.Require().True(sess.QueuePendingMessage("queued", nil))

	s.Require().NoError(s.svc.InterruptSession(sess.ID))
	s.Empty(sess.pendingMessages)
}

// --- Helpers ---------------------------------------------------------------

// cancelledMessageIDs collects the messageIds broadcast as cancelled.
func (s *StopQueuedSuite) cancelledMessageIDs() []string {
	var out []string
	for _, msg := range s.Broadcaster.Messages() {
		push, ok := msg.Payload.(PushSessionEvent)
		if !ok {
			continue
		}
		ev, ok := push.Event.(WireMessageDeliveryEvent)
		if ok && ev.Status == "cancelled" {
			out = append(out, ev.MessageID)
		}
	}
	return out
}
