package gitops

import (
	"errors"
	"fmt"
	"strings"
)

// ErrUnsafePath rejects anything that is not a plain repo-relative file path.
//
// Every path here arrives from a client, so it is checked before it reaches a
// git argument list even though each command also gets a `--` separator. The
// separator stops a path being read as a flag; it does not stop `../../` from
// naming a file outside the work tree.
var ErrUnsafePath = errors.New("unsafe file path")

// SafeRelativePath reports whether p may be used as a pathspec inside a work
// tree: relative, no parent-directory escape, no NUL, no leading dash.
func SafeRelativePath(p string) error {
	if p == "" || strings.ContainsRune(p, '\x00') || strings.HasPrefix(p, "-") {
		return ErrUnsafePath
	}
	slashed := strings.ReplaceAll(p, `\`, "/")
	if strings.HasPrefix(slashed, "/") {
		return ErrUnsafePath
	}
	// A Windows drive or UNC prefix is absolute wherever this runs.
	if len(p) >= 2 && p[1] == ':' {
		return ErrUnsafePath
	}
	for seg := range strings.SplitSeq(slashed, "/") {
		if seg == ".." {
			return ErrUnsafePath
		}
	}
	return nil
}

// RenamePaths splits the two halves of a porcelain rename entry, whose Path is
// `old -> new` rather than a path. Reports false for every other status.
func RenamePaths(path string) (oldPath, newPath string, ok bool) {
	before, after, found := strings.Cut(path, " -> ")
	if !found || before == "" || after == "" {
		return "", "", false
	}
	return before, after, true
}

// RestoreFile resets one tracked path to HEAD, in the index and the work tree
// both. A deleted file comes back; a modified one loses its changes.
func RestoreFile(dir, relPath string) error {
	if err := SafeRelativePath(relPath); err != nil {
		return err
	}
	if out, err := gitRun(dir, "checkout", "HEAD", "--", relPath); err != nil {
		return fmt.Errorf("restore %s: %w: %s", relPath, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// RemoveTrackedFile deletes a path that exists in the index but not in HEAD —
// a newly added file, or the destination half of a staged rename.
func RemoveTrackedFile(dir, relPath string) error {
	if err := SafeRelativePath(relPath); err != nil {
		return err
	}
	if out, err := gitRun(dir, "rm", "-f", "-q", "--", relPath); err != nil {
		return fmt.Errorf("remove %s: %w: %s", relPath, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// RemoveUntrackedFile deletes one untracked path.
//
// Through `git clean` rather than os.Remove: clean refuses to step outside the
// work tree and leaves ignored files alone (no -x), so the blast radius is
// git's own rather than one this code has to argue for.
func RemoveUntrackedFile(dir, relPath string) error {
	if err := SafeRelativePath(relPath); err != nil {
		return err
	}
	if out, err := gitRun(dir, "clean", "-f", "-q", "--", relPath); err != nil {
		return fmt.Errorf("clean %s: %w: %s", relPath, err, strings.TrimSpace(string(out)))
	}
	return nil
}
