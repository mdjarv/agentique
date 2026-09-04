package session

import (
	"errors"
	"testing"
	"time"

	"github.com/allbin/agentkit/runtime"
)

// A fatal ErrorEvent means no TurnCompletedEvent will ever arrive, so the
// pipeline must synthesize a failure outcome for turn subscribers — the
// scheduler's waitForOutcome has no timeout, and before this a scheduled run
// whose turn died on a fatal API error stayed `running` forever.
func TestPipeline_FatalErrorDeliversAbortedOutcome(t *testing.T) {
	sink := newTestSink()
	var aborted []TurnOutcome
	p := newTestPipeline(sink, func(cfg *PipelineConfig) {
		cfg.OnTurnAborted = func(o TurnOutcome) { aborted = append(aborted, o) }
	})
	turn := p.AdvanceTurn()

	p.ProcessEvent(runtime.RateLimitEvent{Status: "rejected", ResetsAt: 4242})
	p.ProcessEvent(runtime.ErrorEvent{Err: errors.New("rate limit exceeded"), Fatal: true})

	if len(aborted) != 1 {
		t.Fatalf("expected 1 aborted outcome, got %d", len(aborted))
	}
	out := aborted[0]
	if out.TurnIndex != turn {
		t.Errorf("TurnIndex = %d, want %d", out.TurnIndex, turn)
	}
	if out.Status != runtime.TurnStatusFailed {
		t.Errorf("Status = %q, want %q", out.Status, runtime.TurnStatusFailed)
	}
	if out.ErrorKind != ErrorKindRateLimit {
		t.Errorf("ErrorKind = %q, want %q", out.ErrorKind, ErrorKindRateLimit)
	}
	if out.RateLimitResetsAt != 4242 {
		t.Errorf("RateLimitResetsAt = %d, want 4242", out.RateLimitResetsAt)
	}
	if out.FinalText != "rate limit exceeded" {
		t.Errorf("FinalText = %q, want the error message fallback", out.FinalText)
	}
	if !p.WaitTurnClosed(10 * time.Millisecond) {
		t.Error("turn should be closed after a fatal abort")
	}
}

// A fatal error landing after the turn already completed must not invent a
// second outcome: the completion was delivered, and the abort path is
// idempotent against it.
func TestPipeline_FatalErrorAfterCompletionDoesNotAbort(t *testing.T) {
	sink := newTestSink()
	abortCalled := false
	p := newTestPipeline(sink, func(cfg *PipelineConfig) {
		cfg.OnTurnAborted = func(TurnOutcome) { abortCalled = true }
	})
	p.AdvanceTurn()

	p.ProcessEvent(runtime.TurnCompletedEvent{Status: runtime.TurnStatusCompleted, Text: "done"})
	p.ProcessEvent(runtime.ErrorEvent{Err: errors.New("late boom"), Fatal: true})

	if abortCalled {
		t.Error("OnTurnAborted must not fire when the turn already completed")
	}
}

// A non-fatal error accumulates into the turn's error kind but must not
// close the turn or deliver anything.
func TestPipeline_NonFatalErrorDoesNotAbort(t *testing.T) {
	sink := newTestSink()
	abortCalled := false
	p := newTestPipeline(sink, func(cfg *PipelineConfig) {
		cfg.OnTurnAborted = func(TurnOutcome) { abortCalled = true }
	})
	p.AdvanceTurn()

	p.ProcessEvent(runtime.ErrorEvent{Err: errors.New("transient"), Fatal: false})

	if abortCalled {
		t.Error("OnTurnAborted must not fire for a non-fatal error")
	}
	if p.WaitTurnClosed(10 * time.Millisecond) {
		t.Error("turn should still be open after a non-fatal error")
	}
}

// The watchdog's fatal verdicts (CLI dead, thinking fail, tool-stall fail)
// come with a runtime StateFailed transition but no CLIEvent, so nothing on
// the pipeline path ever closed the turn: the next turn start burned
// WaitTurnClosed's full timeout and turn subscribers waited forever.
func TestWatchdogFatalAbortsPipelineTurn(t *testing.T) {
	sink := newTestSink()
	p := newTestPipeline(sink)
	turn := p.AdvanceTurn()

	s := &Session{ID: "s1", pipeline: p, turnReg: newTurnRegistry()}
	outcome := s.turnReg.Subscribe(turn)

	handleWatchdogEvent(s, runtime.WatchdogEvent{
		Kind:    runtime.WatchdogCLIDead,
		Message: "CLI process exited while a tool was running",
	})

	select {
	case out := <-outcome:
		if out.Status != runtime.TurnStatusFailed {
			t.Errorf("Status = %q, want %q", out.Status, runtime.TurnStatusFailed)
		}
		if out.SessionClosed {
			t.Error("a watchdog abort is a real failure, not a synthetic session close")
		}
		if out.FinalText != "CLI process exited while a tool was running" {
			t.Errorf("FinalText = %q, want the watchdog message", out.FinalText)
		}
	default:
		t.Fatal("fatal watchdog event did not deliver a turn outcome")
	}
	if !p.WaitTurnClosed(10 * time.Millisecond) {
		t.Error("pipeline turn should be closed after a fatal watchdog event")
	}
}

// A non-fatal watchdog warning leaves the turn alone.
func TestWatchdogWarningDoesNotAbortTurn(t *testing.T) {
	sink := newTestSink()
	p := newTestPipeline(sink)
	turn := p.AdvanceTurn()

	s := &Session{ID: "s1", pipeline: p, turnReg: newTurnRegistry()}
	outcome := s.turnReg.Subscribe(turn)

	handleWatchdogEvent(s, runtime.WatchdogEvent{
		Kind:    runtime.WatchdogThinkingWarn,
		Message: "session may be unresponsive",
	})

	select {
	case <-outcome:
		t.Fatal("a watchdog warning must not resolve the turn")
	default:
	}
	if p.WaitTurnClosed(10 * time.Millisecond) {
		t.Error("turn should still be open after a watchdog warning")
	}
}
