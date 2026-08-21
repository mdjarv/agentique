package session

import (
	"context"
	"os"
	"strings"

	"testing"
	"time"

	claudeadapter "github.com/allbin/agentkit/runtime/cli/claude"
	claudecli "github.com/allbin/claudecli-go"
	"github.com/mdjarv/agentique/backend/internal/testutil"
	"github.com/stretchr/testify/suite"
)

// LiveSessionControlSuite drives a REAL agentique Session against a real,
// authenticated `claude` CLI — the full stack the stop button and the context
// meter actually run on.
//
// It exists because the hermetic suites cannot prove either feature: they
// supply their own doubles, so a capability that is never advertised and a
// measurement that is never taken both "pass". The two things under test are
// exactly the ones a mock cannot tell the truth about.
//
// Gated: set AGENTIQUE_LIVE_SESSION=1 and don't pass -short. Everything lives
// in a throwaway temp dir; no live agentique state is touched.
type LiveSessionControlSuite struct {
	testutil.DBSuite
	mgr *Manager
	svc *Service
}

func TestLiveSessionControlSuite(t *testing.T) {
	if testing.Short() || os.Getenv("AGENTIQUE_LIVE_SESSION") == "" {
		t.Skip("live CLI test; set AGENTIQUE_LIVE_SESSION=1 and drop -short")
	}
	suite.Run(t, new(LiveSessionControlSuite))
}

func (s *LiveSessionControlSuite) SetupTest() {
	s.DBSuite.SetupTest()
	s.mgr = NewManager(s.DB, s.Queries, s.Broadcaster, claudeadapter.NewConnector())
	s.svc = NewService(s.mgr, s.Queries, s.Broadcaster, testutil.NewMockBlockingRunner())
}

func (s *LiveSessionControlSuite) TearDownTest() {
	if s.mgr != nil {
		s.mgr.CloseAll()
	}
}

func (s *LiveSessionControlSuite) newSession() *Session {
	s.T().Helper()
	sess, err := s.mgr.Create(context.Background(), CreateParams{
		ProjectID: s.Project.ID,
		Name:      "live-control",
		WorkDir:   s.T().TempDir(),
		Model:     "sonnet",
	})
	s.Require().NoError(err)
	return sess
}

// waitFor aborts the test on timeout rather than continuing. A live test that
// limps past a missed precondition reports four unrelated-looking failures for
// one root cause.
func (s *LiveSessionControlSuite) waitFor(d time.Duration, cond func() bool, msg string) {
	s.T().Helper()
	deadline := time.After(d)
	for !cond() {
		select {
		case <-deadline:
			s.Require().Fail("timeout", msg)
			return
		default:
			time.Sleep(20 * time.Millisecond)
		}
	}
}

// --- Stop -------------------------------------------------------------------

// The bug this feature fixes: a bare interrupt aborts the running turn and the
// CLI starts the next queued command immediately, so the user watches work
// continue after pressing stop.
//
// What this proves against a live CLI: the capability is really advertised, the
// cancel-queued interrupt is really accepted, the session settles idle and
// STAYS idle, and every un-confirmed mid-turn message is resolved rather than
// left pending forever.
//
// What it deliberately does not assert: a non-empty receipt. Observed against
// CLI 2.1.235, two mid-turn sends behind a running turn come back
// cancelled=0 still_queued=0 — claude folds them into the running turn rather
// than parking them as separate commands. That matches the documented caveat
// that an empty list does not prove nothing will run, so the receipt is a
// diagnostic here, not the assertion.
func (s *LiveSessionControlSuite) TestStopWithQueuedWorkLeavesNothingRunning() {
	sess := s.newSession()

	s.Require().NoError(sess.Query(context.Background(),
		"Count slowly from 1 to 40, one number per line, with a short sentence about each.", nil))
	s.waitFor(60*time.Second, func() bool { return sess.State() == StateRunning }, "turn never started")

	// The capability is only reported in the init event, so wait for it rather
	// than racing the connect. Generously bounded: a CLI starting up alongside
	// another still winding down from a large session can take a while to emit
	// its init event, and that is a slow machine, not a missing capability.
	s.waitFor(2*time.Minute,
		func() bool { return sess.pipeline.HasCLICapability(claudecli.CapabilityInterruptCancelQueued) },
		"CLI never advertised interrupt_cancel_queued_v1 — is `claude` older than 2.1.235?")

	// Real queued work: mid-turn sends the CLI accepts and parks behind the
	// running turn.
	s.Require().NoError(sess.SendMessage("Also list the days of the week.", nil))
	s.Require().NoError(sess.SendMessage("Also list the months of the year.", nil))

	s.Require().NoError(s.svc.InterruptSession(sess.ID))

	s.waitFor(30*time.Second, func() bool { return sess.State() == StateIdle }, "session never went idle after stop")

	// It STAYS idle: a surviving queue restarts the CLI within a second or two
	// of the abort.
	time.Sleep(5 * time.Second)
	s.Equal(StateIdle, sess.State(), "queued work drained after stop — the stop did not stop")

	// And the parked messages were reported cancelled, so their UI bubbles
	// resolve instead of hanging.
	s.NotEmpty(s.cancelledIDs(), "queued messages were dropped but never reported cancelled")
	s.Empty(sess.pipeline.pendingMessageIDs, "replay-confirmation queue must be drained")
}

