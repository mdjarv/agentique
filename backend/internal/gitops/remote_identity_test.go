package gitops

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestCanonicalizeRemoteURL(t *testing.T) {
	cases := map[string]string{
		// The load-bearing equivalence: SSH and HTTPS forms of one GitHub repo.
		"git@github.com:MDjarv/Agentique.git":     "github.com/mdjarv/agentique",
		"ssh://git@github.com/mdjarv/agentique":   "github.com/mdjarv/agentique",
		"https://github.com/mdjarv/agentique.git": "github.com/mdjarv/agentique",
		"https://github.com/mdjarv/agentique":     "github.com/mdjarv/agentique",
		"https://github.com/mdjarv/agentique/":    "github.com/mdjarv/agentique",

		// Credentials and ports never differentiate identity.
		"https://user:pass@github.com/org/repo.git": "github.com/org/repo",
		"ssh://git@github.com:22/org/repo.git":      "github.com/org/repo",
		"https://github.com:443/org/repo":           "github.com/org/repo",

		// Case-insensitive (GitHub semantics), including the .git suffix.
		"HTTPS://GitHub.COM/Org/Repo.GIT": "github.com/org/repo",

		// Other hosts still work structurally.
		"git@gitlab.com:group/sub/repo.git": "gitlab.com/group/sub/repo",

		// No canonical form.
		"":                    "",
		"/home/user/repo":     "",
		"../relative/repo":    "",
		"C:\\repos\\thing":    "",
		"file:///srv/git/x":   "",
		"git@github.com:":     "",
		"https://github.com/": "",
	}
	for input, want := range cases {
		if got := CanonicalizeRemoteURL(input); got != want {
			t.Errorf("CanonicalizeRemoteURL(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestCanonicalRemoteURLPrefersUpstreamThenOrigin(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		if out, err := gitRun(dir, args...); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	run("init")

	if got := CanonicalRemoteURL(dir); got != "" {
		t.Fatalf("no remotes: got %q, want empty", got)
	}

	run("remote", "add", "aaa", "https://github.com/other/first.git")
	if got := CanonicalRemoteURL(dir); got != "github.com/other/first" {
		t.Fatalf("alpha fallback: got %q", got)
	}

	run("remote", "add", "origin", "git@github.com:me/fork.git")
	if got := CanonicalRemoteURL(dir); got != "github.com/me/fork" {
		t.Fatalf("origin preference: got %q", got)
	}

	run("remote", "add", "upstream", "https://github.com/org/canonical.git")
	if got := CanonicalRemoteURL(dir); got != "github.com/org/canonical" {
		t.Fatalf("upstream preference: got %q", got)
	}
}

func TestCanonicalRemoteURLMonorepoSubdir(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	if out, err := gitRun(dir, "init"); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}
	if out, err := gitRun(dir, "remote", "add", "origin", "git@github.com:org/mono.git"); err != nil {
		t.Fatalf("git remote add: %v: %s", err, out)
	}

	sub := filepath.Join(dir, "packages", "ui")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}

	// Repo root: bare key. Subdir project: key qualified with the relative
	// path, so two subdir projects in one repo stay distinct while the same
	// subdir on another machine still matches.
	if got := CanonicalRemoteURL(dir); got != "github.com/org/mono" {
		t.Fatalf("root: got %q", got)
	}
	if got := CanonicalRemoteURL(sub); got != "github.com/org/mono::packages/ui" {
		t.Fatalf("subdir: got %q", got)
	}
}
