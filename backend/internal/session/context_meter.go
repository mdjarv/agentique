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

// maxWindowSeeds bounds how many times a pushed measurement may ask for the
// window it should be divided by. The seed normally lands when the runtime
// attaches; this covers a CLI that was not ready yet. Bounded because a
// provider that pushes measurements but cannot answer the query would
// otherwise buy a control round-trip per model response, forever.
const maxWindowSeeds = 3

// contextMeter keeps the frontend's context-window meter honest, moving it
// while a turn runs and correcting it across compaction.
//
// # Two sources, one reading
//
// The provider answers the same question two ways, and the meter needs both:
//
//   - runtime.ContextUsageEvent is PUSHED, several times per turn — roughly one
//     pair per model response, on both providers as of agentkit v0.6.0 (Claude
//     needs runtime.ConnectParams.PartialMessages; codex pushes regardless).
//     It is what moves the bar mid-turn. It is also coarse: only Model,
//     TotalTokens, MaxTokens, RawMaxTokens and Percentage are set, and it
//     describes the last API call, so it does NOT shrink when the provider
//     compacts.
//   - Session.ContextUsage is PULLED, one control round-trip, and measures the
//     live transcript. It is the only source that is correct straight after a
//     compaction, and the only one that carries the compaction policy
//     (AutoCompactEnabled / AutoCompactThreshold).
//
// So: follow the pushed stream continuously, and keep the pull as the
// correction on the two signals that invalidate it — a completed turn and a
// compaction boundary (contextMeter.Refresh, from EventPipeline's
// OnContextStale) — plus one seed when the runtime attaches.
//
// # One denominator
//
// The two sources can disagree about the window. The pushed event always
// reports the model's HARD window (MaxTokens == RawMaxTokens), where the query
// reports the window the provider actually resolved — which may be a narrower
// compaction-policy one. Dividing the pushed total by the pushed window and the
// pulled total by the pulled window would then make the bar jump between two
// readings of the same session.
//
// The meter therefore remembers the window the QUERY resolved and divides every
// pushed total by it. The resolved one is the right choice, not merely a
// consistent one: it is the window auto-compaction fires against, so it is what
// the frontend's tiers (lib/session/context-tier.ts — "High usage" at 80%) are
// calibrated for. Rendering a policy-narrowed session against its hard window
// would report 15% for a session about to be compacted.
//
// Measured against CLI 2.1.263 the two agree on claude-sonnet-5 (both
// 1000000, with auto-compact at 967000) and on codex/gpt-5-class (both 258400),
// so this reconciliation is currently a guard rather than a correction. It is
// still the guard that has to be here: agentkit documents the narrower answer,
// and the window is the provider's to change.
//
// Percentage is recomputed on BOTH paths, never forwarded. The event computed
// its own against its own window, and the query's is truncated to a whole
// number (41093 of 1000000 answered as 4.0) — a meter whose two sources round
// differently reports two percentages for one reading.
//
// A pushed measurement arriving before any query has answered falls back to the
// event's own window and asks for a seed (bounded by maxWindowSeeds), so the
// first turn of a session still moves. The held window is discarded when the
// event names a different model than the one it was measured for.
//
// # Cost and concurrency
//
// Refresh is non-blocking (callers are on the event loop, which must never
// stall) and single-flighted: refreshes arriving while a measurement is in
// flight collapse into exactly one follow-up, so a burst costs at most two
// round-trips instead of one per event. Observe costs nothing — it is a
// calculation on the event loop, on an event the session already receives.
//
// A provider whose query answers runtime.ErrNotSupported (the testmode
// connector; codex too, before agentkit v0.5.0 taught it the question) latches
// the PULL off for the session's lifetime — that is structural, not transient.
// It must be the
// sentinel and nothing else: codex deliberately returns a different error
// before its first model response, meaning "not yet", and latching on that
// would silence the meter on every session that has not started. Latching the
// pull does not silence the push; a provider that pushes still moves the bar,
// against the event's own window.
//
// A measurement failing is never fatal to the session, and the per-turn value
// (WireResultEvent.ContextWindow) remains the frontend's fallback throughout.
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

	// The last answer from the query: the window every pushed measurement is
	// divided by, the model it was resolved for, and the compaction policy the
	// pushed measurement does not carry.
	window               int
	rawWindow            int
	windowModel          string
	autoCompactEnabled   bool
	autoCompactThreshold int
	// seeds counts window seeds requested by a pushed measurement.
	seeds int
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
	start := m.armRefreshLocked()
	m.mu.Unlock()
	if start {
		go m.loop()
	}
}

