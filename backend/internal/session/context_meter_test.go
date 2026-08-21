package session

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/allbin/agentkit/runtime"
	"github.com/mdjarv/agentique/backend/internal/testutil"
)

// --- Unit: the meter itself ------------------------------------------------

// recordingMeter builds a contextMeter over a scripted query, capturing what it
// emits.
type recordingMeter struct {
	*contextMeter

	mu       sync.Mutex
	emitted  []WireContextUsageEvent
	queries  int
	gate     chan struct{} // when non-nil, each query blocks on a receive
	usage    *runtime.ContextUsage
	queryErr error
}

func newRecordingMeter(usage *runtime.ContextUsage, queryErr error) *recordingMeter {
	r := &recordingMeter{usage: usage, queryErr: queryErr}
	r.contextMeter = newContextMeter("s1",
		func(context.Context) (*runtime.ContextUsage, error) {
			r.mu.Lock()
			r.queries++
			gate, u, err := r.gate, r.usage, r.queryErr
			r.mu.Unlock()
			if gate != nil {
				<-gate
			}
			return u, err
		},
		func(ev WireContextUsageEvent) {
			r.mu.Lock()
			r.emitted = append(r.emitted, ev)
			r.mu.Unlock()
		},
	)
	return r
}

func (r *recordingMeter) counts() (queries, emits int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.queries, len(r.emitted)
}

func (r *recordingMeter) events() []WireContextUsageEvent {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]WireContextUsageEvent(nil), r.emitted...)
}

