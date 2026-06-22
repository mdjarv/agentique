package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	dbpkg "github.com/mdjarv/agentique/backend/db"
	"github.com/mdjarv/agentique/backend/internal/brain"
	"github.com/mdjarv/agentique/backend/internal/memory"
	"github.com/mdjarv/agentique/backend/internal/memory/filestore"
	"github.com/mdjarv/agentique/backend/internal/session"
	"github.com/mdjarv/agentique/backend/internal/store"
)

var (
	backfillProject   string
	backfillLimit     int
	backfillMinEvents int
	backfillDryRun    bool
	backfillForce     bool
	backfillModel     string

	consolidateProject    string
	consolidateScope      string
	consolidateModel      string
	consolidateForce      bool
	consolidateRerun      bool
	consolidateAggressive bool
	consolidateDryRun     bool

	backfillSubsumedSource string
	backfillSubsumedDir    string
	backfillSubsumedDryRun bool
	backfillSubsumedForce  bool

	assignAreasDir    string
	assignAreasDryRun bool
	assignAreasForce  bool
)

func init() {
	backfillCmd.Flags().StringVar(&backfillProject, "project", "", "only this project ID (default: all projects)")
	backfillCmd.Flags().IntVar(&backfillLimit, "limit", 0, "max sessions to process (0 = all)")
	backfillCmd.Flags().IntVar(&backfillMinEvents, "min-events", 8, "skip sessions with fewer events than this")
	backfillCmd.Flags().BoolVar(&backfillDryRun, "dry-run", false, "extract and print candidate facts without writing")
	backfillCmd.Flags().BoolVarP(&backfillForce, "force", "f", false, "skip confirmation prompt")
	backfillCmd.Flags().StringVar(&backfillModel, "model", "haiku", "extraction model: haiku|sonnet|opus (haiku is cheapest for high-volume extraction)")

	consolidateCmd.Flags().StringVar(&consolidateProject, "project", "", "project ID to consolidate (maps to its scope)")
	consolidateCmd.Flags().StringVar(&consolidateScope, "scope", "", "raw scope override (e.g. global); takes precedence over --project")
	consolidateCmd.Flags().StringVar(&consolidateModel, "model", "", "reorganize model: haiku|sonnet|opus (empty = deterministic dedup/decay only, no LLM reorg)")
	consolidateCmd.Flags().BoolVarP(&consolidateForce, "force", "f", false, "skip confirmation prompt")
	consolidateCmd.Flags().BoolVar(&consolidateRerun, "rerun", false, "reorganize even if the scope is unchanged since the last pass (ignore the saved fingerprint)")
	consolidateCmd.Flags().BoolVar(&consolidateAggressive, "aggressive", false, "collapse families of granular facts into broad rules (relaxes the over-deletion guard); requires --model")
	consolidateCmd.Flags().BoolVar(&consolidateDryRun, "dry-run", false, "preview: run the full pass (LLM included) and print the changelog without writing")

	brainImportCmd.Flags().StringArrayVar(&importMap, "map", nil, "pre-resolve a source project to a local one: --map source-slug=local-slug (repeatable)")
	brainImportCmd.Flags().BoolVarP(&importSkipUnmatched, "skip-unmatched", "y", false, "skip source projects with no local match instead of prompting")

	backfillSubsumedCmd.Flags().StringVar(&backfillSubsumedSource, "source", "", "snapshot still holding the deleted originals: a brain markdown dir (e.g. a backup) or an id-bearing `brain export` JSON bundle (required)")
	backfillSubsumedCmd.Flags().StringVar(&backfillSubsumedDir, "brain-dir", "", "target brain directory to repair (default: the live brain next to the database)")
	backfillSubsumedCmd.Flags().BoolVar(&backfillSubsumedDryRun, "dry-run", false, "report what would be filled without writing")
	backfillSubsumedCmd.Flags().BoolVarP(&backfillSubsumedForce, "force", "f", false, "skip the confirmation prompt")
	_ = backfillSubsumedCmd.MarkFlagRequired("source")

	assignAreasCmd.Flags().StringVar(&assignAreasDir, "brain-dir", "", "target brain directory (default: the live brain next to the database)")
	assignAreasCmd.Flags().BoolVar(&assignAreasDryRun, "dry-run", false, "preview the cross-scope areas without writing")
	assignAreasCmd.Flags().BoolVarP(&assignAreasForce, "force", "f", false, "skip the confirmation prompt")

	brainCmd.AddCommand(backfillCmd)
	brainCmd.AddCommand(consolidateCmd)
	brainCmd.AddCommand(backfillSubsumedCmd)
	brainCmd.AddCommand(assignAreasCmd)
	brainCmd.AddCommand(calibrateCmd)
	brainCmd.AddCommand(brainExportCmd)
	brainCmd.AddCommand(brainImportCmd)
	rootCmd.AddCommand(brainCmd)
}

