package session

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/allbin/agentkit/runtime"

	"github.com/mdjarv/agentique/backend/internal/store"
	"github.com/mdjarv/agentique/backend/internal/testutil"
)

// endRecorder records onSessionEnd invocations (the brain ingest sink).
type endRecorder struct {
	mu      sync.Mutex
	calls   int
	project string
	events  int
	fired   chan struct{}
}

func newEndRecorder() *endRecorder { return &endRecorder{fired: make(chan struct{}, 16)} }

func (r *endRecorder) fn(projectID string, events []store.SessionEvent) {
	r.mu.Lock()
	r.calls++
	r.project = projectID
	r.events = len(events)
	r.mu.Unlock()
	select {
	case r.fired <- struct{}{}:
	default:
	}
}

func (r *endRecorder) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.calls
}

func (r *endRecorder) lastEvents() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.events
}

// waitFired waits until the recorder has fired at least `want` times (for the async
// delete/runtime paths), returning false on timeout.
func waitFired(r *endRecorder, want int) bool {
	deadline := time.After(2 * time.Second)
	for {
		if r.count() >= want {
			return true
		}
		select {
		case <-deadline:
			return false
		case <-time.After(5 * time.Millisecond):
		}
	}
}

// --- claimLearn (pure, no DB) ---

func TestClaimLearn_HighWaterMark(t *testing.T) {
	s := &Service{learnHighWater: make(map[string]int)}
	id := "sess-1"
	cases := []struct {
		count int
		want  bool
	}{
		{7, false}, // below minEventsToEncode
		{8, true},  // first claim at the threshold
		{8, false}, // no growth
		{12, true}, // grew
		{5, false}, // shrink (never)
	}
	for _, c := range cases {
		if got := s.claimLearn(id, c.count); got != c.want {
			t.Fatalf("claimLearn(%d)=%v, want %v", c.count, got, c.want)
		}
	}
}

func TestClaimLearn_AtomicUnderRace(t *testing.T) {
	s := &Service{learnHighWater: make(map[string]int)}
	const n = 64
	var winners int32
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if s.claimLearn("race", 8) {
				atomic.AddInt32(&winners, 1)
			}
		}()
	}
	wg.Wait()
	if winners != 1 {
		t.Fatalf("exactly one goroutine should claim the learn, got %d", winners)
	}
}

// --- HandleSessionComplete + delete safety net (real DB via the suite) ---

func (s *ServiceSuite) seedSessionWithEvents(n int) string {
	sess := testutil.SeedSession(s.T(), s.Queries, s.Project.ID, "idle")
	for i := 0; i < n; i++ {
		testutil.SeedEvent(s.T(), s.Queries, sess.ID, 0, int64(i), "text", `{"content":"hi"}`)
	}
	return sess.ID
}

func (s *ServiceSuite) TestDeleteWithoutCompletion_SafetyNet() {
	// Baseline (locked first): a delete with >= minEventsToEncode events ingests once.
	sessionID := s.seedSessionWithEvents(8)
	rec := newEndRecorder()
	s.svc.SetOnSessionEnd(rec.fn)

	s.Require().NoError(s.svc.DeleteSession(context.Background(), sessionID))
	s.Require().True(waitFired(rec, 1), "delete safety net should ingest once")
	s.Equal(1, rec.count())
}

func (s *ServiceSuite) TestHandleSessionComplete_FiresOnceAndKeepsSession() {
	sessionID := s.seedSessionWithEvents(8)
	rec := newEndRecorder()
	s.svc.SetOnSessionEnd(rec.fn)

	s.svc.HandleSessionComplete(s.Project.ID, sessionID)

	s.Equal(1, rec.count(), "completion should ingest exactly once")
	s.Equal(s.Project.ID, rec.project)
	s.Equal(8, rec.lastEvents())

	// The session is NOT deleted; its events remain.
	_, err := s.Queries.GetSession(context.Background(), sessionID)
	s.Require().NoError(err, "completion must not delete the session")
	evs, err := s.Queries.ListEventsBySession(context.Background(), sessionID)
	s.Require().NoError(err)
	s.Len(evs, 8)
}

func (s *ServiceSuite) TestHandleSessionComplete_GatedByMinEvents() {
	sessionID := s.seedSessionWithEvents(7) // below the gate
	rec := newEndRecorder()
	s.svc.SetOnSessionEnd(rec.fn)

	s.svc.HandleSessionComplete(s.Project.ID, sessionID)
	s.Equal(0, rec.count(), "trivial session must not ingest")
}

func (s *ServiceSuite) TestCompletionThenDelete_NoDoubleCapture() {
	sessionID := s.seedSessionWithEvents(8)
	rec := newEndRecorder()
	s.svc.SetOnSessionEnd(rec.fn)

	s.svc.HandleSessionComplete(s.Project.ID, sessionID) // fires (sync)
	s.Require().NoError(s.svc.DeleteSession(context.Background(), sessionID))

	// Delete must NOT re-ingest the same events (high-water already at 8).
	time.Sleep(100 * time.Millisecond) // let any (incorrect) async fire land
	s.Equal(1, rec.count(), "completion then delete must ingest only once total")
}

func (s *ServiceSuite) TestReingestAfterGrowth() {
	sessionID := s.seedSessionWithEvents(8)
	rec := newEndRecorder()
	s.svc.SetOnSessionEnd(rec.fn)

	s.svc.HandleSessionComplete(s.Project.ID, sessionID)
	s.Equal(1, rec.count())
	s.Equal(8, rec.lastEvents())

	// Grow the transcript to 12 events, then complete again — re-ingests with 12.
	for i := 8; i < 12; i++ {
		testutil.SeedEvent(s.T(), s.Queries, sessionID, 0, int64(i), "text", `{"content":"more"}`)
	}
	s.svc.HandleSessionComplete(s.Project.ID, sessionID)
	s.Equal(2, rec.count())
	s.Equal(12, rec.lastEvents())
}

// --- Manager wiring: completion fires on StateDone, not on Idle ---

func (s *ServiceSuite) TestManagerWiresCompletionOnStateDone() {
	var (
		mu      sync.Mutex
		calls   int
		gotProj string
		gotSess string
		fired   = make(chan struct{}, 4)
	)
	s.mgr.OnSessionComplete = func(projectID, sessionID string) {
		mu.Lock()
		calls++
		gotProj, gotSess = projectID, sessionID
		mu.Unlock()
		select {
		case fired <- struct{}{}:
		default:
		}
	}

	sessionID, _ := s.createLiveSession() // Manager installs onComplete via wireCompletion
	sess := s.mgr.Get(sessionID)
	s.Require().NotNil(sess)

	// Idle must NOT fire the completion hook.
	handleRuntimeStateChange(sess, runtime.StateChangeEvent{To: runtime.StateIdle})
	time.Sleep(50 * time.Millisecond)
	mu.Lock()
	idleCalls := calls
	mu.Unlock()
	s.Equal(0, idleCalls, "completion must not fire on Idle")

	// StateDone fires it once with (projectID, sessionID).
	handleRuntimeStateChange(sess, runtime.StateChangeEvent{To: runtime.StateDone})
	select {
	case <-fired:
	case <-time.After(2 * time.Second):
		s.FailNow("completion hook did not fire on StateDone")
	}
	mu.Lock()
	defer mu.Unlock()
	s.Equal(1, calls)
	s.Equal(s.Project.ID, gotProj)
	s.Equal(sessionID, gotSess)
}
