//go:build windows

package procctl

import (
	"errors"
	"os"

	"golang.org/x/sys/windows"
)

// lockFileExclusive takes a non-blocking exclusive byte-range lock on f. Like
// flock, a Windows file lock is released when the handle closes — including on
// abnormal termination — so no stale-lock cleanup is needed.
func lockFileExclusive(f *os.File) error {
	// Lock the whole (possibly empty) file: offset 0, length max.
	err := windows.LockFileEx(
		windows.Handle(f.Fd()),
		windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY,
		0, ^uint32(0), ^uint32(0), new(windows.Overlapped),
	)
	if errors.Is(err, windows.ERROR_LOCK_VIOLATION) || errors.Is(err, windows.ERROR_IO_PENDING) {
		return errWouldBlock
	}
	return err
}

func unlockFile(f *os.File) error {
	return windows.UnlockFileEx(
		windows.Handle(f.Fd()), 0, ^uint32(0), ^uint32(0), new(windows.Overlapped),
	)
}
