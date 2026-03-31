// Package update checks for and applies Agentique binary updates from GitHub Releases.
package update

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const repo = "mdjarv/agentique"

// Release describes a GitHub release.
type Release struct {
	TagName string `json:"tag_name"`
	Assets  []Asset `json:"assets"`
}

// Asset is a single downloadable file in a release.
type Asset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

// CheckResult is the outcome of a version check.
type CheckResult struct {
	Current   string
	Latest    string
	Available bool
	Release   *Release
}

// Check compares the current version against the latest GitHub release.
func Check(currentVersion string) (*CheckResult, error) {
	release, err := fetchLatest()
	if err != nil {
		return nil, fmt.Errorf("fetch latest release: %w", err)
	}

	result := &CheckResult{
		Current: currentVersion,
		Latest:  release.TagName,
		Release: release,
	}

	result.Available = SemverNewer(release.TagName, currentVersion)

	return result, nil
}

// SemverNewer reports whether latest is a newer version than current.
// Both may have an optional "v" prefix. Pre-release versions (e.g. 1.0.0-rc1)
// are considered older than their release counterpart.
func SemverNewer(latest, current string) bool {
	latMaj, latMin, latPatch, latPre := parseSemver(latest)
	curMaj, curMin, curPatch, curPre := parseSemver(current)

	if latMaj != curMaj {
		return latMaj > curMaj
	}
	if latMin != curMin {
		return latMin > curMin
	}
	if latPatch != curPatch {
		return latPatch > curPatch
	}
	// Release (no pre-release) is newer than pre-release of same version.
	if curPre != "" && latPre == "" {
		return true
	}
	return false
}

// parseSemver extracts major, minor, patch and pre-release from a version string.
// Handles "v1.2.3", "1.2.3-rc1", "v0.1.0", etc. Returns 0,0,0,"" on parse failure.
func parseSemver(s string) (major, minor, patch int, pre string) {
	s = strings.TrimPrefix(s, "v")

	// Split off pre-release suffix.
	if idx := strings.IndexByte(s, '-'); idx >= 0 {
		pre = s[idx+1:]
		s = s[:idx]
	}

	parts := strings.SplitN(s, ".", 3)
	if len(parts) >= 1 {
		major, _ = strconv.Atoi(parts[0])
	}
	if len(parts) >= 2 {
		minor, _ = strconv.Atoi(parts[1])
	}
	if len(parts) >= 3 {
		patch, _ = strconv.Atoi(parts[2])
	}
	return
}

// Download fetches the binary for the current platform and verifies its checksum.
// Returns the path to the downloaded file in a temp directory.
func Download(release *Release) (string, error) {
	binaryName := fmt.Sprintf("agentique-%s-%s", runtime.GOOS, runtime.GOARCH)

	var binaryURL, checksumsURL string
	for _, a := range release.Assets {
		switch a.Name {
		case binaryName:
			binaryURL = a.BrowserDownloadURL
		case "checksums.txt":
			checksumsURL = a.BrowserDownloadURL
		}
	}

	if binaryURL == "" {
		return "", fmt.Errorf("no binary found for %s/%s in release %s", runtime.GOOS, runtime.GOARCH, release.TagName)
	}
	if checksumsURL == "" {
		return "", fmt.Errorf("no checksums.txt in release %s", release.TagName)
	}

	tmpDir, err := os.MkdirTemp("", "agentique-update-*")
	if err != nil {
		return "", fmt.Errorf("create temp dir: %w", err)
	}

	binaryPath := filepath.Join(tmpDir, binaryName)
	if err := downloadFile(binaryURL, binaryPath); err != nil {
		os.RemoveAll(tmpDir)
		return "", fmt.Errorf("download binary: %w", err)
	}

	checksumsPath := filepath.Join(tmpDir, "checksums.txt")
	if err := downloadFile(checksumsURL, checksumsPath); err != nil {
		os.RemoveAll(tmpDir)
		return "", fmt.Errorf("download checksums: %w", err)
	}

	if err := verifyChecksum(binaryPath, binaryName, checksumsPath); err != nil {
		os.RemoveAll(tmpDir)
		return "", err
	}

	return binaryPath, nil
}

// Replace atomically replaces the binary at targetPath with newBinary.
func Replace(newBinary, targetPath string) error {
	// Verify target is writable.
	if err := checkWritable(targetPath); err != nil {
		return err
	}

	tmpPath := targetPath + ".new"

	src, err := os.Open(newBinary)
	if err != nil {
		return fmt.Errorf("open new binary: %w", err)
	}
	defer src.Close()

	dst, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}

	if _, err := io.Copy(dst, src); err != nil {
		dst.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("copy binary: %w", err)
	}
	dst.Close()

	if err := os.Rename(tmpPath, targetPath); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("replace binary: %w", err)
	}

	return nil
}

// Cleanup removes the temp directory containing the downloaded binary.
func Cleanup(downloadedPath string) {
	os.RemoveAll(filepath.Dir(downloadedPath))
}

func fetchLatest() (*Release, error) {
	// Prefer gh CLI — handles auth, avoids rate limits.
	if ghPath, err := exec.LookPath("gh"); err == nil {
		out, err := exec.Command(ghPath, "api", fmt.Sprintf("repos/%s/releases/latest", repo)).Output()
		if err == nil {
			var r Release
			if err := json.Unmarshal(out, &r); err == nil {
				return &r, nil
			}
		}
	}

	// Fallback: direct HTTP.
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", repo))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API returned %d", resp.StatusCode)
	}

	var r Release
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return nil, err
	}
	return &r, nil
}

func downloadFile(url, dst string) error {
	client := &http.Client{Timeout: 5 * time.Minute}
	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download returned %d", resp.StatusCode)
	}

	f, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = io.Copy(f, resp.Body)
	return err
}

func verifyChecksum(binaryPath, binaryName, checksumsPath string) error {
	data, err := os.ReadFile(checksumsPath)
	if err != nil {
		return fmt.Errorf("read checksums: %w", err)
	}

	var expected string
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[1] == binaryName {
			expected = fields[0]
			break
		}
	}
	if expected == "" {
		return fmt.Errorf("no checksum found for %s", binaryName)
	}

	f, err := os.Open(binaryPath)
	if err != nil {
		return fmt.Errorf("open binary for checksum: %w", err)
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return fmt.Errorf("hash binary: %w", err)
	}

	actual := hex.EncodeToString(h.Sum(nil))
	if actual != expected {
		return fmt.Errorf("checksum mismatch: expected %s, got %s", expected, actual)
	}

	return nil
}

func checkWritable(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("stat %s: %w", path, err)
	}

	// Try opening for write to check permissions.
	f, err := os.OpenFile(path, os.O_WRONLY, info.Mode())
	if err != nil {
		return fmt.Errorf("cannot write to %s (try: sudo agentique upgrade): %w", path, err)
	}
	f.Close()
	return nil
}
