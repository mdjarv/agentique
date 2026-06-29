package filestore

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mdjarv/agentique/backend/internal/memory"
)

func writeLegacyFile(t *testing.T, dir, scope, id, body string) string {
	t.Helper()
	d := filepath.Join(dir, scope)
	if err := os.MkdirAll(d, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "---\nid: " + id + "\nscope: " + scope + "\ncategory: fact\nsource: consolidated\nuses: 0\n" +
		"created: 2020-01-01T00:00:00Z\nupdated: 2020-01-01T00:00:00Z\n---\n\n" + body + "\n"
	path := filepath.Join(d, id+".md")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestRewriteNormalizedPersistsLabels(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	path := writeLegacyFile(t, dir, "global", "leg1", "a legacy fact")
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	fs := New(dir)

	scanned, rewritten, err := fs.RewriteNormalized(ctx, now, false)
	if err != nil {
		t.Fatal(err)
	}
	if scanned != 1 || rewritten != 1 {
		t.Fatalf("scanned=%d rewritten=%d, want 1/1", scanned, rewritten)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"evidence:", "volatility:", "lifecycle:", "last_used:"} {
		if !strings.Contains(string(raw), key) {
			t.Errorf("backfilled file missing %q:\n%s", key, raw)
		}
	}
	// Second pass is a no-op (idempotent).
	_, rewritten2, err := fs.RewriteNormalized(ctx, now, false)
	if err != nil {
		t.Fatal(err)
	}
	if rewritten2 != 0 {
		t.Fatalf("second pass rewrote %d, want 0 (idempotent)", rewritten2)
	}
}

func TestRewriteNormalizedDryRunWritesNothing(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	path := writeLegacyFile(t, dir, "global", "leg1", "a legacy fact")
	before, _ := os.ReadFile(path)
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)

	scanned, rewritten, err := New(dir).RewriteNormalized(ctx, now, true)
	if err != nil {
		t.Fatal(err)
	}
	if scanned != 1 || rewritten != 1 {
		t.Fatalf("dry-run scanned=%d rewritten=%d, want 1/1 (counts what WOULD change)", scanned, rewritten)
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Fatalf("dry-run must not modify the file")
	}
}

func TestRewriteNormalizedNoOpOnCanonical(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	fs := New(dir)
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)

	r := memory.New(memory.ScopeGlobal, "canonical fact", memory.CategoryFact, memory.SourceConsolidated)
	r.LastUsedAt = now // already stamped; New already sets the labels
	if err := fs.Put(ctx, r); err != nil {
		t.Fatal(err)
	}
	scanned, rewritten, err := fs.RewriteNormalized(ctx, now, false)
	if err != nil {
		t.Fatal(err)
	}
	if scanned != 1 || rewritten != 0 {
		t.Fatalf("canonical scanned=%d rewritten=%d, want 1/0", scanned, rewritten)
	}
}

func TestRewriteNormalizedStampsDisuseClockWhereZero(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	fs := New(dir)
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)

	rz := memory.New(memory.ScopeGlobal, "zero clock fact", memory.CategoryFact, memory.SourceConsolidated) // LastUsedAt zero
	if err := fs.Put(ctx, rz); err != nil {
		t.Fatal(err)
	}
	rn := memory.New(memory.Scope("project-y"), "nonzero clock fact", memory.CategoryFact, memory.SourceConsolidated)
	rn.LastUsedAt = time.Date(2025, 5, 5, 0, 0, 0, 0, time.UTC)
	if err := fs.Put(ctx, rn); err != nil {
		t.Fatal(err)
	}

	if _, _, err := fs.RewriteNormalized(ctx, now, false); err != nil {
		t.Fatal(err)
	}

	gotZ, err := fs.Get(ctx, rz.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !gotZ.LastUsedAt.Equal(now) {
		t.Fatalf("zero-clock fact LastUsedAt=%v, want %v", gotZ.LastUsedAt, now)
	}
	gotN, err := fs.Get(ctx, rn.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !gotN.LastUsedAt.Equal(rn.LastUsedAt) {
		t.Fatalf("non-zero clock must be left unchanged, got %v", gotN.LastUsedAt)
	}
}
