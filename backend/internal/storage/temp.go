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
	// TempKindForeignScratchpad is a Claude scratchpad under the same root that
	// belongs to a checkout agentique does not manage — someone running the CLI
	// directly in a repo rather than through a session. Reported, never reaped
	// as part of a session: see the note below.
	TempKindForeignScratchpad = "foreign-scratchpad"
)

// scratchpadPrefix is the mangled-path prefix every agentique worktree
// scratchpad shares. Derived by asking the janitor for the scratchpad it would
// use for the worktree *base* — the same forward mapping it reaps by — so the
// path-mangling scheme is spelled in exactly one place.
//
// Anything under the scratchpad root without this prefix belongs to some other
// checkout on this machine. It is still **reported**, as its own kind: the
// exclusion is a rule about what agentique may reap on a session's behalf, and
// a directory nobody can see is one nobody can decide about. On the machine
// this was written for it hid 4.05 GB across 217 directories — more than the
// page called reclaimable — while the volume bar read 94% full.
//
// Reporting it is not owning it. A foreign scratchpad is never attributed to a
// session, never part of a reclaim, and never swept: the only way it goes is an
// operator naming it, one directory at a time, through DeleteForeignScratchpad.
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
		ours := prefix != "" && strings.HasPrefix(filepath.Base(dir), prefix)
		kind := TempKindForeignScratchpad
		// A foreign scratchpad has no owner by construction, and must not be
		// given one: `owner` is keyed by the path a session *would* use, so a
		// lookup can only hit for a directory we already matched by prefix.
		// Reading it unconditionally would be correct today and a
		// mis-attribution the moment the mangling scheme changes.
		sessionID := ""
		if ours {
			kind = TempKindScratchpad
			sessionID = owner[dir]
		}
		out = append(out, TempArtifact{
			Kind:      kind,
			Path:      dir,
			SessionID: sessionID,
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
