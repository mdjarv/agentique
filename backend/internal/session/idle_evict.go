package session

import (
	"context"
	"log/slog"
	"time"

	"github.com/mdjarv/agentique/backend/internal/store"
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
//
// It drops the eviction mark with the claim: a session still running is not one
// agentique reclaimed, and leaving the mark set would make the next stop —
// somebody's stop button — read as a sweep's doing.
func (s *Session) clearEvicting() {
	s.mu.Lock()
	s.evicting = false
	s.evictedAt = ""
	s.mu.Unlock()
}

// markEvicted stamps the in-memory eviction mark, so the `stopped` snapshot the
// stop is about to broadcast already carries the reason.
//
// The mirror has to be set before the stop rather than after it. buildLocalSnapshot
// reads these fields, so a mark written afterwards would reach clients only on
// the next push — and the push it missed is exactly the one the chat pane turns
// into a banner.
func (s *Session) markEvicted(at string) {
	s.mu.Lock()
	s.evictedAt = at
	s.mu.Unlock()
}

// EvictedAt reports why this session has no process, "" for every reason that is
// not the idle sweep. Only ever non-empty between a claim and the stop that
// discards the session, which is why the wire reads the row and not this.
func (s *Session) EvictedAt() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.evictedAt
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
//
// Which is why it stamps the eviction mark first. Reusing the manual stop is
// right for the mechanism and wrong for the story: the row it leaves behind is
// byte-identical to one somebody's stop button made, so every surface read a
// reclaim as an interruption. The mark is the difference, and it goes in before
// the stop so the snapshot announcing the stop carries it.
func (s *Service) evictIdleSessions(ttl time.Duration) {
	now := time.Now()
	for _, sess := range s.mgr.LiveSessions() {
		if !sess.beginIdleEvict(ttl, now) {
			continue
		}
		id := sess.ID
		slog.Info("evicting idle session to reclaim resources", "session_id", id, "idle_for", sess.idleFor(now).Round(time.Second))
		s.stampEvicted(sess, now)
		if err := s.StopSession(context.Background(), id); err != nil {
			slog.Warn("idle eviction stop failed; releasing claim", "session_id", id, "error", err)
			sess.clearEvicting()
			s.clearEvictedRow(context.Background(), id)
		}
	}
}

// stampEvicted records that this stop is a reclaim, in the row and in the live
// session's mirror.
//
// A failed write is logged and not fatal. The eviction is worth doing either
// way; the cost of losing the mark is that the session reads as a plain stop,
// which is what it read as before this existed.
func (s *Service) stampEvicted(sess *Session, now time.Time) {
	at := now.UTC().Format(time.RFC3339)
	if err := s.queries.SetSessionEvictedAt(context.Background(), store.SetSessionEvictedAtParams{
		EvictedAt: sqlNullString(at),
		ID:        sess.ID,
	}); err != nil {
		slog.Warn("persist eviction mark failed; session will read as a plain stop",
			"session_id", sess.ID, "error", err)
		return
	}
	sess.markEvicted(at)
}

// clearEvictedRow drops the persisted eviction mark. Used both when a claimed
// stop fails and when a session is resumed — the mark describes the most recent
// stop, so anything that undoes or prevents that stop must undo it too.
func (s *Service) clearEvictedRow(ctx context.Context, sessionID string) {
	if err := s.queries.ClearSessionEvictedAt(ctx, sessionID); err != nil {
		slog.Warn("clear eviction mark failed", "session_id", sessionID, "error", err)
	}
}
