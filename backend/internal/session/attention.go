package session

import "time"

// Attention is the "a human is looking at this session" signal.
//
// Idle eviction measures inactivity from lastActiveAt, which is stamped only on
// turn starts and state transitions — so it answers "when did this session last
// *run* something", not "is anyone using it". Those diverge exactly where it
// hurts: reading a long answer, reviewing a diff, or writing a careful prompt
// all look identical to a session abandoned days ago, and a 30-minute TTL
// evicts the session out from under an active user.
//
// Clients viewing a session send session.attention periodically, which bumps
// the same clock a turn would. An open session therefore never ages into
// eviction, and the TTL goes back to meaning what it says: nobody has touched
// this in TTL.

// MarkActive records that a client is actively viewing this session, deferring
// idle eviction as a turn would.
//
// Deliberately does NOT touch state or turn bookkeeping — attention is not
// activity in the conversational sense, and must not make a session look like
// it did something. lastActiveAt is read only by the idle sweep
// (beginIdleEvict / idleFor), so this is confined to eviction.
//
// It does not clear the unread-completion mark either, and that separation
// survived the mark moving server-side (unseen.go). Attention is a heartbeat: a
// client sends it repeatedly while a tab is merely open and visible, which is
// not the same claim as "the operator has read what came back". The read
// receipt is its own gesture, session.markSeen, sent once. Ranking is unchanged
// too — the deck still puts unread below approval and question, because those
// two hold a process.
func (s *Session) MarkActive() {
	s.mu.Lock()
	defer s.mu.Unlock()
	// An eviction already claimed this session; the stop is in flight and the
	// claim is not ours to release. Bumping the clock now would not save it and
	// would leave a resumed session with a misleading idle-since.
	if s.evicting {
		return
	}
	s.lastActiveAt = time.Now()
}

// MarkSessionAttention defers idle eviction for a session a client is viewing.
//
// Only ever bumps an already-live session: it must not lazy-resume. Opening a
// sleeping session to read its history would otherwise spawn a CLI process
// purely because someone looked at it — the exact resource cost eviction exists
// to reclaim. Unknown or evicted sessions are a silent no-op, so a client can
// send attention for anything it has open without knowing the liveness state.
func (s *Service) MarkSessionAttention(sessionID string) {
	if sess := s.mgr.Get(sessionID); sess != nil {
		sess.MarkActive()
	}
}