func waitFor(t *testing.T, cond func() bool, msg string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timeout: %s", msg)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// The meter renders against MaxTokens, not RawMaxTokens: those differ when a
// compaction-policy window is narrower than the model's hard limit, and
// Remaining() tracks the narrower one.
func TestContextMeter_EmitsResolvedWindow(t *testing.T) {
	m := newRecordingMeter(&runtime.ContextUsage{
		Model:                "claude-opus-5",
		TotalTokens:          44942,
		MaxTokens:            967000,
		RawMaxTokens:         1000000,
		Percentage:           5.0,
		AutoCompactEnabled:   true,
		AutoCompactThreshold: 900000,
	}, nil)

	m.Refresh()
	waitFor(t, func() bool { _, e := m.counts(); return e == 1 }, "no measurement emitted")

	got := m.events()[0]
	if got.Type != "context_usage" {
		t.Errorf("Type = %q, want context_usage", got.Type)
	}
	if got.ContextWindow != 967000 {
		t.Errorf("ContextWindow = %d, want the resolved window 967000", got.ContextWindow)
	}
	if got.RawContextWindow != 1000000 {
		t.Errorf("RawContextWindow = %d, want 1000000", got.RawContextWindow)
	}
	if got.UsedTokens != 44942 || got.Percentage != 5.0 {
		t.Errorf("usage = %d/%v, want 44942/5", got.UsedTokens, got.Percentage)
	}
	if !got.AutoCompactEnabled || got.AutoCompactThreshold != 900000 {
		t.Errorf("auto-compact = %v/%d, want true/900000", got.AutoCompactEnabled, got.AutoCompactThreshold)
	}
}

// Usage is unclamped and can exceed the window. The meter must publish it as
// measured — clamping is the renderer's job, and hiding an over-limit session
// is exactly the wrong lie for this widget to tell.
func TestContextMeter_PublishesOverLimitUsageUnclamped(t *testing.T) {
	m := newRecordingMeter(&runtime.ContextUsage{
		TotalTokens: 250000, MaxTokens: 200000, Percentage: 125,
	}, nil)

	m.Refresh()
	waitFor(t, func() bool { _, e := m.counts(); return e == 1 }, "no measurement emitted")

	if got := m.events()[0].UsedTokens; got != 250000 {
		t.Errorf("UsedTokens = %d, want 250000 unclamped", got)
	}
}

// ErrNotSupported is structural (the testmode connector, codex), so the meter
// latches off rather than paying a control round-trip on every later signal.
func TestContextMeter_LatchesOffWhenUnsupported(t *testing.T) {
	m := newRecordingMeter(nil, runtime.ErrNotSupported)

	m.Refresh()
	waitFor(t, func() bool { q, _ := m.counts(); return q == 1 }, "first query never ran")

	for range 5 {
		m.Refresh()
	}
	time.Sleep(50 * time.Millisecond)

	queries, emits := m.counts()
	if queries != 1 {
		t.Errorf("queries = %d, want 1 — ErrNotSupported must latch the meter off", queries)
	}
	if emits != 0 {
		t.Errorf("emits = %d, want 0", emits)
	}
}

// A transient failure (session evicted, resuming, CLI hiccup) must not latch:
// the next real signal has to try again.
func TestContextMeter_TransientFailureStaysArmed(t *testing.T) {
	m := newRecordingMeter(nil, errors.New("cli went away"))

	m.Refresh()
	waitFor(t, func() bool { q, _ := m.counts(); return q == 1 }, "first query never ran")

	m.mu.Lock()
	m.usage, m.queryErr = &runtime.ContextUsage{TotalTokens: 10, MaxTokens: 100}, nil
	m.mu.Unlock()

	m.Refresh()
	waitFor(t, func() bool { _, e := m.counts(); return e == 1 }, "meter did not retry after a transient failure")
}

// A zero window cannot be rendered as a percentage; publishing it would push a
// divide-by-zero to the frontend.
func TestContextMeter_SkipsZeroWindow(t *testing.T) {
	m := newRecordingMeter(&runtime.ContextUsage{TotalTokens: 100, MaxTokens: 0}, nil)

	m.Refresh()
	waitFor(t, func() bool { q, _ := m.counts(); return q == 1 }, "query never ran")
	time.Sleep(30 * time.Millisecond)

	if _, emits := m.counts(); emits != 0 {
		t.Errorf("emits = %d, want 0 for a zero window", emits)
	}
}

// Each measurement is a control round-trip, so a burst of signals must collapse
// into one follow-up rather than one query per event.
func TestContextMeter_CoalescesBurst(t *testing.T) {
	m := newRecordingMeter(&runtime.ContextUsage{TotalTokens: 1, MaxTokens: 100}, nil)
	gate := make(chan struct{})
	m.mu.Lock()
	m.gate = gate
	m.mu.Unlock()

	m.Refresh() // starts the loop; blocks in the query
	waitFor(t, func() bool { q, _ := m.counts(); return q == 1 }, "first query never started")
	for range 10 {
		m.Refresh() // all collapse into a single follow-up
	}

	m.mu.Lock()
	m.gate = nil
	m.mu.Unlock()
	close(gate)

	waitFor(t, func() bool { _, e := m.counts(); return e == 2 }, "follow-up measurement never ran")
	time.Sleep(50 * time.Millisecond)

	if queries, _ := m.counts(); queries != 2 {
		t.Errorf("queries = %d, want 2 — 11 signals must collapse to one follow-up", queries)
	}
}

// Refresh must never block: it is called from the event loop, which processes
// every CLI event for the session.
func TestContextMeter_RefreshDoesNotBlock(t *testing.T) {
	m := newRecordingMeter(&runtime.ContextUsage{TotalTokens: 1, MaxTokens: 100}, nil)
	gate := make(chan struct{})
	defer close(gate)
	m.mu.Lock()
	m.gate = gate
	m.mu.Unlock()

	done := make(chan struct{})
	go func() {
		m.Refresh()
		m.Refresh()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Refresh blocked on an in-flight measurement")
	}
}

// Stop must silence a measurement already in flight — a closing session has no
// business broadcasting.
func TestContextMeter_StopSuppressesInFlightEmit(t *testing.T) {
	m := newRecordingMeter(&runtime.ContextUsage{TotalTokens: 1, MaxTokens: 100}, nil)
	gate := make(chan struct{})
	m.mu.Lock()
	m.gate = gate
	m.mu.Unlock()

	m.Refresh()
	waitFor(t, func() bool { q, _ := m.counts(); return q == 1 }, "query never started")
	m.Stop()
	close(gate)
	time.Sleep(50 * time.Millisecond)

	if _, emits := m.counts(); emits != 0 {
		t.Errorf("emits = %d, want 0 after Stop", emits)
	}
}

func TestContextMeter_NilIsSafe(t *testing.T) {
	var m *contextMeter
	m.Refresh()
	m.Stop()
}

// --- Integration: the signals that trigger a measurement -------------------

// The whole point: after a compaction the per-turn number is stale-high, and
// the live measurement is what makes the meter drop.
func (s *StopQueuedSuite) TestMeterRemeasuresOnCompactionAndTurnEnd() {
	s.startRunningSession()
	cli := s.capable.Last()
	cli.mu.Lock()
	cli.usage = &runtime.ContextUsage{TotalTokens: 44942, MaxTokens: 967000, Percentage: 5}
	cli.mu.Unlock()

	// The turn reports a nearly-full window; the frontend's per-turn value now
	// says ~96%.
	full := testutil.ResultEvent(0)
	full.ContextWindow = 967000
	full.Usage = runtime.TokenUsage{InputTokens: 930000, OutputTokens: 1000}
	s.Require().NoError(s.Connector.Last().Inject(full))

	s.waitFor(func() bool { return len(s.liveUsageEvents()) >= 1 }, "turn end did not remeasure")

	// A compaction rewrites the transcript. The per-turn number cannot shrink
	// on its own — only a fresh measurement can.
	s.Require().NoError(s.Connector.Last().Inject(runtime.CompactBoundaryEvent{
		Trigger: "auto", PreTokens: 931000,
	}))
	s.waitFor(func() bool { return len(s.liveUsageEvents()) >= 2 }, "compaction did not remeasure")

	live := s.liveUsageEvents()[len(s.liveUsageEvents())-1]
	s.Equal(967000, live.ContextWindow)
	s.Equal(44942, live.UsedTokens,
		"the live measurement must supersede the stale per-turn number")

	// And it is broadcast-only — a point-in-time measurement is not part of the
	// conversation, so history must not resurrect a stale one.
	s.True(isTransient(live), "context_usage must never be persisted")
}

// The plain mock implements neither optional interface. The meter has to fall
// back silently — the per-turn value stays the meter's source, and the session
// keeps working.
func (s *StopQueuedSuite) TestMeterFallsBackWhenAdapterCannotMeasure() {
	s.plainSetup()
	sess := s.startRunningSession()

	s.Require().NoError(s.Connector.Last().Inject(testutil.ResultEvent(0)))
	s.waitFor(func() bool { return sess.State() == StateIdle }, "session never went idle")
	time.Sleep(100 * time.Millisecond)

	s.Empty(s.liveUsageEvents(), "an adapter that cannot measure must emit nothing")
	s.False(sess.meter.armed(), "ErrNotSupported must latch the meter off")
}

// liveUsageEvents collects broadcast live context measurements.
func (s *StopQueuedSuite) liveUsageEvents() []WireContextUsageEvent {
	var out []WireContextUsageEvent
	for _, msg := range s.Broadcaster.Messages() {
		push, ok := msg.Payload.(PushSessionEvent)
		if !ok {
			continue
		}
		if ev, ok := push.Event.(WireContextUsageEvent); ok {
			out = append(out, ev)
		}
	}
	return out
}
