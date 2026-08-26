package session

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/allbin/agentkit/worktree"
	"github.com/mdjarv/agentique/backend/internal/gitops"
	"github.com/mdjarv/agentique/backend/internal/janitor"
	"github.com/mdjarv/agentique/backend/internal/paths"
	"github.com/mdjarv/agentique/backend/internal/store"
)

// JanitorSessions maps DB session rows into the janitor's minimal session view,
// resolving each session's owning project path so a finished worktree can be
// removed git-aware (keeping its branch). Shared by the startup sweep and the
// `prune` CLI so both classify identically.
func JanitorSessions(sessions []store.Session, projectsByID map[string]store.Project) []janitor.Session {
	out := make([]janitor.Session, 0, len(sessions))
	for _, s := range sessions {
		projectPath := ""
		if p, ok := projectsByID[s.ProjectID]; ok {
			projectPath = p.Path
		}
		out = append(out, janitor.Session{
			ID:           s.ID,
			State:        s.State,
			Name:         s.Name,
			WorktreePath: nullStr(s.WorktreePath),
			Branch:       nullStr(s.WorktreeBranch),
			ProjectPath:  projectPath,
			UpdatedAt:    parseDBTime(s.UpdatedAt),
		})
	}
	return out
}

// JanitorProjects maps each project's sanitized name (the worktree's parent
// directory) to its repo path. The janitor uses this both to remove orphan
// worktrees git-aware and to refuse deleting a worktree whose project it does
// not recognize (a wrong/stale DB guard).
func JanitorProjects(projects []store.Project) map[string]string {
	m := make(map[string]string, len(projects))
	for _, p := range projects {
		m[worktree.SanitizeBranch(p.Name)] = p.Path
	}
	return m
}

// parseDBTime parses agentique's RFC3339 timestamp strings; unparseable values
// yield the zero time (treated as "age unknown" by the older-than filter).
func parseDBTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}
	}
	return t
}

// janitorRemover adapts worktreeOps + os.RemoveAll to janitor.Remover.
type janitorRemover struct{ ops worktreeOps }

// NewJanitorRemover returns a janitor.Remover backed by the real git-aware
// worktree operations. Used by the `prune` CLI, which runs out-of-process.
func NewJanitorRemover() janitor.Remover { return janitorRemover{ops: RealWorktreeOps()} }

func (r janitorRemover) RemoveWorktree(ctx context.Context, projectPath, branch, wtPath string) error {
	r.ops.RemoveWorktree(ctx, projectPath, branch, wtPath) // best-effort, logs internally
	if _, err := os.Stat(wtPath); err == nil {
		return fmt.Errorf("worktree still present after removal: %s", wtPath)
	}
	return nil
}

func (r janitorRemover) RemoveAll(path string) error { return os.RemoveAll(path) }

// SweepOrphans reclaims only zero-risk orphaned artifacts: worktrees whose
// session row is gone (and whose project is still recognized), plus /tmp Chrome
// profiles and Claude scratchpads that map to no live/kept session. It never
// touches a session that still has a DB row (finished-session reaping is manual,
// via `agentique prune`). Intended to run once, best-effort, at the production
// server's startup (from the serve command, never from a unit test) — failures
// are logged, never fatal.
func (s *Service) SweepOrphans(ctx context.Context) {
	plan, err := s.buildOrphanPlan(ctx)
	if err != nil {
		slog.Warn("orphan sweep: planning failed", "error", err)
		return
	}
	if len(plan.Reap) == 0 {
		slog.Debug("orphan sweep: nothing to reclaim")
		return
	}
	res := janitor.Execute(ctx, plan, janitorRemover{ops: s.worktree})
	slog.Info("orphan sweep complete",
		"removed", len(res.Removed),
		"failed", len(res.Failed),
		"freed_mb", res.FreedBytes/(1024*1024))
	for _, f := range res.Failed {
		slog.Warn("orphan sweep: removal failed", "kind", f.Item.Kind, "path", f.Item.Path, "error", f.Err)
	}
}

// LiveSessionIDs returns the sessions the runtime still holds. Exposed on the
// Service so a reporting surface can ask "who is live" without reaching into
// the Manager itself.
func (s *Service) LiveSessionIDs() map[string]bool { return s.mgr.LiveIDs() }

