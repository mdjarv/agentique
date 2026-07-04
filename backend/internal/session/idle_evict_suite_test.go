package session

import (
	"context"
	"time"
)

// TestEvictIdleSessions_StopsIdlePastTTL drives the real eviction sweep through
// StopSession: an idle session older than the TTL is closed and removed from the
// live pool with its DB row marked stopped (so the next message lazy-resumes it),
// while a freshly-active session is spared.
func (s *ServiceSuite) TestEvictIdleSessions_StopsIdlePastTTL() {
	ctx := context.Background()
	ttl := 30 * time.Minute

	// Session A: force it idle well past the TTL.
	idA, _ := s.createLiveSession()
	s.Require().True(s.mgr.IsLive(idA))
	sessA := s.mgr.Get(idA)
	s.Require().NotNil(sessA)
	sessA.mu.Lock()
	sessA.state = StateIdle
	sessA.lastActiveAt = time.Now().Add(-time.Hour)
	sessA.mu.Unlock()

	// Session B: recently active — must NOT be evicted.
	idB, _ := s.createLiveSession()
	s.Require().True(s.mgr.IsLive(idB))
	sessB := s.mgr.Get(idB)
	s.Require().NotNil(sessB)
	sessB.mu.Lock()
	sessB.state = StateIdle
	sessB.lastActiveAt = time.Now()
	sessB.mu.Unlock()

	s.svc.evictIdleSessions(ttl)

	// A evicted: gone from the pool, DB says stopped.
	s.False(s.mgr.IsLive(idA), "idle-past-TTL session should be evicted")
	dbA, err := s.Queries.GetSession(ctx, idA)
	s.Require().NoError(err)
	s.Equal(string(StateStopped), dbA.State)

	// B spared: still live.
	s.True(s.mgr.IsLive(idB), "recently active session should be spared")
}

// TestEvictIdleSessions_Disabled is a guard that a zero/negative TTL never starts
// the sweep (SetIdleEvictTimeout is a no-op) — the feature is opt-in.
func (s *ServiceSuite) TestEvictIdleSessions_DisabledByDefault() {
	s.Equal(time.Duration(0), s.svc.idleEvictTimeout)
	// Calling the setter with a non-positive value keeps it disabled.
	s.svc.SetIdleEvictTimeout(0)
	s.Equal(time.Duration(0), s.svc.idleEvictTimeout)
}
