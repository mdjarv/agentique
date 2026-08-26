package main

import (
	"bufio"
	"context"
	"database/sql"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/mdjarv/agentique/backend/internal/gitops"
	"github.com/mdjarv/agentique/backend/internal/janitor"
	"github.com/mdjarv/agentique/backend/internal/paths"
	"github.com/mdjarv/agentique/backend/internal/session"
	"github.com/mdjarv/agentique/backend/internal/store"
	"github.com/spf13/cobra"
)

var (
	pruneApply        bool
	pruneYes          bool
	pruneOrphansOnly  bool
	pruneOlderThan    string
	pruneIncludeDirty bool
)

func init() {
	f := pruneCmd.Flags()
	f.BoolVar(&pruneApply, "apply", false, "actually delete (default: dry-run — report only)")
	f.BoolVarP(&pruneYes, "yes", "y", false, "skip the confirmation prompt (with --apply)")
	f.BoolVar(&pruneOrphansOnly, "orphans-only", false, "only reclaim artifacts whose session row is gone; never finished-session worktrees")
	f.StringVar(&pruneOlderThan, "older-than", "", "restrict finished-session reaping to sessions idle at least this long (e.g. 24h, 168h)")
	f.BoolVar(&pruneIncludeDirty, "include-dirty", false, "also reap worktrees with uncommitted changes (default: skip and report them)")
	rootCmd.AddCommand(pruneCmd)
}

var pruneCmd = &cobra.Command{
	Use:   "prune",
	Short: "Reclaim disk from finished/orphaned session worktrees and /tmp artifacts",
	Long: `Reclaim disk left behind by sessions:

  • worktrees of finished (stopped/done/failed) sessions — the checked-out tree
    (node_modules, build output) is removed, but the session row and git branch
    are kept, so the session stays resumable (the worktree re-provisions on
    resume). Worktrees with uncommitted changes are skipped unless --include-dirty.
  • worktrees whose session row no longer exists (crashes, DB resets).
  • per-session Chrome profiles and Claude scratchpads under the temp dir.

Live sessions (running/idle, or held by a running server) are always spared.

Dry-run by default: it prints exactly what it would delete. Pass --apply to
delete. Use --orphans-only for the zero-risk subset (no finished sessions).`,
	RunE: runPrune,
}

func runPrune(_ *cobra.Command, _ []string) error {
	ctx := context.Background()

	var olderThan time.Duration
	if pruneOlderThan != "" {
		d, err := time.ParseDuration(pruneOlderThan)
		if err != nil {
			return fmt.Errorf("invalid --older-than: %w", err)
		}
		olderThan = d
	}

	queries, cleanup, err := openDBReadOnly()
	if err != nil {
		return err
	}
	defer cleanup()

	plan, err := buildPrunePlan(ctx, queries, janitor.Options{
		IncludeFinished: !pruneOrphansOnly,
		IncludeDirty:    pruneIncludeDirty,
		OlderThan:       olderThan,
		Dirty:           func(p string) bool { d, _ := gitops.HasUncommittedChanges(p); return d },
	})
	if err != nil {
		return err
	}

	printPrunePlan(plan)

	if len(plan.Reap) == 0 {
		return nil
	}
	total := totalReapSize(plan)
	if !pruneApply {
		fmt.Printf("\nDry run — nothing deleted. Re-run with --apply to reclaim %s.\n", humanBytes(total))
		return nil
	}
	if !pruneYes {
		fmt.Printf("\nDelete the %d artifact(s) above (%s)? [y/N] ", len(plan.Reap), humanBytes(total))
		answer, _ := bufio.NewReader(os.Stdin).ReadString('\n')
		if a := strings.TrimSpace(strings.ToLower(answer)); a != "y" && a != "yes" {
			fmt.Println("cancelled")
			return nil
		}
	}

	res := janitor.Execute(ctx, plan, session.NewJanitorRemover())
	fmt.Printf("\nReclaimed %s — %d removed, %d failed.\n", humanBytes(res.FreedBytes), len(res.Removed), len(res.Failed))
	for _, f := range res.Failed {
		fmt.Fprintf(os.Stderr, "  failed %s: %v\n", f.Item.Path, f.Err)
	}
	return nil
}

// openDBReadOnly opens the database for planning only — no migrations, no WAL
// switch, no write of any kind.
//
// `prune` never writes a row: it removes directories. Going through openDB meant
// a dry run, whose entire contract is "report what I would delete", could
// migrate the live database as a side effect of being run — and it did, every
// time. A reporting command gets a reader.
func openDBReadOnly() (*store.Queries, func(), error) {
	// The "file:" prefix is required: without it the driver treats the query
	// string as part of the filename and hands back a writable handle to the
	// real database, which is the opposite of what this function promises.
	db, err := sql.Open("sqlite", "file:"+resolveDBPath()+"?mode=ro")
	if err != nil {
		return nil, nil, fmt.Errorf("open db read-only: %w", err)
	}
	// SQLite pragmas are per-connection and this pool must not open a second
	// one behind our back — same reasoning as store.Open.
	db.SetMaxOpenConns(1)
	if _, err := db.Exec("PRAGMA busy_timeout=5000;"); err != nil {
		_ = db.Close()
		return nil, nil, fmt.Errorf("set busy timeout: %w", err)
	}
	return store.New(db), func() { _ = db.Close() }, nil
}

