package session

import (
	"context"
	"strings"
	"time"

	"github.com/mdjarv/agentique/backend/internal/store"
	"github.com/mdjarv/agentique/backend/internal/testutil"
)

// End-to-end: with MemoryRecallFn wired on the Manager, a session's FIRST turn
// prepends the task-relevant recall block to both the runtime query and the
// persisted prompt event — and never again, so cost stays bounded to one lookup.
func (s *LifecycleSuite) TestFirstTurnInjectsRecall() {
	var seen []string // raw prompts the recall fn was asked about
	s.mgr.MemoryRecallFn = func(_ context.Context, projectID, prompt string) string {
		s.Equal(s.Project.ID, projectID) // wired with the session's project
		seen = append(seen, prompt)
		return "> **Recalled** — deploys run on Tuesdays."
	}

	sess := s.createSession()

	// Turn 1: recall block prepended, recall fn sees the RAW task prompt.
	s.Require().NoError(sess.Query(context.Background(), "when do we deploy?", nil))
	mock := s.Connector.Last()
	q := mock.Queries()
	s.Require().Len(q, 1)
	s.Contains(q[0], "deploys run on Tuesdays")
	s.Contains(q[0], "when do we deploy?")
	s.Less(strings.Index(q[0], "Recalled"), strings.Index(q[0], "when do we deploy?"),
		"recall block should precede the user's prompt")
	s.Equal([]string{"when do we deploy?"}, seen)

	// The persisted prompt event carries the augmented prompt, so the recalled
	// facts are visible in the transcript (not hidden like the system preamble).
	var promptData string
	s.Require().Eventually(func() bool {
		events, err := s.Queries.ListEventsBySession(context.Background(), sess.ID)
		if err != nil || len(events) == 0 || events[0].Type != "prompt" {
			return false
		}
		promptData = events[0].Data
		return true
	}, 2*time.Second, 5*time.Millisecond, "prompt not persisted")
	s.Contains(promptData, "deploys run on Tuesdays")

	// Complete turn 1.
	s.Require().NoError(mock.Inject(testutil.TextEvent("ok")))
	s.Require().NoError(mock.Inject(testutil.ResultEvent(0.01)))
	s.waitForState(sess, StateIdle, 2*time.Second)

	// Turn 2: fire-once gate — no recall injected, fn not called again.
	s.Require().NoError(sess.Query(context.Background(), "and on weekends?", nil))
	q = mock.Queries()
	s.Require().Len(q, 2)
	s.NotContains(q[1], "deploys run on Tuesdays")
	s.Equal("and on weekends?", q[1])
	s.Len(seen, 1)
}

// With no MemoryRecallFn wired (recall disabled), the prompt passes through
// untouched — clean degradation.
func (s *LifecycleSuite) TestRecallDisabledPassesThrough() {
	s.mgr.MemoryRecallFn = nil
	sess := s.createSession()

	s.Require().NoError(sess.Query(context.Background(), "plain prompt", nil))
	q := s.Connector.Last().Queries()
	s.Require().Len(q, 1)
	s.Equal("plain prompt", q[0])

	var events []store.SessionEvent
	s.Require().Eventually(func() bool {
		var err error
		events, err = s.Queries.ListEventsBySession(context.Background(), sess.ID)
		return err == nil && len(events) >= 1 && events[0].Type == "prompt"
	}, 2*time.Second, 5*time.Millisecond, "prompt not persisted")
	s.Contains(events[0].Data, "plain prompt")
	s.NotContains(events[0].Data, "Recalled")
}
