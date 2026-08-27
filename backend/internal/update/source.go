package update

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// The source channel (docs/upgrades.md).
//
// The release channel answers "is there a newer tag published". On a machine
// someone develops on, that question is never asked and never answered:
// Channel() classifies a git-describe build as dev, and a dev build is never
// behind. Meanwhile the thing that actually happens — a commit lands on the
// local checkout and the running server keeps serving what it was built from —
// has nothing watching it.
//
// So: a second source of the same verdict, from a git checkout already on disk.
// It never touches the network, it never fetches, and it reports only what the
// working tree can prove right now.

// SourceStatus is what this server can see about the checkout it was built
// from. Every field is about THIS machine, like the rest of Status.
type SourceStatus struct {
	// Dir is the configured checkout ([update] source-dir).
	Dir string `json:"dir"`
	// Branch is the ref we compare against ([update] source-branch, default
	// "master").
	Branch string `json:"branch"`
	// Head is Branch's commit, short. "" when it could not be read.
	Head string `json:"head,omitempty"`
	// HeadSubject is that commit's subject line, so a row can say what is
	// waiting rather than only how much.
	HeadSubject string `json:"headSubject,omitempty"`
	// BuiltFrom is main.commit — the commit this running binary was built from.
	BuiltFrom string `json:"builtFrom,omitempty"`
	// Ahead is how many commits are on Branch but not in the running build.
	// Zero when in step, and zero when we could not tell.
	Ahead int `json:"ahead"`
	// Behind is the verdict: a clean checkout, on Branch, strictly ahead of the
	// running build. Anything git could not answer leaves this false — unknown
	// is never a licence to act.
	Behind bool `json:"behind"`
	// Dirty reports uncommitted changes in the checkout. A dirty tree suppresses
	// the verdict entirely: unsaved work is not a version, and a build from it
	// would install something no commit describes.
	Dirty bool `json:"dirty"`
	// CheckedOut is the branch the working tree is actually on. When it is not
	// Branch, an in-place build would compile something other than what this
	// status reports, so the verdict is withheld.
	CheckedOut string `json:"checkedOut,omitempty"`
	// Origin is how the running binary was built: only a local build is this
	// checkout's to speak for. A downloaded binary reports "release" here and
	// gets no verdict at all — its version is the release channel's business.
	Origin string `json:"origin,omitempty"`
	// Staged reports that the binary at the install path is not the one running
	// — what `just install` leaves behind until the service restarts. It needs
	// no build, only a restart.
	Staged bool `json:"staged"`
	// InstalledVersion is what the binary at the install path reports, when it
	// differs from the running one.
	InstalledVersion string `json:"installedVersion,omitempty"`
	// StagedIsCurrent reports that the staged binary was built from the branch
	// head — so a restart is the COMPLETE answer and a rebuild would recompile
	// the identical commit. Read from the commit a git-describe version already
	// carries, never by the client, which does no version arithmetic.
	StagedIsCurrent bool `json:"stagedIsCurrent,omitempty"`
	// Buildable is the full preflight: a clean checkout on the right branch, a
	// resolvable toolchain, a writable install dir and a service to restart.
	Buildable bool `json:"buildable"`
	// Blocker names, in a sentence, why there is no action here. It is set for
	// ordinary states too (a dirty tree, another branch) and is not an error.
	Blocker string `json:"blocker,omitempty"`
	// CheckedAt is when the last probe completed (RFC3339 UTC). Stamped on
	// failure too, so a stale answer can be dated.
	CheckedAt string `json:"checkedAt,omitempty"`
	// CheckError is the last check's failure, "" when it succeeded. The previous
	// answer stands, exactly as it does for the release check.
	CheckError string `json:"checkError,omitempty"`
}

