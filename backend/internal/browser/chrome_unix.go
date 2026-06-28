//go:build !windows

package browser

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// chromeBinaries is the search order for Chrome/Chromium binaries on Unix.
var chromeBinaries = []string{
	"google-chrome-stable",
	"google-chrome",
	"chromium-browser",
	"chromium",
}

func findChromeBinary() (string, error) {
	for _, name := range chromeBinaries {
		path, err := exec.LookPath(name)
		if err == nil {
			return path, nil
		}
	}
	// Fallback: a Chromium provisioned by `playwright install` into the user
	// cache. This is what auto-provisioning installs when no system browser exists.
	if path := findPlaywrightChromium(); path != "" {
		return path, nil
	}
	return "", fmt.Errorf("no Chrome/Chromium binary found (tried: %v, and the Playwright cache)", chromeBinaries)
}

// findPlaywrightChromium locates a Chromium installed by `playwright install`
// under the OS user cache dir (~/.cache/ms-playwright on Linux,
// ~/Library/Caches/ms-playwright on macOS). Returns "" if none is present.
func findPlaywrightChromium() string {
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		return ""
	}
	base := filepath.Join(cacheDir, "ms-playwright")
	for _, pat := range []string{
		filepath.Join(base, "chromium-*", "chrome-linux*", "chrome"),
		filepath.Join(base, "chromium-*", "chrome-mac*", "Chromium.app", "Contents", "MacOS", "Chromium"),
	} {
		if hits, _ := filepath.Glob(pat); len(hits) > 0 {
			return hits[len(hits)-1] // latest revision
		}
	}
	return ""
}
