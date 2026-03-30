// Package upgrade checks for and applies binary updates from GitHub Releases.
package upgrade

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/allbin/agentique/backend/internal/paths"
)

const (
	releasesURL = "https://api.github.com/repos/allbin/agentique/releases/latest"
	cacheFile   = "update-check.json"
	cacheTTL    = 24 * time.Hour
)

// Release describes an available update.
type Release struct {
	Version string // tag name (e.g. "v0.3.0")
	URL     string // download URL for current platform asset
	Notes   string // release body (markdown)
}

type ghRelease struct {
	TagName string    `json:"tag_name"`
	Body    string    `json:"body"`
	Assets  []ghAsset `json:"assets"`
}

type ghAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

type checkCache struct {
	LastCheck     time.Time `json:"lastCheck"`
	LatestVersion string    `json:"latestVersion"`
}

// Check queries GitHub for the latest release.
// Returns nil if current version is up to date or if the check fails.
func Check(currentVersion string) (*Release, error) {
	resp, err := http.Get(releasesURL)
	if err != nil {
		return nil, fmt.Errorf("fetch releases: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API returned %d", resp.StatusCode)
	}

	var rel ghRelease
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return nil, fmt.Errorf("decode release: %w", err)
	}

	latestVersion := strings.TrimPrefix(rel.TagName, "v")
	currentClean := strings.TrimPrefix(currentVersion, "v")

	if latestVersion == currentClean || latestVersion == "" {
		return nil, nil // up to date
	}

	assetName := fmt.Sprintf("agentique-%s-%s", runtime.GOOS, runtime.GOARCH)
	var downloadURL string
	for _, a := range rel.Assets {
		if a.Name == assetName {
			downloadURL = a.BrowserDownloadURL
			break
		}
	}

	if downloadURL == "" {
		return nil, fmt.Errorf("no asset matching %s in release %s", assetName, rel.TagName)
	}

	return &Release{
		Version: rel.TagName,
		URL:     downloadURL,
		Notes:   rel.Body,
	}, nil
}

// CheckCached performs a cached update check. Returns nil if the cache
// is fresh and no update was found last time, or if check fails silently.
func CheckCached(currentVersion string) *Release {
	cacheDir := paths.DataDir()
	cachePath := filepath.Join(cacheDir, cacheFile)

	var cache checkCache
	if data, err := os.ReadFile(cachePath); err == nil {
		json.Unmarshal(data, &cache)
	}

	if time.Since(cache.LastCheck) < cacheTTL {
		// Cache is fresh — check if we already know about a newer version.
		latestClean := strings.TrimPrefix(cache.LatestVersion, "v")
		currentClean := strings.TrimPrefix(currentVersion, "v")
		if latestClean != "" && latestClean != currentClean {
			return &Release{Version: cache.LatestVersion}
		}
		return nil
	}

	rel, err := Check(currentVersion)

	// Update cache regardless of outcome.
	cache.LastCheck = time.Now()
	if rel != nil {
		cache.LatestVersion = rel.Version
	} else {
		cache.LatestVersion = currentVersion
	}
	if data, err := json.Marshal(cache); err == nil {
		os.MkdirAll(cacheDir, 0o755)
		os.WriteFile(cachePath, data, 0o644)
	}

	if err != nil || rel == nil {
		return nil
	}
	return rel
}

// Download fetches the release asset to dest.
func Download(release *Release, dest string) error {
	resp, err := http.Get(release.URL)
	if err != nil {
		return fmt.Errorf("download: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download returned %d", resp.StatusCode)
	}

	// Write to temp file in same directory (for atomic rename).
	dir := filepath.Dir(dest)
	tmp, err := os.CreateTemp(dir, "agentique-update-*")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmp.Name()

	_, err = io.Copy(tmp, resp.Body)
	tmp.Close()
	if err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("write download: %w", err)
	}

	if err := os.Chmod(tmpPath, 0o755); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("chmod: %w", err)
	}

	return os.Rename(tmpPath, dest)
}

// Apply replaces the running binary with the downloaded one.
// Creates a .bak backup of the current binary for rollback.
func Apply(downloadPath, binaryPath string) error {
	backupPath := binaryPath + ".bak"

	// Backup current binary.
	if err := os.Rename(binaryPath, backupPath); err != nil {
		return fmt.Errorf("backup current binary: %w", err)
	}

	// Move new binary into place.
	if err := os.Rename(downloadPath, binaryPath); err != nil {
		// Rollback.
		os.Rename(backupPath, binaryPath)
		return fmt.Errorf("replace binary: %w", err)
	}

	return nil
}
