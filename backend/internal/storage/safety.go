package storage

import (
	"github.com/mdjarv/agentique/backend/internal/gitops"
)

// DeleteSafety says whether a session may be deleted outright — row, branch and
// worktree — without losing work that exists nowhere else.
//
// The bar for a bulk destructive action is "the commits are already on the
// project's main line". `worktree_merged` records something narrower: that
// agentique itself performed the merge. A branch merged from a terminal or via a
// PR never sets it, so on a repo worked that way the flag is false for every
// session and the bulk affordance it gates can never appear.
//
// The fact itself is one `rev-list` away, so ask git. The flag stays as a fast
// path that skips the exec, never as the definition.
//
// The verdict fails closed: anything git cannot answer is Unknown, and Unknown
// is not safe.
type DeleteSafety string

const (
	// DeleteSafe — the branch adds nothing the project HEAD does not already
	// have, and the working tree is clean.
	DeleteSafe DeleteSafety = "safe"
	// DeleteBlockedLive — the session is running, idle, or held by the runtime.
	DeleteBlockedLive DeleteSafety = "live"
	// DeleteBlockedAhead — the branch carries commits the project HEAD does not.
	DeleteBlockedAhead DeleteSafety = "ahead"
	// DeleteBlockedDirty — the worktree has uncommitted or untracked changes.
	DeleteBlockedDirty DeleteSafety = "dirty"
	// DeleteBlockedUnknown — git could not answer (missing branch, unreadable
	// repo). Treated as unsafe.
	DeleteBlockedUnknown DeleteSafety = "unknown"
)

// Safe reports whether this verdict clears the bulk-delete bar.
func (s DeleteSafety) Safe() bool { return s == DeleteSafe }

// Reason is the phrase a UI puts in front of a blocked delete. Empty for
// DeleteSafe, because there is nothing to explain.
//
// Note what the dirty check does *not* cover: `git status --porcelain` lists
// untracked files but not ignored ones, which is exactly why a worktree with
// 591 MiB of node_modules reads clean. A `.env` is ignored the same way. The
// copy in front of Delete has to admit that rather than imply the check is
// total.
func (s DeleteSafety) Reason() string {
	switch s {
	case DeleteSafe:
		return ""
	case DeleteBlockedLive:
		return "still running"
	case DeleteBlockedAhead:
		return "has commits that are not on the main branch"
	case DeleteBlockedDirty:
		return "has uncommitted changes"
	default:
		return "could not be checked against the main branch"
	}
}

// SafetyProbe answers the two git questions a verdict needs. Split out from the
// verdict so the rules are testable without a repository on disk.
type SafetyProbe interface {
	// CommitsAhead counts commits on branch that the project's HEAD lacks.
	CommitsAhead(projectPath, branch string) (int, error)
	// HasUncommittedChanges reports whether a worktree has staged, unstaged or
	// untracked changes.
	HasUncommittedChanges(worktreePath string) (bool, error)
}

// gitProbe is the real probe, backed by the repo's own git helpers.
type gitProbe struct{}

// RealSafetyProbe returns the SafetyProbe that shells out to git.
func RealSafetyProbe() SafetyProbe { return gitProbe{} }

func (gitProbe) CommitsAhead(projectPath, branch string) (int, error) {
	return gitops.CommitsAhead(projectPath, branch)
}

func (gitProbe) HasUncommittedChanges(worktreePath string) (bool, error) {
	return gitops.HasUncommittedChanges(worktreePath)
}

// SafetyInput is everything the verdict needs about one session.
type SafetyInput struct {
	// Terminal is true when the session's state is stopped, done or failed.
	Terminal bool
	// Live is true when the runtime still holds the session, whatever the
	// persisted state says.
	Live bool
	// Merged is the `worktree_merged` flag — a fast path only.
	Merged bool

	ProjectPath  string
	Branch       string
	WorktreePath string
}

// Verdicts is the pair of judgements the Storage page needs about one session.
// They are computed together because they share the same two git questions, and
// asking twice would double the exec count on a page that already walks the
// whole worktree tree.
type Verdicts struct {
	// Reclaimable: the reversible verb is offered. Requires only that the
	// session is finished, not held by the runtime, and has no uncommitted work
	// — the branch is kept, so what the branch contains does not matter.
	Reclaimable bool
	// Safety is the delete verdict.
	Safety DeleteSafety
}

// Evaluate judges one session for both verbs.
//
// Order matters. Liveness first: it is the cheapest test and the most absolute,
// and it spares the git calls entirely for the sessions most likely to be hurt
// by them. Then the dirty check, which protects work regardless of what the
// branch looks like — a fully merged branch with uncommitted edits still has
// something to lose, and reclaiming it would throw those edits away. Only after
// the tree is known clean does the merged flag get to skip the rev-list.
func Evaluate(p SafetyProbe, in SafetyInput) Verdicts {
	if in.Live || !in.Terminal {
		return Verdicts{Safety: DeleteBlockedLive}
	}
	if in.WorktreePath != "" {
		dirty, err := p.HasUncommittedChanges(in.WorktreePath)
		if err != nil {
			// Cannot establish that the tree is clean: offer neither verb.
			return Verdicts{Safety: DeleteBlockedUnknown}
		}
		if dirty {
			return Verdicts{Safety: DeleteBlockedDirty}
		}
	}

	// Clean and finished: reclaiming is safe from here on, whatever git says
	// about the branch.
	v := Verdicts{Reclaimable: true}

	switch {
	case in.Merged:
		v.Safety = DeleteSafe
	case in.ProjectPath == "" || in.Branch == "":
		v.Safety = DeleteBlockedUnknown
	default:
		ahead, err := p.CommitsAhead(in.ProjectPath, in.Branch)
		switch {
		case err != nil:
			v.Safety = DeleteBlockedUnknown
		case ahead > 0:
			v.Safety = DeleteBlockedAhead
		default:
			v.Safety = DeleteSafe
		}
	}
	return v
}
