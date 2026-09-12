package server

import (
	"errors"
	"strings"
	"testing"

	"github.com/mdjarv/agentique/backend/internal/assistant"
	"github.com/mdjarv/agentique/backend/internal/session"
)

// carryingBusy is what session's git-op lock hands back: the sentence the
// operator reads, carrying session.ErrBusy in Unwrap rather than appending its
// words to it.
type carryingBusy string

func (e carryingBusy) Error() string { return string(e) }
func (e carryingBusy) Unwrap() error { return session.ErrBusy }

// A turn that opened between the check and the yes is its own outcome: "it
// started working" is a different thing to read than "git refused it", and the
// difference is the ErrBusy the git-op lock now carries. The lock carries it
// without appending its words, so this stands in for that shape rather than a
// %w wrap.
func TestGitOpErrorNamesABusySession(t *testing.T) {
	busy := gitOpError("merge", carryingBusy("session is running"))
	var outcome *assistant.OutcomeError
	if !errors.As(busy, &outcome) {
		t.Fatalf("gitOpError() = %v, want an OutcomeError", busy)
	}
	if !strings.Contains(outcome.Outcome, "started working") {
		t.Errorf("outcome = %q, want the busy reading", outcome.Outcome)
	}

	refused := gitOpError("merge", errors.New("session has no worktree branch"))
	if !errors.As(refused, &outcome) {
		t.Fatalf("gitOpError() = %v, want an OutcomeError", refused)
	}
	if !strings.Contains(outcome.Outcome, "could not merge") {
		t.Errorf("outcome = %q, want git's refusal rather than the busy reading", outcome.Outcome)
	}
	if outcome.Detail == "" {
		t.Error("the detail is what the log gets; it must not be dropped")
	}
}

// The two setters share one failure — the CLI is gone — and it reads as an
// outcome rather than as a crash. Anything else is a real failure and stays
// one.
func TestNotLiveIsAnOutcome(t *testing.T) {
	var outcome *assistant.OutcomeError
	if !errors.As(notLiveOutcome(session.ErrNotLive, "the model"), &outcome) {
		t.Fatal("ErrNotLive is not reported as an outcome")
	}
	if !strings.Contains(outcome.Outcome, "the model") {
		t.Errorf("outcome = %q, want it to name what could not be changed", outcome.Outcome)
	}

	other := errors.New("the database is gone")
	if got := notLiveOutcome(other, "the model"); !errors.Is(got, other) {
		t.Errorf("notLiveOutcome() swallowed a real failure: %v", got)
	}
}

// storage's own predicate is unexported, so this one is spelled here; a state
// missing from it reads as live, which is the fail-closed direction.
func TestTerminalStateMatchesStorage(t *testing.T) {
	for state, want := range map[string]bool{
		"done": true, "stopped": true, "failed": true,
		"idle": false, "running": false, "merging": false, "": false,
	} {
		if got := terminalState(state); got != want {
			t.Errorf("terminalState(%q) = %v, want %v", state, got, want)
		}
	}
}
