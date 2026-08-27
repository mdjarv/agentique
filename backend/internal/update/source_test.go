package update

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The source channel's verdict is the whole feature: everything downstream —
// the chip, the row, the button — is gated on Behind. So these tests are about
// when it is WITHHELD at least as much as when it is set.

func gitRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@e",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@e",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}
	run("init", "-q", "-b", "master")
	run("config", "user.email", "t@e")
	run("config", "user.name", "t")
	writeFile(t, dir, "a.txt", "one")
	run("add", ".")
	run("commit", "-qm", "first")
	return dir
}

func writeFile(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@e",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@e",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

func commitFile(t *testing.T, dir, name, body, msg string) string {
	t.Helper()
	writeFile(t, dir, name, body)
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-qm", msg)
	return git(t, dir, "rev-parse", "--short", "HEAD")
}

func checkerFor(dir, builtFrom string) *SourceChecker {
	return NewSourceChecker(SourceOptions{Dir: dir, BuiltFrom: builtFrom, Origin: OriginLocal})
}

// The gate that decides whether this checkout speaks for this binary at all.
//
// It cannot be inferred, which is the whole reason it is stamped: a local build
// sitting on an exact tag produces the same bare "v1.2.3" CI produces, and
// main.commit is set on both paths. Getting this wrong means offering to
// rebuild over a binary somebody downloaded.
func TestSourceOnlySpeaksForALocalBuild(t *testing.T) {
	dir := gitRepo(t)
	built := git(t, dir, "rev-parse", "--short", "HEAD")
	commitFile(t, dir, "b.txt", "two", "second")

	// Same checkout, same commits — only the origin differs.
	local := NewSourceChecker(SourceOptions{Dir: dir, BuiltFrom: built, Origin: OriginLocal}).
		Refresh(context.Background())
	if !local.Behind {
		t.Fatalf("a local build one commit behind is behind: %+v", local)
	}

	for _, origin := range []BuildOrigin{OriginRelease, OriginUnknown} {
		st := NewSourceChecker(SourceOptions{Dir: dir, BuiltFrom: built, Origin: origin}).
			Refresh(context.Background())
		if st.Behind {
			t.Fatalf("origin %q must produce no verdict: %+v", origin, st)
		}
		if st.Ahead != 0 {
			t.Errorf("origin %q must not report commit counts either: %+v", origin, st)
		}
		if st.Blocker == "" {
			t.Errorf("origin %q must say why it is silent", origin)
		}
		if st.Origin != string(origin) {
			t.Errorf("origin = %q, want %q", st.Origin, origin)
		}
	}
}

// A downloaded release must not even be probed for a staged binary: the whole
// relationship this channel describes does not exist for it.
func TestSourceReleaseBuildSkipsTheStagedProbe(t *testing.T) {
	fake := filepath.Join(t.TempDir(), "installed")
	if err := os.WriteFile(fake, []byte("#!/bin/sh\necho 'agentique v9.9.9'\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	st := NewSourceChecker(SourceOptions{
		Dir:         gitRepo(t),
		BuiltFrom:   "abc1234",
		Origin:      OriginRelease,
		Version:     "v0.1.0",
		InstallPath: fake,
	}).Refresh(context.Background())

	if st.Staged {
		t.Fatal("a release install gets no staged verdict from the source channel")
	}
}

func TestSourceInStepIsNotBehind(t *testing.T) {
	dir := gitRepo(t)
	head := git(t, dir, "rev-parse", "--short", "HEAD")

	st := checkerFor(dir, head).Refresh(context.Background())
	if st.Behind {
		t.Fatal("a checkout in step with the running build must not be behind")
	}
	if st.Ahead != 0 {
		t.Fatalf("ahead = %d, want 0", st.Ahead)
	}
	if st.CheckError != "" {
		t.Fatalf("unexpected check error: %s", st.CheckError)
	}
}

func TestSourceAheadIsBehind(t *testing.T) {
	dir := gitRepo(t)
	built := git(t, dir, "rev-parse", "--short", "HEAD")
	commitFile(t, dir, "b.txt", "two", "second")
	head := commitFile(t, dir, "c.txt", "three", "third")

	st := checkerFor(dir, built).Refresh(context.Background())
	if !st.Behind {
		t.Fatalf("two commits past the build should be behind: %+v", st)
	}
	if st.Ahead != 2 {
		t.Fatalf("ahead = %d, want 2", st.Ahead)
	}
	if st.Head != head {
		t.Fatalf("head = %q, want %q", st.Head, head)
	}
	if st.HeadSubject != "third" {
		t.Fatalf("headSubject = %q, want %q", st.HeadSubject, "third")
	}
}

// A dirty tree suppresses the verdict entirely. Unsaved work is not a version,
// and an in-place build from it would install something no commit describes.
func TestSourceDirtyWithholdsTheVerdict(t *testing.T) {
	dir := gitRepo(t)
	built := git(t, dir, "rev-parse", "--short", "HEAD")
	commitFile(t, dir, "b.txt", "two", "second")
	writeFile(t, dir, "scratch.txt", "not committed")

	st := checkerFor(dir, built).Refresh(context.Background())
	if !st.Dirty {
		t.Fatal("an untracked file must count as dirty")
	}
	if st.Behind {
		t.Fatal("a dirty checkout must not suggest an upgrade")
	}
	if st.Ahead != 1 {
		t.Fatalf("the facts still hold: ahead = %d, want 1", st.Ahead)
	}
	if st.Blocker == "" {
		t.Fatal("a withheld verdict must say why")
	}
}

// Building in place compiles whatever is checked out, so a checkout on another
// branch cannot be offered: the binary would not be the commit we reported.
func TestSourceOtherBranchWithholdsTheVerdict(t *testing.T) {
	dir := gitRepo(t)
	built := git(t, dir, "rev-parse", "--short", "HEAD")
	commitFile(t, dir, "b.txt", "two", "second")
	git(t, dir, "checkout", "-q", "-b", "feature")

	st := checkerFor(dir, built).Refresh(context.Background())
	if st.CheckedOut != "feature" {
		t.Fatalf("checkedOut = %q, want feature", st.CheckedOut)
	}
	if st.Behind {
		t.Fatal("a checkout on another branch must not suggest an upgrade")
	}
	if !strings.Contains(st.Blocker, "feature") {
		t.Fatalf("the blocker must name the branch, got %q", st.Blocker)
	}
}

// Fail closed: a commit this repo does not recognise is a probe that could not
// see, never evidence that the build is current or stale.
func TestSourceUnknownCommitIsNotBehind(t *testing.T) {
	dir := gitRepo(t)
	commitFile(t, dir, "b.txt", "two", "second")

	st := checkerFor(dir, "deadbee").Refresh(context.Background())
	if st.Behind {
		t.Fatal("an unrecognised build commit must never produce a behind verdict")
	}
	if st.Blocker == "" {
		t.Fatal("an unknown comparison must say so")
	}
}

func TestSourceUnstampedBuildIsNotBehind(t *testing.T) {
	dir := gitRepo(t)
	commitFile(t, dir, "b.txt", "two", "second")

	for _, builtFrom := range []string{"", "none", "unknown"} {
		st := checkerFor(dir, builtFrom).Refresh(context.Background())
		if st.Behind {
			t.Fatalf("builtFrom %q must not produce a verdict", builtFrom)
		}
	}
}

func TestSourceNotARepo(t *testing.T) {
	st := checkerFor(t.TempDir(), "abc1234").Refresh(context.Background())
	if st.Behind {
		t.Fatal("a directory that is not a checkout must not be behind")
	}
	if st.CheckError == "" {
		t.Fatal("a non-checkout must report why it could not answer")
	}
}

// The staged check is independent of git: a binary at the install path that is
// not the running one needs a restart, not a build, and must survive a git
// failure.
func TestSourceStagedBinaryDetectedWithoutGit(t *testing.T) {
	dir := t.TempDir()
	fake := filepath.Join(dir, "fake-agentique")
	script := "#!/bin/sh\necho 'agentique v9.9.9-1-gabcdef'\n"
	if err := os.WriteFile(fake, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}

	sc := NewSourceChecker(SourceOptions{
		Dir:         t.TempDir(), // not a repo — the staged check must not care
		BuiltFrom:   "abc1234",
		Version:     "v9.9.9-1-gabcdef", // exactly what the install path reports
		InstallPath: fake,
		Origin:      OriginLocal,
	})
	st := sc.Refresh(context.Background())
	if st.Staged {
		t.Fatal("an installed binary reporting the running version is not staged")
	}

	sc = NewSourceChecker(SourceOptions{
		Dir:         t.TempDir(),
		BuiltFrom:   "abc1234",
		Version:     "v0.1.0",
		InstallPath: fake,
		Origin:      OriginLocal,
	})
	st = sc.Refresh(context.Background())
	if !st.Staged {
		t.Fatal("an installed binary on a different version is staged")
	}
	if st.InstalledVersion != "v9.9.9-1-gabcdef" {
		t.Fatalf("installedVersion = %q", st.InstalledVersion)
	}
}

func TestDescribesCommit(t *testing.T) {
	cases := []struct {
		version, commit string
		want            bool
	}{
		{"v0.6.0-69-g41b8b57", "41b8b57", true},
		{"v0.6.0-69-g41b8b57-dirty", "41b8b57", true},
		// Abbreviation lengths need not agree; the shorter prefixes the longer.
		{"v0.6.0-69-g41b8b57abc", "41b8b57", true},
		{"v0.6.0-69-g41b8b57", "41b8b57abc", true},
		{"v0.6.0-69-g41b8b57", "deadbee", false},
		// A plain release tag names no commit, so it can never prove it is head.
		{"v0.6.0", "41b8b57", false},
		{"", "41b8b57", false},
		{"v0.6.0-69-g41b8b57", "", false},
		{"dev", "41b8b57", false},
	}
	for _, c := range cases {
		if got := describesCommit(c.version, c.commit); got != c.want {
			t.Errorf("describesCommit(%q, %q) = %v, want %v", c.version, c.commit, got, c.want)
		}
	}
}

// The state `just install` leaves behind: a binary at the install path built
// from the branch head, with the process still on the old one. A restart is the
// complete answer there, and the status has to say so or the UI offers a
// two-minute rebuild of the identical commit.
func TestSourceStagedBinaryAtHeadIsCurrent(t *testing.T) {
	dir := gitRepo(t)
	built := git(t, dir, "rev-parse", "--short", "HEAD")
	head := commitFile(t, dir, "b.txt", "two", "second")

	fake := filepath.Join(t.TempDir(), "installed")
	script := "#!/bin/sh\necho 'agentique v1.0.0-1-g" + head + "'\n"
	if err := os.WriteFile(fake, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}

	st := NewSourceChecker(SourceOptions{
		Dir:         dir,
		BuiltFrom:   built,
		Version:     "v1.0.0",
		InstallPath: fake,
		Origin:      OriginLocal,
	}).Refresh(context.Background())

	if !st.Staged {
		t.Fatal("a different installed version is staged")
	}
	if !st.StagedIsCurrent {
		t.Fatalf("the staged binary is the branch head and must say so: %+v", st)
	}
	// The facts about the running process are unchanged by any of that.
	if !st.Behind || st.Ahead != 1 {
		t.Fatalf("the running process is still one commit behind: %+v", st)
	}
}

func TestSourceStagedBinaryBehindHeadIsNotCurrent(t *testing.T) {
	dir := gitRepo(t)
	built := git(t, dir, "rev-parse", "--short", "HEAD")
	mid := commitFile(t, dir, "b.txt", "two", "second")
	commitFile(t, dir, "c.txt", "three", "third")

	fake := filepath.Join(t.TempDir(), "installed")
	script := "#!/bin/sh\necho 'agentique v1.0.0-1-g" + mid + "'\n"
	if err := os.WriteFile(fake, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}

	st := NewSourceChecker(SourceOptions{
		Dir:         dir,
		BuiltFrom:   built,
		Version:     "v1.0.0",
		InstallPath: fake,
		Origin:      OriginLocal,
	}).Refresh(context.Background())

	if !st.Staged {
		t.Fatal("a different installed version is staged")
	}
	if st.StagedIsCurrent {
		t.Fatal("a staged binary the branch has moved past is not current")
	}
}

// A probe that cannot run the binary is not evidence that one is waiting.
func TestSourceStagedFailsClosed(t *testing.T) {
	sc := NewSourceChecker(SourceOptions{
		Dir:         t.TempDir(),
		Version:     "v1.0.0",
		InstallPath: filepath.Join(t.TempDir(), "does-not-exist"),
		Origin:      OriginLocal,
	})
	if sc.Refresh(context.Background()).Staged {
		t.Fatal("an unreadable install path must not report a staged binary")
	}
}

func TestSourceStatusIsCachedAndDated(t *testing.T) {
	dir := gitRepo(t)
	built := git(t, dir, "rev-parse", "--short", "HEAD")
	sc := checkerFor(dir, built)

	// Status before any probe answers from the zero value rather than blocking.
	if got := sc.Status(); got.Dir != dir || got.Branch != DefaultSourceBranch {
		t.Fatalf("unprobed status should still describe its subject: %+v", got)
	}

	sc.Refresh(context.Background())
	st := sc.Status()
	if st.CheckedAt == "" {
		t.Fatal("a completed probe must be dated")
	}
	if _, err := time.Parse(time.RFC3339, st.CheckedAt); err != nil {
		t.Fatalf("checkedAt is not RFC3339: %v", err)
	}
}
