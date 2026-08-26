package janitor

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

// mangle must reproduce Claude Code's observed scratchpad naming exactly.
func TestMangleMatchesClaudeScheme(t *testing.T) {
	in := "/home/codeuser/.local/share/agentique/worktrees/agentique/session-040e0171"
	want := "-home-codeuser--local-share-agentique-worktrees-agentique-session-040e0171"
	if got := mangle(in); got != want {
		t.Fatalf("mangle mismatch:\n got %q\nwant %q", got, want)
	}
}

const base = "/data/worktrees"

func wt(name string) string { return filepath.Join(base, "proj", name) }

// scenario builds a representative snapshot: a live session, a finished session,
// the caller's own session, an orphan worktree, and a finished-but-dirty session.
func scenario() Inputs {
	now := time.Date(2026, 7, 3, 12, 0, 0, 0, time.UTC)
	sessions := []Session{
		{ID: "live", State: "running", WorktreePath: wt("session-live"), Branch: "session-live", ProjectPath: "/repo", UpdatedAt: now},
		{ID: "done", State: "stopped", Name: "Finished", WorktreePath: wt("session-done"), Branch: "session-done", ProjectPath: "/repo", UpdatedAt: now.Add(-48 * time.Hour)},
		{ID: "self", State: "idle", WorktreePath: wt("session-self"), Branch: "session-self", ProjectPath: "/repo", UpdatedAt: now},
		{ID: "dirty", State: "failed", WorktreePath: wt("session-dirty"), Branch: "session-dirty", ProjectPath: "/repo", UpdatedAt: now.Add(-48 * time.Hour)},
	}
	return Inputs{
		Sessions:     sessions,
		Projects:     map[string]string{"proj": "/repo"},
		LiveIDs:      map[string]bool{"live": true},
		CurrentID:    "self",
		WorktreeBase: base,
		WorktreeDirs: []string{
			wt("session-live"), wt("session-done"), wt("session-self"),
			wt("session-dirty"), wt("session-orphan"),
		},
		ChromeDirs: []string{
			"/tmp/" + chromePrefix + "done",
			"/tmp/" + chromePrefix + "live",
			"/tmp/" + chromePrefix + "ghost", // no session row
		},
		ScratchpadDirs: []string{
			filepath.Join(ScratchpadRoot(), mangle(wt("session-live"))),   // spare (live)
			filepath.Join(ScratchpadRoot(), mangle(wt("session-done"))),   // fate follows worktree
			filepath.Join(ScratchpadRoot(), mangle(wt("session-ghost"))),  // in-namespace, no worktree
			filepath.Join(ScratchpadRoot(), "-home-user-someother-thing"), // foreign — ignore
		},
		Now: now,
	}
}

func reapPaths(p Plan) map[string]Item {
	m := make(map[string]Item)
	for _, it := range p.Reap {
		m[filepath.Clean(it.Path)] = it
	}
	return m
}

func hasReap(p Plan, path string) bool {
	_, ok := reapPaths(p)[filepath.Clean(path)]
	return ok
}

func TestCompute_OrphansOnly_SparesEverythingWithARow(t *testing.T) {
	p := Compute(scenario(), Options{IncludeFinished: false})

	// Only the orphan worktree and the ghost/no-row artifacts are reaped.
	if !hasReap(p, wt("session-orphan")) {
		t.Error("orphan worktree should be reaped")
	}
	for _, kept := range []string{wt("session-live"), wt("session-done"), wt("session-self"), wt("session-dirty")} {
		if hasReap(p, kept) {
			t.Errorf("worktree %s must be spared in orphans-only mode", kept)
		}
	}
	// The reaped orphan carries the resolved project for git-aware removal.
	orphan := reapPaths(p)[filepath.Clean(wt("session-orphan"))]
	if orphan.ProjectPath != "/repo" || orphan.Branch != "session-orphan" {
		t.Errorf("orphan worktree missing resolved project/branch: %+v", orphan)
	}
	// Chrome: only the no-row ghost profile is reaped; done (terminal, has row) is spared.
	if !hasReap(p, "/tmp/"+chromePrefix+"ghost") {
		t.Error("no-row chrome profile should be reaped")
	}
	if hasReap(p, "/tmp/"+chromePrefix+"done") {
		t.Error("terminal-session chrome profile must be spared without --include-finished")
	}
	if hasReap(p, "/tmp/"+chromePrefix+"live") {
		t.Error("live chrome profile must never be reaped")
	}
	// Scratchpads: ghost (in-namespace, no worktree) reaped; live/done spared; foreign ignored.
	if !hasReap(p, filepath.Join(ScratchpadRoot(), mangle(wt("session-ghost")))) {
		t.Error("ghost scratchpad should be reaped")
	}
	if hasReap(p, filepath.Join(ScratchpadRoot(), mangle(wt("session-live")))) {
		t.Error("live worktree's scratchpad must be spared")
	}
	if hasReap(p, filepath.Join(ScratchpadRoot(), mangle(wt("session-done")))) {
		t.Error("kept (finished, not reaped) worktree's scratchpad must be spared")
	}
	if hasReap(p, filepath.Join(ScratchpadRoot(), "-home-user-someother-thing")) {
		t.Error("foreign scratchpad must never be touched")
	}
}

