package gitops

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestSafeRelativePathRejectsEscapes(t *testing.T) {
	for _, p := range []string{
		"",
		"/etc/passwd",
		"../secrets",
		"a/../../b",
		`..\windows`,
		"C:/Windows/System32",
		"--upload-pack=touch",
		"a\x00b",
	} {
		if err := SafeRelativePath(p); !errors.Is(err, ErrUnsafePath) {
			t.Errorf("SafeRelativePath(%q) = %v, want ErrUnsafePath", p, err)
		}
	}
}

func TestSafeRelativePathAcceptsOrdinaryPaths(t *testing.T) {
	for _, p := range []string{
		"main.go",
		"internal/session/git_discard.go",
		"a..b/c",
		"dir/..file",
		"weird name with spaces.md",
	} {
		if err := SafeRelativePath(p); err != nil {
			t.Errorf("SafeRelativePath(%q) = %v, want nil", p, err)
		}
	}
}

func TestRenamePaths(t *testing.T) {
	oldPath, newPath, ok := RenamePaths("old/a.go -> new/b.go")
	if !ok || oldPath != "old/a.go" || newPath != "new/b.go" {
		t.Fatalf("RenamePaths = %q, %q, %v", oldPath, newPath, ok)
	}
	if _, _, ok := RenamePaths("plain/path.go"); ok {
		t.Error("a plain path must not read as a rename")
	}
}

// writeNested writes a file, creating its parent directories. The package's
// writeFile helper is flat.
func writeNested(t *testing.T, dir, rel, content string) {
	t.Helper()
	full := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, dir, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, rel))
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	return string(b)
}

func TestRestoreFileUndoesAModification(t *testing.T) {
	dir := initGitRepo(t)
	writeFile(t, dir, "README", "edited")

	if err := RestoreFile(dir, "README"); err != nil {
		t.Fatalf("RestoreFile: %v", err)
	}
	if got := readFile(t, dir, "README"); got != "hello" {
		t.Errorf("content = %q, want the committed one", got)
	}
}

func TestRestoreFileBringsBackADeletion(t *testing.T) {
	dir := initGitRepo(t)
	if err := os.Remove(filepath.Join(dir, "README")); err != nil {
		t.Fatal(err)
	}

	if err := RestoreFile(dir, "README"); err != nil {
		t.Fatalf("RestoreFile: %v", err)
	}
	if got := readFile(t, dir, "README"); got != "hello" {
		t.Errorf("content = %q, want the committed one", got)
	}
}

func TestRemoveUntrackedFileDeletesIt(t *testing.T) {
	dir := initGitRepo(t)
	writeNested(t, dir, "scratch/new.txt", "temp\n")

	if err := RemoveUntrackedFile(dir, "scratch/new.txt"); err != nil {
		t.Fatalf("RemoveUntrackedFile: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "scratch/new.txt")); !os.IsNotExist(err) {
		t.Errorf("file still present: %v", err)
	}
	// The committed file beside it is untouched.
	if got := readFile(t, dir, "README"); got != "hello" {
		t.Errorf("README = %q, want it left alone", got)
	}
}

func TestRemoveUntrackedFileLeavesTrackedContentAlone(t *testing.T) {
	dir := initGitRepo(t)
	writeFile(t, dir, "README", "edited")

	// git clean refuses tracked paths, so a mis-routed call cannot destroy
	// committed work.
	if err := RemoveUntrackedFile(dir, "README"); err != nil {
		t.Fatalf("RemoveUntrackedFile: %v", err)
	}
	if got := readFile(t, dir, "README"); got != "edited" {
		t.Errorf("content = %q, want the edit still there", got)
	}
}

func TestRemoveTrackedFileDropsAStagedAddition(t *testing.T) {
	dir := initGitRepo(t)
	writeFile(t, dir, "added.txt", "brand new\n")
	if out, err := gitRun(dir, "add", "added.txt"); err != nil {
		t.Fatalf("git add: %v: %s", err, out)
	}

	if err := RemoveTrackedFile(dir, "added.txt"); err != nil {
		t.Fatalf("RemoveTrackedFile: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "added.txt")); !os.IsNotExist(err) {
		t.Errorf("file still present: %v", err)
	}
}

func TestDiscardHelpersRefuseAnEscape(t *testing.T) {
	dir := initGitRepo(t)
	outside := filepath.Join(dir, "..", "outside.txt")
	if err := os.WriteFile(outside, []byte("do not touch\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	for name, fn := range map[string]func(string, string) error{
		"RestoreFile":         RestoreFile,
		"RemoveTrackedFile":   RemoveTrackedFile,
		"RemoveUntrackedFile": RemoveUntrackedFile,
	} {
		if err := fn(dir, "../outside.txt"); !errors.Is(err, ErrUnsafePath) {
			t.Errorf("%s = %v, want ErrUnsafePath", name, err)
		}
	}
	if _, err := os.Stat(outside); err != nil {
		t.Errorf("the file outside the repo was touched: %v", err)
	}
}
