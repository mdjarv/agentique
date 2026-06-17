// Package filestore implements memory.Store on the local filesystem using one
// human-readable Markdown file per record: YAML frontmatter plus the fact text
// as the body, organized into per-scope directories. The files are the source of
// truth and are safe to read, grep, hand-edit and version with git; embeddings
// are never written here. Like the core memory package it depends only on the
// standard library, yaml and the sibling memory package, so it lifts into a
// shared library alongside it.
package filestore

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/mdjarv/agentique/backend/internal/memory"
)

// FileStore is a filesystem-backed memory.Store rooted at a directory.
type FileStore struct {
	root string
	mu   sync.Mutex // serializes writes within this process
}

var _ memory.Store = (*FileStore)(nil)

// New returns a FileStore rooted at dir. The directory is created lazily on first
// write.
func New(dir string) *FileStore { return &FileStore{root: dir} }

// frontmatter is the YAML header serialized for each record. Field order here is
// the order written to disk, chosen for human readability.
type frontmatter struct {
	ID          string    `yaml:"id"`
	Scope       string    `yaml:"scope"`
	Category    string    `yaml:"category"`
	Source      string    `yaml:"source"`
	Pinned      bool      `yaml:"pinned,omitempty"`
	Locked      bool      `yaml:"locked,omitempty"`
	Uses        int       `yaml:"uses"`
	Created     time.Time `yaml:"created"`
	Updated     time.Time `yaml:"updated"`
	DerivedFrom []string  `yaml:"derived_from,omitempty"`
	Related     []string  `yaml:"related,omitempty"`
	Community   int       `yaml:"community,omitempty"`
}

func toFrontmatter(r memory.Record) frontmatter {
	return frontmatter{
		ID:          r.ID,
		Scope:       string(r.Scope),
		Category:    string(r.Category),
		Source:      string(r.Source),
		Pinned:      r.Pinned,
		Locked:      r.Locked,
		Uses:        r.Uses,
		Created:     r.CreatedAt.UTC(),
		Updated:     r.UpdatedAt.UTC(),
		DerivedFrom: r.DerivedFrom,
		Related:     r.Related,
		Community:   r.Community,
	}
}

func (m frontmatter) toRecord(body string) memory.Record {
	scope := memory.Scope(m.Scope)
	if scope == "" {
		scope = memory.ScopeGlobal
	}
	return memory.Record{
		ID:          m.ID,
		Scope:       scope,
		Text:        strings.TrimSpace(body),
		Category:    memory.Category(m.Category),
		Source:      memory.Source(m.Source),
		Pinned:      m.Pinned,
		Locked:      m.Locked,
		Uses:        m.Uses,
		CreatedAt:   m.Created,
		UpdatedAt:   m.Updated,
		DerivedFrom: m.DerivedFrom,
		Related:     m.Related,
		Community:   m.Community,
	}
}

const frontmatterDelim = "---"

func encode(r memory.Record) ([]byte, error) {
	y, err := yaml.Marshal(toFrontmatter(r))
	if err != nil {
		return nil, err
	}
	var b strings.Builder
	b.WriteString(frontmatterDelim + "\n")
	b.Write(y)
	b.WriteString(frontmatterDelim + "\n\n")
	b.WriteString(strings.TrimSpace(r.Text))
	b.WriteString("\n")
	return []byte(b.String()), nil
}

func decode(data []byte) (memory.Record, error) {
	s := strings.ReplaceAll(string(data), "\r\n", "\n")
	prefix := frontmatterDelim + "\n"
	if !strings.HasPrefix(s, prefix) {
		return memory.Record{}, fmt.Errorf("filestore: missing frontmatter")
	}
	rest := s[len(prefix):]
	closer := "\n" + frontmatterDelim
	idx := strings.Index(rest, closer)
	if idx < 0 {
		return memory.Record{}, fmt.Errorf("filestore: unterminated frontmatter")
	}
	yamlPart := rest[:idx]
	body := rest[idx+len(closer):]
	body = strings.TrimPrefix(body, "\n") // drop newline right after closing ---

	var meta frontmatter
	if err := yaml.Unmarshal([]byte(yamlPart), &meta); err != nil {
		return memory.Record{}, fmt.Errorf("filestore: parse frontmatter: %w", err)
	}
	if meta.ID == "" {
		return memory.Record{}, fmt.Errorf("filestore: frontmatter missing id")
	}
	return meta.toRecord(body), nil
}

