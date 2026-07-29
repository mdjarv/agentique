package procctl

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// instanceLockName is the lock file kept in the data directory root.
const instanceLockName = "agentique.lock"

// ErrInstanceLocked reports that another live agentique server already owns the
// data directory.
var ErrInstanceLocked = errors.New("another agentique server owns this data directory")

// errWouldBlock is the platform-neutral "someone else holds the lock" signal
// returned by lockFileExclusive.
var errWouldBlock = errors.New("procctl: lock held by another process")

// InstanceLock is an exclusive advisory lock over an agentique data directory.
//
// It exists because single-instance is a property of the *data directory*, not
// of the listen address: two servers sharing a data dir share its database,
// worktrees, session files, and — critically — the CLI subprocesses the orphan
// reaper claims authority over. The address probe alone let a second server on
// a different port start against the same state and reap the first one's live
// sessions.
//
// The lock is held for the process lifetime by an open descriptor, so the OS
// releases it on exit however the process dies (including SIGKILL). There is no
// stale-lock cleanup path to get wrong: a lock file left behind by a crash is
// simply unlocked, and the next server takes it.
type InstanceLock struct {
	path string
	f    *os.File
}

// AcquireInstanceLock takes the exclusive lock for dataDir, creating the
// directory and lock file if needed. It returns ErrInstanceLocked (wrapped, with
// the holding pid when known) if another server holds it.
func AcquireInstanceLock(dataDir string) (*InstanceLock, error) {
	if dataDir == "" {
		return nil, ErrNoOwner
	}
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, fmt.Errorf("create data dir %s: %w", dataDir, err)
	}
	path := filepath.Join(dataDir, instanceLockName)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open instance lock %s: %w", path, err)
	}

	if err := lockFileExclusive(f); err != nil {
		holder := readHolderPID(f) // read before closing drops our descriptor
		f.Close()
		if !errors.Is(err, errWouldBlock) {
			return nil, fmt.Errorf("lock %s: %w", path, err)
		}
		if holder > 0 {
			return nil, fmt.Errorf("%w (pid %d)", ErrInstanceLocked, holder)
		}
		return nil, ErrInstanceLocked
	}

	// Record the holder so a refused starter can name the process it lost to.
	// Advisory only — the lock itself is what enforces exclusion.
	if err := f.Truncate(0); err == nil {
		_, _ = f.WriteAt([]byte(strconv.Itoa(os.Getpid())+"\n"), 0)
	}
	return &InstanceLock{path: path, f: f}, nil
}

// Release drops the lock. Safe to call on a nil lock, so callers can defer it
// unconditionally.
func (l *InstanceLock) Release() error {
	if l == nil || l.f == nil {
		return nil
	}
	// Clear the pid first: once unlocked, a leftover pid would misidentify a
	// live unrelated process as the holder.
	f := l.f
	l.f = nil
	_ = f.Truncate(0)
	return errors.Join(unlockFile(f), f.Close())
}

// readHolderPID returns the pid recorded in an already-open lock file, or 0 when
// it is absent or unparseable (an older server, a crash mid-write).
func readHolderPID(f *os.File) int {
	buf := make([]byte, 32)
	n, err := f.ReadAt(buf, 0)
	if n == 0 && err != nil {
		return 0
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(buf[:n])))
	if err != nil {
		return 0
	}
	return pid
}