var brainCmd = &cobra.Command{
	Use:   "brain",
	Short: "Manage the persistent agent memory (brain)",
}

var backfillCmd = &cobra.Command{
	Use:   "backfill",
	Short: "Retroactively extract durable memories from past session transcripts",
	Long: `Reconstruct the transcript of each past session, distill durable facts with a
cheap model (Haiku), and store them in the brain under that session's project
scope. Facts are deduplicated against existing memories.

Start with --dry-run to review what would be learned before writing anything.`,
	RunE: runBrainBackfill,
}

func runBrainBackfill(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	dbFile := resolveDBPath()
	db, err := store.Open(dbFile)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer db.Close()
	if err := store.RunMigrations(db, dbpkg.Migrations); err != nil {
		return fmt.Errorf("run migrations: %w", err)
	}
	q := store.New(db)

	brainDir := filepath.Join(filepath.Dir(dbFile), "brain")
	svc, err := brain.New(ctx, brain.Config{
		Dir:         brainDir,
		ChromaURL:   os.Getenv("AGENTIQUE_BRAIN_CHROMA_URL"),
		EmbedURL:    os.Getenv("AGENTIQUE_BRAIN_EMBED_URL"),
		EmbedModel:  os.Getenv("AGENTIQUE_BRAIN_EMBED_MODEL"),
		EmbedAPIKey: os.Getenv("AGENTIQUE_BRAIN_EMBED_KEY"),
	})
	if err != nil {
		return fmt.Errorf("init brain: %w", err)
	}

	var sessions []store.Session
	if backfillProject != "" {
		sessions, err = q.ListSessionsByProject(ctx, backfillProject)
	} else {
		sessions, err = q.ListAllSessions(ctx)
	}
	if err != nil {
		return fmt.Errorf("list sessions: %w", err)
	}
	if len(sessions) == 0 {
		fmt.Println("no sessions to process")
		return nil
	}

	if !backfillDryRun && !backfillForce {
		fmt.Printf("About to extract memories from up to %d session(s) into %s using Haiku.\n", len(sessions), brainDir)
		fmt.Print("Proceed? [y/N] ")
		answer, _ := bufio.NewReader(os.Stdin).ReadString('\n')
		if a := strings.TrimSpace(strings.ToLower(answer)); a != "y" && a != "yes" {
			fmt.Println("cancelled")
			return nil
		}
	}

	model, err := brain.ParseModel(backfillModel)
	if err != nil {
		return err
	}
	runner := session.RealBlockingRunner()
	ex := brain.NewClaudeExtractor(runner, model)

	var processed, candidateCount, writtenCount int
	for _, s := range sessions {
		if backfillLimit > 0 && processed >= backfillLimit {
			break
		}
		events, err := q.ListEventsBySession(ctx, s.ID)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  %s: list events failed: %v\n", shortID(s.ID), err)
			continue
		}
		if len(events) < backfillMinEvents {
			continue
		}
		tevents := make([]brain.TranscriptEvent, len(events))
		for i, e := range events {
			tevents[i] = brain.TranscriptEvent{Type: e.Type, Data: e.Data}
		}
		chunks := brain.BuildTranscript(tevents, 12000)
		if len(chunks) == 0 {
			continue
		}
		processed++

		scope := brain.ScopeForProject(s.ProjectID)
		var cands []memory.Candidate
		for _, chunk := range chunks {
			c, err := ex.Extract(ctx, []string{chunk})
			if err != nil {
				fmt.Fprintf(os.Stderr, "  %s: extract failed: %v\n", shortID(s.ID), err)
				continue
			}
			cands = append(cands, c...)
		}
		if len(cands) == 0 {
			continue
		}

		name := s.Name
		if name == "" {
			name = "(unnamed)"
		}
		fmt.Printf("\n%s  %s  [%s]\n", shortID(s.ID), name, scope)
		for _, c := range cands {
			candidateCount++
			fmt.Printf("  • [%s] %s\n", c.Category, c.Text)
			if backfillDryRun {
				continue
			}
			if _, err := svc.Add(ctx, scope, c.Text, c.Category, memory.SourceConsolidated); err != nil {
				fmt.Fprintf(os.Stderr, "    write failed: %v\n", err)
				continue
			}
			writtenCount++
		}
	}

	fmt.Printf("\n%d session(s) processed, %d candidate fact(s)", processed, candidateCount)
	if backfillDryRun {
		fmt.Printf(" (dry run — nothing written)\n")
	} else {
		fmt.Printf(", %d written to the brain (duplicates merged)\n", writtenCount)
	}
	return nil
}

