package storage

import (
	"errors"
	"testing"
)

// fakeProbe answers the two git questions from a fixture, and counts calls so a
// test can assert that a fast path actually skipped the exec.
type fakeProbe struct {
	ahead      int
	aheadErr   error
	dirty      bool
	dirtyErr   error
	aheadCalls int
	dirtyCalls int
}

func (p *fakeProbe) CommitsAhead(string, string) (int, error) {
	p.aheadCalls++
	return p.ahead, p.aheadErr
}

func (p *fakeProbe) HasUncommittedChanges(string) (bool, error) {
	p.dirtyCalls++
	return p.dirty, p.dirtyErr
}

// base is a finished, clean, unmerged session with a branch — the ordinary case.
func base() SafetyInput {
	return SafetyInput{
		Terminal:     true,
		ProjectPath:  "/repos/proj",
		Branch:       "session-abc",
		WorktreePath: "/data/worktrees/proj/session-abc",
	}
}

func TestEvaluate(t *testing.T) {
	tests := []struct {
		name        string
		probe       fakeProbe
		in          SafetyInput
		wantSafety  DeleteSafety
		wantReclaim bool
	}{
		{
			name:        "merged into HEAD and clean is safe for both verbs",
			probe:       fakeProbe{ahead: 0},
			in:          base(),
			wantSafety:  DeleteSafe,
			wantReclaim: true,
		},
		{
			// The finding this whole change exists for: the flag says "agentique
			// did not merge this", git says the commits are already on HEAD.
			name:        "unflagged but zero commits ahead is still safe",
			probe:       fakeProbe{ahead: 0},
			in:          func() SafetyInput { in := base(); in.Merged = false; return in }(),
			wantSafety:  DeleteSafe,
			wantReclaim: true,
		},
		{
			name:        "commits ahead blocks delete but not reclaim",
			probe:       fakeProbe{ahead: 2},
			in:          base(),
			wantSafety:  DeleteBlockedAhead,
			wantReclaim: true,
		},
		{
			name:        "uncommitted changes block both verbs",
			probe:       fakeProbe{dirty: true},
			in:          base(),
			wantSafety:  DeleteBlockedDirty,
			wantReclaim: false,
		},
		{
			// A merged branch with uncommitted edits still has something to lose,
			// so the dirty check must run before the merged fast path.
			name:        "merged flag does not override a dirty tree",
			probe:       fakeProbe{dirty: true},
			in:          func() SafetyInput { in := base(); in.Merged = true; return in }(),
			wantSafety:  DeleteBlockedDirty,
			wantReclaim: false,
		},
		{
			name:        "a running session is never touched",
			probe:       fakeProbe{ahead: 0},
			in:          func() SafetyInput { in := base(); in.Terminal = false; return in }(),
			wantSafety:  DeleteBlockedLive,
			wantReclaim: false,
		},
		{
			name:        "a terminal session the runtime still holds is never touched",
			probe:       fakeProbe{ahead: 0},
			in:          func() SafetyInput { in := base(); in.Live = true; return in }(),
			wantSafety:  DeleteBlockedLive,
			wantReclaim: false,
		},
		{
			name:        "an unanswerable rev-list fails closed on delete only",
			probe:       fakeProbe{aheadErr: errors.New("no such branch")},
			in:          base(),
			wantSafety:  DeleteBlockedUnknown,
			wantReclaim: true,
		},
		{
			name:        "an unanswerable status fails closed on both verbs",
			probe:       fakeProbe{dirtyErr: errors.New("not a repository")},
			in:          base(),
			wantSafety:  DeleteBlockedUnknown,
			wantReclaim: false,
		},
		{
			name:        "a session with no branch cannot be proven safe",
			probe:       fakeProbe{ahead: 0},
			in:          func() SafetyInput { in := base(); in.Branch = ""; return in }(),
			wantSafety:  DeleteBlockedUnknown,
			wantReclaim: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Evaluate(&tt.probe, tt.in)
			if got.Safety != tt.wantSafety {
				t.Errorf("Safety = %q, want %q", got.Safety, tt.wantSafety)
			}
			if got.Reclaimable != tt.wantReclaim {
				t.Errorf("Reclaimable = %v, want %v", got.Reclaimable, tt.wantReclaim)
			}
			if got.Safety.Safe() != (got.Safety == DeleteSafe) {
				t.Errorf("Safe() disagrees with the verdict %q", got.Safety)
			}
		})
	}
}

// The merged flag exists to skip an exec, so prove that it does — otherwise it
// is just a second source of truth.
func TestEvaluateMergedFlagSkipsTheRevList(t *testing.T) {
	p := &fakeProbe{ahead: 99} // would say "ahead" if it were consulted
	in := base()
	in.Merged = true

	got := Evaluate(p, in)
	if got.Safety != DeleteSafe {
		t.Fatalf("Safety = %q, want %q", got.Safety, DeleteSafe)
	}
	if p.aheadCalls != 0 {
		t.Errorf("CommitsAhead called %d times, want 0 — the flag should short-circuit it", p.aheadCalls)
	}
	if p.dirtyCalls != 1 {
		t.Errorf("HasUncommittedChanges called %d times, want 1", p.dirtyCalls)
	}
}

// A live session must cost nothing: no git calls at all, so a page full of busy
// sessions does not pay for verdicts it will not offer.
func TestEvaluateLiveSessionRunsNoGit(t *testing.T) {
	p := &fakeProbe{}
	in := base()
	in.Live = true

	Evaluate(p, in)
	if p.aheadCalls != 0 || p.dirtyCalls != 0 {
		t.Errorf("probed a live session: ahead=%d dirty=%d, want 0/0", p.aheadCalls, p.dirtyCalls)
	}
}

func TestDeleteSafetyReasonIsEmptyOnlyWhenSafe(t *testing.T) {
	all := []DeleteSafety{
		DeleteSafe, DeleteBlockedLive, DeleteBlockedAhead,
		DeleteBlockedDirty, DeleteBlockedUnknown,
	}
	for _, s := range all {
		reason := s.Reason()
		if (reason == "") != (s == DeleteSafe) {
			t.Errorf("%q: Reason() = %q — a blocked verdict must explain itself", s, reason)
		}
	}
}