// ReclaimSessions removes the on-disk artifacts of the named sessions — the
// checked-out worktree, the Chrome profile, the Claude scratchpad — while
// keeping each session row and its git branch. The session stays resumable:
// recoverWorktree re-provisions from the branch on the next message.
//
// This is the reversible verb, so it does not need Delete's bar. It does need
// the two guards the janitor already applies: a live session is never touched,
// and a worktree with uncommitted or untracked changes is spared. Both are
// re-evaluated here from the server's own snapshot rather than trusted from the
// request — the page that produced these ids may be up to a minute stale, and a
// session that woke up in between must come back as a skip, not a removal.
//
// Requested ids that reach nothing are reported as skips too, with the janitor's
// own reason where it has one. A skip is a normal outcome, not an error.
func (s *Service) ReclaimSessions(ctx context.Context, sessionIDs []string) (janitor.Result, []janitor.Skipped, error) {
	if len(sessionIDs) == 0 {
		return janitor.Result{}, nil, nil
	}
	want := make(map[string]bool, len(sessionIDs))
	for _, id := range sessionIDs {
		if id != "" {
			want[id] = true
		}
	}
	if len(want) == 0 {
		return janitor.Result{}, nil, nil
	}

	plan, err := s.buildPlan(ctx, janitor.Options{
		IncludeFinished: true,
		Dirty:           func(p string) bool { d, _ := gitops.HasUncommittedChanges(p); return d },
	})
	if err != nil {
		return janitor.Result{}, nil, err
	}

	selected := janitor.Plan{}
	reached := make(map[string]bool, len(want))
	for _, item := range plan.Reap {
		if !want[item.SessionID] {
			continue
		}
		selected.Reap = append(selected.Reap, item)
		reached[item.SessionID] = true
	}

	// Why each requested session produced nothing. The janitor's spare reasons
	// are the interesting ones ("session is running", "uncommitted changes");
	// anything else simply had no artifacts left on disk.
	reasons := make(map[string]string, len(want))
	for _, sk := range plan.Skipped {
		if want[sk.SessionID] && reasons[sk.SessionID] == "" {
			reasons[sk.SessionID] = sk.Reason
		}
	}
	var skipped []janitor.Skipped
	for id := range want {
		if reached[id] {
			continue
		}
		reason := reasons[id]
		if reason == "" {
			reason = "nothing left on disk for this session"
		}
		skipped = append(skipped, janitor.Skipped{SessionID: id, Reason: reason})
	}

	if len(selected.Reap) == 0 {
		return janitor.Result{}, skipped, nil
	}

	janitor.EnrichSizes(&selected)
	res := janitor.Execute(ctx, selected, janitorRemover{ops: s.worktree})
	slog.Info("reclaim complete",
		"sessions", len(want),
		"removed", len(res.Removed),
		"skipped", len(skipped),
		"failed", len(res.Failed),
		"freed_mb", res.FreedBytes/(1024*1024))
	for _, f := range res.Failed {
		slog.Warn("reclaim: removal failed", "kind", f.Item.Kind, "path", f.Item.Path, "error", f.Err)
	}
	return res, skipped, nil
}

// buildOrphanPlan gathers the DB + registry + disk snapshot and computes an
// orphans-only plan (IncludeFinished off).
func (s *Service) buildOrphanPlan(ctx context.Context) (janitor.Plan, error) {
	return s.buildPlan(ctx, janitor.Options{IncludeFinished: false})
}

// buildPlan gathers the DB + registry + disk snapshot and computes a plan under
// the given options. Shared by the startup orphan sweep and the reclaim path so
// both classify from an identical picture.
func (s *Service) buildPlan(ctx context.Context, opt janitor.Options) (janitor.Plan, error) {
	sessions, err := s.queries.ListAllSessions(ctx)
	if err != nil {
		return janitor.Plan{}, fmt.Errorf("list sessions: %w", err)
	}
	projects, err := s.queries.ListProjects(ctx)
	if err != nil {
		return janitor.Plan{}, fmt.Errorf("list projects: %w", err)
	}
	projByID := make(map[string]store.Project, len(projects))
	for _, p := range projects {
		projByID[p.ID] = p
	}

	base := paths.WorktreeDir()
	worktreeDirs, err := janitor.DiscoverWorktreeDirs(base)
	if err != nil {
		return janitor.Plan{}, err
	}
	chromeDirs, _ := janitor.DiscoverChromeDirs()
	scratchDirs, _ := janitor.DiscoverScratchpadDirs()

	in := janitor.Inputs{
		Sessions:       JanitorSessions(sessions, projByID),
		Projects:       JanitorProjects(projects),
		LiveIDs:        s.mgr.LiveIDs(),
		WorktreeBase:   base,
		WorktreeDirs:   worktreeDirs,
		ChromeDirs:     chromeDirs,
		ScratchpadDirs: scratchDirs,
		Now:            time.Now(),
	}
	return janitor.Compute(in, opt), nil
}
