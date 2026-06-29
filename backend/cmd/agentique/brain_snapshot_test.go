package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// seedBrainDir writes a minimal scope file into a temp brain dir for the pure-FS
// snapshot/restore cores (the cores never decode the markdown).
func seedBrainDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "global"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "global", "aaa.md"), []byte("---\nid: aaa\n---\n\nfact\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".fingerprints.json"), []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestSnapshotCLI_Core(t *testing.T) {
	dir := seedBrainDir(t)

	// snapshot round-trip: the created snapshot appears in the retained list.
	snap, err := runSnapshotCore(dir, 0)
	if err != nil {
		t.Fatal(err)
	}
	if snap.Created.ID == "" || snap.Created.Files == 0 {
		t.Fatalf("snapshot core returned empty result: %+v", snap.Created)
	}
	foundCreated := false
	for _, s := range snap.Retained {
		if s.ID == snap.Created.ID {
			foundCreated = true
		}
	}
	if !foundCreated {
		t.Fatalf("created snapshot %s not in retained list %v", snap.Created.ID, snap.Retained)
	}

	// unknown id → os.ErrNotExist + the available-id listing.
	res, err := runRestoreCore(dir, "20990101T000000Z", 0)
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected os.ErrNotExist for unknown id, got %v", err)
	}
	if len(res.AvailableIDs) == 0 {
		t.Fatalf("unknown-id result should list available ids, got none")
	}

	// restore reports the pre-restore safety snapshot id.
	res, err = runRestoreCore(dir, snap.Created.ID, 0)
	if err != nil {
		t.Fatalf("restore core: %v", err)
	}
	if res.RestoredID != snap.Created.ID {
		t.Fatalf("RestoredID=%q, want %q", res.RestoredID, snap.Created.ID)
	}
	if res.SafetyID == "" {
		t.Fatalf("restore must report a pre-restore safety snapshot id")
	}
}
