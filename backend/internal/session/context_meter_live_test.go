package session

import (
	"context"
	"os"
	"testing"
	"time"

	claudeadapter "github.com/allbin/agentkit/runtime/cli/claude"
	codexadapter "github.com/allbin/agentkit/runtime/cli/codex"
	"github.com/mdjarv/agentique/backend/internal/testutil"
	"github.com/stretchr/testify/suite"
)

// LiveContextMeterSuite drives real provider CLIs to prove the meter follows
// the measurement each one PUSHES, rather than only the one it can be asked
// for.
//
// A hermetic suite cannot answer this. The mock never pushes a
// runtime.ContextUsageEvent, so a meter that silently ignored the whole pushed
// stream would pass every test in context_meter_test.go; and the two things
// most likely to be wrong — whether Claude emits at all under
// ConnectParams.PartialMessages, and whether the two sources agree on a
// denominator — are properties of the live CLI, not of the wiring.
//
// Gated: set AGENTIQUE_LIVE_CONTEXT=1 and don't pass -short. Everything lives
// in a throwaway temp dir; no live agentique state is touched.
type LiveContextMeterSuite struct {
	testutil.DBSuite
	mgr *Manager
}

func TestLiveContextMeterSuite(t *testing.T) {
	if testing.Short() || os.Getenv("AGENTIQUE_LIVE_CONTEXT") == "" {
		t.Skip("live CLI test; set AGENTIQUE_LIVE_CONTEXT=1 and drop -short")
	}
	suite.Run(t, new(LiveContextMeterSuite))
}

func (s *LiveContextMeterSuite) SetupTest() {
	s.DBSuite.SetupTest()
	// ClaudeBaselineOptions, not a bare connector: the connector's own options
	// decide what the CLI streams, so a suite that builds its own would be
	// measuring a configuration nobody runs. That is not hypothetical — an
	// earlier version of this file used a bare connector and reported zero
	// stream chunks for a turn that costs sixteen in production.
	s.mgr = NewManager(s.DB, s.Queries, s.Broadcaster,
		claudeadapter.NewConnector(ClaudeBaselineOptions()...))
	s.mgr.SetProviderConnector("codex", codexadapter.NewConnector())
}

func (s *LiveContextMeterSuite) TearDownTest() {
	if s.mgr != nil {
		s.mgr.CloseAll()
	}
}

// Claude pushes the measurement only under ConnectParams.PartialMessages, which
// manager.go sets on every create/resume. Without that this test sees exactly
// one event, at turn end, and the bar is back to where it was before v0.6.0.
func (s *LiveContextMeterSuite) TestClaudeMovesTheMeterDuringATurn() {
	s.assertMovesDuringATurn("claude", "sonnet")
}

// Codex pushes regardless of the flag, and answers Session.ContextUsage from
// the same notification — so both sources are live here, and both go through
// the same reconciliation.
func (s *LiveContextMeterSuite) TestCodexMovesTheMeterDuringATurn() {
	s.assertMovesDuringATurn("codex", "")
}

// assertMovesDuringATurn runs one tool-using turn — a turn is one API call per
// model response, and the measurement arrives per call, so a turn that only
// answers would prove nothing about mid-turn movement — and asserts the meter
// moved before the turn ended, on one window throughout.
func (s *LiveContextMeterSuite) assertMovesDuringATurn(provider, model string) {
	sess, err := s.mgr.Create(context.Background(), CreateParams{
		ProjectID:       s.Project.ID,
		Name:            "live-context-" + provider,
		WorkDir:         s.T().TempDir(),
		Provider:        provider,
		Model:           model,
		AutoApproveMode: "fullAuto",
	})
	s.Require().NoError(err)

	s.Require().NoError(sess.Query(context.Background(),
		"Use your shell tool to run `echo hello`, then reply with exactly: ok", nil))

	// Count first, read state second. In that order every counted event
	// provably landed while the turn was still open; the other order can credit
	// the completion's own measurement to the middle of the turn.
	midTurn := 0
	deadline := time.After(5 * time.Minute)
	for {
		seen := len(s.usage())
		if sess.State() != StateRunning {
			break
		}
		if seen > midTurn {
			midTurn = seen
		}
		select {
		case <-deadline:
			s.Require().Fail("timeout", "turn never completed")
			return
		case <-time.After(50 * time.Millisecond):
		}
	}

	events := s.usage()
	for i, ev := range events {
		s.T().Logf("[%s] usage %d: %d/%d (%.1f%%) raw=%d autoCompact=%v@%d",
			provider, i, ev.UsedTokens, ev.ContextWindow, ev.Percentage,
			ev.RawContextWindow, ev.AutoCompactEnabled, ev.AutoCompactThreshold)
	}
	// PartialMessages buys the mid-turn measurement and pays for it in chunk
	// events. Logged rather than asserted: the trade is the point, and the
	// number is what makes it possible to argue about.
	s.T().Logf("[%s] %d measurements (%d before the turn ended), %d stream chunks for the same turn",
		provider, len(events), midTurn, s.streamEvents())

	s.GreaterOrEqual(midTurn, 2,
		"the meter must move DURING the turn, not only at its end — is PartialMessages set?")

	// One denominator, or the bar visibly jumps. This is the trap the two
	// sources set: the push reports the model's hard window, the query the
	// narrower compaction-policy one.
	window := events[0].ContextWindow
	for i, ev := range events {
		s.Positive(ev.ContextWindow, "measurement %d has no window", i)
		s.Equal(window, ev.ContextWindow, "measurement %d changed the denominator", i)
		s.Positive(ev.UsedTokens, "measurement %d reports no usage", i)
		s.InDelta(float64(ev.UsedTokens)/float64(ev.ContextWindow)*100, ev.Percentage, 0.01,
			"measurement %d's percentage does not match its own numbers", i)
	}
}

// streamEvents counts the per-chunk stream events the same option produces —
// the cost side of the trade PartialMessages makes.
func (s *LiveContextMeterSuite) streamEvents() int {
	n := 0
	for _, msg := range s.Broadcaster.Messages() {
		if push, ok := msg.Payload.(PushSessionEvent); ok {
			if _, ok := push.Event.(WireStreamEvent); ok {
				n++
			}
		}
	}
	return n
}

func (s *LiveContextMeterSuite) usage() []WireContextUsageEvent {
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
