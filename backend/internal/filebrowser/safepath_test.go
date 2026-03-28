package filebrowser

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSafePath_Root(t *testing.T) {
	root := t.TempDir()
	got, err := safePath(root, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != root {
		t.Fatalf("expected %q, got %q", root, got)
	}
}

func TestSafePath_ValidSubpath(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "src", "main.go")
	os.MkdirAll(filepath.Join(root, "src"), 0o755)
	os.WriteFile(sub, []byte("package main"), 0o644)

	got, err := safePath(root, "src/main.go")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != sub {
		t.Fatalf("expected %q, got %q", sub, got)
	}
}

func TestSafePath_TraversalDotDot(t *testing.T) {
	root := t.TempDir()
	_, err := safePath(root, "../../etc/passwd")
	if err == nil {
		t.Fatal("expected error for path traversal, got nil")
	}
}

func TestSafePath_TraversalMidPath(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "foo"), 0o755)
	_, err := safePath(root, "foo/../../etc/passwd")
	if err == nil {
		t.Fatal("expected error for mid-path traversal, got nil")
	}
}

func TestSafePath_SymlinkEscape(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()

	os.Symlink(outside, filepath.Join(root, "evil"))

	_, err := safePath(root, "evil")
	if err == nil {
		t.Fatal("expected error for symlink escape, got nil")
	}
}

func TestSafePath_SymlinkInsideRoot(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "real")
	os.MkdirAll(target, 0o755)
	os.Symlink(target, filepath.Join(root, "link"))

	got, err := safePath(root, "link")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != target {
		t.Fatalf("expected %q, got %q", target, got)
	}
}

func TestSafePath_NonexistentPath(t *testing.T) {
	root := t.TempDir()
	_, err := safePath(root, "does/not/exist")
	if err == nil {
		t.Fatal("expected error for non-existent path, got nil")
	}
}
