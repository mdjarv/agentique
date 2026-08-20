package gitops

import (
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
)

// Cross-machine project identity (docs/multi-machine-research.md M3): two
// checkouts on different machines are "the same project" when their primary
// git remotes canonicalize to the same key. The key is host/path, lowercased,
// with credentials, ports, and the ".git" suffix stripped — so the same
// GitHub repo cloned over SSH on one machine and HTTPS on another matches:
//
//	git@github.com:Org/Repo.git      -> github.com/org/repo
//	ssh://git@github.com/Org/Repo    -> github.com/org/repo
//	https://github.com/org/repo.git  -> github.com/org/repo
//
// Lowercasing the path is correct for GitHub (owner/repo are
// case-insensitive) and is the deliberate bias — GitHub is the primary
// target. Local-path remotes produce "" (they never group).

// scpLikeRemote matches the scp shorthand "user@host:path" (no scheme). The
// path must not start with a separator, which keeps Windows drive paths
// ("C:\repo") and absolute-path colons out.
var scpLikeRemote = regexp.MustCompile(`^(?:[^@/\\]+@)?([^:/\\]+\.[^:/\\]+):([^/\\].*)$`)

// CanonicalizeRemoteURL reduces a git remote URL to its canonical identity
// key, or "" when the URL has no canonical form (local paths, empty input).
func CanonicalizeRemoteURL(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}

	var host, path string
	if strings.Contains(s, "://") {
		u, err := url.Parse(s)
		if err != nil || u.Hostname() == "" {
			return ""
		}
		host, path = u.Hostname(), u.Path
	} else if m := scpLikeRemote.FindStringSubmatch(s); m != nil {
		host, path = m[1], m[2]
	} else {
		return ""
	}

	// Lowercase before stripping so ".GIT" trims too (GitHub is
	// case-insensitive throughout).
	host = strings.ToLower(host)
	path = strings.ToLower(strings.Trim(path, "/"))
	path = strings.TrimSuffix(path, ".git")
	path = strings.TrimSuffix(path, "/")
	if path == "" {
		return ""
	}
	return host + "/" + path
}

// CanonicalRemoteURL resolves the checkout's primary remote (upstream, else
// origin, else the alphabetically-first remote — the fork-aware preference)
// and canonicalizes it. A project rooted in a repo SUBDIRECTORY gets the
// relative path appended ("host/org/repo::sub/dir"): two monorepo-subdir
// projects share a remote but are distinct projects, while the same subdir
// registered on two machines still matches. Returns "" when the directory
// has no usable remote; errors are folded into "" because a missing identity
// is a normal state, not a failure.
func CanonicalRemoteURL(projectDir string) string {
	key := canonicalRemoteOnly(projectDir)
	if key == "" {
		return ""
	}
	if rel := repoRelativePath(projectDir); rel != "" {
		return key + "::" + rel
	}
	return key
}

// repoRelativePath returns the project dir's path relative to the repo
// toplevel with forward slashes, or "" when the project IS the toplevel (or
// it cannot be determined).
func repoRelativePath(projectDir string) string {
	out, err := gitRun(projectDir, "rev-parse", "--show-toplevel")
	if err != nil {
		return ""
	}
	top := strings.TrimSpace(string(out))
	if top == "" {
		return ""
	}
	rel, err := filepath.Rel(top, projectDir)
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
		return ""
	}
	return filepath.ToSlash(rel)
}

func canonicalRemoteOnly(projectDir string) string {
	names, err := gitRun(projectDir, "remote")
	if err != nil {
		return ""
	}
	remotes := []string{}
	for line := range strings.SplitSeq(strings.TrimSpace(string(names)), "\n") {
		if v := strings.TrimSpace(line); v != "" {
			remotes = append(remotes, v)
		}
	}
	if len(remotes) == 0 {
		return ""
	}

	pick := ""
	for _, preferred := range []string{"upstream", "origin"} {
		for _, name := range remotes {
			if name == preferred {
				pick = name
				break
			}
		}
		if pick != "" {
			break
		}
	}
	if pick == "" {
		min := remotes[0]
		for _, name := range remotes[1:] {
			if name < min {
				min = name
			}
		}
		pick = min
	}

	out, err := gitRun(projectDir, "remote", "get-url", pick)
	if err != nil {
		return ""
	}
	return CanonicalizeRemoteURL(strings.TrimSpace(string(out)))
}
