//go:build windows

package storage

import (
	"os"
	"path/filepath"

	"golang.org/x/sys/windows"
)

// diskStats returns total, user-available, and used bytes for the volume holding
// path. "available" is the quota-aware bytes available to the caller; "used" is
// total minus the volume's overall free space (df semantics), so it matches the
// unix implementation rather than counting only the caller's quota.
func diskStats(path string) (totalBytes, availBytes, usedBytes uint64, err error) {
	p, err := windows.UTF16PtrFromString(existingAncestor(path))
	if err != nil {
		return 0, 0, 0, err
	}
	var freeToCaller, total, totalFree uint64
	if err := windows.GetDiskFreeSpaceEx(p, &freeToCaller, &total, &totalFree); err != nil {
		return 0, 0, 0, err
	}
	var used uint64
	if total > totalFree {
		used = total - totalFree
	}
	return total, freeToCaller, used, nil
}

// existingAncestor returns path if it exists, otherwise the nearest existing
// parent directory. GetDiskFreeSpaceEx requires an existing directory.
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
