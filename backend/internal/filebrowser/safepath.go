package filebrowser

import (
	"errors"
	"path/filepath"
	"strings"
)

var errPathEscape = errors.New("path escapes project root")

// safePath resolves a relative path within root, ensuring the result stays
// inside root even after symlink resolution. Returns the cleaned absolute path.
func safePath(root, relative string) (string, error) {
	root = filepath.Clean(root)

	if relative == "" {
		return root, nil
	}

	joined := filepath.Join(root, relative)
	joined = filepath.Clean(joined)

	// Pre-check before hitting the filesystem.
	if !strings.HasPrefix(joined, root+string(filepath.Separator)) && joined != root {
		return "", errPathEscape
	}

	// Resolve symlinks and re-check.
	resolved, err := filepath.EvalSymlinks(joined)
	if err != nil {
		return "", err
	}

	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", err
	}

	if resolved != resolvedRoot && !strings.HasPrefix(resolved, resolvedRoot+string(filepath.Separator)) {
		return "", errPathEscape
	}

	return resolved, nil
}
