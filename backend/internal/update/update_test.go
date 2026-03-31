package update

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

func TestSemverNewer(t *testing.T) {
	tests := []struct {
		latest, current string
		want            bool
	}{
		// Same version.
		{"v0.1.0", "v0.1.0", false},
		{"0.1.0", "v0.1.0", false},
		{"v0.1.0", "0.1.0", false},

		// Newer versions.
		{"v0.2.0", "v0.1.0", true},
		{"v1.0.0", "v0.9.9", true},
		{"v0.1.1", "v0.1.0", true},
		{"v2.0.0", "v1.99.99", true},

		// Older versions.
		{"v0.1.0", "v0.2.0", false},
		{"v0.9.9", "v1.0.0", false},

		// Pre-release handling.
		{"v0.2.0", "v0.2.0-rc1", true},  // release newer than pre-release
		{"v0.2.0-rc1", "v0.2.0", false}, // pre-release not newer than release
		{"v0.2.0-rc2", "v0.2.0-rc1", false}, // same version, both pre-release
		{"v0.3.0-rc1", "v0.2.0", true},  // higher version even with pre-release

		// Edge cases.
		{"v0.1.0", "dev", true}, // dev parses as 0.0.0
		{"", "", false},
	}

	for _, tt := range tests {
		got := SemverNewer(tt.latest, tt.current)
		if got != tt.want {
			t.Errorf("SemverNewer(%q, %q) = %v, want %v", tt.latest, tt.current, got, tt.want)
		}
	}
}

func TestParseSemver(t *testing.T) {
	tests := []struct {
		input                  string
		major, minor, patch    int
		pre                    string
	}{
		{"v1.2.3", 1, 2, 3, ""},
		{"0.1.0", 0, 1, 0, ""},
		{"v0.2.0-rc1", 0, 2, 0, "rc1"},
		{"v1.0.0-beta.2", 1, 0, 0, "beta.2"},
		{"dev", 0, 0, 0, ""},
		{"", 0, 0, 0, ""},
	}

	for _, tt := range tests {
		maj, min, pat, pre := parseSemver(tt.input)
		if maj != tt.major || min != tt.minor || pat != tt.patch || pre != tt.pre {
			t.Errorf("parseSemver(%q) = (%d, %d, %d, %q), want (%d, %d, %d, %q)",
				tt.input, maj, min, pat, pre, tt.major, tt.minor, tt.patch, tt.pre)
		}
	}
}

func TestVerifyChecksum(t *testing.T) {
	dir := t.TempDir()

	// Create a fake binary.
	binaryContent := []byte("fake-binary-content")
	binaryPath := filepath.Join(dir, "agentique-linux-amd64")
	if err := os.WriteFile(binaryPath, binaryContent, 0o644); err != nil {
		t.Fatal(err)
	}

	// Compute its checksum.
	h := sha256.Sum256(binaryContent)
	checksum := hex.EncodeToString(h[:])

	// Write a valid checksums file.
	checksumsPath := filepath.Join(dir, "checksums.txt")
	checksumsContent := checksum + "  agentique-linux-amd64\n" +
		"0000000000000000000000000000000000000000000000000000000000000000  agentique-darwin-arm64\n"
	if err := os.WriteFile(checksumsPath, []byte(checksumsContent), 0o644); err != nil {
		t.Fatal(err)
	}

	// Valid checksum should pass.
	if err := verifyChecksum(binaryPath, "agentique-linux-amd64", checksumsPath); err != nil {
		t.Errorf("valid checksum failed: %v", err)
	}

	// Wrong binary name should fail.
	if err := verifyChecksum(binaryPath, "agentique-darwin-arm64", checksumsPath); err == nil {
		t.Error("expected checksum mismatch for wrong binary name")
	}

	// Missing binary name should fail.
	if err := verifyChecksum(binaryPath, "agentique-windows-amd64", checksumsPath); err == nil {
		t.Error("expected error for missing binary in checksums")
	}
}

func TestReplaceAtomicity(t *testing.T) {
	dir := t.TempDir()

	// Create the "current" binary.
	targetPath := filepath.Join(dir, "agentique")
	if err := os.WriteFile(targetPath, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}

	// Create the "new" binary.
	newPath := filepath.Join(dir, "agentique-new")
	if err := os.WriteFile(newPath, []byte("new-version"), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := Replace(newPath, targetPath); err != nil {
		t.Fatalf("Replace failed: %v", err)
	}

	// Verify the target has new content.
	got, err := os.ReadFile(targetPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "new-version" {
		t.Errorf("after Replace, target content = %q, want %q", got, "new-version")
	}

	// Verify the target is executable.
	info, err := os.Stat(targetPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&0o111 == 0 {
		t.Error("replaced binary is not executable")
	}

	// Verify no temp file left behind.
	tmpPath := targetPath + ".new"
	if _, err := os.Stat(tmpPath); err == nil {
		t.Error("temp file was not cleaned up")
	}
}
