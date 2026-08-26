package session

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/mdjarv/agentique/backend/internal/gitops"
)

// discardRepo builds a repo with one committed file and returns its path.
func discardRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	for _, args := range [][]string{
		{"init", "-q"},
		{"config", "user.email", "test@example.com"},
		{"config", "user.name", "Test"},
	} {
		runGit(t, dir, args...)
	}
	writeAt(t, dir, "kept.txt", "original\n")
	runGit(t, dir, "add", "-A")
	runGit(t, dir, "commit", "-q", "-m", "first")
	return dir
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
}

func writeAt(t *testing.T, dir, rel, content string) {
	t.Helper()
	full := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func exists(dir, rel string) bool {
	_, err := os.Stat(filepath.Join(dir, rel))
	return err == nil
}

func TestDiscardOneRefusesAPathGitDoesNotReportChanged(t *testing.T) {
	dir := discardRepo(t)
	files := []gitops.FileStatus{{Path: "other.txt", Status: "modified"}}

	// The allowlist is the real guard: a path outside git's own list of changes
	// is refused whatever it names, and nothing is touched.
	if err := discardOne(dir, files, "kept.txt"); err == nil {
		t.Fatal("expected a refusal for an unreported path")
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "kept.txt")); string(got) != "original\n" {
		t.Errorf("kept.txt = %q, want it untouched", got)
	}
}

func TestDiscardOneUndoesAModification(t *testing.T) {
	dir := discardRepo(t)
	writeAt(t, dir, "kept.txt", "edited\n")

	if err := discardOne(dir, []gitops.FileStatus{{Path: "kept.txt", Status: "modified"}}, "kept.txt"); err != nil {
		t.Fatalf("discardOne: %v", err)
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "kept.txt")); string(got) != "original\n" {
		t.Errorf("kept.txt = %q, want the committed content", got)
	}
}

func TestDiscardOneDeletesAnUntrackedFile(t *testing.T) {
	dir := discardRepo(t)
	writeAt(t, dir, "new.txt", "scratch\n")

	if err := discardOne(dir, []gitops.FileStatus{{Path: "new.txt", Status: "untracked"}}, "new.txt"); err != nil {
		t.Fatalf("discardOne: %v", err)
	}
	if exists(dir, "new.txt") {
		t.Error("new.txt survived")
	}
}

func TestDiscardOneDropsAStagedAddition(t *testing.T) {
	dir := discardRepo(t)
	writeAt(t, dir, "added.txt", "staged\n")
	runGit(t, dir, "add", "added.txt")

	// "added" has no HEAD version to restore, so restoring is not the undo.
	if err := discardOne(dir, []gitops.FileStatus{{Path: "added.txt", Status: "added"}}, "added.txt"); err != nil {
		t.Fatalf("discardOne: %v", err)
	}
	if exists(dir, "added.txt") {
		t.Error("added.txt survived")
	}
}

func TestDiscardOneUndoesBothHalvesOfARename(t *testing.T) {
	dir := discardRepo(t)
	runGit(t, dir, "mv", "kept.txt", "moved.txt")

	files := []gitops.FileStatus{{Path: "kept.txt -> moved.txt", Status: "renamed"}}
	if err := discardOne(dir, files, "moved.txt"); err != nil {
		t.Fatalf("discardOne: %v", err)
	}
	if exists(dir, "moved.txt") {
		t.Error("the destination survived — half an undo leaves two copies")
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "kept.txt")); string(got) != "original\n" {
		t.Errorf("kept.txt = %q, want the source restored", got)
	}
}

func TestDiscardOneRestoresADeletion(t *testing.T) {
	dir := discardRepo(t)
	if err := os.Remove(filepath.Join(dir, "kept.txt")); err != nil {
		t.Fatal(err)
	}

	if err := discardOne(dir, []gitops.FileStatus{{Path: "kept.txt", Status: "deleted"}}, "kept.txt"); err != nil {
		t.Fatalf("discardOne: %v", err)
	}
	if !exists(dir, "kept.txt") {
		t.Error("kept.txt was not brought back")
	}
}
