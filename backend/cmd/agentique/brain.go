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

	consolidateProject string
	consolidateScope   string
	consolidateModel   string
	consolidateForce   bool
	consolidateRerun   bool
	consolidateDryRun  bool
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
	consolidateCmd.Flags().BoolVar(&consolidateDryRun, "dry-run", false, "preview: run the full pass (LLM included) and print the changelog without writing")

	brainImportCmd.Flags().StringArrayVar(&importMap, "map", nil, "pre-resolve a source project to a local one: --map source-slug=local-slug (repeatable)")
	brainImportCmd.Flags().BoolVarP(&importSkipUnmatched, "skip-unmatched", "y", false, "skip source projects with no local match instead of prompting")

	brainCmd.AddCommand(backfillCmd)
	brainCmd.AddCommand(consolidateCmd)
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
	Short: "Run the consolidation 'sleep' pass over one scope",
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
	mode := "deterministic dedup/decay only"
	if consolidateModel != "" {
		model, err := brain.ParseModel(consolidateModel)
		if err != nil {
			return err
		}
		ex = brain.NewClaudeExtractor(session.RealBlockingRunner(), model)
		mode = fmt.Sprintf("LLM reorganization via %s", consolidateModel)
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

	rep, err := svc.Consolidate(ctx, scope, ex, memory.DecayPolicy{}, consolidateDryRun, consolidateRerun)
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
			Scope: string(r.Scope), Text: r.Text, Category: string(r.Category),
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
