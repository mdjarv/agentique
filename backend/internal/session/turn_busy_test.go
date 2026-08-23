package session

import (
	"errors"
	"testing"

	"github.com/allbin/agentkit/runtime"
)

// The pipeline's turn-open flag guards OUTCOME ATTRIBUTION, not "is this
// machine busy" — that question is the runtime's (Session.TurnInFlight, backed
// by runtime.Session.TurnInFlight). What is tested here is the narrower
// agentique concern: a turn start must never advance the turn counter
// underneath a completion the pipeline has not processed yet, and a turn that
// will never complete must not make the next start wait out the full timeout.

func TestPipelineTurnClosesOnCompletion(t *testing.T) {
	p := newTestPipeline(newTestSink())

	p.AdvanceTurn()
	if !p.turnOpen {
		t.Fatal("a started turn is open for attribution")
	}

	p.ProcessEvent(runtime.TurnCompletedEvent{Status: runtime.TurnStatusCompleted})
	if p.turnOpen {
		t.Fatal("a processed completion closes the turn")
	}
}

func TestPipelineTurnStaysOpenAcrossAWorkflowPlaceholder(t *testing.T) {
	p := newTestPipeline(newTestSink())
	p.AdvanceTurn()

	// A dynamic workflow's launch placeholder is not the turn's answer — the
	// real completion comes later and owns the attribution.
	p.ProcessEvent(runtime.TurnCompletedEvent{Status: runtime.TurnStatusCompleted, WorkflowPending: true})
	if !p.turnOpen {
		t.Fatal("a pending workflow turn is still open")
	}
}

func TestPipelineFatalErrorClosesTheTurn(t *testing.T) {
	p := newTestPipeline(newTestSink())
	p.AdvanceTurn()

	// No TurnCompletedEvent will ever arrive; leaving it open would make the
	// next turn start burn WaitTurnClosed's whole timeout.
	p.ProcessEvent(runtime.ErrorEvent{Err: errors.New("cli died"), Fatal: true})
	if p.turnOpen {
		t.Fatal("a fatally-failed turn must not stay open")
	}
}

func TestCloseTurn_IsIdempotent(t *testing.T) {
	p := newTestPipeline(newTestSink())
	p.AdvanceTurn()

	p.CloseTurn()
	p.CloseTurn() // must not panic on the already-closed channel
	if p.turnOpen {
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
