package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestBackfillLabelsSnapshotAndIdempotent(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "global"), 0o755); err != nil {
		t.Fatal(err)
	}
	legacy := "---\nid: leg1\nscope: global\ncategory: fact\nsource: consolidated\nuses: 0\n" +
		"created: 2020-01-01T00:00:00Z\nupdated: 2020-01-01T00:00:00Z\n---\n\na legacy fact\n"
	legacyPath := filepath.Join(dir, "global", "leg1.md")
	if err := os.WriteFile(legacyPath, []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}

	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	res, err := runBackfillLabelsCore(ctx, dir, 0, now, false)
	if err != nil {
		t.Fatal(err)
	}
	if res.SnapshotID == "" {
		t.Fatal("backfill core must take a snapshot")
	}
	if res.Scanned != 1 || res.Rewritten != 1 {
		t.Fatalf("scanned=%d rewritten=%d, want 1/1", res.Scanned, res.Rewritten)
	}

	// The snapshot holds the ORIGINAL (label-less) file (read before any second run).
	snapBytes, err := os.ReadFile(filepath.Join(dir, ".snapshots", res.SnapshotID, "global", "leg1.md"))
	if err != nil {
		t.Fatalf("snapshot missing the original file: %v", err)
	}
	if string(snapBytes) != legacy {
		t.Fatalf("snapshot is not the pre-backfill original")
	}

	// The live file is now labeled + disuse-clock-stamped.
	live, err := os.ReadFile(legacyPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"evidence:", "volatility:", "lifecycle:", "last_used:"} {
		if !strings.Contains(string(live), key) {
			t.Errorf("live file missing %q after backfill", key)
		}
	}

	// A second run rewrites nothing (idempotent).
	res2, err := runBackfillLabelsCore(ctx, dir, 0, now, false)
	if err != nil {
		t.Fatal(err)
	}
	if res2.Rewritten != 0 {
		t.Fatalf("second backfill rewrote %d, want 0", res2.Rewritten)
	}
}
