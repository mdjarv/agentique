package session

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/mdjarv/agentique/backend/internal/store"
	"github.com/mdjarv/agentique/backend/internal/testutil"
)

func storeSessionWithUnseen(at string) store.Session {
	return store.Session{UnseenCompletedAt: sql.NullString{String: at, Valid: at != ""}}
}

// waitForUnseen polls the persisted row until its unread-completion mark
// matches want. The mark is written off the runtime's broadcast loop, so a
// completed turn does not mean the row is written yet.
func (s *ServiceSuite) waitForUnseen(sessionID string, want bool) string {
	s.T().Helper()
	deadline := time.After(2 * time.Second)
	for {
		dbSess, err := s.Queries.GetSession(context.Background(), sessionID)
		s.Require().NoError(err)
		if dbSess.UnseenCompletedAt.Valid == want {
			return dbSess.UnseenCompletedAt.String
		}
		select {
		case <-deadline:
			s.Require().Failf("timeout waiting for unseen mark",
				"want valid=%v, got %+v", want, dbSess.UnseenCompletedAt)
		default:
			time.Sleep(5 * time.Millisecond)
		}
	}
}

// waitForStatePush polls the captured broadcasts for a session.state snapshot
// whose unread mark is (or is not) set. Polling rather than reading once: the
// row is written before the push that announces it.
func (s *ServiceSuite) waitForStatePush(sessionID string, marked bool, msg string) {
	s.T().Helper()
	deadline := time.After(2 * time.Second)
	for {
		for _, m := range s.Broadcaster.MessagesOfType("session.state") {
			snap, ok := m.Payload.(GitSnapshot)
			if ok && snap.SessionID == sessionID && (snap.UnseenCompletedAt != "") == marked {
				return
			}
		}
		select {
		case <-deadline:
			s.Require().Fail(msg)
			return
		default:
			time.Sleep(5 * time.Millisecond)
		}
	}
}

// runTurn drives one complete turn on a live session.
func (s *ServiceSuite) runTurn(sess *Session, mock *testutil.MockCLISession, prompt string) {
	s.T().Helper()
	s.Require().NoError(sess.Query(context.Background(), prompt, nil))
	s.Require().NoError(mock.Inject(testutil.TextEvent("an answer")))
	s.Require().NoError(mock.Inject(testutil.ResultEvent(0.01)))
	waitForState(s.T(), sess, StateIdle)
}

// A completed turn is news the operator has not read. The mark used to live in
// one browser tab, so it died on reload and no other client could see it; the
// server owns it now, which is what makes it answerable over a voice socket.
func (s *ServiceSuite) TestTurnCompletionMarksUnseen() {
	sessionID, mock := s.createLiveSession()
	sess := s.mgr.Get(sessionID)
	s.Require().NotNil(sess)

	before, err := s.Queries.GetSession(context.Background(), sessionID)
	s.Require().NoError(err)
	s.Require().False(before.UnseenCompletedAt.Valid, "precondition: nothing unread yet")

	s.runTurn(sess, mock, "do the thing")

	at := s.waitForUnseen(sessionID, true)
	// UTC RFC3339 seconds, because SQLite compares TEXT lexicographically.
	parsed, perr := time.Parse(time.RFC3339, at)
	s.Require().NoError(perr, "the mark must be RFC3339")
	s.Equal(at, parsed.UTC().Format(time.RFC3339), "and UTC at seconds precision")

	s.Equal(at, sess.UnseenCompletedAt(), "the live mirror agrees with the row")
}

// Every client learns, or the mark is no better than the tab-local flag it
// replaced. It rides the same session.state push every other session field
// change does.
func (s *ServiceSuite) TestTurnCompletionBroadcastsTheMark() {
	sessionID, mock := s.createLiveSession()
	sess := s.mgr.Get(sessionID)
	s.Require().NotNil(sess)

	s.runTurn(sess, mock, "do the thing")
	s.waitForStatePush(sessionID, true, "a session.state push must carry the unread mark")
}

// Schedule attention is its own channel: an hourly loop must not bold a row on
// every fire. Runs that need the operator say so through schedule.updated, not
// through this mark.
func (s *ServiceSuite) TestScheduleOriginTurnLeavesNothingUnread() {
	sessionID, mock := s.createLiveSession()
	sess := s.mgr.Get(sessionID)
	s.Require().NotNil(sess)

	_, outcome, err := sess.QueryWithOutcome(context.Background(), "the hourly sweep", nil,
		QueryOrigin{Kind: "schedule", ScheduleID: "sched-1", RunID: "run-1"})
	s.Require().NoError(err)
	s.Require().NoError(mock.Inject(testutil.ResultEvent(0.01)))
	select {
	case <-outcome:
	case <-time.After(2 * time.Second):
		s.Require().Fail("timeout waiting for the scheduled turn to complete")
	}
	waitForState(s.T(), sess, StateIdle)

	// The mark is written asynchronously, so proving absence needs a moment to
	// pass rather than an immediate read.
	time.Sleep(150 * time.Millisecond)
	dbSess, err := s.Queries.GetSession(context.Background(), sessionID)
	s.Require().NoError(err)
	s.False(dbSess.UnseenCompletedAt.Valid, "a schedule fire must leave nothing unread")
	s.Empty(sess.UnseenCompletedAt(), "and the live mirror must agree")

	// A user turn on the same session still marks: the exclusion is per turn,
	// not a property the session acquires.
	s.runTurn(sess, mock, "and now something I asked for")
	s.waitForUnseen(sessionID, true)
}

