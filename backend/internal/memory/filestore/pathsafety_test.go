package filestore

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/mdjarv/agentique/backend/internal/memory"
)

// Record IDs reach this store straight from HTTP path parameters and from agent
// tool arguments, and they are used both as a path component and as a glob
// pattern. These lock both halves closed.

func TestDeleteRejectsTraversingID(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "brain")
	if err := os.MkdirAll(filepath.Join(root, "global"), 0o755); err != nil {
		t.Fatal(err)
	}
	victim := filepath.Join(base, "victim.md")
	if err := os.WriteFile(victim, []byte("important"), 0o644); err != nil {
		t.Fatal(err)
	}

	f := New(root)
	err := f.Delete(context.Background(), "../../victim")
	if !errors.Is(err, ErrInvalidID) {
		t.Fatalf("Delete(traversing id) = %v, want ErrInvalidID", err)
	}
	if _, statErr := os.Stat(victim); statErr != nil {
		t.Errorf("file outside the store root was removed: %v", statErr)
	}
}

func TestDeleteRejectsGlobWildcardID(t *testing.T) {
	root := t.TempDir()
	f := New(root)
	ctx := context.Background()
	for _, id := range []string{"aaa", "bbb"} {
		if err := f.Put(ctx, memory.Record{ID: id, Scope: memory.ScopeGlobal, Text: id}); err != nil {
			t.Fatal(err)
		}
	}

	// "*" is a valid filename character in a URL but a glob metacharacter here:
	// unguarded it selects every record in the store.
	if err := f.Delete(ctx, "*"); !errors.Is(err, ErrInvalidID) {
		t.Fatalf("Delete(\"*\") = %v, want ErrInvalidID", err)
	}
	recs, err := f.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 2 {
		t.Errorf("store holds %d records after Delete(\"*\"), want 2", len(recs))
	}
}

func TestGetAndPutRejectUnsafeIDs(t *testing.T) {
	f := New(t.TempDir())
	ctx := context.Background()
	for _, id := range []string{"../escape", `..\escape`, "sub/dir", "wild*card", "q?mark", "br[ack]et", "..", "."} {
		if _, err := f.Get(ctx, id); !errors.Is(err, ErrInvalidID) {
			t.Errorf("Get(%q) = %v, want ErrInvalidID", id, err)
		}
		err := f.Put(ctx, memory.Record{ID: id, Scope: memory.ScopeGlobal, Text: "x"})
		if !errors.Is(err, ErrInvalidID) {
			t.Errorf("Put(%q) = %v, want ErrInvalidID", id, err)
		}
	}
}

func TestOrdinaryIDsStillWork(t *testing.T) {
	f := New(t.TempDir())
	ctx := context.Background()
	// UUIDs (what Record.New mints) and hand-written slugs must be unaffected.
	for _, id := range []string{"6f1c0e3a-6c4a-4f7e-9c2b-6d7f8a9b0c1d", "feedback_git_push", "brain.design-log"} {
		if err := f.Put(ctx, memory.Record{ID: id, Scope: memory.ScopeGlobal, Text: "hello"}); err != nil {
			t.Fatalf("Put(%q): %v", id, err)
		}
		rec, err := f.Get(ctx, id)
		if err != nil {
			t.Fatalf("Get(%q): %v", id, err)
		}
		if rec.ID != id {
			t.Errorf("round-trip id = %q, want %q", rec.ID, id)
		}
		if err := f.Delete(ctx, id); err != nil {
			t.Fatalf("Delete(%q): %v", id, err)
		}
	}
}
