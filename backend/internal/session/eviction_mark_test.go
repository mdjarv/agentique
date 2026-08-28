package session

import (
	"context"
	"testing"
	"time"

	"github.com/mdjarv/agentique/backend/internal/testutil"
	"github.com/stretchr/testify/suite"
)

// The idle sweep reclaims a session's CLI through the same StopSession the stop
// button takes, so the row it leaves is byte-identical to a deliberate stop.
// These pin the one bit that separates them, because everything downstream —
// the rest token, the suppressed resume banner — is derived from it.
type EvictionMarkSuite struct {
	testutil.DBSuite
	mgr *Manager
	svc *Service
}

func TestEvictionMarkSuite(t *testing.T) {
	suite.Run(t, new(EvictionMarkSuite))
}

func (s *EvictionMarkSuite) SetupTest() {
	s.DBSuite.SetupTest()
	s.mgr = NewManager(s.DB, s.Queries, s.Broadcaster, connectorAdapter{s.Connector})
	s.svc = NewService(s.mgr, s.Queries, s.Broadcaster, testutil.NewMockBlockingRunner())
}

// idleSince backdates the session's idle clock so the next sweep claims it,
// rather than making the test wait out a real TTL.
func (s *EvictionMarkSuite) idleSince(sess *Session, d time.Duration) {
	sess.mu.Lock()
	sess.lastActiveAt = time.Now().Add(-d)
	sess.mu.Unlock()
}

func (s *EvictionMarkSuite) newIdleSession() *Session {
	sess, err := s.mgr.Create(context.Background(), CreateParams{
		ProjectID: s.Project.ID,
		Name:      "evictable",
		WorkDir:   s.T().TempDir(),
		Model:     "opus",
	})
	s.Require().NoError(err)
	s.Require().Equal(StateIdle, sess.State())
	return sess
}

func (s *EvictionMarkSuite) TestSweepStampsTheRow() {
	sess := s.newIdleSession()
	s.idleSince(sess, time.Hour)

	s.svc.evictIdleSessions(time.Minute)

	dbSess, err := s.Queries.GetSession(context.Background(), sess.ID)
	s.Require().NoError(err)
	s.Equal("stopped", dbSess.State, "eviction still goes through the ordinary stop")
	s.True(dbSess.EvictedAt.Valid, "an evicted session must say who stopped it")
	// UTC RFC3339 seconds, like every other timestamp in this schema, because
	// SQLite compares TEXT lexicographically.
	_, parseErr := time.Parse(time.RFC3339, dbSess.EvictedAt.String)
	s.NoError(parseErr, "eviction stamp must sort as time")
}

// The mark has to be in memory before the stop, not after it: the stopped
// snapshot is built from the live session's own fields, and a client that
// learned the reason one push later would already have drawn the banner.
func (s *EvictionMarkSuite) TestStopSnapshotCarriesTheReason() {
	sess := s.newIdleSession()
	sess.markEvicted("2026-08-28T10:00:00Z")

	snap := sess.buildLocalSnapshot(StateStopped)

	s.Equal("2026-08-28T10:00:00Z", snap.EvictedAt)
}

// A person's stop is not agentique's doing and must keep reading as an
// interruption — that is the whole distinction.
func (s *EvictionMarkSuite) TestManualStopLeavesNoMark() {
	sess := s.newIdleSession()

	s.Require().NoError(s.svc.StopSession(context.Background(), sess.ID))

	dbSess, err := s.Queries.GetSession(context.Background(), sess.ID)
	s.Require().NoError(err)
	s.Equal("stopped", dbSess.State)
	s.False(dbSess.EvictedAt.Valid, "a deliberate stop must not claim to be a reclaim")
}

// The mark describes the most recent stop. A session that has run since is no
// longer one agentique reclaimed, so the next stop must not inherit the word.
func (s *EvictionMarkSuite) TestResumeClearsTheMark() {
	sess := s.newIdleSession()
	s.idleSince(sess, time.Hour)
	s.svc.evictIdleSessions(time.Minute)
	s.Require().Eventually(func() bool {
		row, err := s.Queries.GetSession(context.Background(), sess.ID)
		return err == nil && row.EvictedAt.Valid
	}, 2*time.Second, 5*time.Millisecond, "eviction never stamped the row")

	_, err := s.svc.ensureLive(context.Background(), sess.ID)
	s.Require().NoError(err)

	dbSess, err := s.Queries.GetSession(context.Background(), sess.ID)
	s.Require().NoError(err)
	s.False(dbSess.EvictedAt.Valid, "a resumed session is not an evicted one")
}

// The claim is released when the stop behind it fails, and the mark goes with
// it: a session that is still running was never reclaimed.
func TestClearEvictingDropsTheMark(t *testing.T) {
	sess := &Session{ID: "t", state: StateIdle, lastActiveAt: time.Now().Add(-time.Hour)}
	if !sess.beginIdleEvict(time.Minute, time.Now()) {
		t.Fatal("expected claim")
	}
	sess.markEvicted("2026-08-28T10:00:00Z")

	sess.clearEvicting()

	if got := sess.EvictedAt(); got != "" {
		t.Fatalf("EvictedAt = %q after clearEvicting, want empty", got)
	}
}