// buildPrunePlan gathers the DB + disk snapshot and computes a plan. It reads the
// DB directly so it works offline; when a server is running it additionally
// spares every session the server reports as connected (belt-and-suspenders
// beyond the persisted state).
func buildPrunePlan(ctx context.Context, queries *store.Queries, opt janitor.Options) (janitor.Plan, error) {
	sessions, err := queries.ListAllSessions(ctx)
	if err != nil {
		return janitor.Plan{}, fmt.Errorf("list sessions: %w", err)
	}
	projects, err := queries.ListProjects(ctx)
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
		Sessions:       session.JanitorSessions(sessions, projByID),
		Projects:       session.JanitorProjects(projects),
		LiveIDs:        liveIDsFromServer(),
		WorktreeBase:   base,
		WorktreeDirs:   worktreeDirs,
		ChromeDirs:     chromeDirs,
		ScratchpadDirs: scratchDirs,
		Now:            time.Now(),
	}
	plan := janitor.Compute(in, opt)
	janitor.EnrichSizes(&plan)
	return plan, nil
}

// liveIDsFromServer returns the set of connected session IDs from a running
// server, or nil if no server is reachable (offline mode relies on DB state).
func liveIDsFromServer() map[string]bool {
	if !isServerRunning() {
		return nil
	}
	sessions, err := fetchJSON[[]sessionBrief](apiClient(), baseURL()+"/api/sessions")
	if err != nil {
		return nil
	}
	live := make(map[string]bool)
	for _, s := range sessions {
		if s.Connected {
			live[s.ID] = true
		}
	}
	return live
}

var pruneKindLabel = map[janitor.Kind]string{
	janitor.KindFinishedWorktree: "finished worktree",
	janitor.KindOrphanWorktree:   "orphan worktree",
	janitor.KindChromeProfile:    "chrome profile",
	janitor.KindScratchpad:       "scratchpad",
}

// printPrunePlan reports the reclaim candidates (largest first) and a summary of
// what was spared. This is the report the user reviews before --apply.
func printPrunePlan(plan janitor.Plan) {
	if len(plan.Reap) == 0 {
		fmt.Println("Nothing to reclaim.")
		printSpared(plan)
		return
	}

	items := append([]janitor.Item(nil), plan.Reap...)
	sort.Slice(items, func(i, j int) bool { return items[i].SizeBytes > items[j].SizeBytes })

	fmt.Printf("Reclaim candidates (%d):\n\n", len(items))
	fmt.Printf("  %-9s  %-17s  %-9s  %-8s  %-24s  %s\n", "SIZE", "KIND", "SESSION", "STATE", "NAME", "PATH")
	for _, it := range items {
		fmt.Printf("  %-9s  %-17s  %-9s  %-8s  %-24s  %s\n",
			humanBytes(it.SizeBytes), pruneKindLabel[it.Kind],
			sessionCol(it), stateCol(it), truncate(it.SessionName, 24), it.Path)
	}
	fmt.Printf("\n  total: %s across %d artifact(s)\n", humanBytes(totalReapSize(plan)), len(items))
	printSpared(plan)
}

// printSpared summarizes the artifacts that were considered but kept.
func printSpared(plan janitor.Plan) {
	if len(plan.Skipped) == 0 {
		return
	}
	fmt.Printf("\nSpared (%d):\n", len(plan.Skipped))
	for _, s := range plan.Skipped {
		fmt.Printf("  - %-18s %s  (%s)\n", pruneKindLabel[s.Kind], s.Path, s.Reason)
	}
}

func sessionCol(it janitor.Item) string {
	if it.SessionID == "" {
		return "-"
	}
	return shortID(it.SessionID)
}

func stateCol(it janitor.Item) string {
	state := it.State
	if state == "" {
		state = "-" // orphan: no session row
	}
	if it.Dirty {
		state += "*" // starred: had uncommitted changes
	}
	return state
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	if n <= 1 {
		return s[:n]
	}
	return s[:n-1] + "…"
}

func totalReapSize(plan janitor.Plan) int64 {
	var total int64
	for _, it := range plan.Reap {
		total += it.SizeBytes
	}
	return total
}

// humanBytes formats a byte count as a human-readable size (KiB/MiB/GiB…).
func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for x := n / unit; x >= unit; x /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}