// closeTurnLocked releases the next turn's start BEFORE onTurnComplete's
// goroutine reads the origin. With a single latest-value origin pair, the
// goroutine for schedule turn N read turn N+1's origin, mismatched, and
// treated the schedule fire as user-origin — bolding a row for an hourly
// loop. Origins are keyed by turn index now; this pins that interleaving.
func (s *ServiceSuite) TestScheduleOriginSurvivesRacingNextTurnStart() {
	sessionID, _ := s.createLiveSession()
	sess := s.mgr.Get(sessionID)
	s.Require().NotNil(sess)

	const scheduleTurn = 7
	sess.recordTurnOrigin(scheduleTurn, QueryOrigin{Kind: "schedule", ScheduleID: "sched-1", RunID: "run-1"})
	// The racing next turn records its origin before the schedule turn's
	// completion goroutine runs.
	sess.recordTurnOrigin(scheduleTurn+1, QueryOrigin{})

	sess.markUnseenCompletion(scheduleTurn)

	dbSess, err := s.Queries.GetSession(context.Background(), sessionID)
	s.Require().NoError(err)
	s.False(dbSess.UnseenCompletedAt.Valid,
		"a schedule fire must leave nothing unread even when the next turn already started")

	// And the racing user turn still marks when its own completion lands.
	sess.markUnseenCompletion(scheduleTurn + 1)
	s.waitForUnseen(sessionID, true)
}

// The read receipt. Idempotent on purpose — a client reconnecting, or opening
// the same session twice, should not have to know whether the mark was still
// set.
func (s *ServiceSuite) TestMarkSessionSeenClearsAndIsIdempotent() {
	sessionID, mock := s.createLiveSession()
	sess := s.mgr.Get(sessionID)
	s.Require().NotNil(sess)

	s.runTurn(sess, mock, "do the thing")
	s.waitForUnseen(sessionID, true)

	ctx := context.Background()
	s.Require().NoError(s.svc.MarkSessionSeen(ctx, sessionID))

	dbSess, err := s.Queries.GetSession(ctx, sessionID)
	s.Require().NoError(err)
	s.False(dbSess.UnseenCompletedAt.Valid, "the receipt clears the row")
	s.Empty(sess.UnseenCompletedAt(), "and the live mirror")

	// Again, on an already-clear session.
	s.Require().NoError(s.svc.MarkSessionSeen(ctx, sessionID), "clearing twice is a no-op success")
	dbSess, err = s.Queries.GetSession(ctx, sessionID)
	s.Require().NoError(err)
	s.False(dbSess.UnseenCompletedAt.Valid)
}

// Clearing broadcasts too: a client that did not send the receipt still has to
// stop showing the badge.
func (s *ServiceSuite) TestMarkSessionSeenBroadcastsTheClear() {
	sessionID, mock := s.createLiveSession()
	sess := s.mgr.Get(sessionID)
	s.Require().NotNil(sess)

	s.runTurn(sess, mock, "do the thing")
	// Drain the marking push before clearing, so the snapshot asserted below
	// cannot be one recorded before the receipt.
	s.waitForStatePush(sessionID, true, "precondition: the mark was broadcast")
	s.Broadcaster.Reset()

	s.Require().NoError(s.svc.MarkSessionSeen(context.Background(), sessionID))

	// Absent means nothing waiting, never unchanged.
	s.waitForStatePush(sessionID, false, "clearing must publish a snapshot without the mark")
}

func (s *ServiceSuite) TestMarkSessionSeenUnknownSession() {
	err := s.svc.MarkSessionSeen(context.Background(), "6f7b1e5c-0000-4000-8000-000000000000")
	s.ErrorIs(err, ErrNotFound)
}

// The wire carries it, or nothing downstream can rank on it. Optional, so a
// peer that predates the field is not rejected wholesale.
func (s *ServiceSuite) TestSessionInfoCarriesUnseenCompletedAt() {
	sessionID, mock := s.createLiveSession()
	sess := s.mgr.Get(sessionID)
	s.Require().NotNil(sess)

	infoBefore := s.findSession(sessionID)
	s.Nil(infoBefore.UnseenCompletedAt, "absent while there is nothing unread")

	s.runTurn(sess, mock, "do the thing")
	at := s.waitForUnseen(sessionID, true)

	info := s.findSession(sessionID)
	s.Require().NotNil(info.UnseenCompletedAt, "SessionInfo must surface the mark")
	s.Equal(at, *info.UnseenCompletedAt)

	s.Require().NoError(s.svc.MarkSessionSeen(context.Background(), sessionID))
	s.Nil(s.findSession(sessionID).UnseenCompletedAt, "and drop it once read")
}

func (s *ServiceSuite) findSession(sessionID string) SessionInfo {
	s.T().Helper()
	list, err := s.svc.ListSessions(context.Background(), s.Project.ID)
	s.Require().NoError(err)
	for _, info := range list.Sessions {
		if info.ID == sessionID {
			return info
		}
	}
	s.Require().Failf("session missing from ListSessions", "id %s", sessionID)
	return SessionInfo{}
}

// A resumed session must not silently drop a mark nobody read: the first
// snapshot it broadcasts is built from memory, so the mirror has to be seeded
// from the row.
func TestApplyPostResumeFlagsSeedsUnseen(t *testing.T) {
	sess := &Session{}
	applyPostResumeFlags(sess, storeSessionWithUnseen("2026-08-26T10:00:00Z"))
	if got := sess.UnseenCompletedAt(); got != "2026-08-26T10:00:00Z" {
		t.Fatalf("unseen mirror = %q, want the persisted stamp", got)
	}
}
