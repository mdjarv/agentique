package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/mdjarv/agentique/backend/internal/brain"
	"github.com/mdjarv/agentique/backend/internal/memory/filestore"
)

var (
	backfillLabelsDir       string
	backfillLabelsDryRun    bool
	backfillLabelsForce     bool
	backfillLabelsNoReindex bool
)

func init() {
	backfillLabelsCmd.Flags().StringVar(&backfillLabelsDir, "brain-dir", "", "target brain directory (default: the live brain next to the database)")
	backfillLabelsCmd.Flags().BoolVar(&backfillLabelsDryRun, "dry-run", false, "report what would change without writing")
	backfillLabelsCmd.Flags().BoolVarP(&backfillLabelsForce, "force", "f", false, "skip the confirmation prompt")
	backfillLabelsCmd.Flags().BoolVar(&backfillLabelsNoReindex, "no-reindex", false, "skip the Chroma reindex (metadata stays stale until the next reindex)")
	brainCmd.AddCommand(backfillLabelsCmd)
}

var backfillLabelsCmd = &cobra.Command{
	Use:   "backfill-labels",
	Short: "One-time: seed Evidence/Volatility/Lifecycle defaults + start the disuse clock + Chroma",
	Long: `Persist the controlled-vocabulary labels (Evidence/Volatility/Lifecycle) onto the
existing markdown files, stamping last_used where it is currently zero so the disuse
clock starts at this migration boundary (not the ancient updated timestamp), then
reindex Chroma so its metadata carries volatility/lifecycle.

Snapshot-first and idempotent (a second run rewrites nothing). Run with the server idle
and restart afterward so the live read-through cache reloads.`,
	RunE: runBrainBackfillLabels,
}

type backfillLabelsResult struct {
	SnapshotID string
	Scanned    int
	Rewritten  int
}

// runBackfillLabelsCore is the FS-only testable core: snapshot the brain (unless dryRun, via
// brain.Snapshot — the single Band-1 snapshot mechanism) then RewriteNormalized. The Chroma
// reindex is left to the cobra wrapper because it needs a live brain.Service.
func runBackfillLabelsCore(ctx context.Context, brainDir string, retain int, now time.Time, dryRun bool) (backfillLabelsResult, error) {
	var res backfillLabelsResult
	if !dryRun {
		info, err := brain.Snapshot(brainDir, retain)
		if err != nil {
			return res, fmt.Errorf("pre-backfill snapshot: %w", err)
		}
		res.SnapshotID = info.ID
	}
	scanned, rewritten, err := filestore.New(brainDir).RewriteNormalized(ctx, now, dryRun)
	if err != nil {
		return res, err
	}
	res.Scanned, res.Rewritten = scanned, rewritten
	return res, nil
}

func runBrainBackfillLabels(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	brainDir := backfillLabelsDir
	customDir := brainDir != ""
	if !customDir {
		brainDir = brainDirForCLI()
	}

	if !backfillLabelsForce && !backfillLabelsDryRun {
		fmt.Printf("Backfill labels + start the disuse clock for the brain at %s? A snapshot is written first. [y/N] ", brainDir)
		line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
		if strings.ToLower(strings.TrimSpace(line)) != "y" {
			fmt.Println("aborted")
			return nil
		}
	}

	res, err := runBackfillLabelsCore(ctx, brainDir, 0, time.Now().UTC(), backfillLabelsDryRun)
	if err != nil {
		return fmt.Errorf("brain backfill-labels: %w", err)
	}
	if res.SnapshotID != "" {
		fmt.Printf("pre-backfill snapshot: %s\n", res.SnapshotID)
	}
	suffix := ""
	if backfillLabelsDryRun {
		suffix = " (dry-run, nothing written)"
	}
	if res.Scanned == 0 {
		fmt.Println("nothing to backfill")
	} else {
		fmt.Printf("scanned %d record(s), rewrote %d%s\n", res.Scanned, res.Rewritten, suffix)
	}

	// Reindex Chroma so it picks up the new volatility/lifecycle metadata keys. Only for the
	// live brain (a --brain-dir override has no associated service); skipped on dry-run / --no-reindex.
	if backfillLabelsDryRun || backfillLabelsNoReindex || customDir {
		return nil
	}
	svc, err := newBrainService(ctx, resolveDBPath())
	if err != nil {
		return fmt.Errorf("brain backfill-labels: build service for reindex: %w", err)
	}
	if !svc.SemanticEnabled() {
		fmt.Println("semantic recall not configured — skipping Chroma reindex")
		return nil
	}
	fmt.Println("reindexing Chroma with volatility/lifecycle metadata…")
	if err := svc.Reindex(ctx); err != nil {
		return fmt.Errorf("brain backfill-labels: reindex: %w", err)
	}
	fmt.Println("reindex complete")
	return nil
}