// A scratchpad's directory name carries only a mangled worktree path, so without
// the forward map a caller reclaiming "this session" would leave the scratchpad
// — usually the largest artifact the session owns — behind.
func TestCompute_ScratchpadCarriesItsSessionID(t *testing.T) {
	p := Compute(scenario(), Options{IncludeFinished: true})

	want := filepath.Join(ScratchpadRoot(), mangle(wt("session-done")))
	for _, it := range p.Reap {
		if it.Kind != KindScratchpad || it.Path != want {
			continue
		}
		if it.SessionID != "done" {
			t.Fatalf("scratchpad SessionID = %q, want done", it.SessionID)
		}
		if it.SessionName != "Finished" {
			t.Fatalf("scratchpad SessionName = %q, want Finished", it.SessionName)
		}
		return
	}
	t.Fatalf("finished session's scratchpad was not reaped: %s", want)
}

// The ghost scratchpad maps to no session row at all; it stays attributable to
// nothing rather than being guessed onto a neighbour.
func TestCompute_OrphanScratchpadHasNoSessionID(t *testing.T) {
	p := Compute(scenario(), Options{IncludeFinished: true})

	want := filepath.Join(ScratchpadRoot(), mangle(wt("session-ghost")))
	for _, it := range p.Reap {
		if it.Kind == KindScratchpad && it.Path == want && it.SessionID != "" {
			t.Fatalf("orphan scratchpad SessionID = %q, want empty", it.SessionID)
		}
	}
}

func TestCompute_IncludeFinished_ReapsTerminalNotLive(t *testing.T) {
	p := Compute(scenario(), Options{IncludeFinished: true})

	if !hasReap(p, wt("session-done")) {
		t.Error("finished worktree should be reaped with --include-finished")
	}
	if !hasReap(p, wt("session-orphan")) {
		t.Error("orphan worktree should still be reaped")
	}
	// Live and current session artifacts are never reaped.
	for _, kept := range []string{wt("session-live"), wt("session-self")} {
		if hasReap(p, kept) {
			t.Errorf("worktree %s must be spared (live/current)", kept)
		}
	}
	// The finished worktree's scratchpad follows it into the reap set.
	if !hasReap(p, filepath.Join(ScratchpadRoot(), mangle(wt("session-done")))) {
		t.Error("finished worktree's scratchpad should be reaped alongside it")
	}
	// The reaped finished worktree carries project/branch for git-aware removal.
	it := reapPaths(p)[filepath.Clean(wt("session-done"))]
	if it.ProjectPath != "/repo" || it.Branch != "session-done" {
		t.Errorf("finished worktree item missing git metadata: %+v", it)
	}
	if it.Kind != KindFinishedWorktree {
		t.Errorf("expected KindFinishedWorktree, got %s", it.Kind)
	}
	if reapPaths(p)[filepath.Clean(wt("session-orphan"))].Kind != KindOrphanWorktree {
		t.Error("orphan worktree should be KindOrphanWorktree")
	}
}

func TestCompute_DirtyGuard(t *testing.T) {
	dirtyChecker := func(path string) bool { return filepath.Clean(path) == filepath.Clean(wt("session-dirty")) }

	// Without IncludeDirty the dirty finished worktree is spared and reported.
	p := Compute(scenario(), Options{IncludeFinished: true, Dirty: dirtyChecker})
	if hasReap(p, wt("session-dirty")) {
		t.Error("dirty worktree must be spared without --include-dirty")
	}
	foundSkip := false
	for _, s := range p.Skipped {
		if filepath.Clean(s.Path) == filepath.Clean(wt("session-dirty")) {
			foundSkip = true
		}
	}
	if !foundSkip {
		t.Error("dirty worktree should appear in Skipped with a reason")
	}

	// With IncludeDirty it is reaped and flagged Dirty.
	p = Compute(scenario(), Options{IncludeFinished: true, IncludeDirty: true, Dirty: dirtyChecker})
	it, ok := reapPaths(p)[filepath.Clean(wt("session-dirty"))]
	if !ok || !it.Dirty {
		t.Errorf("dirty worktree should be reaped and flagged with --include-dirty: ok=%v item=%+v", ok, it)
	}
}

