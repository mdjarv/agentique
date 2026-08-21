package session

import (
	"testing"

	"github.com/allbin/agentkit/runtime"
)

// resolvedIDs splits recorded broadcasts into approval and question
// resolutions.
func resolvedIDs(msgs []any) (approvals, questions []string) {
	for _, m := range msgs {
		switch p := m.(type) {
		case PushApprovalResolved:
			approvals = append(approvals, p.ApprovalID)
		case PushQuestionResolved:
			questions = append(questions, p.QuestionID)
		}
	}
	return approvals, questions
}

func newWithdrawalTestSession() (*Session, *[]any) {
	var got []any
	sess := newPermTestSession("manual", "default")
	sess.broadcast = func(_ string, payload any) { got = append(got, payload) }
	return sess, &got
}

// A prompt can now leave the runtime's queue with no user answer. There is no
// reply path to broadcast a resolution on, so without noticing the
// disappearance the UI keeps a dead banner up and every click on it fails with
// ErrPendingNotFound.
func TestResolveWithdrawnPrompts_ClearsVanishedApproval(t *testing.T) {
	sess, got := newWithdrawalTestSession()
	defer sess.cancelCtx()

	sess.resolveWithdrawnPrompts(&runtime.PendingApproval{ID: "a1"}, nil)
	if approvals, _ := resolvedIDs(*got); len(approvals) != 0 {
		t.Fatalf("surfacing an approval must not resolve anything, got %v", approvals)
	}

	// The runtime withdraws it: pending state is empty, no answer submitted.
	sess.resolveWithdrawnPrompts(nil, nil)

	approvals, _ := resolvedIDs(*got)
	if len(approvals) != 1 || approvals[0] != "a1" {
		t.Errorf("resolved approvals = %v, want [a1]", approvals)
	}
}

func TestResolveWithdrawnPrompts_ClearsVanishedQuestion(t *testing.T) {
	sess, got := newWithdrawalTestSession()
	defer sess.cancelCtx()

	sess.resolveWithdrawnPrompts(nil, &runtime.PendingQuestion{ID: "q1"})
	sess.resolveWithdrawnPrompts(nil, nil)

	_, questions := resolvedIDs(*got)
	if len(questions) != 1 || questions[0] != "q1" {
		t.Errorf("resolved questions = %v, want [q1]", questions)
	}
}

// A prompt replaced by a different one is just as gone as one withdrawn to
// nothing — the UI must not keep showing the old id.
func TestResolveWithdrawnPrompts_ClearsReplacedPrompt(t *testing.T) {
	sess, got := newWithdrawalTestSession()
	defer sess.cancelCtx()

	sess.resolveWithdrawnPrompts(&runtime.PendingApproval{ID: "a1"}, nil)
	sess.resolveWithdrawnPrompts(&runtime.PendingApproval{ID: "a2"}, nil)

	approvals, _ := resolvedIDs(*got)
	if len(approvals) != 1 || approvals[0] != "a1" {
		t.Errorf("resolved approvals = %v, want [a1] (replaced, not a2)", approvals)
	}
}

// Repeated events for the same still-pending prompt must not resolve it —
// that would clear a banner the user is actively looking at.
func TestResolveWithdrawnPrompts_StablePromptIsNotResolved(t *testing.T) {
	sess, got := newWithdrawalTestSession()
	defer sess.cancelCtx()

	for range 3 {
		sess.resolveWithdrawnPrompts(&runtime.PendingApproval{ID: "a1"}, &runtime.PendingQuestion{ID: "q1"})
	}

	approvals, questions := resolvedIDs(*got)
	if len(approvals) != 0 || len(questions) != 0 {
		t.Errorf("resolved %v/%v, want nothing while both stay pending", approvals, questions)
	}
}

// Once cleared, the same id must not be resolved a second time.
func TestResolveWithdrawnPrompts_ResolvesOnce(t *testing.T) {
	sess, got := newWithdrawalTestSession()
	defer sess.cancelCtx()

	sess.resolveWithdrawnPrompts(&runtime.PendingApproval{ID: "a1"}, nil)
	sess.resolveWithdrawnPrompts(nil, nil)
	sess.resolveWithdrawnPrompts(nil, nil)

	approvals, _ := resolvedIDs(*got)
	if len(approvals) != 1 {
		t.Errorf("resolved approvals = %v, want exactly one clear", approvals)
	}
}

// An approval and a question are tracked independently: one going away must
// not clear the other.
func TestResolveWithdrawnPrompts_TracksApprovalAndQuestionIndependently(t *testing.T) {
	sess, got := newWithdrawalTestSession()
	defer sess.cancelCtx()

	sess.resolveWithdrawnPrompts(&runtime.PendingApproval{ID: "a1"}, &runtime.PendingQuestion{ID: "q1"})
	sess.resolveWithdrawnPrompts(nil, &runtime.PendingQuestion{ID: "q1"})

	approvals, questions := resolvedIDs(*got)
	if len(approvals) != 1 || approvals[0] != "a1" {
		t.Errorf("resolved approvals = %v, want [a1]", approvals)
	}
	if len(questions) != 0 {
		t.Errorf("resolved questions = %v, want none — q1 is still pending", questions)
	}
}
