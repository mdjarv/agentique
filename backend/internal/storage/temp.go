package storage

import (
	"path/filepath"
	"strings"

	"github.com/mdjarv/agentique/backend/internal/janitor"
	"github.com/mdjarv/agentique/backend/internal/paths"
)

// Agentique's footprint does not stop at its data directory. Every session that
// drives a browser gets a Chrome profile under the OS temp dir, and every
// session whose CLI writes scratch files gets a Claude scratchpad there too.
// Those two together were larger than any single worktree on the machine this
// was written on, and the Storage page reported neither, because it walked
// paths.DataDir() and nothing else.
//
// They are reported as their own group rather than folded into the data-dir
// categories: "Agentique data" is a claim about one directory, and quietly
// widening it would make the number wrong in a different way.

// Temp artifact kinds, mirroring the janitor's own vocabulary.
const (
	TempKindChrome     = "chrome-profile"
	TempKindScratchpad = "scratchpad"
)

// scratchpadPrefix is the mangled-path prefix every agentique worktree
// scratchpad shares. Derived by asking the janitor for the scratchpad it would
// use for the worktree *base* — the same forward mapping it reaps by — so the
// path-mangling scheme is spelled in exactly one place.
//
// Anything under the scratchpad root without this prefix belongs to some other
// checkout on this machine and is none of our business.
func scratchpadPrefix() string {
	base := filepath.Clean(paths.WorktreeDir())
	dir := janitor.ScratchpadDir(base)
	if dir == "" {
		return ""
	}
	return filepath.Base(dir) + "-"
}

// discoverTempArtifacts finds agentique's per-session artifacts outside the data
// directory and attributes each to its session where it can.
//
// Attribution runs forward — from a session to the path it would own — rather
// than by un-mangling a directory name, so a scheme change breaks discovery
// loudly instead of mis-attributing someone else's directory to a session.
// A path that matches no session is still reported, with an empty SessionID:
// that is an orphan, and hiding it would be the same under-reporting this
// function exists to fix.
func discoverTempArtifacts(sessions []sessionRef) []TempArtifact {
	owner := make(map[string]string, len(sessions)*2)
	for _, s := range sessions {
		if p := janitor.ChromeProfilePath(s.ID); p != "" {
			owner[filepath.Clean(p)] = s.ID
		}
		if s.WorktreePath == "" {
			continue
		}
		if p := janitor.ScratchpadDir(s.WorktreePath); p != "" {
			owner[filepath.Clean(p)] = s.ID
		}
	}

	out := make([]TempArtifact, 0, 8)

	chromeDirs, _ := janitor.DiscoverChromeDirs()
	for _, raw := range chromeDirs {
		dir := filepath.Clean(raw)
		out = append(out, TempArtifact{
			Kind:      TempKindChrome,
			Path:      dir,
			SessionID: owner[dir],
			Bytes:     janitor.DirSize(dir),
		})
	}

	prefix := scratchpadPrefix()
	scratchDirs, _ := janitor.DiscoverScratchpadDirs()
	for _, raw := range scratchDirs {
		dir := filepath.Clean(raw)
		if prefix == "" || !strings.HasPrefix(filepath.Base(dir), prefix) {
			continue // another checkout's scratchpad — not ours to report or reap
		}
		out = append(out, TempArtifact{
			Kind:      TempKindScratchpad,
			Path:      dir,
			SessionID: owner[dir],
			Bytes:     janitor.DirSize(dir),
		})
	}
	return out
}

// sessionRef is the slice of a session row the temp discovery needs.
type sessionRef struct {
	ID           string
	WorktreePath string
}
