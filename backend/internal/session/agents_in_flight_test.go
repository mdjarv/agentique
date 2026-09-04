package session

import (
	"context"
	"testing"
	"time"

	"github.com/allbin/agentkit/runtime"
	"github.com/mdjarv/agentique/backend/internal/testutil"
)

// The sidebar and deck cannot show "agents out" for a session whose event
// stream they never loaded, so the count rides session.state. These tests pin
// the counting semantics the wire field is built on.

func TestPipeline_AgentsInFlightCounting(t *testing.T) {
	sink := newTestSink()
	var counts []int
	p := newTestPipeline(sink, func(cfg *PipelineConfig) {
		cfg.OnAgentsInFlight = func(n int) { counts = append(counts, n) }
	})
	p.AdvanceTurn()

	// A background shell task rides the same stream and is never counted.
	p.ProcessEvent(runtime.SubagentEvent{Subtype: "task_started", TaskID: "sh1"})
	// Two subagents out.
	p.ProcessEvent(runtime.SubagentEvent{Subtype: "task_started", TaskID: "a1", TaskType: "local_agent"})
	p.ProcessEvent(runtime.SubagentEvent{Subtype: "task_started", TaskID: "a2", TaskType: "local_agent"})
	// A duplicate start is not a third agent.
	p.ProcessEvent(runtime.SubagentEvent{Subtype: "task_started", TaskID: "a1", TaskType: "local_agent"})
	// The terminal arrives twice (a progress and a notification both report
	// it) and, on older CLIs, without a taskType — decrement exactly once,
	// judged by membership rather than the event's own stamping.
	p.ProcessEvent(runtime.SubagentEvent{Subtype: "task_progress", TaskID: "a1", Status: "completed"})
	p.ProcessEvent(runtime.SubagentEvent{Subtype: "task_notification", TaskID: "a1", Status: "completed"})
	// The shell task's terminal touches nothing.
	p.ProcessEvent(runtime.SubagentEvent{Subtype: "task_notification", TaskID: "sh1", Status: "completed"})
	// A deliberate shutdown ends a run the same as a completion does.
	p.ProcessEvent(runtime.SubagentEvent{Subtype: "task_notification", TaskID: "a2", Status: "stopped"})

	want := []int{1, 2, 1, 0}
	if len(counts) != len(want) {
		t.Fatalf("callback fired %d times (%v), want %v", len(counts), counts, want)
	}
	for i := range want {
		if counts[i] != want[i] {
			t.Fatalf("counts = %v, want %v", counts, want)
		}
	}
	if got := p.AgentsInFlight(); got != 0 {
		t.Errorf("AgentsInFlight = %d, want 0", got)
	}
}

// A background subagent outlives the turn that spawned it — the count must
// survive the turn boundary and reset only with the CLI process.
func TestPipeline_AgentsInFlightSurvivesTurnEndAndResets(t *testing.T) {
	sink := newTestSink()
	fired := 0
	p := newTestPipeline(sink, func(cfg *PipelineConfig) {
		cfg.OnAgentsInFlight = func(int) { fired++ }
	})
	p.AdvanceTurn()

	p.ProcessEvent(runtime.SubagentEvent{Subtype: "task_started", TaskID: "bg", TaskType: "local_agent"})
	p.ProcessEvent(runtime.TurnCompletedEvent{Status: runtime.TurnStatusCompleted})

	if got := p.AgentsInFlight(); got != 1 {
		t.Fatalf("AgentsInFlight after turn end = %d, want 1 (background agent still out)", got)
	}

	firedBefore := fired
	p.ResetAgentTasks()
	if got := p.AgentsInFlight(); got != 0 {
		t.Errorf("AgentsInFlight after reset = %d, want 0", got)
	}
	if fired != firedBefore {
		t.Error("ResetAgentTasks must not fire the callback — the stop's own state push carries the zero")
	}
}

// waitForAgentsPush polls the captured broadcasts for a session.state
// snapshot carrying the wanted in-flight count. The field is a pointer on the
// wire: present-and-zero is a reading, absent means "not reported".
func (s *ServiceSuite) waitForAgentsPush(sessionID string, want int) {
	s.T().Helper()
	deadline := time.After(2 * time.Second)
	for {
		for _, m := range s.Broadcaster.MessagesOfType("session.state") {
			snap, ok := m.Payload.(GitSnapshot)
			if ok && snap.SessionID == sessionID && snap.AgentsInFlight != nil && *snap.AgentsInFlight == want {
				return
			}
		}
		select {
		case <-deadline:
			s.Require().Failf("timeout waiting for agentsInFlight push", "want %d", want)
		default:
			time.Sleep(5 * time.Millisecond)
		}
	}
}

func (s *ServiceSuite) TestAgentsInFlightRidesStatePushAndStopZeroes() {
	sessionID, mock := s.createLiveSession()
	sess := s.mgr.Get(sessionID)
	s.Require().NotNil(sess)
	s.Require().NoError(sess.Query(context.Background(), "spawn a background agent", nil))

	s.Require().NoError(mock.Inject(runtime.SubagentEvent{
		Subtype: "task_started", TaskID: "a1", TaskType: "local_agent", Description: "research",
	}))
	s.Require().Eventually(func() bool { return sess.AgentsInFlight() == 1 },
		2*time.Second, 5*time.Millisecond, "mirror never learned the agent")
	s.waitForAgentsPush(sessionID, 1)

	// The turn ends; the background agent stays out.
	s.Require().NoError(mock.Inject(testutil.ResultEvent(0.01)))
	waitForState(s.T(), sess, StateIdle)
	s.Equal(1, sess.AgentsInFlight(), "a background agent outlives the turn that spawned it")

	// Stopping the CLI takes its children with it: the stopped push carries
	// the zero, and the mirror agrees.
	s.Broadcaster.Reset()
	s.Require().NoError(s.svc.StopSession(context.Background(), sessionID))
	s.waitForAgentsPush(sessionID, 0)
	s.Require().Eventually(func() bool { return sess.AgentsInFlight() == 0 },
		2*time.Second, 5*time.Millisecond, "mirror never reset on stop")
}
