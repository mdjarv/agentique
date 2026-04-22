package session

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	claudecli "github.com/allbin/claudecli-go"
	"github.com/mdjarv/agentique/backend/internal/testutil"
	"github.com/stretchr/testify/suite"
)

// --- Pure unit tests for trackActivity ---

func TestTrackActivity_TopLevelToolUseAddsInFlight(t *testing.T) {
	s := newTestSession()
	defer s.cancelCtx()

	next := s.trackActivity(&claudecli.ToolUseEvent{ID: "t1", Name: "Bash"})

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.inFlightTools["t1"]; !ok {
		t.Fatalf("expected t1 tracked as in-flight")
	}
	if next != toolLivenessInterval {
		t.Fatalf("expected toolLivenessInterval, got %s", next)
	}
}

func TestTrackActivity_SubagentToolUseIgnored(t *testing.T) {
	s := newTestSession()
	defer s.cancelCtx()

	next := s.trackActivity(&claudecli.ToolUseEvent{ID: "t1", Name: "Grep", ParentToolUseID: "parent-agent"})

	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.inFlightTools) != 0 {
		t.Fatalf("subagent tool use should not be tracked, got %d entries", len(s.inFlightTools))
	}
	if next != thinkingWarnAfter {
		t.Fatalf("expected thinkingWarnAfter (no tools in flight), got %s", next)
	}
}

func TestTrackActivity_ToolResultEventRemovesInFlight(t *testing.T) {
	s := newTestSession()
	defer s.cancelCtx()

	s.trackActivity(&claudecli.ToolUseEvent{ID: "t1", Name: "Bash"})
	s.trackActivity(&claudecli.ToolUseEvent{ID: "t2", Name: "Read"})
	next := s.trackActivity(&claudecli.ToolResultEvent{ToolUseID: "t1"})

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.inFlightTools["t1"]; ok {
		t.Fatalf("t1 should be removed after tool_result")
	}
	if _, ok := s.inFlightTools["t2"]; !ok {
		t.Fatalf("t2 should remain in-flight")
	}
	if next != toolLivenessInterval {
		t.Fatalf("expected toolLivenessInterval (t2 still in flight), got %s", next)
	}
}

func TestTrackActivity_UserEventToolResultRemovesInFlight(t *testing.T) {
	s := newTestSession()
	defer s.cancelCtx()

	s.trackActivity(&claudecli.ToolUseEvent{ID: "t1", Name: "Bash"})
	next := s.trackActivity(&claudecli.UserEvent{
		Content: []claudecli.UserContent{
			{Type: "tool_result", ToolUseID: "t1"},
		},
	})

	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.inFlightTools) != 0 {
		t.Fatalf("expected all tools cleared, got %d", len(s.inFlightTools))
	}
	if next != thinkingWarnAfter {
		t.Fatalf("expected thinkingWarnAfter, got %s", next)
	}
}

func TestTrackActivity_ResultEventClearsInFlight(t *testing.T) {
	s := newTestSession()
	defer s.cancelCtx()

	s.trackActivity(&claudecli.ToolUseEvent{ID: "t1", Name: "Bash"})
	s.trackActivity(&claudecli.ToolUseEvent{ID: "t2", Name: "Read"})
	next := s.trackActivity(&claudecli.ResultEvent{StopReason: "end_turn"})

	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.inFlightTools) != 0 {
		t.Fatalf("ResultEvent should clear all in-flight tools, got %d", len(s.inFlightTools))
	}
	if next != thinkingWarnAfter {
		t.Fatalf("expected thinkingWarnAfter after result, got %s", next)
	}
}

func TestTrackActivity_TextEventNoop(t *testing.T) {
	s := newTestSession()
	defer s.cancelCtx()

	s.trackActivity(&claudecli.ToolUseEvent{ID: "t1", Name: "Bash"})
	next := s.trackActivity(&claudecli.TextEvent{Content: "thinking..."})

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.inFlightTools["t1"]; !ok {
		t.Fatalf("TextEvent must not clear in-flight tools")
	}
	if next != toolLivenessInterval {
		t.Fatalf("expected toolLivenessInterval (t1 still in flight), got %s", next)
	}
}