var consolidateCmd = &cobra.Command{
	Use:   "consolidate",
	Short: "Run consolidation over one scope",
	Long: `Run the consolidation pass for a scope: distill captures, merge duplicates,
abstract repeated specifics into general rules, and decay stale facts.

Without --model this performs deterministic dedup/decay only. With --model the
named model drives the LLM reorganization — use a strong model (opus) here, since
the pass is infrequent and judgment-heavy. Large scopes are chunked automatically.
Pinned, locked and human-edited facts are never touched.`,
	RunE: runBrainConsolidate,
}

func runBrainConsolidate(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	scope := memory.Scope(consolidateScope)
	if scope == "" {
		if consolidateProject == "" {
			return fmt.Errorf("provide --project <id> or --scope <scope>")
		}
		scope = brain.ScopeForProject(consolidateProject)
	}

	dbFile := resolveDBPath()
	brainDir := filepath.Join(filepath.Dir(dbFile), "brain")
	svc, err := brain.New(ctx, brain.Config{
		Dir:         brainDir,
		ChromaURL:   os.Getenv("AGENTIQUE_BRAIN_CHROMA_URL"),
		EmbedURL:    os.Getenv("AGENTIQUE_BRAIN_EMBED_URL"),
		EmbedModel:  os.Getenv("AGENTIQUE_BRAIN_EMBED_MODEL"),
		EmbedAPIKey: os.Getenv("AGENTIQUE_BRAIN_EMBED_KEY"),
	})
	if err != nil {
		return fmt.Errorf("init brain: %w", err)
	}

	var ex memory.Extractor
	opts := brain.ConsolidateOpts{Force: consolidateRerun}
	mode := "deterministic dedup/decay only"
	if consolidateModel != "" {
		model, err := brain.ParseModel(consolidateModel)
		if err != nil {
			return err
		}
		var exOpts []brain.ExtractorOption
		strategy := "conservative"
		if consolidateAggressive {
			exOpts = append(exOpts, brain.WithAggressiveReorganize())
			opts.MinSurvivorRatio = brain.AggressiveMinSurvivorRatio
			strategy = "aggressive"
		}
		ex = brain.NewClaudeExtractor(session.RealBlockingRunner(), model, exOpts...)
		mode = fmt.Sprintf("%s LLM reorganization via %s", strategy, consolidateModel)
	}

	// A dry run writes nothing, so it needs no confirmation.
	if !consolidateForce && !consolidateDryRun {
		fmt.Printf("About to consolidate scope %s (%s).\n", scope, mode)
		fmt.Print("Proceed? [y/N] ")
		answer, _ := bufio.NewReader(os.Stdin).ReadString('\n')
		if a := strings.TrimSpace(strings.ToLower(answer)); a != "y" && a != "yes" {
			fmt.Println("cancelled")
			return nil
		}
	}
	if consolidateDryRun {
		fmt.Printf("DRY RUN — scope %s (%s); nothing will be written.\n", scope, mode)
	}

	rep, err := svc.Consolidate(ctx, scope, ex, memory.DecayPolicy{}, consolidateDryRun, opts)
	if err != nil {
		return fmt.Errorf("consolidate: %w", err)
	}
	printConsolidateReport(rep)
	return nil
}

func printConsolidateReport(rep memory.Report) {
	if rep.Skipped {
		fmt.Println("\nreorganization skipped — set unchanged since last pass")
	}
	if rep.ReorgRefused {
		fmt.Println("\n⚠ reorganization REFUSED by the over-deletion safety net (would remove >half the set)")
	}
	for _, c := range rep.Rewritten {
		fmt.Printf("\n~ rewrote:\n    - %s\n    + %s\n", c.Before.Text, c.After.Text)
	}
	for _, r := range rep.Abstracted {
		fmt.Printf("\n+ abstracted: %s\n", r.Text)
	}
	for _, r := range rep.Deleted {
		fmt.Printf("\n- removed: %s\n", r.Text)
	}
	for _, r := range rep.Decayed {
		fmt.Printf("\n- decayed: %s\n", r.Text)
	}
	fmt.Printf("\n%d promoted, %d rewritten, %d abstracted, %d deleted, %d decayed\n",
		len(rep.Promoted), len(rep.Rewritten), len(rep.Abstracted), len(rep.Deleted), len(rep.Decayed))
}

// --- Subsumed backfill ------------------------------------------------------

