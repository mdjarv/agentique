//go:build !windows

package browser

import (
	"fmt"
	"os/exec"
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
	return "", fmt.Errorf("no Chrome/Chromium binary found (tried: %v)", chromeBinaries)
}
