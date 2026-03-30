//go:build !windows

package doctor

import "syscall"

// freeSpaceMB returns free disk space in megabytes for the given path.
func freeSpaceMB(path string) (uint64, error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return 0, err
	}
	return (stat.Bavail * uint64(stat.Bsize)) / (1024 * 1024), nil
}
