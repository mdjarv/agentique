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
)

func init() {
	backfillCmd.Flags().StringVar(&backfillProject, "project", "", "only this project ID (default: all projects)")
	backfillCmd.Flags().IntVar(&backfillLimit, "limit", 0, "max sessions to process (0 = all)")
	backfillCmd.Flags().IntVar(&backfillMinEvents, "min-events", 8, "skip sessions with fewer events than this")
	backfillCmd.Flags().BoolVar(&backfillDryRun, "dry-run", false, "extract and print candidate facts without writing")
	backfillCmd.Flags().BoolVarP(&backfillForce, "force", "f", false, "skip confirmation prompt")
	brainCmd.AddCommand(backfillCmd)
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

	runner := session.RealBlockingRunner()
	ex := brain.NewHaikuExtractor(runner)

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
