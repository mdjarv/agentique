package session

import (
	"errors"
	"testing"

	"github.com/allbin/agentkit/runtime"
)

// A restart is not a pause (docs/upgrades.md): anything that restarts the
// server asks the turn lifecycle first, and these are the answers it gets.

func TestTurnOpen_TracksTheTurnLifecycle(t *testing.T) {
	p := newTestPipeline(newTestSink())

	if p.TurnOpen() {
		t.Fatal("a fresh pipeline has no turn in flight")
	}

	p.AdvanceTurn()
	if !p.TurnOpen() {
		t.Fatal("a started turn is in flight")
	}

	p.ProcessEvent(runtime.TurnCompletedEvent{Status: runtime.TurnStatusCompleted})
	if p.TurnOpen() {
		t.Fatal("a completed turn is not in flight")
	}
}

func TestTurnOpen_FatalErrorEndsTheTurn(t *testing.T) {
	p := newTestPipeline(newTestSink())
	p.AdvanceTurn()

	// A fatal error means no TurnCompletedEvent will ever arrive. Leaving the
	// turn open would make the machine look permanently busy.
	p.ProcessEvent(runtime.ErrorEvent{Err: errors.New("cli died"), Fatal: true})
	if p.TurnOpen() {
		t.Fatal("a fatally-failed turn must not read as in flight")
	}
}

func TestTurnOpen_WorkflowPendingKeepsTheTurnOpen(t *testing.T) {
	p := newTestPipeline(newTestSink())
	p.AdvanceTurn()

	// A dynamic workflow's placeholder completion is not the end of the turn —
	// the agent is still working, so a restart would still cost it.
	p.ProcessEvent(runtime.TurnCompletedEvent{Status: runtime.TurnStatusCompleted, WorkflowPending: true})
	if !p.TurnOpen() {
		t.Fatal("a pending workflow turn is still in flight")
	}
}

func TestCloseTurn_IsIdempotent(t *testing.T) {
	p := newTestPipeline(newTestSink())
	p.AdvanceTurn()

	p.CloseTurn()
	p.CloseTurn() // a second close must not panic on the already-closed channel
	if p.TurnOpen() {
		t.Fatal("turn should be closed")
	}
}

func TestCloseTurn_ReleasesWaiters(t *testing.T) {
	p := newTestPipeline(newTestSink())
	p.AdvanceTurn()

	done := make(chan bool, 1)
	go func() { done <- p.WaitTurnClosed(5e9) }()

	p.CloseTurn()
	if ok := <-done; !ok {
		t.Fatal("CloseTurn must release WaitTurnClosed rather than let it time out")
	}
}
