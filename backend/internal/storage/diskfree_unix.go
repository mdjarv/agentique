//go:build !windows

package storage

import "syscall"

// diskStats returns total, user-available, and used bytes for the filesystem
// holding path. "used" follows df semantics (blocks − free, so it counts
// root-reserved blocks as used); "available" (Bavail) excludes reserved blocks.
// Block counts are multiplied by Bsize, the portable field across the unix
// Statfs_t variants (on Linux it equals the fragment size f_frsize).
func diskStats(path string) (totalBytes, availBytes, usedBytes uint64, err error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return 0, 0, 0, err
	}
	bsize := uint64(stat.Bsize)
	total := stat.Blocks * bsize
	avail := stat.Bavail * bsize
	var used uint64
	if stat.Blocks > stat.Bfree {
		used = (stat.Blocks - stat.Bfree) * bsize
	}
	return total, avail, used, nil
}
