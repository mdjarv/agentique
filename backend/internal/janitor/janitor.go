// Package janitor reclaims disk left behind by finished agentique sessions:
// git worktrees (each carrying a full node_modules), per-session Chrome browser
// profiles under the OS temp dir, and Claude Code scratchpad directories.
//
// The core is a pure planner (Compute) that takes an already-gathered snapshot
// of sessions and on-disk artifacts and decides what is safe to remove. All IO —
// discovering directories, sizing them, and the removals themselves — lives in
// the caller-facing helpers (Discover, DirSize, Execute) so Compute stays
// trivially testable and deterministic.
//
// Safety model. A live session's artifacts are never touched. "Live" is the
// union of three signals, and any one of them spares an artifact:
//   - the session is present in the running server's in-memory registry,
//   - its persisted state is non-terminal (idle/running), or
//   - it is the caller's own session.
//
// The classification, never mtime, is what guards against deleting active work.
// Two further guards defend against a wrong or stale database (which would make
// every real worktree look orphaned): an empty session set reaps nothing, and an
// orphan worktree whose owning project is unrecognized is spared, not deleted.
package janitor

import (
	"path/filepath"
	"strings"
	"time"
)

// Kind classifies a reclaimable artifact.
type Kind string

const (
	// KindOrphanWorktree is an on-disk worktree with no matching session row.
	KindOrphanWorktree Kind = "orphan-worktree"
	// KindFinishedWorktree is the worktree of a terminal-state (stopped/done/
	// failed) session that still exists in the DB. Reaping keeps the session
	// row and git branch — only the checked-out tree (node_modules, build
	// output, uncommitted changes) is removed; resume re-provisions it.
	KindFinishedWorktree Kind = "finished-worktree"
	// KindChromeProfile is a per-session Chrome --user-data-dir under TempDir.
	KindChromeProfile Kind = "chrome-profile"
	// KindScratchpad is a Claude Code scratchpad dir keyed by worktree path.
	KindScratchpad Kind = "scratchpad"
)

// terminalStates mirrors the CLI's terminalStates — sessions in these states
// have stopped running and their worktree is a reclaim candidate.
var terminalStates = map[string]bool{
	"done":    true,
	"stopped": true,
	"failed":  true,
}

// IsTerminal reports whether a session state is terminal (stopped/done/failed).
func IsTerminal(state string) bool { return terminalStates[state] }

// Session is the minimal per-session view the planner needs. The caller builds
// these from the DB (and resolves ProjectPath from the owning project so a
// finished worktree can be removed git-aware).
type Session struct {
	ID           string
	State        string
	Name         string
	WorktreePath string // absolute; "" when the session has no private worktree
	Branch       string
	ProjectPath  string // owning repo path; "" => plain directory removal
	UpdatedAt    time.Time
}

// Item is one artifact selected for removal.
type Item struct {
	Kind        Kind
	Path        string
	SessionID   string // "" when the artifact maps to no known session
	SessionName string
	State       string // session state, "" for orphans
	ProjectPath string // worktree items only; "" => plain removal
	Branch      string // worktree items only
	Dirty       bool   // worktree had uncommitted changes (reaped only under IncludeDirty)
	SizeBytes   int64  // populated by the caller (Compute does no IO)
}

// Skipped records an artifact that was considered but spared, with the reason.
type Skipped struct {
	Kind      Kind
	Path      string
	SessionID string
	Reason    string
}

// Plan is the output of planning: what to reap and what was deliberately spared.
type Plan struct {
	Reap    []Item
	Skipped []Skipped
}

// Inputs is the fully-gathered snapshot the pure planner operates on.
type Inputs struct {
	Sessions       []Session         // every session known to the DB
	Projects       map[string]string // sanitized project dir name -> repo path
	LiveIDs        map[string]bool   // sessions live in the runtime registry (always spared)
	CurrentID      string            // caller's own session; never touched
	WorktreeBase   string            // paths.WorktreeDir(); roots our scratchpad namespace
	WorktreeDirs   []string          // worktree dirs found on disk
	ChromeDirs     []string          // agentique-chrome-* dirs under TempDir
	ScratchpadDirs []string          // entries under the Claude scratchpad root
	Now            time.Time
}