// BuildOrigin says how the running binary was produced. It is stamped at build
// time because it cannot be worked out afterwards: a local build sitting on an
// exact tag stamps the bare tag, byte-identical to CI's, and main.commit is set
// either way. Without it the source channel would offer to rebuild over a
// binary somebody downloaded from a release page.
type BuildOrigin string

const (
	// OriginLocal is `just build` — this machine compiled it, from a checkout.
	OriginLocal BuildOrigin = "local"
	// OriginRelease is a published asset, whether installed by install.sh, by
	// the release channel's own apply, or by hand.
	OriginRelease BuildOrigin = "release"
	// OriginUnknown is a plain `go build` with no ldflags. Fail closed.
	OriginUnknown BuildOrigin = ""
)

// SourceOptions configures a SourceChecker.
type SourceOptions struct {
	// Dir is the checkout to watch. Required — an unset source-dir means the
	// channel is simply off, and nothing here runs.
	Dir string
	// Origin is how the running binary was built. Only OriginLocal produces a
	// verdict: a downloaded binary's version is the release channel's business,
	// and a checkout that happens to sit beside it is not where it came from.
	Origin BuildOrigin
	// Branch is the ref to compare against; "" means "master".
	Branch string
	// BuiltFrom is main.commit, the commit the running binary was built from.
	BuiltFrom string
	// Version is main.version, used to spot a staged binary at InstallPath.
	Version string
	// InstallPath is the binary the service would start. Empty disables the
	// staged check.
	InstallPath string
	// Interval is the background re-check period (default 1h), matching the
	// release check's beat.
	Interval time.Duration
	// Now is injected in tests.
	Now func() time.Time
}

// DefaultSourceBranch is the ref the source channel compares against when
// [update] source-branch is unset.
const DefaultSourceBranch = "master"

func (o SourceOptions) withDefaults() SourceOptions {
	if o.Branch == "" {
		o.Branch = DefaultSourceBranch
	}
	if o.Interval <= 0 {
		o.Interval = time.Hour
	}
	if o.Now == nil {
		o.Now = func() time.Time { return time.Now().UTC() }
	}
	return o
}

// SourceChecker holds the last answer the checkout gave and refreshes it on the
// same slow beat as the release check. Status never performs IO: a failed probe
// keeps the previous answer and its age.
type SourceChecker struct {
	opts SourceOptions

	// probeMu serializes probes so a burst of refreshes is one pass over git.
	probeMu sync.Mutex

	mu   sync.RWMutex
	last SourceStatus

	done     chan struct{}
	stopOnce sync.Once
	wg       sync.WaitGroup
}

// NewSourceChecker builds a SourceChecker. It performs no IO — the poll loop
// starts from serve.go's production block, never from a constructor a test
// might call.
func NewSourceChecker(opts SourceOptions) *SourceChecker {
	o := opts.withDefaults()
	return &SourceChecker{
		opts: o,
		last: SourceStatus{Dir: o.Dir, Branch: o.Branch, BuiltFrom: o.BuiltFrom},
		done: make(chan struct{}),
	}
}

// Start probes once and then re-probes on Interval until Stop.
func (s *SourceChecker) Start(ctx context.Context) {
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.probe(ctx)
		t := time.NewTicker(s.opts.Interval)
		defer t.Stop()
		for {
			select {
			case <-s.done:
				return
			case <-ctx.Done():
				return
			case <-t.C:
				s.probe(ctx)
			}
		}
	}()
}

// Stop halts the poll loop and waits for an in-flight probe to park.
func (s *SourceChecker) Stop() {
	s.stopOnce.Do(func() { close(s.done) })
	s.wg.Wait()
}

// Status returns the cached answer. Never blocks on git.
func (s *SourceChecker) Status() SourceStatus {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.last
}

// Refresh re-probes now and returns the result — the "check again" affordance.
func (s *SourceChecker) Refresh(ctx context.Context) SourceStatus {
	s.probe(ctx)
	return s.Status()
}

