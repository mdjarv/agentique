package session

import (
	"context"
	"fmt"

	"github.com/allbin/agentique/backend/internal/store"
)

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
// Any transition not listed here is rejected.
var validTransitions = map[State]map[State]bool{
	StateIdle:    {StateRunning: true, StateFailed: true, StateMerging: true, StateStopped: true, StateDone: true},
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

// validateTransition returns an error if the transition is not allowed.
func validateTransition(from, to State, sessionID string) error {
	if from.CanTransitionTo(to) {
		return nil
	}
	return fmt.Errorf("session %s: invalid state transition %s -> %s", sessionID, from, to)
}

// State returns the current session state.
func (s *Session) State() State {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state
}

func (s *Session) setState(state State) error {
	s.mu.Lock()
	if err := validateTransition(s.state, state, s.ID); err != nil {
		s.mu.Unlock()
		return err
	}
	s.state = state
	s.mu.Unlock()
	s.broadcastState(state)
	_ = s.queries.UpdateSessionState(context.Background(), store.UpdateSessionStateParams{
		State: string(state),
		ID:    s.ID,
	})
	return nil
}

// TryLockForMerge atomically transitions to StateMerging if the session is not running.
func (s *Session) TryLockForMerge() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state == StateRunning {
		return fmt.Errorf("session is running")
	}
	if !s.state.CanTransitionTo(StateMerging) {
		return fmt.Errorf("cannot merge from state %s", string(s.state))
	}
	s.state = StateMerging
	return nil
}

// UnlockMerge transitions back from StateMerging to newState.
func (s *Session) UnlockMerge(newState State) error {
	s.mu.Lock()
	if s.state != StateMerging {
		s.mu.Unlock()
		return fmt.Errorf("session %s: not in merging state", s.ID)
	}
	if err := validateTransition(s.state, newState, s.ID); err != nil {
		s.mu.Unlock()
		return err
	}
	s.state = newState
	s.mu.Unlock()
	return nil
}