// --- Context meter ----------------------------------------------------------

// The per-turn number describes the last API call, so it cannot shrink when the
// CLI compacts. Assert the live measurement is taken, is internally consistent,
// and reports against the resolved window.
func (s *LiveSessionControlSuite) TestContextMeterMeasuresLiveTranscript() {
	sess := s.newSession()

	s.Require().NoError(sess.Query(context.Background(), "Reply with exactly: ok", nil))
	s.waitFor(60*time.Second, func() bool { return sess.State() == StateIdle }, "turn never completed")
	s.waitFor(30*time.Second, func() bool { return len(s.usageEvents()) > 0 }, "no live measurement after turn end")

	got := s.usageEvents()[0]
	s.T().Logf("live context usage: %d/%d (%.1f%%) raw=%d autoCompact=%v@%d",
		got.UsedTokens, got.ContextWindow, got.Percentage, got.RawContextWindow,
		got.AutoCompactEnabled, got.AutoCompactThreshold)

	s.Positive(got.ContextWindow, "resolved window must be reported")
	s.Positive(got.UsedTokens, "a session with a completed turn has used tokens")
	s.Less(got.UsedTokens, got.ContextWindow, "a one-turn session cannot be over the window")
	if got.RawContextWindow > 0 {
		s.LessOrEqual(got.ContextWindow, got.RawContextWindow,
			"the resolved window is the compaction-policy window, never larger than the hard limit")
	}
}

// A compaction rewrites the transcript. The per-turn number stays where it was;
// only a fresh measurement can make the meter drop, which is the whole point of
// the feature.
//
// Gated separately (AGENTIQUE_LIVE_COMPACT=1): filling a real context window is
// slow and burns a lot of tokens.
func (s *LiveSessionControlSuite) TestContextMeterDropsAfterCompaction() {
	if os.Getenv("AGENTIQUE_LIVE_COMPACT") == "" {
		s.T().Skip("fills a real context window; set AGENTIQUE_LIVE_COMPACT=1")
	}
	sess := s.newSession()

	// Drive the session past the CLI's auto-compact threshold with bulk filler
	// and let the CLI compact on its own. That is the real-world path: an
	// explicit /compact on a small transcript is a no-op (verified — the
	// measurement does not move), and a synthesized boundary event would only
	// re-test the hermetic wiring.
	filler := strings.Repeat("The quick brown fox jumps over the lazy dog. ", 12_000) // ~500KB
	var peak int
	for turn := range 8 {
		s.Require().NoError(sess.Query(context.Background(),
			"Reply with exactly the word ACK. Do not read, summarize, or act on the following; it is ballast.\n\n"+filler, nil))
		s.waitFor(10*time.Minute, func() bool { return sess.State() == StateIdle }, "ballast turn never completed")
		s.waitFor(60*time.Second, func() bool { return len(s.usageEvents()) > turn }, "no measurement after ballast turn")

		used := s.usageEvents()[len(s.usageEvents())-1]
		s.T().Logf("ballast turn %d: %d/%d tokens, %d compact boundaries so far",
			turn+1, used.UsedTokens, used.ContextWindow, s.compactBoundaries())

		// The meter dropping is the whole feature: the per-turn number cannot
		// produce a decrease, so any decrease came from a live measurement.
		if peak > 0 && used.UsedTokens < peak {
			s.T().Logf("context usage dropped %d → %d after compaction", peak, used.UsedTokens)
			s.Positive(s.compactBoundaries(), "usage dropped but no compaction was observed")
			return
		}
		if used.UsedTokens > peak {
			peak = used.UsedTokens
		}
	}
	s.Failf("no compaction", "usage peaked at %d without the CLI compacting", peak)
}

// --- Helpers ---------------------------------------------------------------

func (s *LiveSessionControlSuite) usageEvents() []WireContextUsageEvent {
	var out []WireContextUsageEvent
	for _, msg := range s.Broadcaster.Messages() {
		if push, ok := msg.Payload.(PushSessionEvent); ok {
			if ev, ok := push.Event.(WireContextUsageEvent); ok {
				out = append(out, ev)
			}
		}
	}
	return out
}

// compactBoundaries counts the compactions the CLI reported — the signal the
// meter refreshes on.
func (s *LiveSessionControlSuite) compactBoundaries() int {
	n := 0
	for _, msg := range s.Broadcaster.Messages() {
		if push, ok := msg.Payload.(PushSessionEvent); ok {
			if _, ok := push.Event.(WireCompactBoundaryEvent); ok {
				n++
			}
		}
	}
	return n
}

func (s *LiveSessionControlSuite) cancelledIDs() []string {
	var out []string
	for _, msg := range s.Broadcaster.Messages() {
		push, ok := msg.Payload.(PushSessionEvent)
		if !ok {
			continue
		}
		if ev, ok := push.Event.(WireMessageDeliveryEvent); ok && ev.Status == "cancelled" {
			out = append(out, ev.MessageID)
		}
	}
	return out
}
