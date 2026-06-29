package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/mdjarv/agentique/backend/internal/brain"
)

var (
	snapshotRetainFlag int
	restoreRetainFlag  int
	restoreForceFlag   bool
)

func init() {
	brainSnapshotCmd.Flags().IntVar(&snapshotRetainFlag, "retain", 0, "max snapshots to keep (0 = default 7)")
	brainRestoreCmd.Flags().IntVar(&restoreRetainFlag, "retain", 0, "max snapshots to keep (0 = default 7)")
	brainRestoreCmd.Flags().BoolVarP(&restoreForceFlag, "force", "f", false, "skip the confirmation prompt and the running-server guard")
	brainCmd.AddCommand(brainSnapshotCmd)
	brainCmd.AddCommand(brainRestoreCmd)
}

// brainDirForCLI resolves the live brain directory (sibling of the database file).
func brainDirForCLI() string {
	return filepath.Join(filepath.Dir(resolveDBPath()), "brain")
}

// --- snapshot ---

type snapshotResult struct {
	Created  brain.SnapshotInfo
	Retained []brain.SnapshotInfo
}

// runSnapshotCore is the IO-free core: take a snapshot and return it plus the retained
// set. Pure FS (no brain.New / Chroma), so it is unit-testable over a temp dir.
func runSnapshotCore(brainDir string, retain int) (snapshotResult, error) {
	info, err := brain.Snapshot(brainDir, retain)
	if err != nil {
		return snapshotResult{}, err
	}
	retained, err := brain.ListSnapshots(brainDir)
	if err != nil {
		return snapshotResult{Created: info}, err
	}
	return snapshotResult{Created: info, Retained: retained}, nil
}

var brainSnapshotCmd = &cobra.Command{
	Use:   "snapshot",
	Short: "Take a restorable filesystem snapshot of the brain (brain/.snapshots/<ts>/)",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		res, err := runSnapshotCore(brainDirForCLI(), snapshotRetainFlag)
		if err != nil {
			return fmt.Errorf("brain snapshot: %w", err)
		}
		fmt.Printf("snapshot %s — %d files, %d bytes\n", res.Created.ID, res.Created.Files, res.Created.Bytes)
		fmt.Printf("retained %d snapshot(s):\n", len(res.Retained))
		for _, s := range res.Retained {
			fmt.Printf("  %s  (%d files)\n", s.ID, s.Files)
		}
		return nil
	},
}

// --- restore ---

type restoreResult struct {
	RestoredID   string   // the snapshot restored (empty on the unknown-id error path)
	SafetyID     string   // the pre-restore safety snapshot written by brain.Restore
	AvailableIDs []string // the snapshot ids that existed when restore was attempted
}

// runRestoreCore is the IO-free core: validate the id (returning the available ids and an
// os.ErrNotExist on a miss), restore, then report the pre-restore safety snapshot id (the
// newest snapshot after a successful restore — brain.Restore writes it as its first step).
func runRestoreCore(brainDir, id string, retain int) (restoreResult, error) {
	avail, err := brain.ListSnapshots(brainDir)
	if err != nil {
		return restoreResult{}, err
	}
	ids := make([]string, len(avail))
	known := false
	for i, s := range avail {
		ids[i] = s.ID
		if s.ID == id {
			known = true
		}
	}
	if !known {
		return restoreResult{AvailableIDs: ids}, fmt.Errorf("brain: snapshot %q not found: %w", id, os.ErrNotExist)
	}
	if err := brain.Restore(brainDir, id, retain); err != nil {
		return restoreResult{AvailableIDs: ids}, err
	}
	after, err := brain.ListSnapshots(brainDir)
	if err != nil {
		return restoreResult{RestoredID: id, AvailableIDs: ids}, err
	}
	safety := ""
	if len(after) > 0 {
		safety = after[0].ID // newest = the pre-restore safety snapshot
	}
	return restoreResult{RestoredID: id, SafetyID: safety, AvailableIDs: ids}, nil
}

var brainRestoreCmd = &cobra.Command{
	Use:   "restore <snapshot-id>",
	Short: "Restore the brain to a snapshot (writes a pre-restore safety snapshot first)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		brainDir := brainDirForCLI()
		id := args[0]

		// Restore rewrites files under a live read-through cache; refuse against a running
		// server unless forced (offline-only for M1).
		if pid, alive := readServerPID(); alive && !restoreForceFlag {
			return fmt.Errorf("a server appears to be running (pid %d); stop it before restoring, or pass --force", pid)
		}

		// Validate up front so a typo prints the available ids instead of clobbering nothing.
		avail, err := brain.ListSnapshots(brainDir)
		if err != nil {
			return fmt.Errorf("brain restore: %w", err)
		}
		found := false
		for _, s := range avail {
			if s.ID == id {
				found = true
				break
			}
		}
		if !found {
			fmt.Fprintf(os.Stderr, "snapshot %q not found. Available:\n", id)
			for _, s := range avail {
				fmt.Fprintf(os.Stderr, "  %s\n", s.ID)
			}
			return fmt.Errorf("unknown snapshot %q", id)
		}

		if !restoreForceFlag {
			fmt.Printf("Restore brain to snapshot %s? A pre-restore safety snapshot is written first. [y/N] ", id)
			line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
			if strings.ToLower(strings.TrimSpace(line)) != "y" {
				fmt.Println("aborted")
				return nil
			}
		}

		res, err := runRestoreCore(brainDir, id, restoreRetainFlag)
		if err != nil {
			return fmt.Errorf("brain restore: %w", err)
		}
		fmt.Printf("restored brain to %s (pre-restore safety snapshot: %s)\n", res.RestoredID, res.SafetyID)
		return nil
	},
}