var backfillSubsumedCmd = &cobra.Command{
	Use:   "backfill-subsumed",
	Short: "One-time: rebuild empty Subsumed provenance on promoted facts from a snapshot",
	Long: `Repair facts promoted into global before Subsumed was snapshotted at apply time.

Those facts carry DerivedFrom ids but an empty Subsumed, so the review surface
degrades to "originals not retained": the reviewer judges a merged summary without
seeing the per-project facts it was built from. This pass resolves each fact's
DerivedFrom ids against a snapshot that still holds the (since-deleted) originals
and fills Subsumed with their scope+text.

--source is that snapshot. The originals live id-keyed only in a brain markdown
directory, so point --source at one (a backup brain dir is ideal). A 'brain export'
JSON bundle works too, but only if it was written after this change added ids — an
older id-less bundle resolves nothing.

Only global facts with DerivedFrom set and Subsumed empty are touched, so the pass
is idempotent: re-running it after a successful fill is a no-op.

Start with --dry-run to see what would be filled before writing anything.`,
	RunE: runBrainBackfillSubsumed,
}

// subsumedBackfillStats summarizes a backfill pass for reporting.
type subsumedBackfillStats struct {
	SourceRecords  int // id-keyed records found in the snapshot
	Eligible       int // global facts with DerivedFrom set and Subsumed empty
	FullyMatched   int // eligible facts where every DerivedFrom id resolved
	PartialMatched int // eligible facts where some (not all) ids resolved
	Unrecoverable  int // eligible facts where no id resolved
	MatchedIDs     int // DerivedFrom ids resolved against the snapshot
	DanglingIDs    int // DerivedFrom ids absent from the snapshot
}

// Recoverable is the count of facts that gained at least one Subsumed source.
func (s subsumedBackfillStats) Recoverable() int { return s.FullyMatched + s.PartialMatched }

func runBrainBackfillSubsumed(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	brainDir := backfillSubsumedDir
	if brainDir == "" {
		brainDir = filepath.Join(filepath.Dir(resolveDBPath()), "brain")
	}

	target := filestore.New(brainDir)
	stats, toWrite, err := computeSubsumedBackfill(ctx, target, backfillSubsumedSource)
	if err != nil {
		return err
	}

	if stats.SourceRecords == 0 {
		return fmt.Errorf("source %q has no id-keyed records — an old `brain export` bundle predates record ids; point --source at a brain markdown directory (e.g. a backup) that still holds the deleted originals", backfillSubsumedSource)
	}

	fmt.Printf("target brain:    %s\n", brainDir)
	fmt.Printf("source snapshot: %s (%d id-keyed records)\n", backfillSubsumedSource, stats.SourceRecords)
	fmt.Printf("eligible promoted facts (DerivedFrom set, Subsumed empty): %d\n", stats.Eligible)
	fmt.Printf("  recoverable: %d (%d fully, %d partial)\n", stats.Recoverable(), stats.FullyMatched, stats.PartialMatched)
	fmt.Printf("  unrecoverable (no source matched): %d\n", stats.Unrecoverable)
	fmt.Printf("DerivedFrom ids: %d matched, %d dangling\n", stats.MatchedIDs, stats.DanglingIDs)

	if len(toWrite) == 0 {
		fmt.Println("\nnothing to backfill (already filled, or no source matched)")
		return nil
	}

	if backfillSubsumedDryRun {
		fmt.Println("\nDRY RUN — nothing written. Facts that would gain Subsumed provenance:")
		printSubsumedPreview(toWrite)
		return nil
	}

	if !backfillSubsumedForce {
		fmt.Printf("\nAbout to write Subsumed provenance to %d fact(s) in %s.\n", len(toWrite), brainDir)
		fmt.Print("Proceed? [y/N] ")
		answer, _ := bufio.NewReader(os.Stdin).ReadString('\n')
		if a := strings.TrimSpace(strings.ToLower(answer)); a != "y" && a != "yes" {
			fmt.Println("cancelled")
			return nil
		}
	}

	written, err := writeSubsumedBackfill(ctx, target, toWrite)
	if err != nil {
		return err
	}
	fmt.Printf("\nbackfilled Subsumed on %d fact(s)\n", written)
	return nil
}

// computeSubsumedBackfill is the read-only core of the backfill: it builds the id
// index from the snapshot, lists the target's global facts, and resolves their
// DerivedFrom ids. It writes nothing — the returned slice is the work to apply.
func computeSubsumedBackfill(ctx context.Context, target memory.Store, sourcePath string) (subsumedBackfillStats, []memory.SubsumedBackfill, error) {
	index, srcCount, err := loadSubsumedIndex(ctx, sourcePath)
	if err != nil {
		return subsumedBackfillStats{}, nil, fmt.Errorf("load source %q: %w", sourcePath, err)
	}
	// Only global facts are promotion outputs; restricting here keeps the pass aligned
	// with the cross-scope-promotion semantics of Subsumed.
	recs, err := target.List(ctx, memory.ScopeGlobal)
	if err != nil {
		return subsumedBackfillStats{}, nil, fmt.Errorf("list target brain: %w", err)
	}

	results := memory.BackfillSubsumed(recs, index)
	stats := subsumedBackfillStats{SourceRecords: srcCount, Eligible: len(results)}
	var toWrite []memory.SubsumedBackfill
	for _, r := range results {
		stats.MatchedIDs += len(r.MatchedIDs)
		stats.DanglingIDs += len(r.UnmatchedIDs)
		if len(r.MatchedIDs) == 0 {
			stats.Unrecoverable++
			continue
		}
		if len(r.UnmatchedIDs) > 0 {
			stats.PartialMatched++
		} else {
			stats.FullyMatched++
		}
		toWrite = append(toWrite, r)
	}
	return stats, toWrite, nil
}

