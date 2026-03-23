package session

import "log"

// State represents a session lifecycle state.
type State string

const (
	StateIdle    State = "idle"
	StateRunning State = "running"
	StateFailed  State = "failed"
	StateDone    State = "done"
	StateStopped State = "stopped"
	StateMerging State = "merging"
)

// validTransitions defines allowed state transitions.
// Any transition not listed here is invalid (logged but not blocked).
var validTransitions = map[State]map[State]bool{
	StateIdle:    {StateRunning: true, StateMerging: true, StateStopped: true, StateDone: true},
	StateRunning: {StateIdle: true, StateFailed: true, StateDone: true},
	StateFailed:  {StateIdle: true, StateStopped: true, StateDone: true},
	StateDone:    {StateIdle: true, StateStopped: true},
	StateStopped: {StateDone: true},
	StateMerging: {StateIdle: true, StateFailed: true, StateDone: true},
}

// CanTransitionTo returns true if transitioning from s to next is valid.
func (s State) CanTransitionTo(next State) bool {
	allowed, ok := validTransitions[s]
	if !ok {
		return false
	}
	return allowed[next]
}

// validateTransition logs a warning if the transition is invalid.
// Returns true if valid.
func validateTransition(from, to State, sessionID string) bool {
	if from.CanTransitionTo(to) {
		return true
	}
	log.Printf("session %s: invalid state transition %s -> %s", sessionID, from, to)
	return false
}
