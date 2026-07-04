package session

import (
	"context"
	"log/slog"
	"time"
)

// idleSweepInterval bounds how often the idle-eviction sweep runs, derived from
// the configured TTL (quarter of it) and clamped to a sane range so a short TTL
// doesn't busy-loop and a long one still checks periodically.
const (
	idleSweepMinInterval = 1 * time.Minute
	idleSweepMaxInterval = 5 * time.Minute
)

// beginIdleEvict atomically claims this session for idle eviction. It returns
// true — with the session marked evicting so a concurrent Query is refused — only
// when the session is idle, unclaimed, has no buffered (queued) messages, and has
// been idle at least ttl. Guarded by s.mu, so it is mutually exclusive with
// validateAndPrepareQuery's turn commit: whichever takes the lock first wins.
//
// The caller MUST follow a true result with a stop (which discards the session)
// or clearEvicting (to release the claim) — otherwise the session refuses queries
// forever.
func (s *Session) beginIdleEvict(ttl time.Duration, now time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.evicting || s.state != StateIdle {
		return false
	}
	// A session with buffered mid-turn messages (codex) is about to flush a turn
	// at the next idle boundary — not truly idle.
	if len(s.pendingMessages) > 0 {
		return false
	}
	if now.Sub(s.lastActiveAt) < ttl {
		return false
	}
	s.evicting = true
	return true
}

// clearEvicting releases an eviction claim made by beginIdleEvict. Used when the
// stop that was meant to follow the claim fails, so the session stays usable.
func (s *Session) clearEvicting() {
	s.mu.Lock()
	s.evicting = false
	s.mu.Unlock()
}

// idleFor reports how long the session has been idle relative to now (its last
// turn start / state transition). Only meaningful while the session is idle.
func (s *Session) idleFor(now time.Time) time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	return now.Sub(s.lastActiveAt)
}

// SetIdleEvictTimeout configures idle-session eviction and, when d > 0, starts
// the background sweep. A non-positive d leaves the feature disabled. Call once,
// after construction (like SetGitService / SetBrowserService).
func (s *Service) SetIdleEvictTimeout(d time.Duration) {
	s.idleEvictTimeout = d
	if d > 0 {
		go s.sweepIdleSessions()
	}
}

// sweepIdleSessions periodically evicts sessions idle past the configured TTL,
// reclaiming each one's CLI process and Playwright/Chrome subtree. The session's
// DB row and Claude session id are preserved, so the next message transparently
// lazy-resumes it (Service.ensureLive). Exits on Close.
func (s *Service) sweepIdleSessions() {
	ttl := s.idleEvictTimeout
	interval := ttl / 4
	if interval < idleSweepMinInterval {
		interval = idleSweepMinInterval
	}
	if interval > idleSweepMaxInterval {
		interval = idleSweepMaxInterval
	}
	slog.Info("idle session eviction enabled", "ttl", ttl, "check_interval", interval)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-s.done:
			return
		case <-ticker.C:
			s.evictIdleSessions(ttl)
		}
	}
}

// evictIdleSessions stops every live session idle for at least ttl. Each claim is
// atomic (beginIdleEvict) so a session that starts a turn between the snapshot and
// the claim is skipped. Reuses StopSession, so browser cleanup, git-version
// seeding, and the stopped-state DB write all happen exactly as for a manual stop.
func (s *Service) evictIdleSessions(ttl time.Duration) {
	now := time.Now()
	for _, sess := range s.mgr.LiveSessions() {
		if !sess.beginIdleEvict(ttl, now) {
			continue
		}
		id := sess.ID
		slog.Info("evicting idle session to reclaim resources", "session_id", id, "idle_for", sess.idleFor(now).Round(time.Second))
		if err := s.StopSession(context.Background(), id); err != nil {
			slog.Warn("idle eviction stop failed; releasing claim", "session_id", id, "error", err)
			sess.clearEvicting()
		}
	}
}