// Dir and Branch are what the applier builds from.
func (s *SourceChecker) Dir() string    { return s.opts.Dir }
func (s *SourceChecker) Branch() string { return s.opts.Branch }

// probe reads the checkout once and folds the result into the cache.
func (s *SourceChecker) probe(ctx context.Context) {
	s.probeMu.Lock()
	defer s.probeMu.Unlock()

	st := s.evaluate(ctx)
	st.CheckedAt = s.opts.Now().Format(time.RFC3339)

	s.mu.Lock()
	s.last = st
	s.mu.Unlock()
}

// evaluate is the whole verdict, and it fails closed at every step: anything
// git cannot answer leaves Behind false.
func (s *SourceChecker) evaluate(ctx context.Context) SourceStatus {
	st := SourceStatus{
		Dir:       s.opts.Dir,
		Branch:    s.opts.Branch,
		BuiltFrom: s.opts.BuiltFrom,
		Origin:    string(s.opts.Origin),
	}

	// This binary has to be one this checkout produced. A downloaded release
	// sitting next to a clone of the same repo is the release channel's
	// business, and telling its owner their "local master has moved" would be
	// describing a relationship that does not exist. Unknown fails closed for
	// the same reason.
	if s.opts.Origin != OriginLocal {
		st.Blocker = notLocalBlocker(s.opts.Origin)
		return st
	}

	// The staged check is independent of git: the binary on disk can be ahead of
	// the running process whatever the checkout says, and that state needs only
	// a restart. Probe it first so it survives a git failure below.
	if v, ok := s.installedVersion(ctx); ok && v != s.opts.Version {
		st.Staged = true
		st.InstalledVersion = v
	}

	if s.opts.Dir == "" {
		st.Blocker = "no source checkout is configured"
		return st
	}
	if !isGitRepo(s.opts.Dir) {
		st.CheckError = fmt.Sprintf("%s is not a git checkout", s.opts.Dir)
		st.Blocker = st.CheckError
		return st
	}

	branch, err := gitLine(ctx, s.opts.Dir, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		st.CheckError = fmt.Sprintf("read current branch: %v", err)
		return st
	}
	st.CheckedOut = branch

	dirty, err := gitDirty(ctx, s.opts.Dir)
	if err != nil {
		st.CheckError = fmt.Sprintf("read working tree: %v", err)
		return st
	}
	st.Dirty = dirty

	head, err := gitLine(ctx, s.opts.Dir, "rev-parse", "--short", s.opts.Branch)
	if err != nil {
		st.CheckError = fmt.Sprintf("read %s: %v", s.opts.Branch, err)
		return st
	}
	st.Head = head
	if subject, serr := gitLine(ctx, s.opts.Dir, "log", "-1", "--format=%s", s.opts.Branch); serr == nil {
		st.HeadSubject = subject
	}
	// A staged binary built from the head needs a restart, not a rebuild.
	st.StagedIsCurrent = st.Staged && describesCommit(st.InstalledVersion, head)

	// How far the branch has moved past the running build. A build whose commit
	// this repo does not recognise — a release binary, a rebased history, a
	// shallow clone — makes this fail, and unknown is not behind.
	if s.opts.BuiltFrom == "" || s.opts.BuiltFrom == "none" || s.opts.BuiltFrom == "unknown" {
		st.Blocker = "this build does not record the commit it came from"
		return st
	}
	ahead, err := gitCount(ctx, s.opts.Dir, s.opts.BuiltFrom+".."+s.opts.Branch)
	if err != nil {
		st.CheckError = fmt.Sprintf("compare %s against the running build: %v", s.opts.Branch, err)
		st.Blocker = "the running build's commit is not in this checkout"
		return st
	}
	st.Ahead = ahead

	// Everything below decides whether to SAY something, not whether the facts
	// are true. A dirty tree or another branch is an ordinary state, so it gets
	// a blocker rather than an error, and the verdict is withheld.
	switch {
	case dirty:
		st.Blocker = "the checkout has uncommitted changes"
	case branch != s.opts.Branch:
		st.Blocker = fmt.Sprintf("the checkout is on %s, not %s", branch, s.opts.Branch)
	case ahead == 0:
		// In step. Not a blocker, not news.
	default:
		st.Behind = true
	}
	return st
}

