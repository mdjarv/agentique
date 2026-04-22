package session

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	claudecli "github.com/allbin/claudecli-go"
	"github.com/mdjarv/agentique/backend/internal/testutil"
	"github.com/stretchr/testify/suite"
)

// --- Pure unit tests for observeActivity ---

func TestObserveActivity_TransitionToAwaitingToolResult(t *testing.T) {
	s := newTestSession()
	defer s.cancelCtx()

	next := s.observeActivity(&claudecli.CLIStateChangeEvent{State: claudecli.ActivityAwaitingToolResult})

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.activityState != claudecli.ActivityAwaitingToolResult {
		t.Fatalf("expected AwaitingToolResult, got %s", s.activityState)
	}
	if next != toolLivenessInterval {
		t.Fatalf("expected toolLivenessInterval, got %s", next)
	}
}

func TestObserveActivity_TransitionBackToThinking(t *testing.T) {
	s := newTestSession()
	defer s.cancelCtx()

	s.observeActivity(&claudecli.CLIStateChangeEvent{State: claudecli.ActivityAwaitingToolResult})
	next := s.observeActivity(&claudecli.CLIStateChangeEvent{State: claudecli.ActivityThinking})

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.activityState != claudecli.ActivityThinking {
		t.Fatalf("expected Thinking, got %s", s.activityState)
	}
	if next != thinkingWarnAfter {
		t.Fatalf("expected thinkingWarnAfter, got %s", next)
	}
}

func TestObserveActivity_TransitionResetsStallWarning(t *testing.T) {
	s := newTestSession()
	defer s.cancelCtx()

	s.mu.Lock()
	s.toolStallWarned = true
	s.mu.Unlock()

	s.observeActivity(&claudecli.CLIStateChangeEvent{State: claudecli.ActivityAwaitingToolResult})

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.toolStallWarned {
		t.Fatal("toolStallWarned should reset on activity state change")
	}
}

func TestObserveActivity_NonStateEventUsesCurrentState(t *testing.T) {
	s := newTestSession()
	defer s.cancelCtx()

	s.observeActivity(&claudecli.CLIStateChangeEvent{State: claudecli.ActivityAwaitingToolResult})
	next := s.observeActivity(&claudecli.TextEvent{Content: "noise"})

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.activityState != claudecli.ActivityAwaitingToolResult {
		t.Fatalf("activity state must not change on non-state events, got %s", s.activityState)
	}
	if next != toolLivenessInterval {
		t.Fatalf("expected interval to match current state, got %s", next)
	}
}

// newTestSession returns a minimally-initialized Session suitable for unit tests
// that exercise activity tracking. Does NOT start the event loop.
func newTestSession() *Session {
	ctx, cancel := context.WithCancel(context.Background())
	return &Session{
		ID:             "unit-test",
		ctx:            ctx,
		cancelCtx:      cancel,
		state:          StateIdle,
		activityState:  claudecli.ActivityIdle,
		approvalState:  newApprovalState(),
		eventLoopDone:  make(chan struct{}),
		stateChangedCh: make(chan struct{}, 1),
		broadcast:      func(string, any) {},
	}
}

// --- Integration tests for the watchdog loop ---

type WatchdogSuite struct {
	testutil.DBSuite
	mgr *Manager
	svc *Service

	origThinkingWarn time.Duration
	origThinkingFail time.Duration
	origToolLiveness time.Duration
	origToolStall    time.Duration
}

func TestWatchdogSuite(t *testing.T) {
	suite.Run(t, new(WatchdogSuite))
}

func (s *WatchdogSuite) SetupTest() {
	s.DBSuite.SetupTest()
	s.mgr = NewManager(s.DB, s.Queries, s.Broadcaster, connectorAdapter{s.Connector})
	s.svc = NewService(s.mgr, s.Queries, s.Broadcaster, testutil.NewMockBlockingRunner())

	s.origThinkingWarn = thinkingWarnAfter
	s.origThinkingFail = thinkingFailAfter
	s.origToolLiveness = toolLivenessInterval
	s.origToolStall = toolStallWarnAfter
	thinkingWarnAfter = 100 * time.Millisecond
	thinkingFailAfter = 300 * time.Millisecond
	toolLivenessInterval = 50 * time.Millisecond
	toolStallWarnAfter = 200 * time.Millisecond
}

func (s *WatchdogSuite) TearDownTest() {
	thinkingWarnAfter = s.origThinkingWarn
	thinkingFailAfter = s.origThinkingFail
	toolLivenessInterval = s.origToolLiveness
	toolStallWarnAfter = s.origToolStall
}

func (s *WatchdogSuite) createSession() *Session {
	sess, err := s.mgr.Create(context.Background(), CreateParams{
		ProjectID: s.Project.ID,
		Name:      "watchdog-test",
		WorkDir:   s.T().TempDir(),
		Model:     "opus",
	})
	s.Require().NoError(err)
	return sess
}

// broadcastContent extracts the content field from captured session.event broadcasts.
func (s *WatchdogSuite) broadcastContent() []string {
	msgs := s.Broadcaster.MessagesOfType("session.event")
	out := make([]string, 0, len(msgs))
	for _, m := range msgs {
		raw, _ := json.Marshal(m.Payload)
		var probe struct {
			Event struct {
				Content string `json:"content"`
			} `json:"event"`
		}
		if err := json.Unmarshal(raw, &probe); err != nil {
			continue
		}
		if probe.Event.Content != "" {
			out = append(out, probe.Event.Content)
		}
	}
	return out
}

