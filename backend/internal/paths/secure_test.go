package paths

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestSecureDataDirCreatesOwnerOnly(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("mode bits are advisory on Windows")
	}
	root := t.TempDir()
	dir := filepath.Join(root, "agentique")
	t.Setenv("AGENTIQUE_HOME", dir)

	if err := SecureDataDir(); err != nil {
		t.Fatalf("SecureDataDir: %v", err)
	}
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o700 {
		t.Errorf("mode = %o, want 700", got)
	}
}

// Installs created by an earlier version have a 0755 data dir full of
// credentials. They must be fixed forward on the next start, not left as-is.
func TestSecureDataDirTightensAnExistingLooseDir(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("mode bits are advisory on Windows")
	}
	dir := filepath.Join(t.TempDir(), "agentique")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENTIQUE_HOME", dir)

	if err := SecureDataDir(); err != nil {
		t.Fatalf("SecureDataDir: %v", err)
	}
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o700 {
		t.Errorf("mode = %o, want 700 (an existing loose dir must be tightened)", got)
	}
}

func TestSecureFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("mode bits are advisory on Windows")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "agentique.db")
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := SecureFile(path); err != nil {
		t.Fatalf("SecureFile: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Errorf("mode = %o, want 600", got)
	}

	// The -wal/-shm sidecars only exist while a connection is open, so a
	// missing path is the normal case, not a failure.
	if err := SecureFile(filepath.Join(dir, "agentique.db-wal")); err != nil {
		t.Errorf("SecureFile(missing) = %v, want nil", err)
	}
}