// notLocalBlocker words the refusal for a binary this checkout did not build.
// It is an ordinary state, not a fault, so it reads as a fact about the install
// rather than as something the operator got wrong.
func notLocalBlocker(origin BuildOrigin) string {
	if origin == OriginRelease {
		return "this agentique was installed from a release, so its updates come from the release channel"
	}
	return "this build does not record where it came from, so it is not treated as a local build"
}

// installedVersion asks the binary at the install path what it is. That is
// agentique running agentique, not a provider CLI — the invariant that forbids
// exec'ing claude or codex is about binaries another library owns, and this one
// is ours. It runs once an hour, not per request.
//
// Any failure returns false: a probe that could not see is not evidence of a
// staged binary.
func (s *SourceChecker) installedVersion(ctx context.Context) (string, bool) {
	if s.opts.InstallPath == "" || s.opts.Version == "" {
		return "", false
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, s.opts.InstallPath, "--version").Output()
	if err != nil {
		return "", false
	}
	// "agentique v0.6.0-67-g1ae969a"
	fields := strings.Fields(strings.TrimSpace(string(out)))
	if len(fields) < 2 {
		return "", false
	}
	return fields[len(fields)-1], true
}

// describesCommit reports whether a `git describe` version names this commit.
//
// The stamp is "<tag>-<n>-g<sha>", optionally "-dirty". A plain release tag
// carries no commit at all and can never match — which is the right answer: we
// cannot prove a release binary is the branch head, so we do not claim it.
//
// The two abbreviations need not be the same length (describe and rev-parse
// pick their own), so the shorter one prefixing the longer is the match.
func describesCommit(version, commit string) bool {
	if version == "" || commit == "" {
		return false
	}
	v := strings.TrimSuffix(version, "-dirty")
	i := strings.LastIndex(v, "-g")
	if i < 0 {
		return false
	}
	sha := v[i+2:]
	if sha == "" {
		return false
	}
	return strings.HasPrefix(sha, commit) || strings.HasPrefix(commit, sha)
}

// --- git plumbing ---
//
// Deliberately local rather than reaching for internal/gitops: that package is
// about a session's worktree and carries gh, PR and merge concerns this has no
// business importing. These four read-only calls are the whole surface.

// isGitRepo accepts a .git directory and a .git FILE alike — the latter is what
// a linked worktree has, and a checkout is no less real for being one.
func isGitRepo(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, ".git"))
	return err == nil
}

func gitLine(ctx context.Context, dir string, args ...string) (string, error) {
	out, err := gitOut(ctx, dir, args...)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

func gitCount(ctx context.Context, dir, spec string) (int, error) {
	out, err := gitLine(ctx, dir, "rev-list", "--count", spec)
	if err != nil {
		return 0, err
	}
	n, err := strconv.Atoi(out)
	if err != nil {
		return 0, fmt.Errorf("unparseable count %q: %w", out, err)
	}
	return n, nil
}

func gitDirty(ctx context.Context, dir string) (bool, error) {
	// --porcelain sees untracked files as well as modified tracked ones, which
	// is the behaviour we want: an untracked file can change what a build
	// produces just as surely as an edit can.
	out, err := gitOut(ctx, dir, "status", "--porcelain")
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(out) != "", nil
}

func gitOut(ctx context.Context, dir string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) && len(ee.Stderr) > 0 {
			return "", fmt.Errorf("git %s: %s", strings.Join(args, " "), strings.TrimSpace(string(ee.Stderr)))
		}
		return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return string(out), nil
}