// writeSubsumedBackfill persists the filled records, returning the number written.
func writeSubsumedBackfill(ctx context.Context, target memory.Store, work []memory.SubsumedBackfill) (int, error) {
	written := 0
	for _, w := range work {
		if err := target.Put(ctx, w.Record); err != nil {
			return written, fmt.Errorf("write %s: %w", w.Record.ID, err)
		}
		written++
	}
	return written, nil
}

// loadSubsumedIndex builds an id -> SubsumedSource index from a snapshot that still
// holds the deleted originals. The snapshot is a brain markdown directory (the
// id-keyed source of truth) or a `brain export` JSON bundle (only entries carrying
// an id contribute). Returns the index and the number of id-keyed records seen.
func loadSubsumedIndex(ctx context.Context, path string) (map[string]memory.SubsumedSource, int, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, 0, err
	}

	idx := map[string]memory.SubsumedSource{}
	if info.IsDir() {
		recs, lerr := filestore.New(path).List(ctx)
		if lerr != nil {
			return nil, 0, lerr
		}
		for _, r := range recs {
			idx[r.ID] = memory.SubsumedSource{Scope: r.Scope, Text: r.Text}
		}
		return idx, len(recs), nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, 0, err
	}
	var bundle brainBundle
	if err := json.Unmarshal(data, &bundle); err != nil {
		return nil, 0, fmt.Errorf("parse bundle: %w", err)
	}
	n := 0
	for _, m := range bundle.Memories {
		if m.ID == "" {
			continue
		}
		n++
		idx[m.ID] = memory.SubsumedSource{Scope: memory.Scope(m.Scope), Text: m.Text}
	}
	return idx, n, nil
}

// printSubsumedPreview shows each fact and the sources that would be folded into it.
func printSubsumedPreview(work []memory.SubsumedBackfill) {
	for _, w := range work {
		fmt.Printf("\n%s  %s\n", shortID(w.Record.ID), w.Record.Text)
		for _, s := range w.Record.Subsumed {
			fmt.Printf("    ← [%s] %s\n", s.Scope, s.Text)
		}
		if len(w.UnmatchedIDs) > 0 {
			fmt.Printf("    (%d source id(s) still dangling)\n", len(w.UnmatchedIDs))
		}
	}
}

// --- Cross-scope areas ------------------------------------------------------

var assignAreasCmd = &cobra.Command{
	Use:   "assign-areas",
	Short: "Recompute cross-scope topic areas and stamp them onto each fact (Record.Area)",
	Long: `Group facts into cross-scope topic "areas" — topics that recur across two or more
scopes — and persist each fact's area onto Record.Area. Areas power the graph's
"by area" colouring/regions and (soon) cross-area recall.

Normally this runs automatically on scheduled consolidation, consolidate-all, and global promotion.
This one-shot command populates areas on demand (e.g. right after upgrading) so they
show up without waiting for a pass. It only writes the rebuildable Area index — facts'
text and provenance are untouched. Start with --dry-run to preview.`,
	RunE: runBrainAssignAreas,
}

func runBrainAssignAreas(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	brainDir := assignAreasDir
	if brainDir == "" {
		brainDir = filepath.Join(filepath.Dir(resolveDBPath()), "brain")
	}
	store := filestore.New(brainDir)

	infos, err := memory.PreviewAreas(ctx, store, memory.DefaultAreaThreshold, memory.DefaultMinPromotionScopes)
	if err != nil {
		return fmt.Errorf("preview areas: %w", err)
	}
	facts := 0
	for _, a := range infos {
		facts += a.Size
	}
	fmt.Printf("brain: %s\n", brainDir)
	fmt.Printf("%d cross-scope areas covering %d facts\n", len(infos), facts)
	for i, a := range infos {
		if i >= 20 {
			fmt.Printf("  … and %d more\n", len(infos)-20)
			break
		}
		fmt.Printf("  • %3d facts / %d scopes — %s\n", a.Size, len(a.Scopes), a.Label)
	}

	if assignAreasDryRun {
		fmt.Println("\ndry run — nothing written")
		return nil
	}
	if len(infos) == 0 {
		fmt.Println("\nno cross-scope areas to assign")
		return nil
	}
	if !assignAreasForce {
		fmt.Printf("\nAbout to stamp Record.Area on the facts above in %s.\n", brainDir)
		fmt.Print("Proceed? [y/N] ")
		answer, _ := bufio.NewReader(os.Stdin).ReadString('\n')
		if a := strings.TrimSpace(strings.ToLower(answer)); a != "y" && a != "yes" {
			fmt.Println("cancelled")
			return nil
		}
	}

	n, err := memory.AssignAreas(ctx, store, memory.DefaultAreaThreshold, memory.DefaultMinPromotionScopes)
	if err != nil {
		return fmt.Errorf("assign areas: %w", err)
	}
	fmt.Printf("\nassigned areas to %d fact(s)\n", n)
	return nil
}