// sanitizeScope maps a scope to a safe directory name.
func sanitizeScope(s memory.Scope) string {
	str := string(s)
	if str == "" {
		str = string(memory.ScopeGlobal)
	}
	repl := strings.NewReplacer(string(os.PathSeparator), "-", "/", "-", "\\", "-", "..", "-")
	return repl.Replace(str)
}

func (f *FileStore) scopeDir(scope memory.Scope) string {
	return filepath.Join(f.root, sanitizeScope(scope))
}

func (f *FileStore) glob(id string) ([]string, error) {
	return filepath.Glob(filepath.Join(f.root, "*", id+".md"))
}

// Put inserts or replaces a record. The write is atomic (temp file + rename). If
// the record's scope changed, any stale copy under another scope is removed.
func (f *FileStore) Put(_ context.Context, r memory.Record) error {
	if r.ID == "" {
		return fmt.Errorf("filestore: record has no ID")
	}
	f.mu.Lock()
	defer f.mu.Unlock()

	dir := f.scopeDir(r.Scope)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := encode(r)
	if err != nil {
		return err
	}
	dest := filepath.Join(dir, r.ID+".md")
	if err := atomicWrite(dest, data); err != nil {
		return err
	}
	return f.removeOthers(r.ID, dest)
}

func (f *FileStore) removeOthers(id, keep string) error {
	matches, err := f.glob(id)
	if err != nil {
		return err
	}
	for _, m := range matches {
		if m == keep {
			continue
		}
		if err := os.Remove(m); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

func atomicWrite(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	_, werr := tmp.Write(data)
	cerr := tmp.Close()
	if werr != nil {
		os.Remove(name)
		return werr
	}
	if cerr != nil {
		os.Remove(name)
		return cerr
	}
	if err := os.Rename(name, path); err != nil {
		os.Remove(name)
		return err
	}
	return nil
}

// Get returns the record with the given ID from any scope, or memory.ErrNotFound.
func (f *FileStore) Get(_ context.Context, id string) (memory.Record, error) {
	matches, err := f.glob(id)
	if err != nil {
		return memory.Record{}, err
	}
	if len(matches) == 0 {
		return memory.Record{}, memory.ErrNotFound
	}
	data, err := os.ReadFile(matches[0])
	if err != nil {
		return memory.Record{}, err
	}
	return decode(data)
}

// Delete removes the record with the given ID from any scope. A missing ID is
// not an error.
func (f *FileStore) Delete(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	matches, err := f.glob(id)
	if err != nil {
		return err
	}
	for _, m := range matches {
		if err := os.Remove(m); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

// List returns records in the given scopes (all scopes when none are given),
// sorted by creation time. Files that fail to parse are surfaced as errors so a
// corrupt hand-edit is caught rather than silently dropped.
func (f *FileStore) List(_ context.Context, scopes ...memory.Scope) ([]memory.Record, error) {
	var dirs []string
	if len(scopes) == 0 {
		entries, err := os.ReadDir(f.root)
		if err != nil {
			if os.IsNotExist(err) {
				return nil, nil
			}
			return nil, err
		}
		for _, e := range entries {
			if e.IsDir() {
				dirs = append(dirs, filepath.Join(f.root, e.Name()))
			}
		}
	} else {
		for _, s := range scopes {
			dirs = append(dirs, f.scopeDir(s))
		}
	}

	var out []memory.Record
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
				continue
			}
			data, err := os.ReadFile(filepath.Join(dir, e.Name()))
			if err != nil {
				return nil, err
			}
			r, err := decode(data)
			if err != nil {
				return nil, fmt.Errorf("filestore: %s: %w", filepath.Join(dir, e.Name()), err)
			}
			out = append(out, r)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].ID < out[j].ID
		}
		return out[i].CreatedAt.Before(out[j].CreatedAt)
	})
	return out, nil
}