func (s *WatchdogSuite) countMatching(substr string) int {
	n := 0
	for _, c := range s.broadcastContent() {
		if strings.Contains(c, substr) {
			n++
		}
	}
	return n
}

// TestThinkingStateFailsOnSilence verifies the original watchdog behavior:
// when no tool is in flight and the CLI goes silent, warn fires then session fails.
func (s *WatchdogSuite) TestThinkingStateFailsOnSilence() {
	sess := s.createSession()
	s.Require().NoError(sess.Query(context.Background(), "hello", nil))
	mock := s.Connector.Last()
	// Drive activity to Thinking so the watchdog uses event-silence timing.
	s.Require().NoError(mock.Inject(&claudecli.CLIStateChangeEvent{State: claudecli.ActivityThinking}))

	s.Eventually(func() bool {
		return s.countMatching("may be unresponsive") > 0
	}, 2*time.Second, 10*time.Millisecond, "expected warn broadcast")

	s.Eventually(func() bool {
		return sess.State() == StateFailed
	}, 2*time.Second, 10*time.Millisecond, "expected session failure after silence")
}

// TestToolExecutingSurvivesSilence is the regression test for the reported issue:
// a long tool call with no events must NOT trigger the 2-minute warning or the
// 5-minute failure. Session stays healthy as long as the CLI process is alive.
func (s *WatchdogSuite) TestToolExecutingSurvivesSilence() {
	sess := s.createSession()
	s.Require().NoError(sess.Query(context.Background(), "run reviewbot", nil))

	mock := s.Connector.Last()
	s.Require().NoError(mock.Inject(&claudecli.CLIStateChangeEvent{State: claudecli.ActivityAwaitingToolResult}))
	s.Require().NoError(mock.Inject(testutil.ToolUseEvent("t1", "Bash", map[string]string{"cmd": "sleep 9999"})))

	// Stdout has just been written (mock defaults to time.Now()), so no stall warning.
	time.Sleep(600 * time.Millisecond) // well past thinkingFailAfter

	s.Equal(StateRunning, sess.State(), "session should remain running during long tool")
	s.Equal(0, s.countMatching("may be unresponsive"))
}

// TestToolExecutingFailsWhenCLIDies: if the CLI process dies mid-tool, watchdog
// detects it via State() and fails the session rather than waiting forever.
func (s *WatchdogSuite) TestToolExecutingFailsWhenCLIDies() {
	sess := s.createSession()
	s.Require().NoError(sess.Query(context.Background(), "run a tool", nil))

	mock := s.Connector.Last()
	s.Require().NoError(mock.Inject(&claudecli.CLIStateChangeEvent{State: claudecli.ActivityAwaitingToolResult}))
	s.Require().NoError(mock.Inject(testutil.ToolUseEvent("t1", "Bash", map[string]string{"cmd": "x"})))

	mock.SetCLIState(claudecli.StateFailed)

	s.Eventually(func() bool {
		return sess.State() == StateFailed
	}, 2*time.Second, 10*time.Millisecond, "expected session to fail when CLI dies mid-tool")
}

// TestStdoutStallEmitsInfoWarning: if the CLI process is alive but stdout has
// been silent longer than toolStallWarnAfter during AwaitingToolResult, emit a
// non-fatal informational broadcast. Session stays Running.
func (s *WatchdogSuite) TestStdoutStallEmitsInfoWarning() {
	sess := s.createSession()
	s.Require().NoError(sess.Query(context.Background(), "slow tool", nil))

	mock := s.Connector.Last()
	s.Require().NoError(mock.Inject(&claudecli.CLIStateChangeEvent{State: claudecli.ActivityAwaitingToolResult}))
	s.Require().NoError(mock.Inject(testutil.ToolUseEvent("t1", "Bash", nil)))

	// Simulate long stdout silence.
	mock.SetLastStdoutAt(time.Now().Add(-5 * time.Second))

	s.Eventually(func() bool {
		return s.countMatching("stdout silent") > 0
	}, 2*time.Second, 10*time.Millisecond, "expected stdout-stall warning")

	s.Equal(StateRunning, sess.State(), "stdout stall is informational, not fatal")

	// Should only fire once per stall episode, not on every tick.
	time.Sleep(200 * time.Millisecond)
	s.Equal(1, s.countMatching("stdout silent"), "stdout-stall warning must not repeat")
}

// TestToolCompletionResumesThinkingTimeout: when the CLI returns to Thinking,
// event-silence timing kicks back in and a genuine stall fails the session.
func (s *WatchdogSuite) TestToolCompletionResumesThinkingTimeout() {
	sess := s.createSession()
	s.Require().NoError(sess.Query(context.Background(), "run then think", nil))

	mock := s.Connector.Last()
	s.Require().NoError(mock.Inject(&claudecli.CLIStateChangeEvent{State: claudecli.ActivityAwaitingToolResult}))
	s.Require().NoError(mock.Inject(testutil.ToolUseEvent("t1", "Bash", nil)))
	time.Sleep(400 * time.Millisecond) // past thinkingFailAfter, but tool still running

	s.Equal(StateRunning, sess.State())
	s.Equal(0, s.countMatching("may be unresponsive"))

	// Tool completes. CLI transitions back to Thinking.
	s.Require().NoError(mock.Inject(&claudecli.CLIStateChangeEvent{State: claudecli.ActivityThinking}))
	s.Require().NoError(mock.Inject(testutil.ToolResultEvent("t1", "ok")))

	s.Eventually(func() bool {
		return s.countMatching("may be unresponsive") > 0
	}, 2*time.Second, 10*time.Millisecond, "warn should fire once back to Thinking")
}