// --- Calibrate --------------------------------------------------------------

var calibrateCmd = &cobra.Command{
	Use:   "calibrate",
	Short: "Measure the corpus's cosine distribution and derive model-specific semantic thresholds",
	Long: `Embed every durable fact, measure the pairwise cosine distribution of the whole
brain, and derive the model-specific semantic thresholds from its percentiles: the
cosine "related" line (the link threshold + recall vouch bar) from a high percentile,
the vector veto floor from a low one. This replaces hand-tuning a constant per
embedding model (docs/brain-semantic-recall.md).

It prints the distribution and the derived thresholds next to today's defaults; it
writes nothing. To make the server use derived thresholds at boot, set
AGENTIQUE_BRAIN_AUTOCAL=1 (an explicit AGENTIQUE_BRAIN_SEMANTIC_THRESHOLD /
AGENTIQUE_BRAIN_VECTOR_VETO still wins per-knob).

Requires a configured embedder + Chroma (AGENTIQUE_BRAIN_CHROMA_URL /
AGENTIQUE_BRAIN_EMBED_URL / AGENTIQUE_BRAIN_EMBED_MODEL).`,
	RunE: runBrainCalibrate,
}

func runBrainCalibrate(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	svc, err := newBrainService(ctx, resolveDBPath())
	if err != nil {
		return err
	}
	if !svc.SemanticEnabled() {
		return fmt.Errorf("calibrate needs a configured embedder + Chroma — set AGENTIQUE_BRAIN_CHROMA_URL, AGENTIQUE_BRAIN_EMBED_URL and AGENTIQUE_BRAIN_EMBED_MODEL")
	}

	fmt.Println("embedding the corpus and measuring its pairwise cosine distribution…")
	res, err := svc.Calibrate(ctx)
	if err != nil {
		return fmt.Errorf("calibrate: %w", err)
	}
	s := res.Sample
	fmt.Printf("\npairs measured: %d   (min %.4f, mean %.4f, max %.4f)\n", s.Len(), s.Min(), s.Mean(), s.Max())
	fmt.Println("cosine distribution:")
	for _, p := range []float64{0.01, 0.05, 0.10, 0.25, 0.50, 0.75, 0.90, 0.95, 0.99, 0.995} {
		fmt.Printf("  p%-5.1f = %.4f\n", p*100, s.Percentile(p))
	}

	if !res.OK {
		fmt.Printf("\ncorpus too thin to calibrate (%d pairs) — the server would keep the defaults\n", s.Len())
		fmt.Printf("defaults: cosineThreshold=%.4f  vectorVeto=%.4f\n", memory.DefaultSemanticThreshold, memory.DefaultVectorVetoScore)
		return nil
	}

	fmt.Printf("\nderived thresholds (related p%.1f / veto p%.1f):\n", memory.DefaultRelatedPercentile*100, memory.DefaultVetoPercentile*100)
	fmt.Printf("  cosineThreshold (link + vouch) = %.4f   (default %.4f)\n", res.Thresholds.CosineThreshold, memory.DefaultSemanticThreshold)
	fmt.Printf("  vectorVeto (actively unrelated) = %.4f   (default %.4f)\n", res.Thresholds.VectorVeto, memory.DefaultVectorVetoScore)
	fmt.Println("\nset AGENTIQUE_BRAIN_AUTOCAL=1 to have the server derive these at boot.")
	return nil
}

// --- Export / import --------------------------------------------------------

const brainBundleVersion = 1

// brainBundle is the portable export format. Project scopes are keyed by a
// machine-local UUID, so the manifest records each one's name+slug; import maps
// those to local projects (by slug) and skips any that don't match.
type brainBundle struct {
	Version  int                      `json:"version"`
	Projects map[string]bundleProject `json:"projects"` // scope -> source project identity
	Memories []bundleMemory           `json:"memories"`
}

type bundleProject struct {
	Name string `json:"name"`
	Slug string `json:"slug"`
}

