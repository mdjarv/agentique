package filestore

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/mdjarv/agentique/backend/internal/memory"
)

func ctx() context.Context { return context.Background() }

func sampleRecord() memory.Record {
	r := memory.New(memory.Scope("project-x"), "Use just targets, never raw npx.", memory.CategoryPreference, memory.SourceAgent)
	r.ID = "rec-1"
	r.Pinned = true
	r.Locked = true
	r.Uses = 3
	r.Helped = 2
	r.CreatedAt = time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC)
	r.UpdatedAt = time.Date(2026, 6, 16, 12, 30, 0, 0, time.UTC)
	r.LastUsedAt = time.Date(2026, 6, 17, 9, 0, 0, 0, time.UTC)
	r.DerivedFrom = []string{"cap-1", "cap-2"}
	r.Related = []string{"rec-9"}
	r.Community = 4
	return r
}

func TestPutGetRoundTrip(t *testing.T) {
	fs := New(t.TempDir())
	want := sampleRecord()
	if err := fs.Put(ctx(), want); err != nil {
		t.Fatal(err)
	}
	got, err := fs.Get(ctx(), "rec-1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Text != want.Text || got.Category != want.Category || got.Source != want.Source {
		t.Fatalf("core fields mismatch: %+v", got)
	}
	if got.Scope != want.Scope || !got.Pinned || !got.Locked || got.Uses != 3 || got.Helped != 2 {
		t.Fatalf("metadata mismatch: %+v", got)
	}
	if !got.CreatedAt.Equal(want.CreatedAt) || !got.UpdatedAt.Equal(want.UpdatedAt) {
		t.Fatalf("timestamps mismatch: %v / %v", got.CreatedAt, got.UpdatedAt)
	}
	if !got.LastUsedAt.Equal(want.LastUsedAt) {
		t.Fatalf("last-used mismatch: got %v want %v", got.LastUsedAt, want.LastUsedAt)
	}
	if !reflect.DeepEqual(got.DerivedFrom, want.DerivedFrom) || !reflect.DeepEqual(got.Related, want.Related) {
		t.Fatalf("links mismatch: %+v", got)
	}
	if got.Community != want.Community {
		t.Fatalf("community mismatch: got %d want %d", got.Community, want.Community)
	}
}

func TestListByScope(t *testing.T) {
	fs := New(t.TempDir())
	a := memory.New(memory.Scope("proj-a"), "fact a", memory.CategoryFact, memory.SourceAgent)
	b := memory.New(memory.Scope("proj-b"), "fact b", memory.CategoryFact, memory.SourceAgent)
	g := memory.New(memory.ScopeGlobal, "global fact", memory.CategoryFact, memory.SourceAgent)
	for _, r := range []memory.Record{a, b, g} {
		if err := fs.Put(ctx(), r); err != nil {
			t.Fatal(err)
		}
	}
	all, _ := fs.List(ctx())
	if len(all) != 3 {
		t.Fatalf("List() all = %d, want 3", len(all))
	}
	scoped, _ := fs.List(ctx(), memory.Scope("proj-a"), memory.ScopeGlobal)
	if len(scoped) != 2 {
		t.Fatalf("List(proj-a, global) = %d, want 2", len(scoped))
	}
}

func TestHandEditReflected(t *testing.T) {
	root := t.TempDir()
	fs := New(root)
	// A human writes a memory file by hand.
	dir := filepath.Join(root, "global")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "---\nid: hand-1\nscope: global\ncategory: preference\nsource: human\nuses: 0\ncreated: 2026-01-01T00:00:00Z\nupdated: 2026-01-01T00:00:00Z\n---\n\nUse tabs, not spaces.\n"
	if err := os.WriteFile(filepath.Join(dir, "hand-1.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := fs.Get(ctx(), "hand-1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Text != "Use tabs, not spaces." {
		t.Fatalf("hand-edited body not read: %q", got.Text)
	}
	if got.Source != memory.SourceHuman || got.Category != memory.CategoryPreference {
		t.Fatalf("hand-edited metadata not read: %+v", got)
	}
}

func TestScopeChangeRemovesStaleCopy(t *testing.T) {
	root := t.TempDir()
	fs := New(root)
	r := memory.New(memory.Scope("old"), "movable fact", memory.CategoryFact, memory.SourceAgent)
	r.ID = "mv-1"
	if err := fs.Put(ctx(), r); err != nil {
		t.Fatal(err)
	}
	r.Scope = memory.Scope("new")
	if err := fs.Put(ctx(), r); err != nil {
		t.Fatal(err)
	}
	matches, _ := filepath.Glob(filepath.Join(root, "*", "mv-1.md"))
	if len(matches) != 1 {
		t.Fatalf("expected exactly 1 file after scope change, got %v", matches)
	}
	if filepath.Base(filepath.Dir(matches[0])) != "new" {
		t.Fatalf("file not under new scope: %s", matches[0])
	}
}

func TestDelete(t *testing.T) {
	fs := New(t.TempDir())
	r := memory.New(memory.ScopeGlobal, "ephemeral", memory.CategoryFact, memory.SourceAgent)
	r.ID = "del-1"
	_ = fs.Put(ctx(), r)
	if err := fs.Delete(ctx(), "del-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := fs.Get(ctx(), "del-1"); err != memory.ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
	// deleting again is not an error
	if err := fs.Delete(ctx(), "del-1"); err != nil {
		t.Fatalf("re-delete should be nil, got %v", err)
	}
}

func TestCorruptFileSurfacesError(t *testing.T) {
	root := t.TempDir()
	fs := New(root)
	dir := filepath.Join(root, "global")
	_ = os.MkdirAll(dir, 0o755)
	_ = os.WriteFile(filepath.Join(dir, "bad.md"), []byte("no frontmatter here"), 0o644)
	if _, err := fs.List(ctx()); err == nil {
		t.Fatal("expected error listing a corrupt file, got nil")
	}
}

func TestGetMissing(t *testing.T) {
	fs := New(t.TempDir())
	if _, err := fs.Get(ctx(), "nope"); err != memory.ErrNotFound {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}
