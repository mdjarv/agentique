//go:build !windows

package procctl

import (
	"errors"
	"os"
	"syscall"
)

// lockFileExclusive takes a non-blocking exclusive flock on f. flock is tied to
// the open file description, so the kernel drops it when the process exits by
// any means — no stale locks to reap.
func lockFileExclusive(f *os.File) error {
	err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if errors.Is(err, syscall.EWOULDBLOCK) {
		return errWouldBlock
	}
	return err
}

func unlockFile(f *os.File) error {
	return syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
}
