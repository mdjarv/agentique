package janitor

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
)

const chromePrefix = "agentique-chrome-"

// ChromeProfilePath returns the Chrome --user-data-dir agentique uses for a
// session. Mirrors browser.Manager's own construction; kept here so cleanup and
// launch derive the same path.
func ChromeProfilePath(sessionID string) string {
	return filepath.Join(os.TempDir(), chromePrefix+sessionID)
}

// ScratchpadRoot returns the Claude Code scratchpad root for the current user
// (TempDir/claude-<uid>). On platforms without a uid (Windows), Getuid is -1;
// the resulting path simply won't match any real scratchpad, so reaping is a
// no-op there rather than wrong.
func ScratchpadRoot() string {
	return filepath.Join(os.TempDir(), "claude-"+strconv.Itoa(os.Getuid()))
}

// ScratchpadDir returns the scratchpad directory for a given worktree path.
// Best-effort: coupled to Claude Code's path-mangling scheme.
func ScratchpadDir(worktreePath string) string {
	if worktreePath == "" {
		return ""
	}
	return filepath.Join(ScratchpadRoot(), mangle(filepath.Clean(worktreePath)))
}

// DiscoverWorktreeDirs lists on-disk worktree directories under base, matching
// agentique's <base>/<project>/session-<id> layout. Missing base => empty.
func DiscoverWorktreeDirs(base string) ([]string, error) {
	projects, err := os.ReadDir(base)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read worktree base: %w", err)
	}
	var dirs []string
	for _, proj := range projects {
		if !proj.IsDir() {
			continue
		}
		projDir := filepath.Join(base, proj.Name())
		sessions, err := os.ReadDir(projDir)
		if err != nil {
			continue // unreadable project dir: skip, don't fail the whole sweep
		}
		for _, s := range sessions {
			if s.IsDir() {
				dirs = append(dirs, filepath.Join(projDir, s.Name()))
			}
		}
	}
	return dirs, nil
}

// DiscoverChromeDirs lists agentique-chrome-* profile directories under TempDir.
func DiscoverChromeDirs() ([]string, error) {
	entries, err := os.ReadDir(os.TempDir())
	if err != nil {
		return nil, fmt.Errorf("read temp dir: %w", err)
	}
	var dirs []string
	for _, e := range entries {
		if e.IsDir() && len(e.Name()) > len(chromePrefix) && e.Name()[:len(chromePrefix)] == chromePrefix {
			dirs = append(dirs, filepath.Join(os.TempDir(), e.Name()))
		}
	}
	return dirs, nil
}

// DiscoverScratchpadDirs lists entries under the Claude scratchpad root. Missing
// root => empty (no Claude scratchpads on this host).
func DiscoverScratchpadDirs() ([]string, error) {
	root := ScratchpadRoot()
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read scratchpad root: %w", err)
	}
	var dirs []string
	for _, e := range entries {
		if e.IsDir() {
			dirs = append(dirs, filepath.Join(root, e.Name()))
		}
	}
	return dirs, nil
}

// DirSize returns the total size in bytes of a directory tree. Unreadable
// entries are skipped rather than failing the walk.
func DirSize(path string) int64 {
	var total int64
	_ = filepath.WalkDir(path, func(_ string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if info, err := d.Info(); err == nil {
			total += info.Size()
		}
		return nil
	})
	return total
}

// EnrichSizes fills in SizeBytes for every reap item (used for dry-run display
// and freed-byte accounting).
func EnrichSizes(p *Plan) {
	for i := range p.Reap {
		if p.Reap[i].SizeBytes == 0 {
			p.Reap[i].SizeBytes = DirSize(p.Reap[i].Path)
		}
	}
}

// Remover performs the actual filesystem removals. Split from planning so the
// planner stays pure and callers (CLI, startup sweep) can inject git-aware
// worktree removal appropriate to their layer.
type Remover interface {
	// RemoveWorktree removes a git worktree while keeping its branch. projectPath
	// is the owning repo; branch and wtPath identify the worktree.
	RemoveWorktree(ctx context.Context, projectPath, branch, wtPath string) error
	// RemoveAll removes a plain directory tree.
	RemoveAll(path string) error
}

// FailedItem pairs a reap item with the error that prevented its removal.
type FailedItem struct {
	Item Item
	Err  error
}

// Result summarizes an Execute run.
type Result struct {
	Removed    []Item
	Failed     []FailedItem
	FreedBytes int64
}

// Execute carries out a plan's removals. Worktree items with a known project
// path are removed git-aware (keeping the branch); everything else is a plain
// directory removal. Sizes are measured just before removal so FreedBytes
// reflects what was actually reclaimed.
func Execute(ctx context.Context, p Plan, r Remover) Result {
	var res Result
	for _, item := range p.Reap {
		size := item.SizeBytes
		if size == 0 {
			size = DirSize(item.Path)
		}
		var err error
		if (item.Kind == KindOrphanWorktree || item.Kind == KindFinishedWorktree) && item.ProjectPath != "" {
			err = r.RemoveWorktree(ctx, item.ProjectPath, item.Branch, item.Path)
		} else {
			err = r.RemoveAll(item.Path)
		}
		if err != nil {
			res.Failed = append(res.Failed, FailedItem{Item: item, Err: err})
			continue
		}
		res.Removed = append(res.Removed, item)
		res.FreedBytes += size
	}
	return res
}
