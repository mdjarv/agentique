package session

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/allbin/agentkit/runtime"
	"github.com/allbin/agentkit/sqliteops"
	"github.com/mdjarv/agentique/backend/internal/store"
)

// State represents a session lifecycle state. Mirrors the runtime states with
// the agentique-only StateMerging overlay used during git operations.
type State string

const (
	StateIdle    State = "idle"
	StateRunning State = "running"
	StateFailed  State = "failed"
	StateDone    State = "done"
	StateStopped State = "stopped"
	StateMerging State = "merging"
)

// validTransitions defines allowed state transitions for agentique-side
// (manual) transitions: setState, MarkDone, the merging dance. Runtime-driven
// transitions (Idle/Running/Failed/Done/Stopped) come through the broadcast
// hook and bypass validation since the runtime owns its own ordering.
var validTransitions = map[State]map[State]bool{
	StateIdle:    {StateRunning: true, StateFailed: true, StateMerging: true, StateStopped: true, StateDone: true},
	StateRunning: {StateIdle: true, StateFailed: true, StateDone: true},
	StateFailed:  {StateIdle: true, StateStopped: true, StateDone: true},
	StateDone:    {StateIdle: true, StateStopped: true},
	StateStopped: {StateIdle: true, StateDone: true},
	StateMerging: {StateIdle: true, StateFailed: true, StateDone: true, StateStopped: true},
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

// mapRuntimeState maps a runtime.State to the agentique-side State enum.
func mapRuntimeState(rt runtime.State) State {
	switch rt {
	case runtime.StateIdle:
		return StateIdle
	case runtime.StateRunning:
		return StateRunning
	case runtime.StateFailed:
		return StateFailed
	case runtime.StateDone:
		return StateDone
	case runtime.StateStopped:
		return StateStopped
	}
	return StateIdle
}

// State returns the current session state.
func (s *Session) State() State {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state
}

// setState validates the transition, persists the new state to the DB, updates
// the in-memory state, and broadcasts a session.state snapshot. Used by
// agentique-driven transitions (MarkDone, runtime broadcast hook, merging).
func (s *Session) setState(state State) error {
	s.mu.Lock()
	if err := validateTransition(s.state, state, s.ID); err != nil {
		s.mu.Unlock()
		return err
	}
	s.persistState(state)
	s.state = state
	s.mu.Unlock()

	select {
	case s.stateChangedCh <- struct{}{}:
	default:
	}
	s.broadcastState(state)
	return nil
}

func (s *Session) persistState(state State) {
	if err := sqliteops.RetryWrite(func() error {
		return s.queries.UpdateSessionState(context.Background(), store.UpdateSessionStateParams{
			State: string(state),
			ID:    s.ID,
		})
	}); err != nil {
		slog.Error("persist session state failed", "session_id", s.ID, "state", state, "error", err)
	}
}

// TryLockForGitOp atomically transitions to StateMerging if the session is not running
// and records which git operation is in progress (e.g. "rebasing", "merging", "creating_pr").
// Does NOT broadcast — callers should broadcast explicitly right before the long-running
// operation so that early validation failures don't leave the frontend stuck on "merging".
func (s *Session) TryLockForGitOp(operation string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state == StateRunning {
		return fmt.Errorf("session is running")
	}
	if !s.state.CanTransitionTo(StateMerging) {
		return fmt.Errorf("cannot start %s from state %s", operation, string(s.state))
	}
	s.state = StateMerging
	s.git.gitOperation = operation
	return nil
}

// UnlockGitOp transitions back from StateMerging to newState and clears the git operation.
func (s *Session) UnlockGitOp(newState State) error {
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
	s.git.gitOperation = ""
	s.mu.Unlock()
	return nil
}
