package content

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// OpenInRoot opens rel inside dir for serving. maxBytes bounds the file; zero
// or less means unbounded.
//
// The containment is [os.Root], not a check followed by an open: the directory
// is written by agents, and an agent can swap a checked path for a symlink
// between the check and the open. os.Root resolves every component relative
// to the root's descriptor and refuses a symlink that leaves it — including an
// absolute one, even when its target is inside, which is the one thing this
// costs over the EvalSymlinks guard it replaces.
func OpenInRoot(dir, rel string, maxBytes int64) (Item, error) {
	name, err := localName(rel)
	if err != nil {
		return Item{}, err
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return Item{}, ErrNotFound
		}
		return Item{}, fmt.Errorf("open root: %w", err)
	}
	defer root.Close()

	f, err := root.Open(name)
	if err != nil {
		return Item{}, openError(err)
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return Item{}, fmt.Errorf("stat: %w", err)
	}
	// Never a directory listing of an agent's scratch space.
	if info.IsDir() {
		f.Close()
		return Item{}, ErrNotFound
	}
	if maxBytes > 0 && info.Size() > maxBytes {
		f.Close()
		return Item{}, ErrTooLarge
	}
	return Item{Name: info.Name(), Size: info.Size(), ModTime: info.ModTime(), Body: f, close: f.Close}, nil
}

// OpenRoot opens dir as a root for a caller that needs more than one file from
// it, such as a directory listing. rel is validated as [OpenInRoot] validates
// it and returned in the form the root's methods take.
func OpenRoot(dir, rel string) (*os.Root, string, error) {
	name := "."
	if rel != "" {
		var err error
		if name, err = localName(rel); err != nil {
			return nil, "", err
		}
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, "", ErrNotFound
		}
		return nil, "", fmt.Errorf("open root: %w", err)
	}
	return root, name, nil
}

// FromBytes is an Item over bytes already in memory.
func FromBytes(name string, body []byte, immutable bool) Item {
	return Item{Name: name, Size: int64(len(body)), Body: bytes.NewReader(body), Immutable: immutable}
}

// CheckName reports whether rel could name something inside a root, without
// touching a filesystem — for a caller that must refuse a bad name before
// asking another machine for it.
func CheckName(rel string) error {
	_, err := localName(rel)
	return err
}

// localName is rel in the root's own form, refused before any filesystem call
// when it could not name something inside a root: empty, absolute, or climbing
// out with "..". "notes..md" is an ordinary name.
func localName(rel string) (string, error) {
	name := filepath.FromSlash(rel)
	if !filepath.IsLocal(name) {
		return "", ErrInvalidPath
	}
	return filepath.Clean(name), nil
}

// openError maps an os.Root failure. A missing file is not found; anything
// else — a symlink out of the root chief among them — is a path this root
// will not serve.
func openError(err error) error {
	if errors.Is(err, fs.ErrNotExist) {
		return ErrNotFound
	}
	return fmt.Errorf("%w: %w", ErrInvalidPath, err)
}