// Options tunes how aggressive the plan is.
type Options struct {
	// IncludeFinished reaps terminal-state session worktrees (and their
	// chrome/scratchpad siblings). Off => orphans-only, the zero-risk baseline
	// used by the startup sweep.
	IncludeFinished bool
	// IncludeDirty also reaps worktrees with uncommitted changes. Off => such
	// worktrees are spared and reported, so committed work is never silently
	// lost on a reap.
	IncludeDirty bool
	// OlderThan, when > 0, restricts finished-worktree reaping to sessions whose
	// last update is at least this old.
	OlderThan time.Duration
	// Dirty reports whether a worktree has uncommitted changes. nil => treat as
	// clean (the startup sweep never reaps finished worktrees, so it needs no
	// git check).
	Dirty func(worktreePath string) bool
}

// spared holds the set of sessions whose artifacts must never be touched.
type spared struct {
	byID       map[string]Session // all sessions, for chrome/scratchpad mapping
	worktreeTo map[string]Session // cleaned worktree path -> session
	live       func(Session) bool
}

// Compute decides what is safe to reclaim from the given snapshot. It performs
// no IO and is deterministic given Inputs/Options.
func Compute(in Inputs, opt Options) Plan {
	var p Plan

	// Guard: a DB with no sessions cannot authoritatively declare anything an
	// orphan. This is almost always a wrong or freshly-initialized DB pointed at
	// a populated disk (or a mis-isolated test). Reap nothing rather than delete
	// every real worktree.
	if len(in.Sessions) == 0 {
		return p
	}

	sp := spared{
		byID:       make(map[string]Session, len(in.Sessions)),
		worktreeTo: make(map[string]Session, len(in.Sessions)),
	}
	for _, s := range in.Sessions {
		sp.byID[s.ID] = s
		if s.WorktreePath != "" {
			sp.worktreeTo[filepath.Clean(s.WorktreePath)] = s
		}
	}
	sp.live = func(s Session) bool {
		return s.ID == in.CurrentID || in.LiveIDs[s.ID] || !IsTerminal(s.State)
	}

	// A scratchpad's own directory name carries no session id — only the mangled
	// worktree path it was derived from. Map that forward here so a reaped
	// scratchpad can name the session it belongs to, which is what lets a caller
	// reclaim "this session" rather than "these paths".
	mangledOwner := make(map[string]string, len(in.Sessions))
	for _, s := range in.Sessions {
		if s.WorktreePath != "" {
			mangledOwner[mangle(filepath.Clean(s.WorktreePath))] = s.ID
		}
	}

	spareMangled := planWorktrees(in, opt, sp, &p)
	planChrome(in, opt, sp, &p)
	planScratchpads(in, sp, spareMangled, mangledOwner, &p)
	return p
}

// planWorktrees classifies every on-disk worktree and returns the set of mangled
// scratchpad names that correspond to spared (kept) worktrees.
func planWorktrees(in Inputs, opt Options, sp spared, p *Plan) map[string]bool {
	spareMangled := make(map[string]bool)
	for _, raw := range in.WorktreeDirs {
		wt := filepath.Clean(raw)
		sess, known := sp.worktreeTo[wt]

		if !known {
			// No session row: a truly-orphaned worktree — but only reap it if we
			// can tie it to a known project. An unrecognized project means the DB
			// is likely wrong/stale, so spare it rather than risk deleting live work.
			projPath, recognized := in.Projects[filepath.Base(filepath.Dir(wt))]
			if !recognized {
				p.Skipped = append(p.Skipped, Skipped{KindOrphanWorktree, wt, "", "unrecognized project — spared (guards against a wrong/stale DB)"})
				spareMangled[mangle(wt)] = true
				continue
			}
			item := Item{Kind: KindOrphanWorktree, Path: wt, ProjectPath: projPath, Branch: filepath.Base(wt)}
			if !considerWorktree(item, opt, p) {
				spareMangled[mangle(wt)] = true
			}
			continue
		}

		if sp.live(sess) {
			p.Skipped = append(p.Skipped, Skipped{KindFinishedWorktree, wt, sess.ID, spareReason(sess, in.CurrentID)})
			spareMangled[mangle(wt)] = true
			continue
		}
		// Terminal, non-live session.
		if !opt.IncludeFinished {
			p.Skipped = append(p.Skipped, Skipped{KindFinishedWorktree, wt, sess.ID, "finished session (use --include-finished / prune to reap)"})
			spareMangled[mangle(wt)] = true
			continue
		}
		if opt.OlderThan > 0 && !sess.UpdatedAt.IsZero() && in.Now.Sub(sess.UpdatedAt) < opt.OlderThan {
			p.Skipped = append(p.Skipped, Skipped{KindFinishedWorktree, wt, sess.ID, "more recent than --older-than"})
			spareMangled[mangle(wt)] = true
			continue
		}
		item := Item{
			Kind:        KindFinishedWorktree,
			Path:        wt,
			SessionID:   sess.ID,
			SessionName: sess.Name,
			State:       sess.State,
			ProjectPath: sess.ProjectPath,
			Branch:      sess.Branch,
		}
		if !considerWorktree(item, opt, p) {
			spareMangled[mangle(wt)] = true
		}
	}
	return spareMangled
}

