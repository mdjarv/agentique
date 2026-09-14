package peer

import (
	"testing"
	"time"
)

func TestJudgeSend(t *testing.T) {
	on := Settings{AcceptActions: true}
	good := SendFacts{WorktreeBranch: "session-abc", AutoApproveMode: "fullAuto"}
	tests := []struct {
		name     string
		settings Settings
		policy   string
		facts    SendFacts
		want     string
	}{
		{"off by default", Settings{}, "", good, ReasonActionsOff},
		{"accepted", on, "", good, ""},
		{"policy needs its own opt-in", on, "p1", good, ReasonPoliciesOff},
		{"policy accepted", Settings{AcceptActions: true, AcceptPolicies: true}, "p1", good, ""},
		{"policies without actions is still off", Settings{AcceptPolicies: true}, "p1", good, ReasonActionsOff},
		{"archived", on, "", SendFacts{Archived: true, WorktreeBranch: "b", AutoApproveMode: "fullAuto"}, ReasonArchived},
		{"main worktree", on, "", SendFacts{AutoApproveMode: "fullAuto"}, ReasonMainWorktree},
		{"accept edits can still block", on, "", SendFacts{WorktreeBranch: "b", AutoApproveMode: "acceptEdits"}, ReasonNotFullAuto},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := JudgeSend(tt.settings, tt.policy, tt.facts).Reason; got != tt.want {
				t.Errorf("reason = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestJudgeCreate(t *testing.T) {
	on := Settings{AcceptActions: true}
	if got := JudgeCreate(Settings{}, "", 0).Reason; got != ReasonActionsOff {
		t.Errorf("off: %q", got)
	}
	if got := JudgeCreate(on, "", MaxInFlightAssistant-1).Reason; got != "" {
		t.Errorf("under the cap: %q", got)
	}
	if got := JudgeCreate(on, "", MaxInFlightAssistant).Reason; got != ReasonInFlight {
		t.Errorf("at the cap: %q", got)
	}
}

func TestLimiterSlides(t *testing.T) {
	now := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)
	l := newLimiter(func() time.Time { return now })
	for i := 0; i < 3; i++ {
		if !l.take("k", 3, time.Minute) {
			t.Fatalf("take %d refused under the limit", i)
		}
	}
	if l.take("k", 3, time.Minute) {
		t.Fatal("fourth take inside the window was allowed")
	}
	if !l.take("other", 3, time.Minute) {
		t.Fatal("keys share a window")
	}
	now = now.Add(61 * time.Second)
	if !l.take("k", 3, time.Minute) {
		t.Fatal("window did not slide")
	}
}
