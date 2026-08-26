package storage

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mdjarv/agentique/backend/internal/janitor"
	"github.com/mdjarv/agentique/backend/internal/paths"
)

// mkdirWith creates dir and puts a file of n bytes in it, so DirSize reports
// something distinguishable.
func mkdirWith(t *testing.T, dir string, n int) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "blob"), make([]byte, n), 0o644); err != nil {
		t.Fatalf("write in %s: %v", dir, err)
	}
}

// isolate points both the data dir and the temp dir at fresh scratch
// directories, so discovery never sees the developer's real machine.
func isolate(t *testing.T) {
	t.Helper()
	t.Setenv("AGENTIQUE_HOME", t.TempDir())
	t.Setenv("TMPDIR", t.TempDir())
}

func TestDiscoverTempArtifactsAttributesToSessions(t *testing.T) {
	isolate(t)

	wt := filepath.Join(paths.WorktreeDir(), "proj", "session-abc")
	mkdirWith(t, janitor.ChromeProfilePath("abc"), 100)
	mkdirWith(t, janitor.ScratchpadDir(wt), 200)

	got := discoverTempArtifacts([]sessionRef{{ID: "abc", WorktreePath: wt}})
	if len(got) != 2 {
		t.Fatalf("expected 2 artifacts, got %d: %+v", len(got), got)
	}

	byKind := map[string]TempArtifact{}
	for _, a := range got {
		byKind[a.Kind] = a
	}

	chrome, ok := byKind[TempKindChrome]
	if !ok {
		t.Fatal("no chrome profile discovered")
	}
	if chrome.SessionID != "abc" {
		t.Errorf("chrome SessionID = %q, want abc", chrome.SessionID)
	}
	if chrome.Bytes != 100 {
		t.Errorf("chrome Bytes = %d, want 100", chrome.Bytes)
	}

	scratch, ok := byKind[TempKindScratchpad]
	if !ok {
		t.Fatal("no scratchpad discovered")
	}
	if scratch.SessionID != "abc" {
		t.Errorf("scratchpad SessionID = %q, want abc", scratch.SessionID)
	}
	if scratch.Bytes != 200 {
		t.Errorf("scratchpad Bytes = %d, want 200", scratch.Bytes)
	}
}

// The scratchpad root is shared with every other checkout on the machine. A
// scratchpad for a repo agentique does not manage must not be reported — the
// page would invite the user to reclaim someone else's working directory.
func TestDiscoverTempArtifactsIgnoresForeignScratchpads(t *testing.T) {
	isolate(t)

	root := janitor.ScratchpadRoot()
	mkdirWith(t, filepath.Join(root, "-home-someone-git-unrelated"), 500)

	wt := filepath.Join(paths.WorktreeDir(), "proj", "session-abc")
	mkdirWith(t, janitor.ScratchpadDir(wt), 200)

	got := discoverTempArtifacts([]sessionRef{{ID: "abc", WorktreePath: wt}})
	for _, a := range got {
		if strings.Contains(a.Path, "unrelated") {
			t.Fatalf("reported a foreign scratchpad: %s", a.Path)
		}
	}
	if len(got) != 1 {
		t.Fatalf("expected only our scratchpad, got %d: %+v", len(got), got)
	}
}

// An artifact whose session row is gone is still ours and still on the disk, so
// it is reported with an empty SessionID rather than dropped. Hiding it would
// reproduce the under-reporting this discovery exists to fix.
func TestDiscoverTempArtifactsReportsOrphans(t *testing.T) {
	isolate(t)

	gone := filepath.Join(paths.WorktreeDir(), "proj", "session-gone")
	mkdirWith(t, janitor.ScratchpadDir(gone), 300)
	mkdirWith(t, janitor.ChromeProfilePath("vanished"), 400)

	got := discoverTempArtifacts(nil)
	if len(got) != 2 {
		t.Fatalf("expected 2 orphan artifacts, got %d: %+v", len(got), got)
	}
	for _, a := range got {
		if a.SessionID != "" {
			t.Errorf("%s: SessionID = %q, want empty", a.Kind, a.SessionID)
		}
		if a.Bytes == 0 {
			t.Errorf("%s: Bytes = 0, want the directory's size", a.Kind)
		}
	}
}
