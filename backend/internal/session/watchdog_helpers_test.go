package session

import (
	"sync/atomic"
	"testing"
	"time"

	claudecli "github.com/allbin/claudecli-go"
)

// These tests exercise the watchdog helpers extracted from startEventLoop.
// They use minimal hand-rolled Session structs (bypassing Manager.Create) so
// every helper branch can be hit deterministically without timer-driven
// flakiness or DB plumbing.

// --- handleWatchdogTick: routing layer ---

func TestHandleWatchdogTick_ReArmsWhenStateIsNotRunning(t *testing.T) {
	for _, st := range []State{StateIdle, StateMerging, StateStopped, StateDone, StateFailed} {
		s := &Session{state: st, broadcast: noopBroadcast}
		next, warned, terminate := s.handleWatchdogTick("evt", time.Now(), false)
		if terminate {
			t.Errorf("state %s: should not terminate", st)
		}
		if next != thinkingWarnAfter {
			t.Errorf("state %s: expected thinkingWarnAfter, got %v", st, next)
		}
		if warned {
			t.Errorf("state %s: warned should be unchanged (false)", st)
		}
	}
}

func TestHandleWatchdogTick_ReArmsWhenWaitingForUserApproval(t *testing.T) {
	s := &Session{
		state:     StateRunning,
		broadcast: noopBroadcast,
	}
	s.pendingApprovals = map[string]*pendingApproval{
		"a1": {ch: make(chan *claudecli.PermissionResponse, 1)},
	}

	next, warned, terminate := s.handleWatchdogTick("evt", time.Now(), false)
	if terminate {
		t.Error("waiting for user: should not terminate")
	}
	if next != thinkingWarnAfter {
		t.Errorf("waiting for user: expected thinkingWarnAfter, got %v", next)
	}
	_ = warned
}

func TestHandleWatchdogTick_ReArmsWhenWaitingForUserQuestion(t *testing.T) {
	s := &Session{
		state:     StateRunning,
		broadcast: noopBroadcast,
	}
	s.pendingQuestions = map[string]*pendingQuestion{
		"q1": {ch: make(chan map[string]string, 1)},
	}

	next, _, terminate := s.handleWatchdogTick("evt", time.Now(), false)
	if terminate {
		t.Error("waiting for user: should not terminate")
	}
	if next != thinkingWarnAfter {
		t.Errorf("waiting for user: expected thinkingWarnAfter, got %v", next)
	}
}

func TestHandleWatchdogTick_RoutesToThinkingForIdleActivity(t *testing.T) {
	// StateRunning + ActivityIdle (no tool) + recent event time → thinking
	// path returns the remaining-window re-arm.
	s := &Session{
		state:         StateRunning,
		activityState: claudecli.ActivityIdle,
		broadcast:     noopBroadcast,
	}
	last := time.Now() // very recent — within the warn window
	next, warned, terminate := s.handleWatchdogTick("evt", last, false)
	if terminate {
		t.Error("recent event: should not terminate")
	}
	if next <= 0 || next > thinkingWarnAfter {
		t.Errorf("recent event: expected re-arm within (0, thinkingWarnAfter], got %v", next)
	}
	if warned {
		t.Error("recent event: warned should remain false")
	}
}

// --- handleThinkingWatchdog: state machine ---

func TestHandleThinkingWatchdog_UnderWindowReArmsForRemainder(t *testing.T) {
	s := &Session{ID: "x", broadcast: noopBroadcast}
	last := time.Now().Add(-thinkingWarnAfter / 2)
	next, warned, terminate := s.handleThinkingWatchdog("evt", last, false)

	if terminate {
		t.Error("under window: should not terminate")
	}
	if warned {
		t.Error("under window: warned should remain false")
	}
	if next <= 0 || next > thinkingWarnAfter {
		t.Errorf("under window: expected re-arm in (0, thinkingWarnAfter], got %v", next)
	}
}

func TestHandleThinkingWatchdog_FirstHitEmitsWarnAndReturnsTrue(t *testing.T) {
	var broadcasts atomic.Int32
	s := &Session{
		ID:        "x",
		broadcast: func(_ string, _ any) { broadcasts.Add(1) },
	}
	last := time.Now().Add(-thinkingWarnAfter - time.Second)

	next, warned, terminate := s.handleThinkingWatchdog("evt", last, false)

	if terminate {
		t.Error("warn path: should not terminate yet")
	}
	if !warned {
		t.Error("warn path: warned should flip to true")
	}
	if got := broadcasts.Load(); got != 1 {
		t.Errorf("warn path: expected 1 broadcast, got %d", got)
	}
	expected := thinkingFailAfter - thinkingWarnAfter
	if next != expected {
		t.Errorf("warn path: expected re-arm = %v (failAfter - warnAfter), got %v", expected, next)
	}
}

func TestHandleThinkingWatchdog_SubsequentTickAfterWarnIsIdempotent(t *testing.T) {
	// Second tick within the same stall episode (warned already true). The
	// failure path will run setState(Failed) which needs queries, so we
	// only verify that the warn-broadcast does NOT fire a second time when
	// warned=true and we're past the warn window but before fail.
	var broadcasts atomic.Int32
	s := &Session{
		ID:        "x",
		broadcast: func(_ string, _ any) { broadcasts.Add(1) },
	}
	// Past warn window but cap how far past so we exercise warned=true case.
	// Using exactly thinkingWarnAfter+1ms means sinceLastEvent passes the warn
	// gate; warned=true skips the broadcast and falls through to the
	// "watchdog timeout" failure branch.
	last := time.Now().Add(-thinkingWarnAfter - time.Millisecond)

	// Caller passes warned=true → skip warn broadcast, proceed to fail.
	// We expect terminate=true. setState would be invoked but with no
	// queries on the Session it returns an error that's logged, not panicked.
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("setState panicked unexpectedly: %v", r)
		}
	}()
	_, _, terminate := s.handleThinkingWatchdog("evt", last, true)

	if !terminate {
		t.Error("warned=true past window: should terminate")
	}
	if got := broadcasts.Load(); got != 0 {
		t.Errorf("warned=true: expected NO additional broadcast, got %d", got)
	}
}

// --- handleEventChannelClose: terminal-state decision ---

func TestHandleEventChannelClose_NoOpForTerminalStates(t *testing.T) {
	for _, st := range []State{StateDone, StateFailed, StateStopped} {
		var broadcasts atomic.Int32
		s := &Session{
			ID:        "x",
			state:     st,
			broadcast: func(_ string, _ any) { broadcasts.Add(1) },
		}
		s.handleEventChannelClose(nil)

		if s.state != st {
			t.Errorf("state %s: should not change, got %s", st, s.state)
		}
		if got := broadcasts.Load(); got != 0 {
			t.Errorf("state %s: terminal close should not broadcast, got %d", st, got)
		}
	}
}

// --- helpers ---

func noopBroadcast(_ string, _ any) {}