type bundleMemory struct {
	// ID is the record's stable id. It is carried so a bundle exported before a
	// cross-scope promotion deleted its source facts can serve as the snapshot for
	// `brain backfill-subsumed`. omitempty keeps older id-less bundles round-trippable.
	ID       string `json:"id,omitempty"`
	Scope    string `json:"scope"`
	Text     string `json:"text"`
	Category string `json:"category"`
	Source   string `json:"source"`
	Pinned   bool   `json:"pinned"`
	Locked   bool   `json:"locked"`
}

var brainExportCmd = &cobra.Command{
	Use:   "export <file>",
	Short: "Export the brain to a portable JSON bundle",
	Long: `Write all memories to a JSON file you can carry to another machine. Project
scopes are tagged with their project name/slug so 'brain import' can re-map them
to the local projects (unknown projects are skipped or mapped interactively).`,
	Args: cobra.ExactArgs(1),
	RunE: runBrainExport,
}

func runBrainExport(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	dbFile := resolveDBPath()
	db, err := store.Open(dbFile)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer db.Close()
	q := store.New(db)

	svc, err := newBrainService(ctx, dbFile)
	if err != nil {
		return err
	}
	recs, err := svc.List(ctx)
	if err != nil {
		return fmt.Errorf("list memories: %w", err)
	}

	bundle := brainBundle{Version: brainBundleVersion, Projects: map[string]bundleProject{}}
	for _, r := range recs {
		bundle.Memories = append(bundle.Memories, bundleMemory{
			ID: r.ID, Scope: string(r.Scope), Text: r.Text, Category: string(r.Category),
			Source: string(r.Source), Pinned: r.Pinned, Locked: r.Locked,
		})
		scope := string(r.Scope)
		if !strings.HasPrefix(scope, "project:") {
			continue
		}
		if _, ok := bundle.Projects[scope]; ok {
			continue
		}
		if p, perr := q.GetProject(ctx, strings.TrimPrefix(scope, "project:")); perr == nil {
			bundle.Projects[scope] = bundleProject{Name: p.Name, Slug: p.Slug}
		}
	}

	data, err := json.MarshalIndent(bundle, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(args[0], data, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", args[0], err)
	}
	fmt.Printf("exported %d memories (%d project scopes) to %s\n", len(bundle.Memories), len(bundle.Projects), args[0])
	return nil
}

var (
	importMap           []string
	importSkipUnmatched bool
)

var brainImportCmd = &cobra.Command{
	Use:   "import <file>",
	Short: "Import a brain bundle, mapping its projects to local ones",
	Long: `Merge a bundle from 'brain export' into the local brain. Global memories merge
directly. Project memories are matched to local projects by slug; an unmatched
source project is resolved interactively (pick a local project, skip, or send to
global) unless --skip-unmatched is given or --map pre-resolves it. Duplicates are
skipped, so importing is idempotent.`,
	Args: cobra.ExactArgs(1),
	RunE: runBrainImport,
}

func runBrainImport(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	dbFile := resolveDBPath()
	db, err := store.Open(dbFile)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer db.Close()
	q := store.New(db)

	data, err := os.ReadFile(args[0])
	if err != nil {
		return fmt.Errorf("read %s: %w", args[0], err)
	}
	var bundle brainBundle
	if err := json.Unmarshal(data, &bundle); err != nil {
		return fmt.Errorf("parse bundle: %w", err)
	}

	svc, err := newBrainService(ctx, dbFile)
	if err != nil {
		return err
	}

	// Local projects, indexed by slug (and name) for matching.
	projects, err := q.ListProjects(ctx)
	if err != nil {
		return fmt.Errorf("list projects: %w", err)
	}
	bySlug := map[string]store.Project{}
	byName := map[string]store.Project{}
	for _, p := range projects {
		bySlug[strings.ToLower(p.Slug)] = p
		byName[strings.ToLower(p.Name)] = p
	}
	// --map source-slug=local-slug overrides.
	overrides := map[string]string{}
	for _, m := range importMap {
		parts := strings.SplitN(m, "=", 2)
		if len(parts) != 2 {
			return fmt.Errorf("invalid --map %q (want source-slug=local-slug)", m)
		}
		overrides[strings.ToLower(strings.TrimSpace(parts[0]))] = strings.ToLower(strings.TrimSpace(parts[1]))
	}

	// Group memories by source scope.
	bySource := map[string][]memory.Record{}
	for _, m := range bundle.Memories {
		bySource[m.Scope] = append(bySource[m.Scope], memory.Record{
			Text: m.Text, Category: memory.Category(m.Category), Source: memory.Source(m.Source),
			Pinned: m.Pinned, Locked: m.Locked,
		})
	}
	sources := make([]string, 0, len(bySource))
	for s := range bySource {
		sources = append(sources, s)
	}
	sort.Strings(sources)

	reader := bufio.NewReader(os.Stdin)
	imported, skipped := 0, 0
	var skippedNames []string

	for _, srcScope := range sources {
		recs := bySource[srcScope]
		target, ok := resolveTarget(srcScope, bundle.Projects[srcScope], bySlug, byName, overrides, reader)
		if !ok {
			skipped++
			label := srcScope
			if p, has := bundle.Projects[srcScope]; has && p.Name != "" {
				label = p.Name
			}
			skippedNames = append(skippedNames, label)
			continue
		}
		n, err := svc.ImportRecords(ctx, target, recs)
		if err != nil {
			return fmt.Errorf("import into %s: %w", target, err)
		}
		imported += n
		fmt.Printf("  %s → %s: %d new (%d total in bundle)\n", srcScope, target, n, len(recs))
	}

	fmt.Printf("\nimported %d new memories", imported)
	if skipped > 0 {
		fmt.Printf("; skipped %d source project(s): %s", skipped, strings.Join(skippedNames, ", "))
	}
	fmt.Println()
	return nil
}

// resolveTarget decides where a source scope's memories land. Global maps to
// global. A project scope is matched by --map override, then by slug, then name;
// if still unmatched it prompts (unless --skip-unmatched). Returns ok=false to skip.
func resolveTarget(srcScope string, src bundleProject, bySlug, byName map[string]store.Project, overrides map[string]string, reader *bufio.Reader) (memory.Scope, bool) {
	if srcScope == string(memory.ScopeGlobal) || !strings.HasPrefix(srcScope, "project:") {
		return memory.ScopeGlobal, true
	}
	slug := strings.ToLower(src.Slug)
	name := strings.ToLower(src.Name)

	// 1) Explicit --map override (keyed by source slug or name).
	if localSlug, has := overrides[slug]; has {
		if p, ok := bySlug[localSlug]; ok {
			return brain.ScopeForProject(p.ID), true
		}
	}
	if localSlug, has := overrides[name]; has {
		if p, ok := bySlug[localSlug]; ok {
			return brain.ScopeForProject(p.ID), true
		}
	}
	// 2) Auto-match by slug, then name.
	if p, ok := bySlug[slug]; ok && slug != "" {
		return brain.ScopeForProject(p.ID), true
	}
	if p, ok := byName[name]; ok && name != "" {
		return brain.ScopeForProject(p.ID), true
	}
	// 3) Unmatched.
	if importSkipUnmatched {
		return "", false
	}
	return promptForTarget(src, bySlug, reader)
}

// promptForTarget asks the user to map an unmatched source project to a local one.
func promptForTarget(src bundleProject, bySlug map[string]store.Project, reader *bufio.Reader) (memory.Scope, bool) {
	locals := make([]store.Project, 0, len(bySlug))
	for _, p := range bySlug {
		locals = append(locals, p)
	}
	sort.Slice(locals, func(i, j int) bool { return locals[i].Name < locals[j].Name })

	label := src.Name
	if label == "" {
		label = src.Slug
	}
	fmt.Printf("\nSource project %q has no local match. Map to:\n", label)
	for i, p := range locals {
		fmt.Printf("  [%d] %s\n", i+1, p.Name)
	}
	fmt.Print("  [s] skip   [g] import as global\n> ")
	answer, _ := reader.ReadString('\n')
	answer = strings.TrimSpace(strings.ToLower(answer))
	switch answer {
	case "", "s":
		return "", false
	case "g":
		return memory.ScopeGlobal, true
	}
	var idx int
	if _, err := fmt.Sscanf(answer, "%d", &idx); err == nil && idx >= 1 && idx <= len(locals) {
		return brain.ScopeForProject(locals[idx-1].ID), true
	}
	fmt.Println("  (unrecognized — skipping)")
	return "", false
}

// newBrainService builds a brain Service rooted at the data dir next to dbFile.
func newBrainService(ctx context.Context, dbFile string) (*brain.Service, error) {
	svc, err := brain.New(ctx, brain.Config{
		Dir:         filepath.Join(filepath.Dir(dbFile), "brain"),
		ChromaURL:   os.Getenv("AGENTIQUE_BRAIN_CHROMA_URL"),
		EmbedURL:    os.Getenv("AGENTIQUE_BRAIN_EMBED_URL"),
		EmbedModel:  os.Getenv("AGENTIQUE_BRAIN_EMBED_MODEL"),
		EmbedAPIKey: os.Getenv("AGENTIQUE_BRAIN_EMBED_KEY"),
	})
	if err != nil {
		return nil, fmt.Errorf("init brain: %w", err)
	}
	return svc, nil
}
