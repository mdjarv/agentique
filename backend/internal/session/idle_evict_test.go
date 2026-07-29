package session

import (
	"sync"
	"testing"
	"time"

	"github.com/allbin/agentkit/runtime"
)

func TestBeginIdleEvictConditions(t *testing.T) {
	ttl := 30 * time.Minute
	now := time.Now()

	cases := []struct {
		name   string
		mutate func(s *Session)
		want   bool
	}{
		{
			name:   "idle past ttl claims",
			mutate: func(s *Session) { s.state = StateIdle; s.lastActiveAt = now.Add(-ttl - time.Minute) },
			want:   true,
		},
		{
			name:   "idle but too recent",
			mutate: func(s *Session) { s.state = StateIdle; s.lastActiveAt = now.Add(-time.Minute) },
			want:   false,
		},
		{
			name:   "running is never idle",
			mutate: func(s *Session) { s.state = StateRunning; s.lastActiveAt = now.Add(-time.Hour) },
			want:   false,
		},
		{
			name:   "merging is never idle",
			mutate: func(s *Session) { s.state = StateMerging; s.lastActiveAt = now.Add(-time.Hour) },
			want:   false,
		},
		{
			name: "already evicting",
			mutate: func(s *Session) {
				s.state = StateIdle
				s.lastActiveAt = now.Add(-time.Hour)
				s.evicting = true
			},
			want: false,
		},
		{
			name: "buffered messages not idle",
			mutate: func(s *Session) {
				s.state = StateIdle
				s.lastActiveAt = now.Add(-time.Hour)
				s.pendingMessages = []pendingMessage{{}}
			},
			want: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := &Session{ID: "t"}
			tc.mutate(s)
			got := s.beginIdleEvict(ttl, now)
			if got != tc.want {
				t.Fatalf("beginIdleEvict = %v, want %v", got, tc.want)
			}
			if got && !s.evicting {
				t.Errorf("claim succeeded but evicting flag not set")
			}
		})
	}
}

func TestClearEvicting(t *testing.T) {
	s := &Session{ID: "t", state: StateIdle, lastActiveAt: time.Now().Add(-time.Hour)}
	if !s.beginIdleEvict(time.Minute, time.Now()) {
		t.Fatal("expected claim")
	}
	s.clearEvicting()
	if s.evicting {
		t.Fatal("clearEvicting did not release the claim")
	}
	// A released session can be claimed again.
	if !s.beginIdleEvict(time.Minute, time.Now()) {
		t.Fatal("expected re-claim after clear")
	}
}

// TestIdleEvictVsQueryMutualExclusion asserts the core race invariant: for an
// idle session past its TTL, an eviction claim and a concurrent turn-start
// (validateAndPrepareQuery) are mutually exclusive — exactly one wins. Run under
// -race. Both operations serialize on s.mu; whichever wins first either sets
// evicting (turn refused) or refreshes lastActiveAt (claim skipped).
func TestIdleEvictVsQueryMutualExclusion(t *testing.T) {
	ttl := 10 * time.Millisecond
	for i := 0; i < 2000; i++ {
		s := &Session{
			ID:           "t",
			state:        StateIdle,
			lastActiveAt: time.Now().Add(-time.Hour), // idle past ttl
			rt:           &runtime.Session{},         // non-nil so query passes the liveness check
		}

		var claimed, queried bool
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			claimed = s.beginIdleEvict(ttl, time.Now())
		}()
		go func() {
			defer wg.Done()
			_, _, _, err := s.validateAndPrepareQuery(QueryOrigin{})
			queried = err == nil
		}()
		wg.Wait()

		if claimed == queried {
			t.Fatalf("iter %d: mutual exclusion violated: claimed=%v queried=%v (exactly one must win)", i, claimed, queried)
		}
	}
}
