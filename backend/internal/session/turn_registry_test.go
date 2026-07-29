package session

import (
	"sync"
	"testing"
	"time"

	"github.com/allbin/agentkit/runtime"
)

func recvOutcome(t *testing.T, ch <-chan TurnOutcome) TurnOutcome {
	t.Helper()
	select {
	case out := <-ch:
		return out
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for turn outcome")
		return TurnOutcome{}
	}
}

func TestTurnRegistry_DeliverResolvesSubscribedTurn(t *testing.T) {
	r := newTurnRegistry()
	ch := r.Subscribe(3)

	r.Deliver(TurnOutcome{TurnIndex: 3, Status: runtime.TurnStatusCompleted, FinalText: "done"})

	out := recvOutcome(t, ch)
	if out.TurnIndex != 3 || out.FinalText != "done" || out.SessionClosed {
		t.Errorf("unexpected outcome: %+v", out)
	}
}

func TestTurnRegistry_DeliveryIsPerTurn(t *testing.T) {
	r := newTurnRegistry()
	ch3 := r.Subscribe(3)
	ch4 := r.Subscribe(4)

	r.Deliver(TurnOutcome{TurnIndex: 4, FinalText: "four"})

	if out := recvOutcome(t, ch4); out.FinalText != "four" {
		t.Errorf("turn 4 got %+v", out)
	}
	select {
	case out := <-ch3:
		t.Errorf("turn 3 must not resolve from turn 4's delivery, got %+v", out)
	default:
	}
}

func TestTurnRegistry_MultipleSubscribersSameTurn(t *testing.T) {
	r := newTurnRegistry()
	a := r.Subscribe(1)
	b := r.Subscribe(1)

	r.Deliver(TurnOutcome{TurnIndex: 1, FinalText: "shared"})

	if out := recvOutcome(t, a); out.FinalText != "shared" {
		t.Errorf("subscriber a got %+v", out)
	}
	if out := recvOutcome(t, b); out.FinalText != "shared" {
		t.Errorf("subscriber b got %+v", out)
	}
}

func TestTurnRegistry_DeliverWithoutSubscribersIsNoop(t *testing.T) {
	r := newTurnRegistry()
	r.Deliver(TurnOutcome{TurnIndex: 9})
	// A later subscriber for the same index waits for a future delivery —
	// outcomes are not retained.
	ch := r.Subscribe(9)
	select {
	case out := <-ch:
		t.Errorf("stale outcome must not be replayed, got %+v", out)
	default:
	}
}

func TestTurnRegistry_CloseResolvesOpenSubscriptions(t *testing.T) {
	r := newTurnRegistry()
	ch := r.Subscribe(7)

	r.Close()

	out := recvOutcome(t, ch)
	if !out.SessionClosed || out.TurnIndex != 7 {
		t.Errorf("expected synthetic SessionClosed for turn 7, got %+v", out)
	}

	// Subscribing after close resolves immediately.
	late := r.Subscribe(8)
	if out := recvOutcome(t, late); !out.SessionClosed {
		t.Errorf("late subscription must resolve SessionClosed, got %+v", out)
	}

	// Idempotent.
	r.Close()
}

func TestTurnRegistry_ConcurrentSubscribeDeliverClose(t *testing.T) {
	r := newTurnRegistry()
	var wg sync.WaitGroup
	const turns = 50

	results := make([]TurnOutcome, turns)
	for i := 0; i < turns; i++ {
		ch := r.Subscribe(i)
		wg.Add(1)
		go func(i int, ch <-chan TurnOutcome) {
			defer wg.Done()
			results[i] = recvOutcome(t, ch)
		}(i, ch)
	}

	var deliver sync.WaitGroup
	for i := 0; i < turns/2; i++ {
		deliver.Add(1)
		go func(i int) {
			defer deliver.Done()
			r.Deliver(TurnOutcome{TurnIndex: i, FinalText: "delivered"})
		}(i)
	}
	deliver.Wait()
	// Everything not delivered resolves via Close.
	r.Close()
	wg.Wait()

	for i := 0; i < turns/2; i++ {
		if results[i].SessionClosed || results[i].FinalText != "delivered" {
			t.Errorf("turn %d: expected delivery, got %+v", i, results[i])
		}
	}
	for i := turns / 2; i < turns; i++ {
		if !results[i].SessionClosed {
			t.Errorf("turn %d: expected SessionClosed, got %+v", i, results[i])
		}
	}
}

