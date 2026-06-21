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

	return "", fmt.Errorf("no Chrome/Chromium/Edge binary found on PATH or in standard install locations")
}
