package voice

import (
	"testing"
	"time"
)

func TestPhaseChoosesTheIdleRule(t *testing.T) {
	base := 90 * time.Second
	if got := phaseGathering.idleTimeout(base); got != base {
		t.Errorf("gathering idle = %v, want the conversational timeout %v", got, base)
	}
	// Quiet while a run works is the expected state; the short rule would hang
	// up in the middle of every real task.
	if got := phaseWorking.idleTimeout(base); got != workingIdleCeiling {
		t.Errorf("working idle = %v, want the backstop %v", got, workingIdleCeiling)
	}
	if workingIdleCeiling <= base {
		t.Error("the working ceiling must be longer than the conversational timeout")
	}
}
