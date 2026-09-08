package session

import (
	"context"
	"database/sql"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mdjarv/agentique/backend/internal/gitops"
	"github.com/mdjarv/agentique/backend/internal/store"
	"github.com/mdjarv/agentique/backend/internal/testutil"
)

func TestBranchStatusCache_GetRequiresMatchingKey(t *testing.T) {
	c := newBranchStatusCache()
	key := branchStatusKey{projectPath: "/repo", branch: "feature"}
	if _, ok := c.get("s1", key); ok {
		t.Fatal("empty cache answered")
	}
	c.put("s1", key, branchStatus{CommitsAhead: 2})
	e, ok := c.get("s1", key)
	if !ok || e.status.CommitsAhead != 2 {
		t.Fatalf("get = %+v, %v", e, ok)
	}
	// A session whose branch changed is a different question.
	if _, ok := c.get("s1", branchStatusKey{projectPath: "/repo", branch: "other"}); ok {
		t.Fatal("served a status computed for another branch")
	}
	c.forget("s1")
	if _, ok := c.get("s1", key); ok {
		t.Fatal("forgotten entry still served")
	}
}

func TestBranchStatusCache_StaleAfterTTL(t *testing.T) {
	c := newBranchStatusCache()
	now := time.Now()
	c.now = func() time.Time { return now }
	key := branchStatusKey{projectPath: "/repo", branch: "feature"}
	c.put("s1", key, branchStatus{})
	e, _ := c.get("s1", key)
	if c.stale(e) {
		t.Fatal("fresh entry reported stale")
	}
	now = now.Add(branchStatusTTL + time.Second)
	if !c.stale(e) {
		t.Fatal("entry past the TTL reported fresh")
	}
}

// One refresh per queued session, whatever the number of requests, on one
// worker, and requests made before a refresher exists are kept.
func TestBranchStatusCache_RequestsCoalesceOntoOneWorker(t *testing.T) {
	c := newBranchStatusCache()
	c.request("s1")
	c.request("s1")
	c.request("s2")
	if c.pending() != 2 {
		t.Fatalf("pending = %d, want 2 (s1 coalesced)", c.pending())
	}

	var mu sync.Mutex
	var seen []string
	var inFlight, maxInFlight atomic.Int32
	done := make(chan struct{}, 4)
	c.setRefresher(func(id string) {
		n := inFlight.Add(1)
		for {
			m := maxInFlight.Load()
			if n <= m || maxInFlight.CompareAndSwap(m, n) {
				break
			}
		}
		time.Sleep(5 * time.Millisecond)
		inFlight.Add(-1)
		mu.Lock()
		seen = append(seen, id)
		mu.Unlock()
		done <- struct{}{}
	})
	for i := 0; i < 2; i++ {
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatal("refresher did not run for every queued session")
		}
	}
	mu.Lock()
	got := append([]string(nil), seen...)
	mu.Unlock()
	if len(got) != 2 || got[0] != "s1" || got[1] != "s2" {
		t.Fatalf("refreshed %v, want [s1 s2] in request order", got)
	}
	if maxInFlight.Load() != 1 {
		t.Fatalf("refreshes overlapped (%d in flight); the worker is one on purpose", maxInFlight.Load())
	}

	// A session already refreshed can be requested again.
	c.request("s1")
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("second request for s1 never ran")
	}
}

// countingQuerier answers like mockBranchQuerier and counts the git calls a
// status costs, so a test can prove the list did not pay them.
type countingQuerier struct {
	mockBranchQuerier
	calls atomic.Int32
}

func (q *countingQuerier) CommitsAhead(dir, branch string) (int, error) {
	q.calls.Add(1)
	return q.mockBranchQuerier.CommitsAhead(dir, branch)
}

func (s *ServiceSuite) seedWorktreeSession(branch string) string {
	sess := testutil.SeedSession(s.T(), s.Queries, s.Project.ID, "stopped")
	dir := s.T().TempDir()
	s.Require().NoError(s.Queries.UpdateSessionWorktree(context.Background(), store.UpdateSessionWorktreeParams{
		WorkDir:         dir,
		WorktreePath:    sql.NullString{String: dir, Valid: true},
		WorktreeBranch:  sql.NullString{String: branch, Valid: true},
		WorktreeBaseSha: sql.NullString{String: "abc", Valid: true},
		ID:              sess.ID,
	}))
	return sess.ID
}

func infoIn(list ListSessionsResult, id string) SessionInfo {
	for _, info := range list.Sessions {
		if info.ID == id {
			return info
		}
	}
	return SessionInfo{}
}

// session.list never shells out: the first answer is what the cache holds,
// the worker lands the real status as a session.state push, and every list
// after that serves it from the cache without a git call.
func (s *ServiceSuite) TestListSessions_BranchStatusComesFromTheCache() {
	q := &countingQuerier{mockBranchQuerier: mockBranchQuerier{
		branchExists: true,
		ahead:        3,
		mergeResult:  gitops.MergeTreeResult{Clean: true},
	}}
	s.mgr.SetBranchStatusQuerier(q)
	s.svc.SetGitService(NewGitService(s.mgr, s.Queries, s.Broadcaster, testutil.NewMockBlockingRunner()))
	id := s.seedWorktreeSession("feature-cache")
	ctx := context.Background()

	first, err := s.svc.ListSessions(ctx, s.Project.ID)
	s.Require().NoError(err)
	s.Equal(0, infoIn(first, id).CommitsAhead, "a cold list answers before git does")

	s.Require().Eventually(func() bool {
		for _, m := range s.Broadcaster.MessagesOfType("session.state") {
			if snap, ok := m.Payload.(GitSnapshot); ok && snap.SessionID == id && snap.CommitsAhead == 3 {
				return true
			}
		}
		return false
	}, 3*time.Second, 10*time.Millisecond, "the worker must land the status as a session.state push")

	calls := q.calls.Load()
	second, err := s.svc.ListSessions(ctx, s.Project.ID)
	s.Require().NoError(err)
	s.Equal(3, infoIn(second, id).CommitsAhead)
	s.Equal("clean", infoIn(second, id).MergeStatus)
	third, err := s.svc.ListSessions(ctx, s.Project.ID)
	s.Require().NoError(err)
	s.Equal(3, infoIn(third, id).CommitsAhead)
	s.Equal(calls, q.calls.Load(), "a warm list must not shell out")
}