// newTestSession returns a minimally-initialized Session suitable for unit tests
// that exercise trackActivity. It does NOT start the event loop.
func newTestSession() *Session {
	ctx, cancel := context.WithCancel(context.Background())
	return &Session{
		ID:             "unit-test",
		ctx:            ctx,
		cancelCtx:      cancel,
		state:          StateIdle,
		inFlightTools:  make(map[string]inFlightTool),
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

	origThinkingWarn    time.Duration
	origThinkingFail    time.Duration
	origToolLiveness    time.Duration
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
	thinkingWarnAfter = 100 * time.Millisecond
	thinkingFailAfter = 300 * time.Millisecond
	toolLivenessInterval = 50 * time.Millisecond
}

func (s *WatchdogSuite) TearDownTest() {
	thinkingWarnAfter = s.origThinkingWarn
	thinkingFailAfter = s.origThinkingFail
	toolLivenessInterval = s.origToolLiveness
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

// countUnresponsiveWarnings returns how many "may be unresponsive" broadcasts fired.
func (s *WatchdogSuite) countUnresponsiveWarnings() int {
	msgs := s.Broadcaster.MessagesOfType("session.event")
	count := 0
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
		if probe.Event.Content == "session may be unresponsive — no activity for 2 minutes" {
			count++
		}
	}
	return count
}

// TestThinkingStateFailsOnSilence verifies the original watchdog behavior still works:
// when no tools are in flight and the CLI goes silent, warn fires then session fails.
func (s *WatchdogSuite) TestThinkingStateFailsOnSilence() {
	sess := s.createSession()
	s.Require().NoError(sess.Query(context.Background(), "hello", nil))

	// No events at all — pure Thinking silence.
	s.Eventually(func() bool {
		return s.countUnresponsiveWarnings() > 0
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
	s.Require().NoError(mock.Inject(testutil.ToolUseEvent("t1", "Bash", map[string]string{"cmd": "sleep 9999"})))

	// Wait for well past all Thinking thresholds (warn=100ms, fail=300ms).
	time.Sleep(600 * time.Millisecond)

	s.Equal(StateRunning, sess.State(), "session should remain running during long tool")
	s.Equal(0, s.countUnresponsiveWarnings(), "no unresponsive warning while tool in flight")
}

// TestToolExecutingFailsWhenCLIDies verifies the liveness cross-check: if the
// CLI process dies while a tool is in flight, the watchdog detects it and
// fails the session rather than waiting forever.
func (s *WatchdogSuite) TestToolExecutingFailsWhenCLIDies() {
	sess := s.createSession()
	s.Require().NoError(sess.Query(context.Background(), "run a tool", nil))

	mock := s.Connector.Last()
	s.Require().NoError(mock.Inject(testutil.ToolUseEvent("t1", "Bash", map[string]string{"cmd": "x"})))

	// Simulate process death without delivering a tool_result or closing events.
	mock.SetCLIState(claudecli.StateFailed)

	s.Eventually(func() bool {
		return sess.State() == StateFailed
	}, 2*time.Second, 10*time.Millisecond, "expected session to fail when CLI dies mid-tool")
}

// TestToolCompletionResumesThinkingTimeout ensures that once a tool finishes,
// the watchdog goes back to event-silence timing for the Thinking state.
func (s *WatchdogSuite) TestToolCompletionResumesThinkingTimeout() {
	sess := s.createSession()
	s.Require().NoError(sess.Query(context.Background(), "run then think", nil))

	mock := s.Connector.Last()
	s.Require().NoError(mock.Inject(testutil.ToolUseEvent("t1", "Bash", map[string]string{"cmd": "x"})))
	time.Sleep(400 * time.Millisecond) // past thinkingFailAfter, but tool in flight

	s.Equal(StateRunning, sess.State())
	s.Equal(0, s.countUnresponsiveWarnings())

	// Tool completes; model is now Thinking again. Further silence should trip the warn.
	s.Require().NoError(mock.Inject(testutil.ToolResultEvent("t1", "ok")))

	s.Eventually(func() bool {
		return s.countUnresponsiveWarnings() > 0
	}, 2*time.Second, 10*time.Millisecond, "warn should fire once back to Thinking")
}