func TestCompute_OlderThanFilter(t *testing.T) {
	// done/dirty are 48h old; a 72h threshold spares them.
	p := Compute(scenario(), Options{IncludeFinished: true, OlderThan: 72 * time.Hour})
	if hasReap(p, wt("session-done")) {
		t.Error("worktree younger than --older-than must be spared")
	}
	// A 24h threshold lets them through.
	p = Compute(scenario(), Options{IncludeFinished: true, OlderThan: 24 * time.Hour})
	if !hasReap(p, wt("session-done")) {
		t.Error("worktree older than --older-than should be reaped")
	}
}

// TestCompute_EmptySessionsReapsNothing is the key regression guard: an empty
// session set (wrong/fresh DB) must never make real worktrees look orphaned.
func TestCompute_EmptySessionsReapsNothing(t *testing.T) {
	in := scenario()
	in.Sessions = nil // simulate a wrong/fresh DB pointed at a populated disk
	p := Compute(in, Options{IncludeFinished: true})
	if len(p.Reap) != 0 {
		t.Fatalf("empty session set must reap nothing, got %d items: %+v", len(p.Reap), p.Reap)
	}
}

// TestCompute_UnrecognizedProjectSparesOrphan guards the wrong-DB case where
// sessions exist but the orphan's project is unknown.
func TestCompute_UnrecognizedProjectSparesOrphan(t *testing.T) {
	in := scenario()
	in.WorktreeDirs = append(in.WorktreeDirs, filepath.Join(base, "unknownproj", "session-x"))
	p := Compute(in, Options{IncludeFinished: true})
	if hasReap(p, filepath.Join(base, "unknownproj", "session-x")) {
		t.Error("orphan worktree in an unrecognized project must be spared")
	}
}

// fakeRemover records calls and can be told to fail specific paths.
type fakeRemover struct {
	worktrees map[string]bool // path -> git-aware removal happened
	dirs      map[string]bool // path -> plain removal happened
	failPath  string
}

func (f *fakeRemover) RemoveWorktree(_ context.Context, _, _, wtPath string) error {
	if wtPath == f.failPath {
		return errors.New("boom")
	}
	f.worktrees[wtPath] = true
	return nil
}

func (f *fakeRemover) RemoveAll(path string) error {
	if path == f.failPath {
		return errors.New("boom")
	}
	f.dirs[path] = true
	return nil
}

func TestExecute_RoutesRemovalsAndCountsFreed(t *testing.T) {
	p := Plan{Reap: []Item{
		{Kind: KindFinishedWorktree, Path: wt("session-done"), ProjectPath: "/repo", Branch: "session-done", SizeBytes: 1000},
		{Kind: KindOrphanWorktree, Path: wt("session-orphan"), SizeBytes: 500}, // no project => plain removal
		{Kind: KindChromeProfile, Path: "/tmp/" + chromePrefix + "done", SizeBytes: 200},
		{Kind: KindScratchpad, Path: "/tmp/claude-1000/x", SizeBytes: 50},
	}}
	f := &fakeRemover{worktrees: map[string]bool{}, dirs: map[string]bool{}}
	res := Execute(context.Background(), p, f)

	if !f.worktrees[wt("session-done")] {
		t.Error("finished worktree should be removed git-aware")
	}
	if !f.dirs[wt("session-orphan")] {
		t.Error("project-less orphan worktree should use plain removal")
	}
	if !f.dirs["/tmp/"+chromePrefix+"done"] || !f.dirs["/tmp/claude-1000/x"] {
		t.Error("chrome and scratchpad should use plain removal")
	}
	if res.FreedBytes != 1750 {
		t.Errorf("FreedBytes = %d, want 1750", res.FreedBytes)
	}
	if len(res.Failed) != 0 {
		t.Errorf("unexpected failures: %+v", res.Failed)
	}
}

func TestExecute_RecordsFailuresWithoutCountingBytes(t *testing.T) {
	p := Plan{Reap: []Item{
		{Kind: KindChromeProfile, Path: "/tmp/a", SizeBytes: 100},
		{Kind: KindChromeProfile, Path: "/tmp/b", SizeBytes: 200},
	}}
	f := &fakeRemover{worktrees: map[string]bool{}, dirs: map[string]bool{}, failPath: "/tmp/a"}
	res := Execute(context.Background(), p, f)
	if len(res.Failed) != 1 || res.Failed[0].Item.Path != "/tmp/a" {
		t.Errorf("expected failure for /tmp/a, got %+v", res.Failed)
	}
	if res.FreedBytes != 200 {
		t.Errorf("FreedBytes = %d, want 200 (only the successful removal)", res.FreedBytes)
	}
}
