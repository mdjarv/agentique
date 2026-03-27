package session

import "testing"

func TestValidTransitions(t *testing.T) {
	valid := []struct {
		from, to State
	}{
		// From idle
		{StateIdle, StateRunning},
		{StateIdle, StateFailed},
		{StateIdle, StateMerging},
		{StateIdle, StateStopped},
		{StateIdle, StateDone},
		// From running
		{StateRunning, StateIdle},
		{StateRunning, StateFailed},
		{StateRunning, StateDone},
		// From failed
		{StateFailed, StateIdle},
		{StateFailed, StateStopped},
		{StateFailed, StateDone},
		// From done
		{StateDone, StateIdle},
		{StateDone, StateStopped},
		// From stopped
		{StateStopped, StateDone},
		// From merging
		{StateMerging, StateIdle},
		{StateMerging, StateFailed},
		{StateMerging, StateDone},
		{StateMerging, StateStopped},
	}

	for _, tc := range valid {
		if !tc.from.CanTransitionTo(tc.to) {
			t.Errorf("%s -> %s should be valid", tc.from, tc.to)
		}
	}
}

func TestInvalidTransitions(t *testing.T) {
	invalid := []struct {
		from, to State
	}{
		// Running cannot go to merging or stopped directly
		{StateRunning, StateMerging},
		{StateRunning, StateStopped},
		// Done cannot go to running, failed, or merging
		{StateDone, StateRunning},
		{StateDone, StateFailed},
		{StateDone, StateMerging},
		// Stopped is terminal except for done
		{StateStopped, StateIdle},
		{StateStopped, StateRunning},
		{StateStopped, StateFailed},
		{StateStopped, StateMerging},
		// Failed cannot go to running or merging
		{StateFailed, StateRunning},
		{StateFailed, StateMerging},
	}

	for _, tc := range invalid {
		if tc.from.CanTransitionTo(tc.to) {
			t.Errorf("%s -> %s should be invalid", tc.from, tc.to)
		}
	}
}

func TestValidateTransition(t *testing.T) {
	if err := validateTransition(StateIdle, StateRunning, "test"); err != nil {
		t.Errorf("expected nil, got %v", err)
	}
	if err := validateTransition(StateStopped, StateRunning, "test"); err == nil {
		t.Error("expected error for stopped -> running")
	}
}