// armRefreshLocked records a refresh request and reports whether the caller
// must start the measure loop. Caller holds m.mu.
func (m *contextMeter) armRefreshLocked() bool {
	if m.stopped || m.unsupported {
		return false
	}
	if m.inflight {
		m.again = true
		return false
	}
	m.inflight = true
	return true
}

// Observe folds a pushed measurement into the meter and broadcasts it. Called
// from the event loop for every runtime.ContextUsageEvent, so it must not
// block: it divides, emits, and returns.
//
// The pushed measurement is coarse — the compaction policy comes from the last
// query and rides along unchanged, so the frontend does not watch it flicker
// off between turns.
func (m *contextMeter) Observe(u runtime.ContextUsage) {
	if m == nil || u.TotalTokens < 0 {
		return
	}

	m.mu.Lock()
	if m.stopped {
		m.mu.Unlock()
		return
	}
	ev, seed := m.foldLocked(u)
	start := false
	if seed {
		m.seeds++
		start = m.armRefreshLocked()
	}
	m.mu.Unlock()

	if start {
		go m.loop()
	}
	if ev == nil {
		return
	}
	m.emit(*ev)
}

// foldLocked resolves a pushed measurement against the held window. Returns the
// event to emit (nil when there is no renderable window) and whether a window
// seed is worth asking for. Caller holds m.mu.
func (m *contextMeter) foldLocked(u runtime.ContextUsage) (*WireContextUsageEvent, bool) {
	// A model switch invalidates the held window: it was resolved for the
	// previous model's policy.
	if m.window > 0 && u.Model != "" && m.windowModel != "" && u.Model != m.windowModel {
		m.window, m.rawWindow, m.windowModel = 0, 0, ""
		m.autoCompactEnabled, m.autoCompactThreshold = false, 0
	}

	window, raw := m.window, m.rawWindow
	autoEnabled, autoThreshold := m.autoCompactEnabled, m.autoCompactThreshold
	seeded := window > 0
	if !seeded {
		// No query has answered yet. The event's own (hard) window keeps the
		// first turn moving; a seed corrects the denominator behind it.
		window, raw = u.MaxTokens, u.RawMaxTokens
		autoEnabled, autoThreshold = false, 0
	}
	if raw <= 0 {
		raw = u.RawMaxTokens
	}

	seed := !seeded && !m.unsupported && m.seeds < maxWindowSeeds
	if window <= 0 {
		// A window of zero cannot be rendered as a percentage.
		return nil, seed
	}

	return &WireContextUsageEvent{
		Type:                 "context_usage",
		ContextWindow:        window,
		UsedTokens:           u.TotalTokens,
		Percentage:           percentOf(u.TotalTokens, window),
		RawContextWindow:     raw,
		AutoCompactEnabled:   autoEnabled,
		AutoCompactThreshold: autoThreshold,
	}, seed
}

// percentOf renders usage against a window as a percentage. Unclamped, because
// the reading is: a session over its window is worth showing as over.
func percentOf(used, window int) float64 {
	if window <= 0 {
		return 0
	}
	return float64(used) / float64(window) * 100
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
		// The sentinel and nothing else. Codex answers a different, non-sentinel
		// error before its first model response ("not yet"), and latching on
		// that would silence the meter on every session that has not started.
		if errors.Is(err, runtime.ErrNotSupported) {
			m.mu.Lock()
			m.unsupported = true
			m.mu.Unlock()
			slog.Debug("context usage query unsupported by provider; following pushed measurements only",
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
	// This is the answer every pushed measurement is divided by from here on.
	m.window = usage.MaxTokens
	m.rawWindow = usage.RawMaxTokens
	m.windowModel = usage.Model
	m.autoCompactEnabled = usage.AutoCompactEnabled
	m.autoCompactThreshold = usage.AutoCompactThreshold
	stopped := m.stopped
	m.mu.Unlock()
	if stopped {
		return
	}

	m.emit(WireContextUsageEvent{
		Type:          "context_usage",
		ContextWindow: usage.MaxTokens,
		UsedTokens:    usage.TotalTokens,
		// Recomputed, not forwarded. The provider truncates its own percentage
		// to a whole number (41093 of 1000000 answered as 4.0, measured against
		// CLI 2.1.263), and a meter whose two sources round differently reports
		// two percentages for one reading.
		Percentage:           percentOf(usage.TotalTokens, usage.MaxTokens),
		RawContextWindow:     usage.RawMaxTokens,
		AutoCompactEnabled:   usage.AutoCompactEnabled,
		AutoCompactThreshold: usage.AutoCompactThreshold,
	})
}