func TestClassifyErrorKind(t *testing.T) {
	cases := []struct {
		name string
		ev   runtime.ErrorEvent
		want string
	}{
		{"kind wins", runtime.ErrorEvent{Kind: "rate_limit"}, ErrorKindRateLimit},
		{"rate limit message", runtime.ErrorEvent{Err: errString("Rate limit exceeded, retry later")}, ErrorKindRateLimit},
		{"429", runtime.ErrorEvent{Err: errString("HTTP 429 from upstream")}, ErrorKindRateLimit},
		{"usage limit", runtime.ErrorEvent{Err: errString("usage limit reached until 17:00")}, ErrorKindRateLimit},
		{"overloaded", runtime.ErrorEvent{Err: errString("Overloaded")}, ErrorKindOverloaded},
		{"529", runtime.ErrorEvent{Err: errString("status 529")}, ErrorKindOverloaded},
		{"context window", runtime.ErrorEvent{Err: errString("prompt is too long: context window exceeded")}, ErrorKindContext},
		{"other", runtime.ErrorEvent{Err: errString("something broke")}, ErrorKindOther},
		{"nil err", runtime.ErrorEvent{}, ErrorKindOther},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := classifyErrorKind(tc.ev); got != tc.want {
				t.Errorf("classifyErrorKind(%v) = %q, want %q", tc.ev, got, tc.want)
			}
		})
	}
}

func TestStrongerErrorKind(t *testing.T) {
	if got := strongerErrorKind(ErrorKindOther, ErrorKindRateLimit); got != ErrorKindRateLimit {
		t.Errorf("specific must beat other, got %q", got)
	}
	if got := strongerErrorKind(ErrorKindRateLimit, ErrorKindOther); got != ErrorKindRateLimit {
		t.Errorf("other must not demote specific, got %q", got)
	}
	if got := strongerErrorKind("", ErrorKindOther); got != ErrorKindOther {
		t.Errorf("other beats empty, got %q", got)
	}
	if got := strongerErrorKind(ErrorKindContext, ErrorKindRateLimit); got != ErrorKindContext {
		t.Errorf("equal rank keeps current, got %q", got)
	}
}

type errString string

func (e errString) Error() string { return string(e) }

// The runtime fires the Idle state hook BEFORE the pipeline processes the
// TurnCompletedEvent (agentkit finishTurn), so a turn started in that window
// could advance turnIndex under the unprocessed completion and steal its
// outcome. WaitTurnClosed is the guard: regression for the misattribution.
func TestPipeline_WaitTurnClosedGuardsOutcomeAttribution(t *testing.T) {
	sink := newTestSink()
	var got []TurnOutcome
	p := newTestPipeline(sink, func(cfg *PipelineConfig) {
		cfg.OnTurnComplete = func(o TurnOutcome) { got = append(got, o) }
	})

	first := p.AdvanceTurn()

	// Turn open, completion unprocessed: a would-be starter must wait.
	if p.WaitTurnClosed(20 * time.Millisecond) {
		t.Fatal("WaitTurnClosed must block while the completion is unprocessed")
	}

	// Completion drains → waiters release → the next turn may start.
	p.ProcessEvent(testResultEvent(0.01))
	if !p.WaitTurnClosed(time.Second) {
		t.Fatal("WaitTurnClosed must return once the completion processed")
	}
	second := p.AdvanceTurn()
	p.ProcessEvent(testResultEvent(0.01))

	if len(got) != 2 || got[0].TurnIndex != first || got[1].TurnIndex != second {
		t.Fatalf("outcome attribution wrong: %+v (want turns %d then %d)", got, first, second)
	}
}

func TestPipeline_FatalErrorClosesTurn(t *testing.T) {
	sink := newTestSink()
	p := newTestPipeline(sink)
	p.AdvanceTurn()

	p.ProcessEvent(runtime.ErrorEvent{Fatal: true, Err: errString("CLI died")})

	if !p.WaitTurnClosed(time.Second) {
		t.Fatal("a fatal error must release turn-start waiters — no completion will ever arrive")
	}
}
