package paths

import (
	"fmt"
	"os"
	"runtime"
)

// The data directory holds the database — which stores auth session tokens,
// every paired machine's bearer token, and pairing/invite tokens in plaintext —
// plus its WAL, timed backups, the brain corpus, session files and worktrees.
// The admin secret and the machine signing key beside them are written 0600,
// which is pointless if the directory they live in is traversable and the
// database next to them is world-readable.
//
// Owner-only on the DIRECTORY is what closes the whole class: it covers every
// file already there, everything the timed backup writes later (its mode is
// SQLite's to choose, not ours), and anything a future feature drops in.
//
// Called from the serve command's production block, never from a constructor —
// this touches the operator's real data directory (CLAUDE.md).
const dataDirMode = 0o700

// SecureDataDir creates the data directory owner-only, and tightens it if an
// earlier version created it group- or world-readable. Existing installs are
// fixed forward on the next start, the same way the admin secret is.
//
// On Windows the mode bits are advisory (Go maps only the read-only bit), so
// this is a no-op there rather than a false promise; ACL support would be a
// separate piece of work.
func SecureDataDir() error {
	dir := DataDir()
	if err := os.MkdirAll(dir, dataDirMode); err != nil {
		return fmt.Errorf("create data directory %s: %w", dir, err)
	}
	if runtime.GOOS == "windows" {
		return nil
	}
	info, err := os.Stat(dir)
	if err != nil {
		return fmt.Errorf("stat data directory %s: %w", dir, err)
	}
	if info.Mode().Perm() == dataDirMode {
		return nil
	}
	if err := os.Chmod(dir, dataDirMode); err != nil {
		return fmt.Errorf("restrict data directory %s: %w", dir, err)
	}
	return nil
}

// SecureFile restricts one file to owner-only. Used for the database and the
// sidecars SQLite creates beside it (-wal, -shm), which inherit the process
// umask rather than any mode we choose. A missing file is not an error: the
// sidecars only exist while a connection is open.
func SecureFile(path string) error {
	if runtime.GOOS == "windows" {
		return nil
	}
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if info.Mode().Perm() == 0o600 {
		return nil
	}
	return os.Chmod(path, 0o600)
}
