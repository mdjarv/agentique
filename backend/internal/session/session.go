package session

import (
	"context"
	"fmt"
	"sync"

	claudecli "github.com/allbin/claudecli-go"
)

// Session state constants.
const (
	StateIdle    = "idle"
	StateRunning = "running"
	StateFailed  = "failed"
	StateDone    = "done"
	StateStopped = "stopped"
)

// Session wraps a single claudecli-go interactive session.
type Session struct {
	ID    string
	state string

	mu      sync.Mutex
	cliSess *claudecli.Session
	onEvent func(sessionID string, event any)
	onState func(sessionID string, state string)
}

func newSession(id string, cliSess *claudecli.Session, onEvent func(string, any), onState func(string, string)) *Session {
	s := &Session{
		ID:      id,
		state:   StateIdle,
		cliSess: cliSess,
		onEvent: onEvent,
		onState: onState,
	}
	onState(id, StateIdle)
	s.startEventLoop()
	return s
}

// State returns the current session state.
func (s *Session) State() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state
}

// SetCallbacks replaces the event and state callbacks so a new WebSocket
// connection can adopt (subscribe to) an existing live session.
func (s *Session) SetCallbacks(onEvent func(string, any), onState func(string, string)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onEvent = onEvent
	s.onState = onState
}

// Query sends a prompt to the Claude session and starts streaming events.
func (s *Session) Query(ctx context.Context, prompt string) error {
	s.mu.Lock()
	if s.state != StateIdle {
		st := s.state
		s.mu.Unlock()
		return fmt.Errorf("session is %s, not %s", st, StateIdle)
	}
	s.state = StateRunning
	s.mu.Unlock()

	s.onState(s.ID, StateRunning)

	if err := s.cliSess.Query(prompt); err != nil {
		s.setState(StateFailed)
		return err
	}

	return nil
}

// startEventLoop begins reading events from the claudecli-go session.
// Called once after session creation. The loop runs for the lifetime of the
// session, forwarding events and detecting turn boundaries via ResultEvent.
func (s *Session) startEventLoop() {
	go func() {
		for event := range s.cliSess.Events() {
			wireEvent := ToWireEvent(event)
			if wireEvent != nil {
				s.mu.Lock()
				cb := s.onEvent
				s.mu.Unlock()
				cb(s.ID, wireEvent)
			}

			// ResultEvent marks the end of a turn.
			if _, ok := event.(*claudecli.ResultEvent); ok {
				s.setState(StateIdle)
			}

			// Fatal error ends the session.
			if errEv, ok := event.(*claudecli.ErrorEvent); ok && errEv.Fatal {
				s.setState(StateFailed)
			}
		}

		// Channel closed means session process ended.
		s.setState(StateDone)
	}()
}

func (s *Session) setState(state string) {
	s.mu.Lock()
	s.state = state
	cb := s.onState
	s.mu.Unlock()
	cb(s.ID, state)
}

// Close gracefully shuts down the claudecli-go session.
func (s *Session) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cliSess != nil {
		s.cliSess.Close()
		s.cliSess = nil
	}
	s.state = StateDone
}
