package storage

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mdjarv/agentique/backend/internal/janitor"
	"github.com/mdjarv/agentique/backend/internal/paths"
)

// safeForeignScratchpadPath is the only guard between an HTTP query parameter
// and os.RemoveAll on a directory agentique did not create. Everything it must
// refuse is enumerated here.
func TestSafeForeignScratchpadPathRefusals(t *testing.T) {
	root := filepath.Clean(janitor.ScratchpadRoot())
	if root == "" || root == string(os.PathSeparator) {
		t.Skip("no scratchpad root on this host")
	}
	sep := string(os.PathSeparator)

	refused := []struct {
		name string
		path string
	}{
		{"the root itself", root},
		{"the root with a trailing slash", root + sep},
		{"the parent", filepath.Dir(root)},
		{"a sibling of the root", filepath.Join(filepath.Dir(root), "claude-other")},
		{"an unrelated absolute path", "/etc"},
		{"a relative path", "some-dir"},
		{"traversal out of the root", filepath.Join(root, "..", "elsewhere")},
		{"traversal back in through the parent", filepath.Join(root, "..", filepath.Base(root), "..", "..", "etc")},
		{"a nested path inside a scratchpad", filepath.Join(root, "some-dir", "nested")},
		{"a deeply nested path", filepath.Join(root, "a", "b", "c")},
		{"empty", ""},
	}
	for _, tt := range refused {
		if got, err := safeForeignScratchpadPath(tt.path); err == nil {
			t.Errorf("safeForeignScratchpadPath(%s = %q) allowed %q, want refusal", tt.name, tt.path, got)
		}
	}
}

// A scratchpad that belongs to an agentique worktree is refused by name: it
// goes when its session is reclaimed, and removing it from under a live session
// would break a running CLI.
func TestSafeForeignScratchpadPathRefusesOwnedScratchpads(t *testing.T) {
	prefix := scratchpadPrefix()
	if prefix == "" {
		t.Skip("no scratchpad prefix on this host")
	}
	root := filepath.Clean(janitor.ScratchpadRoot())
	owned := filepath.Join(root, prefix+"Agentique-session-abc123")

	_, err := safeForeignScratchpadPath(owned)
	if err == nil {
		t.Fatalf("allowed an owned scratchpad %q", owned)
	}
	if !strings.Contains(err.Error(), "belongs to a session") {
		t.Errorf("refusal for an owned scratchpad said %q; it should name the reason", err)
	}
}

// The one shape it must accept: a direct child of the root that no worktree owns.
func TestSafeForeignScratchpadPathAcceptsAForeignChild(t *testing.T) {
	root := filepath.Clean(janitor.ScratchpadRoot())
	if root == "" || root == string(os.PathSeparator) {
		t.Skip("no scratchpad root on this host")
	}
	target := filepath.Join(root, "-home-someone-git-otherproject")

	got, err := safeForeignScratchpadPath(target)
	if err != nil {
		t.Fatalf("refused a foreign scratchpad %q: %v", target, err)
	}
	if got != target {
		t.Errorf("returned %q, want %q", got, target)
	}
}

// An uncleaned path that resolves to a legal target is accepted as its cleaned
// form — never passed through with the traversal still in it.
func TestSafeForeignScratchpadPathReturnsTheCleanedPath(t *testing.T) {
	root := filepath.Clean(janitor.ScratchpadRoot())
	if root == "" || root == string(os.PathSeparator) {
		t.Skip("no scratchpad root on this host")
	}
	messy := filepath.Join(root, "a", "..", "-home-someone-git-x")

	got, err := safeForeignScratchpadPath(messy)
	if err != nil {
		t.Fatalf("refused %q: %v", messy, err)
	}
	if strings.Contains(got, "..") {
		t.Errorf("returned an uncleaned path %q", got)
	}
	if got != filepath.Join(root, "-home-someone-git-x") {
		t.Errorf("returned %q, want the cleaned child path", got)
	}
}

// End to end through the guard: a scratchpad discovery actually reports is one
// the endpoint will accept, and an owned one it reports is one the endpoint
// refuses. The two halves have to agree, or the page offers a button that 403s.
func TestTheGuardAgreesWithWhatDiscoveryReports(t *testing.T) {
	isolate(t)

	root := janitor.ScratchpadRoot()
	foreign := filepath.Join(root, "-home-someone-git-unrelated")
	mkdirWith(t, foreign, 500)

	wt := filepath.Join(paths.WorktreeDir(), "proj", "session-abc")
	owned := janitor.ScratchpadDir(wt)
	mkdirWith(t, owned, 200)

	for _, a := range discoverTempArtifacts([]sessionRef{{ID: "abc", WorktreePath: wt}}) {
		_, err := safeForeignScratchpadPath(a.Path)
		switch a.Kind {
		case TempKindForeignScratchpad:
			if err != nil {
				t.Errorf("discovery reported %q as foreign but the guard refused it: %v", a.Path, err)
			}
		case TempKindScratchpad:
			if err == nil {
				t.Errorf("the guard accepted %q, which belongs to session %q", a.Path, a.SessionID)
			}
		}
	}
}
