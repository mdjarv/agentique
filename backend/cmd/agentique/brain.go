package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
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
	consolidateCmd.Flags().BoolVar(&consolidateDryRun, "dry-run", false, "preview: run the full pass (LLM included) and print the changelog without writing")

	brainCmd.AddCommand(backfillCmd)
	brainCmd.AddCommand(consolidateCmd)
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

	rep, err := svc.Consolidate(ctx, scope, ex, memory.DecayPolicy{}, consolidateDryRun)
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
