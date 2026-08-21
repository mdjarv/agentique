package session

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/allbin/agentkit/runtime"
)

// contextUsageTimeout bounds one live measurement. The query is a control
// round-trip to the provider CLI; a hung CLI must not leak goroutines or keep
// the meter permanently in flight (which would swallow every later refresh).
const contextUsageTimeout = 10 * time.Second

// contextMeter keeps the frontend's context-window meter honest across
// compaction.
//
// The per-turn number (WireResultEvent.ContextWindow) describes the last API
// call: it does not shrink when the provider compacts, and drifts upward until
// the next turn. This meter measures the live transcript instead, on the two
// signals that actually invalidate the old number — a completed turn and a
// compaction boundary — and broadcasts a WireContextUsageEvent.
//
// It is deliberately signal-driven, never polled: each measurement is a control
// round-trip to the CLI. Refresh is non-blocking (callers are on the event
// loop, which must never stall) and single-flighted: refreshes arriving while a
// measurement is in flight collapse into exactly one follow-up, so a burst
// costs at most two round-trips instead of one per event.
//
// A provider that cannot answer (runtime.ErrNotSupported — the testmode
// connector, codex) latches the meter off for the session's lifetime, and the
// per-turn value remains the meter's source. A measurement failing is never
// fatal to the session.
type contextMeter struct {
	sessionID string
	query     func(ctx context.Context) (*runtime.ContextUsage, error)
	emit      func(WireContextUsageEvent)

	mu sync.Mutex
	// inflight is true while the measure loop owns the meter; again records a
	// refresh that arrived during one, collapsing a burst into one follow-up.
	inflight bool
	again    bool
	// unsupported latches on ErrNotSupported — structural, not transient, so
	// there is no point paying the round-trip again on this session.
	unsupported bool
	stopped     bool
}

// newContextMeter wires a meter to a session's live-usage query and its wire
// emitter. Starts no goroutines — the first Refresh does.
func newContextMeter(
	sessionID string,
	query func(ctx context.Context) (*runtime.ContextUsage, error),
	emit func(WireContextUsageEvent),
) *contextMeter {
	return &contextMeter{sessionID: sessionID, query: query, emit: emit}
}

// Refresh requests a live measurement. Non-blocking and safe to call from the
// event loop: it either starts the measure loop or marks the in-flight one for
// one more pass. A no-op once the provider has answered ErrNotSupported.
func (m *contextMeter) Refresh() {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.stopped || m.unsupported {
		return
	}
	if m.inflight {
		m.again = true
		return
	}
	m.inflight = true
	go m.loop()
}

// Stop latches the meter off. In-flight measurements finish (their context
// timeout bounds them) but no longer emit.
func (m *contextMeter) Stop() {
	if m == nil {
		return
	}
	m.mu.Lock()
	m.stopped = true
	m.again = false
	m.mu.Unlock()
}

// armed reports whether the meter will still answer a Refresh — false once the
// provider has said it cannot measure, or once the session has stopped.
func (m *contextMeter) armed() bool {
	if m == nil {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return !m.stopped && !m.unsupported
}

// loop measures until no refresh has queued up behind the last one.
func (m *contextMeter) loop() {
	for {
		m.measure()

		m.mu.Lock()
		if m.stopped || m.unsupported || !m.again {
			m.inflight = false
			m.mu.Unlock()
			return
		}
		m.again = false
		m.mu.Unlock()
	}
}

// measure runs one query and broadcasts the result. Every failure path is
// non-fatal: the per-turn context number stays the fallback.
func (m *contextMeter) measure() {
	ctx, cancel := context.WithTimeout(context.Background(), contextUsageTimeout)
	defer cancel()

	usage, err := m.query(ctx)
	if err != nil {
		if errors.Is(err, runtime.ErrNotSupported) {
			m.mu.Lock()
			m.unsupported = true
			m.mu.Unlock()
			slog.Debug("context usage unsupported by provider; falling back to per-turn value",
				"session_id", m.sessionID)
			return
		}
		// ErrNotLive / transport failures are transient (evicted, resuming,
		// closing): keep the meter armed for the next signal.
		slog.Debug("context usage query failed", "session_id", m.sessionID, "error", err)
		return
	}
	if usage == nil || usage.MaxTokens <= 0 {
		// A window of zero cannot be rendered as a percentage; treat it as no
		// answer rather than publishing a divide-by-zero to the frontend.
		return
	}

	m.mu.Lock()
	stopped := m.stopped
	m.mu.Unlock()
	if stopped {
		return
	}

	m.emit(WireContextUsageEvent{
		Type:                 "context_usage",
		ContextWindow:        usage.MaxTokens,
		UsedTokens:           usage.TotalTokens,
		Percentage:           usage.Percentage,
		RawContextWindow:     usage.RawMaxTokens,
		AutoCompactEnabled:   usage.AutoCompactEnabled,
		AutoCompactThreshold: usage.AutoCompactThreshold,
	})
}
