package update

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Building the source channel's binary (docs/upgrades.md).
//
// The release channel downloads a binary someone else built and proves it with
// a checksum. The source channel compiles one here, so there is nothing to
// check a digest against — the evidence that it is the right binary is that we
// refused to build unless the checkout was clean and on the target branch.
//
// What replaces the byte counter is the log tail. A download has a total to
// count against; a build does not, and "is it hung?" is asked just as often —
// usually while npm is doing something silent for ninety seconds.

// buildLogLines is how much of the build's output rides the progress event. Big
// enough to show what step is running and what an error said, small enough that
// a websocket frame every 250ms stays cheap.
const buildLogLines = 12

// BuildRecipe is the command the source channel runs to produce a binary.
//
// It is `just build`, not `just install`: the install recipe also rewrites the
// systemd unit and regenerates completions. Rewriting the unit from INSIDE the
// service re-bakes Environment=PATH from the service's own environment, which
// narrows PATH a little more on every upgrade. The swap is ours to perform
// (installOver), and it is already the tested one.
var BuildRecipe = []string{"just", "build"}

// buildArtifact is where the recipe leaves the binary, relative to the checkout.
const buildArtifact = "agentique"

// buildTools are resolved before the button is ever offered. Never offer a
// button that cannot work — and on a service whose PATH was captured at install
// time, "npm is not on PATH" is a live failure mode rather than a hypothetical
// one. See docs/upgrades.md.
var buildTools = []string{"just", "go", "git", "node", "npm"}

// ErrToolMissing means the build's toolchain is not resolvable from the
// server's own PATH — which is not the PATH of the shell you tested in.
var ErrToolMissing = errors.New("build tool not found on the server's PATH")

// checkBuildTools resolves every tool the recipe needs, naming the first one
// missing. It looks the way the build will look: from this process's PATH, not
// a login shell's.
func checkBuildTools(lookPath func(string) (string, error)) error {
	if lookPath == nil {
		lookPath = exec.LookPath
	}
	for _, tool := range buildTools {
		if _, err := lookPath(tool); err != nil {
			return fmt.Errorf("%w: %s", ErrToolMissing, tool)
		}
	}
	return nil
}

// logTail keeps the last N lines of a build's output, safe to read from the
// progress goroutine while the build writes to it.
type logTail struct {
	mu    sync.Mutex
	lines []string
	n     int
}

func newLogTail(n int) *logTail { return &logTail{n: n} }

func (l *logTail) add(line string) {
	line = strings.TrimRight(line, "\r\n")
	if strings.TrimSpace(line) == "" {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.lines = append(l.lines, line)
	if len(l.lines) > l.n {
		l.lines = l.lines[len(l.lines)-l.n:]
	}
}

func (l *logTail) snapshot() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.lines) == 0 {
		return nil
	}
	out := make([]string, len(l.lines))
	copy(out, l.lines)
	return out
}

// last returns the final line, for the one-line summary a failure needs.
func (l *logTail) last() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.lines) == 0 {
		return ""
	}
	return l.lines[len(l.lines)-1]
}

// runBuild executes the recipe in dir, streaming its output into tail and
// calling onLine as each line lands (throttled by the caller).
//
// Cancellation kills the process and gives it a moment to take its children
// with it. Under systemd the whole unit shares a cgroup with KillMode=
// control-group, so a stray npm cannot outlive the server in any case; the
// WaitDelay is what stops a cancelled build holding this goroutine open.
func runBuild(ctx context.Context, dir string, tail *logTail, onLine func()) error {
	if len(BuildRecipe) == 0 {
		return errors.New("no build recipe configured")
	}
	cmd := exec.CommandContext(ctx, BuildRecipe[0], BuildRecipe[1:]...) //nolint:gosec // fixed recipe, not user input
	cmd.Dir = dir
	// A build inherits the server's environment deliberately: if the answer
	// differs from what the service itself can see, the preflight lied.
	cmd.Env = os.Environ()
	cmd.WaitDelay = 5 * time.Second

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("build stdout: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("build stderr: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start %s: %w", strings.Join(BuildRecipe, " "), err)
	}

	var wg sync.WaitGroup
	wg.Add(2)
	scan := func(r io.Reader) {
		defer wg.Done()
		sc := bufio.NewScanner(r)
		// A go build error or an npm stack trace can exceed the default 64 kB
		// line budget; a build that "failed to scan" reports nothing useful.
		sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for sc.Scan() {
			tail.add(sc.Text())
			if onLine != nil {
				onLine()
			}
		}
	}
	go scan(stdout)
	go scan(stderr)
	wg.Wait()

	if err := cmd.Wait(); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		// The exit code alone tells the reader nothing. The last line usually
		// tells them everything.
		if line := tail.last(); line != "" {
			return fmt.Errorf("%s failed: %w — %s", strings.Join(BuildRecipe, " "), err, line)
		}
		return fmt.Errorf("%s failed: %w", strings.Join(BuildRecipe, " "), err)
	}
	return nil
}

// builtBinary is the artifact the recipe leaves behind, proven to exist and to
// be executable. A recipe that exits 0 and produces nothing is a failure we
// must not install over the running binary.
func builtBinary(dir string) (string, error) {
	path := filepath.Join(dir, buildArtifact)
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("the build left no %s in %s: %w", buildArtifact, dir, err)
	}
	if info.IsDir() || info.Size() == 0 {
		return "", fmt.Errorf("%s is not a binary", path)
	}
	return path, nil
}
