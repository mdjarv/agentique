package project

import (
	"github.com/mdjarv/agentique/backend/internal/gitops"
	"github.com/mdjarv/agentique/backend/internal/store"
)

// Kind is what a project's folder is, and so which features its sessions get.
type Kind string

const (
	// KindGit is a folder that is itself a repository root: sessions can take
	// a linked worktree, and the branch, diff and merge surfaces apply.
	KindGit Kind = "git"
	// KindFolder is any other folder, including one inside some other
	// repository. Sessions run straight in it, and nothing git applies.
	KindFolder Kind = "folder"
)

// KindOf reports the kind of the folder at path (gitops.IsRepoRoot).
//
// It is derived on every read, never stored: one stat is cheaper than keeping a
// column honest, and `git init` in the folder, or deleting its .git, changes
// the answer on the next list without anything having to notice.
func KindOf(path string) Kind {
	if gitops.IsRepoRoot(path) {
		return KindGit
	}
	return KindFolder
}

// Wire is a project as clients receive it: the stored row plus what is derived
// from its folder.
//
// Kind is optional on the wire. A peer on a release from before it sends no
// kind, and the client reads that as "not reported" — the git features that
// peer has always offered — never as a plain folder.
type Wire struct {
	store.Project
	Kind Kind `json:"kind,omitempty"`
}

// ToWire derives a project's wire form.
func ToWire(p store.Project) Wire {
	return Wire{Project: p, Kind: KindOf(p.Path)}
}

// ToWireList derives the wire form of every project.
func ToWireList(ps []store.Project) []Wire {
	out := make([]Wire, len(ps))
	for i, p := range ps {
		out[i] = ToWire(p)
	}
	return out
}