// considerWorktree applies the dirty guard and appends the item to Reap or
// Skipped. It returns true when the worktree was queued for removal.
func considerWorktree(item Item, opt Options, p *Plan) bool {
	if opt.Dirty != nil && opt.Dirty(item.Path) {
		if !opt.IncludeDirty {
			p.Skipped = append(p.Skipped, Skipped{item.Kind, item.Path, item.SessionID, "uncommitted changes (use --include-dirty to reap)"})
			return false
		}
		item.Dirty = true
	}
	p.Reap = append(p.Reap, item)
	return true
}

// planChrome maps agentique-chrome-<id> dirs to sessions by id. A no-row profile
// is always an orphan and reaped. A profile whose session still exists is reaped
// only when it is terminal AND IncludeFinished is set — keeping the profile
// grouped with its worktree's fate so the orphan-only startup sweep never
// discards a resumable session's logged-in browser state.
func planChrome(in Inputs, opt Options, sp spared, p *Plan) {
	for _, raw := range in.ChromeDirs {
		dir := filepath.Clean(raw)
		id := strings.TrimPrefix(filepath.Base(dir), chromePrefix)
		if id == filepath.Base(dir) {
			continue // not one of ours
		}
		sess, known := sp.byID[id]
		if known {
			if sp.live(sess) {
				p.Skipped = append(p.Skipped, Skipped{KindChromeProfile, dir, id, spareReason(sess, in.CurrentID)})
				continue
			}
			if !opt.IncludeFinished {
				p.Skipped = append(p.Skipped, Skipped{KindChromeProfile, dir, id, "finished session (use --include-finished / prune to reap)"})
				continue
			}
		}
		p.Reap = append(p.Reap, Item{Kind: KindChromeProfile, Path: dir, SessionID: id, SessionName: sess.Name})
	}
}

// planScratchpads only ever touches scratchpads under our worktree namespace
// (their mangled name starts with the mangled worktree base). A scratchpad whose
// mangled name matches a spared worktree is kept; anything else in our namespace
// is orphaned and reaped. Foreign scratchpads are ignored entirely.
func planScratchpads(in Inputs, sp spared, spareMangled map[string]bool, mangledOwner map[string]string, p *Plan) {
	if in.WorktreeBase == "" {
		return
	}
	prefix := mangle(filepath.Clean(in.WorktreeBase)) + "-"
	for _, raw := range in.ScratchpadDirs {
		dir := filepath.Clean(raw)
		base := filepath.Base(dir)
		if !strings.HasPrefix(base, prefix) {
			continue // not an agentique worktree scratchpad — leave it alone
		}
		owner := mangledOwner[base]
		if spareMangled[base] {
			p.Skipped = append(p.Skipped, Skipped{KindScratchpad, dir, owner, "belongs to a live/kept worktree"})
			continue
		}
		p.Reap = append(p.Reap, Item{Kind: KindScratchpad, Path: dir, SessionID: owner, SessionName: sp.byID[owner].Name})
	}
}

func spareReason(s Session, currentID string) string {
	switch {
	case s.ID == currentID:
		return "current session"
	case !IsTerminal(s.State):
		return "session is " + s.State
	default:
		return "session is live"
	}
}

// mangle reproduces Claude Code's scratchpad directory naming: every '/' and '.'
// in an absolute path becomes '-'. Best-effort — coupled to that scheme, so
// scratchpad reaping is always gated on a match, never a glob.
func mangle(path string) string {
	return strings.Map(func(r rune) rune {
		if r == '/' || r == '.' {
			return '-'
		}
		return r
	}, path)
}
