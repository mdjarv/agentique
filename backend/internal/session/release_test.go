package session

import (
	"context"
	"time"
)

// leadChannel creates a channel, joins leadID as "lead" (which wires the
// @release callback) and each worker as "worker".
func (s *ChannelSuite) leadChannel(name, leadID string, workerIDs ...string) string {
	ctx := context.Background()
	ch, err := s.svc.CreateChannel(ctx, s.Project.ID, name)
	s.Require().NoError(err)
	_, err = s.svc.JoinChannel(ctx, leadID, ch.ID, "lead")
	s.Require().NoError(err)
	for _, w := range workerIDs {
		_, jerr := s.svc.JoinChannel(ctx, w, ch.ID, "worker")
		s.Require().NoError(jerr)
	}
	return ch.ID
}

func (s *ChannelSuite) releaseCallback(leadID string) func(string, ReleaseWorkersRequest) (string, error) {
	lead := s.mgr.Get(leadID)
	s.Require().NotNil(lead, "lead session must be live")
	lead.mu.Lock()
	cb := lead.channel.onReleaseWorkers
	lead.mu.Unlock()
	s.Require().NotNil(cb, "@release callback should be wired for a lead")
	return cb
}

func (s *ChannelSuite) isArchived(sessionID string) bool {
	dbSess, err := s.Queries.GetSession(context.Background(), sessionID)
	s.Require().NoError(err)
	return dbSess.ArchivedAt.Valid && dbSess.ArchivedAt.String != ""
}

// @release with no named subset files every idle worker away, keeping the row
// (and its branch/worktree) — the reversible teardown.
func (s *ChannelSuite) TestRelease_AllIdleWorkers() {
	leadID, _ := s.createNamedSession("Lead")
	w1, _ := s.createWorkerSession("Alpha", leadID)
	w2, _ := s.createWorkerSession("Beta", leadID)
	s.leadChannel("sweep", leadID, w1, w2)

	summary, err := s.releaseCallback(leadID)(leadID, ReleaseWorkersRequest{})
	s.Require().NoError(err)

	s.True(s.isArchived(w1), "Alpha should be archived")
	s.True(s.isArchived(w2), "Beta should be archived")
	s.False(s.isArchived(leadID), "the lead never releases itself")
	s.False(s.mgr.IsLive(w1), "archiving an idle worker releases its CLI")
	s.Contains(summary, "Alpha")
	s.Contains(summary, "Beta")
}

// A worker running a turn is refused, never interrupted, and named back so the
// lead can wait for it — its row stays un-archived and its CLI keeps running.
func (s *ChannelSuite) TestRelease_RefusesBusyWorker() {
	leadID, _ := s.createNamedSession("Lead")
	busyID, _ := s.createWorkerSession("Busy", leadID)
	idleID, _ := s.createWorkerSession("Idle", leadID)
	s.leadChannel("mixed", leadID, busyID, idleID)

	// Drive Busy into a turn that never completes (MockBlockingRunner).
	s.Require().NoError(s.svc.QuerySession(context.Background(), busyID, "work", nil))
	busy := s.mgr.Get(busyID)
	s.Require().Eventually(busy.TurnInFlight, 2*time.Second, 5*time.Millisecond)

	summary, err := s.releaseCallback(leadID)(leadID, ReleaseWorkersRequest{})
	s.Require().NoError(err)

	s.False(s.isArchived(busyID), "a busy worker is never archived")
	s.True(s.mgr.IsLive(busyID), "a busy worker keeps its CLI")
	s.True(s.isArchived(idleID), "the idle worker beside it is still released")
	s.Contains(summary, "Busy")
	s.Contains(summary, "Idle")
}

// A named subset releases only those workers, and a name matching no releasable
// worker is reported rather than silently ignored.
func (s *ChannelSuite) TestRelease_NamedSubsetAndUnknown() {
	leadID, _ := s.createNamedSession("Lead")
	w1, _ := s.createWorkerSession("Keep", leadID)
	w2, _ := s.createWorkerSession("Drop", leadID)
	s.leadChannel("subset", leadID, w1, w2)

	summary, err := s.releaseCallback(leadID)(leadID,
		ReleaseWorkersRequest{Workers: []string{"Drop", "Ghost"}})
	s.Require().NoError(err)

	s.True(s.isArchived(w2), "Drop should be archived")
	s.False(s.isArchived(w1), "Keep was not named, so it stays")
	s.Contains(summary, "Drop")
	s.Contains(summary, "Ghost", "an unmatched name is reported")
}

// A worker that also belongs to another channel is left alone: archiving is
// per-session and global, so filing it away here would pull it out of a channel
// this lead does not own.
func (s *ChannelSuite) TestRelease_SkipsMultiChannelWorker() {
	leadID, _ := s.createNamedSession("Lead")
	shared, _ := s.createWorkerSession("Shared", leadID)
	s.leadChannel("primary", leadID, shared)
	// Shared also lives in a second channel.
	s.createChannelWithMembers("secondary", shared)

	summary, err := s.releaseCallback(leadID)(leadID, ReleaseWorkersRequest{})
	s.Require().NoError(err)

	s.False(s.isArchived(shared), "a multi-channel worker is not released")
	s.Contains(summary, "No workers to release")
}

// Only a lead gets the @release callback — a worker cannot release its peers.
func (s *ChannelSuite) TestRelease_WorkerHasNoCallback() {
	leadID, _ := s.createNamedSession("Lead")
	workerID, _ := s.createWorkerSession("Worker", leadID)
	s.leadChannel("team", leadID, workerID)

	worker := s.mgr.Get(workerID)
	s.Require().NotNil(worker)
	worker.mu.Lock()
	cb := worker.channel.onReleaseWorkers
	worker.mu.Unlock()
	s.Nil(cb, "a non-lead worker has no @release callback")
}
