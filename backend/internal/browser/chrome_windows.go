//go:build windows

package browser

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// chromeNames are the PATH-resolvable executable names to try first. Chrome and
// Edge are usually NOT on PATH on Windows, so PATH lookup is only a fast path —
// the well-known install locations below are the reliable source.
var chromeNames = []string{"chrome.exe", "msedge.exe", "chromium.exe"}

// findChromeBinary locates Chrome/Chromium/Edge on Windows. It first tries PATH,
// then the standard per-machine and per-user install locations. Edge ships with
// Windows 11, so it is the dependable fallback.
func findChromeBinary() (string, error) {
	for _, name := range chromeNames {
		if path, err := exec.LookPath(name); err == nil {
			return path, nil
		}
	}

	for _, dir := range []string{
		os.Getenv("ProgramFiles"),
		os.Getenv("ProgramFiles(x86)"),
		os.Getenv("LocalAppData"),
	} {
		if dir == "" {
			continue
		}
		for _, rel := range []string{
			`Google\Chrome\Application\chrome.exe`,
			`Chromium\Application\chrome.exe`,
			`Microsoft\Edge\Application\msedge.exe`,
		} {
			candidate := filepath.Join(dir, rel)
			if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
				return candidate, nil
			}
		}
	}

	// Fallback: a Chromium provisioned by `playwright install` into the user
	// cache. This is what auto-provisioning installs when no system browser exists.
	if path := findPlaywrightChromium(); path != "" {
		return path, nil
	}

	return "", fmt.Errorf("no Chrome/Chromium/Edge binary found on PATH, in standard install locations, or the Playwright cache")
}

// findPlaywrightChromium locates a Chromium installed by `playwright install`
// under the OS user cache dir (%LocalAppData%\ms-playwright). Returns "" if none.
func findPlaywrightChromium() string {
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		return ""
	}
	pat := filepath.Join(cacheDir, "ms-playwright", "chromium-*", "chrome-win*", "chrome.exe")
	if hits, _ := filepath.Glob(pat); len(hits) > 0 {
		return hits[len(hits)-1] // latest revision
	}
	return ""
}
