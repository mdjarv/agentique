package session

import (
	"testing"
	"time"
)

// The bug: idleness is measured from the last turn, so a session someone is
// reading looks identical to one abandoned days ago and the sweep evicts it
// mid-use. Attention has to defer eviction exactly as a turn would.
func TestMarkActive_DefersIdleEviction(t *testing.T) {
	ttl := 30 * time.Minute
	sess := &Session{ID: "s1", state: StateIdle, lastActiveAt: time.Now().Add(-2 * ttl)}

	// Precondition: without attention this session is evictable.
	if !sess.beginIdleEvict(ttl, time.Now()) {
		t.Fatal("a session idle for 2x TTL should be evictable")
	}
	sess.clearEvicting()

	sess.MarkActive()

	if sess.beginIdleEvict(ttl, time.Now()) {
		t.Error("a session being viewed must not be evicted")
	}
}

// Attention is not conversational activity. Bumping state or turn bookkeeping
// would make a session someone merely looked at appear to have done something.
func TestMarkActive_TouchesNothingButTheEvictionClock(t *testing.T) {
	sess := &Session{ID: "s2", state: StateIdle, queryCount: 3}
	before := sess.lastActiveAt

	sess.MarkActive()

	if !sess.lastActiveAt.After(before) {
		t.Error("MarkActive must advance the eviction clock")
	}
	if sess.state != StateIdle {
		t.Errorf("state = %s, want unchanged idle", sess.state)
	}
	if sess.queryCount != 3 {
		t.Errorf("queryCount = %d, want unchanged 3", sess.queryCount)
	}
}

// An eviction that has already claimed the session is mid-stop. Bumping the
// clock cannot save it, and would leave a resumed session claiming it was
// active at a moment it was actually being torn down.
func TestMarkActive_IgnoredOnceEvictionClaimed(t *testing.T) {
	ttl := 30 * time.Minute
	sess := &Session{ID: "s3", state: StateIdle, lastActiveAt: time.Now().Add(-2 * ttl)}
	if !sess.beginIdleEvict(ttl, time.Now()) {
		t.Fatal("expected the claim to succeed")
	}
	claimed := sess.lastActiveAt

	sess.MarkActive()

	if !sess.lastActiveAt.Equal(claimed) {
		t.Error("MarkActive must not move the clock on a session already claimed for eviction")
	}
}

// A running session is never evictable, so attention is harmless there — but it
// must not disturb the turn either.
func TestMarkActive_SafeWhileRunning(t *testing.T) {
	sess := &Session{ID: "s4", state: StateRunning}
	sess.MarkActive()
	if sess.state != StateRunning {
		t.Errorf("state = %s, want running", sess.state)
	}
	if sess.beginIdleEvict(30*time.Minute, time.Now()) {
		t.Error("a running session must never be evictable")
	}
}
