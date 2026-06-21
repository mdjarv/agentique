//go:build windows

package doctor

import (
	"os"
	"path/filepath"

	"golang.org/x/sys/windows"
)

// freeSpaceMB returns free disk space in megabytes for the volume holding path.
// It reports bytes available to the calling user (quota-aware), matching the
// Bavail semantics of the unix implementation.
func freeSpaceMB(path string) (uint64, error) {
	p, err := windows.UTF16PtrFromString(existingAncestor(path))
	if err != nil {
		return 0, err
	}
	var freeToCaller, total, totalFree uint64
	if err := windows.GetDiskFreeSpaceEx(p, &freeToCaller, &total, &totalFree); err != nil {
		return 0, err
	}
	return freeToCaller / (1024 * 1024), nil
}

// existingAncestor returns path if it exists, otherwise the nearest existing
// parent directory. GetDiskFreeSpaceEx requires an existing directory; the data
// dir may not be created yet when doctor runs.
func existingAncestor(path string) string {
	for {
		if _, err := os.Stat(path); err == nil {
			return path
		}
		parent := filepath.Dir(path)
		if parent == path {
			return path
		}
		path = parent
	}
}
