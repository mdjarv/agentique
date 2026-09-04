package session

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/mdjarv/agentique/backend/internal/store"
)

// Unseen completion: "this session finished something and nobody has read it".
//
// It used to be client state. The browser's chat store set a flag when a result
// landed in a tab that was not looking at that session, and cleared it when the
// user opened one. That made it invisible to every other client and gone on
// reload, so "what came back while I was away" was a different answer in each
// tab and no answer at all to anything outside the browser. The switchboard
// asks that question over a voice socket, so the fact needs one owner: the
// sessions row, broadcast like any other session field.
//
// The rules the client had are kept, and they are all the rules there are:
//
//   - A completed turn sets it. Completion is the news; a turn that never
//     completes is not.
//   - Schedule-origin turns do not. An hourly loop must not bold a row on every
//     fire; a run that needs the operator says so through schedule attention,
//     which is its own channel (docs/scheduled-loops.md).
//   - Only an explicit read receipt clears it — Service.MarkSessionSeen, from
//     the session.markSeen RPC. Nothing infers "seen" from a state change: a
//     session that starts a new turn has not thereby been read.
//
// Ranking stays where it was. The deck's Needs-you band still puts unread below
// approval and question (lib/session/priority.ts), because the two that hold a
// process come first. This owns the fact, not its priority.

// originPruneWindow bounds how far behind the newest turn an unconsumed
// origin entry may trail before recordTurnOrigin drops it. Only a turn whose
// end never reached markUnseenCompletion (a fatal abort) leaves one behind,
// and nothing can ask about it once this many turns have passed.
const originPruneWindow = 8

// recordTurnOrigin remembers which origin started the given turn, so
// markUnseenCompletion can recognise a schedule fire when that turn ends. The
// TurnOutcome does not carry the origin and should not: it describes what
// happened, not who asked. Keyed by turn index, never a latest-value pair:
// the next turn's start is released before this turn's completion goroutine
// reads the origin (see the turnOrigins field comment).
func (s *Session) recordTurnOrigin(turnIndex int, origin QueryOrigin) {
	s.mu.Lock()
	if s.turnOrigins == nil {
		s.turnOrigins = make(map[int]string)
	}
	s.turnOrigins[turnIndex] = origin.Kind
	for idx := range s.turnOrigins {
		if idx < turnIndex-originPruneWindow {
			delete(s.turnOrigins, idx)
		}
	}
	s.mu.Unlock()
}

// markUnseenCompletion stamps the session as having finished something nobody
// has read, and broadcasts the change.
//
// Called from the turn-end seam on a goroutine: it writes a row and builds a
// full snapshot (which re-reads git status), and the callback it hangs off runs
// on the runtime's broadcast loop.
//
// Re-stamping an already-marked session is deliberate. The mark is "there is
// unread work here", so the timestamp names the most recent completion rather
// than the oldest unread one — the same thing an unread badge means everywhere
// else.
func (s *Session) markUnseenCompletion(turnIndex int) {
	// Consume this turn's own origin entry. An unrecorded turn falls back to
	// "user", the marking case — same as before the origins were keyed.
	s.mu.Lock()
	kind := s.turnOrigins[turnIndex]
	delete(s.turnOrigins, turnIndex)
	s.mu.Unlock()
	if kind == "schedule" {
		return
	}

	at := nowUTC()
	// Persist before mutating memory, the same order Archive uses: a failed
	// write must not leave the session presenting an unread mark that the next
	// restart contradicts.
	if err := s.queries.SetSessionUnseenCompletedAt(context.Background(), store.SetSessionUnseenCompletedAtParams{
		UnseenCompletedAt: sqlNullString(at),
		ID:                s.ID,
	}); err != nil {
		slog.Error("persist unseen completion failed", "session_id", s.ID, "error", err)
		return
	}

	s.mu.Lock()
	s.unseenCompletedAt = at
	state := s.state
	s.mu.Unlock()

	s.broadcastState(state)
}

// MarkSeen clears the unread-completion mark on a live session and broadcasts
// the change — the exact inverse of markUnseenCompletion, and the same shape as
// Unarchive: one field to clear and no residue.
//
// Synchronous, unlike the marking side: the caller is an operator gesture
// arriving over the control plane, not the runtime's broadcast loop.
func (s *Session) MarkSeen() error {
	if err := s.queries.ClearSessionUnseenCompletedAt(context.Background(), s.ID); err != nil {
		return fmt.Errorf("clear session unseen completion: %w", err)
	}

	s.mu.Lock()
	s.unseenCompletedAt = ""
	state := s.state
	s.mu.Unlock()

	s.broadcastState(state)
	return nil
}

// MarkUnseenCompletedAt seeds the mirror from the persisted row when a session
// comes back to life, so the first snapshot a resumed session broadcasts does
// not silently drop a mark the operator has not read yet.
func (s *Session) MarkUnseenCompletedAt(at string) {
	s.mu.Lock()
	s.unseenCompletedAt = at
	s.mu.Unlock()
}

// UnseenCompletedAt reports the live session's unread-completion marker, "" when
// there is nothing waiting to be read.
func (s *Session) UnseenCompletedAt() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.unseenCompletedAt
}
