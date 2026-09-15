package assistant

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// A create a paired machine had and did not answer is not a create that
// failed: the machine may have made the session. So the words say the outcome
// is unknown and rule a retry out, and nothing is journaled as created.
func TestACreateNobodyAnsweredSaysTheOutcomeIsUnknown(t *testing.T) {
	t.Parallel()
	dir := &fakeDirectory{
		projects: []ProjectRow{{ID: "zp", Name: "seisiun", MachineID: "zbook", MachineName: "zbook", Reach: ReachPeer}},
		createErr: &OutcomeUnknownError{Machine: "zbook",
			Err: errors.New("context deadline exceeded (Client.Timeout exceeded while awaiting headers)")},
	}
	svc, _, _ := newTestService(t, WithDirectory(dir), WithDispatcher(&fakeDispatcher{}))

	payload, err := svc.Invoke(context.Background(), VerbCreateSession,
		map[string]any{"project": "seisiun", "machine": "zbook", "prompt": "run the tests"})
	if err != nil {
		t.Fatalf("Invoke() = %v", err)
	}
	if reason, _ := payload[reasonKey].(string); reason != "create-outcome-unknown" {
		t.Errorf("reason = %q, want create-outcome-unknown", reason)
	}
	said, _ := payload["error"].(string)
	if !strings.Contains(said, "UNKNOWN") || !strings.Contains(said, "zbook") || !strings.Contains(said, "do not try it again") {
		t.Errorf("answer = %q, want the machine named, the outcome unknown and a retry ruled out", said)
	}
	if strings.Contains(said, "could not be created") {
		t.Errorf("answer = %q: an unanswered create is not a failed one", said)
	}
	if created := entriesOfKind(t, svc, JournalSessionCreated); len(created) != 0 {
		t.Errorf("journaled %d creations for a create whose outcome nobody knows", len(created))
	}
}

// The same for a prompt: a paired machine that had it and did not answer may
// have started the turn, so "nothing was sent" would be a claim nobody can make.
func TestASendNobodyAnsweredSaysTheOutcomeIsUnknown(t *testing.T) {
	t.Parallel()
	dir := &fakeDirectory{sessions: []SessionRow{{ID: "s1", Name: "Plugin Testing", ProjectName: "seisiun"}}}
	disp := &fakeDispatcher{err: &OutcomeUnknownError{Machine: "zbook", Err: errors.New("connection reset by peer")}}
	svc, _, _ := newTestService(t, WithDirectory(dir), WithDispatcher(disp))

	payload, err := svc.Invoke(context.Background(), VerbRunPrompt,
		map[string]any{"session_id": "s1", "prompt": "run the tests"})
	if err != nil {
		t.Fatalf("Invoke() = %v", err)
	}
	if reason, _ := payload[reasonKey].(string); reason != reasonDispatchUnknown {
		t.Errorf("reason = %q, want %s", reason, reasonDispatchUnknown)
	}
	said, _ := payload["error"].(string)
	if !strings.Contains(said, "UNKNOWN") || strings.Contains(said, "NOTHING WAS SENT") {
		t.Errorf("answer = %q, want the outcome unknown and no claim that nothing went", said)
	}
}

// A create that worked followed by a send nobody answered: the session is real
// and whether the prompt reached it is unknown. Saying it did not go is how
// the same prompt is sent to it twice.
func TestACreatedSessionWhosePromptNobodyAnsweredIsNotCalledEmpty(t *testing.T) {
	t.Parallel()
	dir := &fakeDirectory{
		projects: []ProjectRow{{ID: "p1", Name: "riff", Slug: "riff"}},
		created:  SessionRow{ID: "s9", Name: "Reconnect Fix", ProjectName: "riff"},
	}
	disp := &fakeDispatcher{err: &OutcomeUnknownError{Machine: "zbook", Err: errors.New("i/o timeout")}}
	svc, _, _ := newTestService(t, WithDirectory(dir), WithDispatcher(disp))

	payload, err := svc.Invoke(context.Background(), VerbCreateSession,
		map[string]any{"project": "riff", "prompt": "fix the reconnect"})
	if err != nil {
		t.Fatalf("Invoke() = %v", err)
	}
	said, _ := payload["error"].(string)
	if !strings.Contains(said, "was created") || !strings.Contains(said, "UNKNOWN") {
		t.Errorf("answer = %q, want the session named as created and the prompt's fate unknown", said)
	}
	if strings.Contains(said, "did NOT go") {
		t.Errorf("answer = %q: a prompt nobody answered for is not a prompt that did not go", said)
	}
}
