package session

import (
	"sync"
	"time"

	"github.com/allbin/agentkit/runtime"
)

// Error kinds classified from a turn's error/rate-limit events, in increasing
// specificity. The pipeline keeps the most specific kind seen during a turn;
// consumers (the scheduler's deferred/error split) treat rate_limit and
// overloaded as transient, context and other as real failures.
const (
	ErrorKindRateLimit  = "rate_limit"
	ErrorKindOverloaded = "overloaded"
	ErrorKindContext    = "context"
	ErrorKindOther      = "other"
)

// QueryOrigin identifies a turn's initiator. The zero value means a human
// (composer) turn. Schedule-origin turns are tagged on the persisted prompt
// row and the turn-started push so the timeline can render them as scheduled
// runs, and they skip brain recall injection — every fire on an evicted
// session would otherwise re-inject the same facts and inflate their `uses`
// counters with no corresponding outcome signal.
type QueryOrigin struct {
	Kind         string `json:"kind"` // "" (user) | "schedule"
	ScheduleID   string `json:"scheduleId,omitempty"`
	RunID        string `json:"runId,omitempty"`
	ScheduleName string `json:"scheduleName,omitempty"`
}

// TurnOutcome is the completion payload delivered to turn subscribers: the
// terminal fact of one turn, identified by its persisted turn index.
type TurnOutcome struct {
	TurnIndex int
	Status    runtime.TurnStatus
	// FinalText is the turn's final assistant text: TurnCompletedEvent.Text
	// when the provider populates it (claude), else the last top-level
	// AssistantTextEvent accumulated by the pipeline (codex — its adapter
	// leaves Text empty).
	FinalText string
	// ErrorKind classifies the strongest error signal observed during the
	// turn ("" when none): one of the ErrorKind* constants.
	ErrorKind     string
	Duration      time.Duration
	Usage         runtime.TokenUsage
	ContextWindow int
	// SessionClosed marks a synthetic delivery: the session was closed or
	// stopped before the turn completed. Status is empty in that case.
	SessionClosed bool
}

// turnRegistry resolves turn completions to the callers that started the
// turns. Subscriptions are keyed by turn index — the identity allocated by
// EventPipeline.AdvanceTurn and persisted with the prompt row — so a
// subscriber always receives the outcome of exactly the turn it initiated,
// never a neighbour started by a pending-flush replay, another scheduler
// fire, or a human.
//
// Delivery is one-shot per turn and never blocks the event-loop goroutine:
// every subscriber channel is buffered for its single delivery. Registry
// lifetime matches the Session object; Close synthesizes a SessionClosed
// outcome to every open subscription so no subscriber is left waiting on a
// turn whose CLI is gone.
type turnRegistry struct {
	mu     sync.Mutex
	closed bool
	subs   map[int][]chan TurnOutcome
}

func newTurnRegistry() *turnRegistry {
	return &turnRegistry{subs: make(map[int][]chan TurnOutcome)}
}

// Subscribe registers for the outcome of turnIndex and returns a channel that
// receives exactly one TurnOutcome: the turn's completion, or a synthetic
// SessionClosed if the session is torn down first. Subscribing on a closed
// registry resolves immediately.
func (r *turnRegistry) Subscribe(turnIndex int) <-chan TurnOutcome {
	ch := make(chan TurnOutcome, 1)
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		ch <- TurnOutcome{TurnIndex: turnIndex, SessionClosed: true}
		return ch
	}
	r.subs[turnIndex] = append(r.subs[turnIndex], ch)
	r.mu.Unlock()
	return ch
}

// Deliver resolves every subscription for outcome.TurnIndex and forgets them.
// Safe to call from the event-loop goroutine: sends fill each subscriber's
// single buffer slot and never block.
func (r *turnRegistry) Deliver(outcome TurnOutcome) {
	r.mu.Lock()
	chans := r.subs[outcome.TurnIndex]
	delete(r.subs, outcome.TurnIndex)
	r.mu.Unlock()
	for _, ch := range chans {
		ch <- outcome
	}
}

// Close resolves every open subscription with a synthetic SessionClosed
// outcome. Idempotent.
func (r *turnRegistry) Close() {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return
	}
	r.closed = true
	subs := r.subs
	r.subs = make(map[int][]chan TurnOutcome)
	r.mu.Unlock()
	for idx, chans := range subs {
		for _, ch := range chans {
			ch <- TurnOutcome{TurnIndex: idx, SessionClosed: true}
		}
	}
}
