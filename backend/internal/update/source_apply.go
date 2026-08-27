package update

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"
)

// The source channel's half of the applier (docs/upgrades.md).
//
// Preflight is where this channel earns the right to act. The release channel
// proves its binary with a checksum against a document it downloaded; there is
// no equivalent here, because we compiled the thing ourselves. What stands in
// for it is a refusal: build only from a clean checkout sitting on the branch
// whose commit we reported. Then the binary that gets installed is, by
// construction, the one the status described.

var (
	// ErrNoSource means no checkout is configured ([update] source-dir), so the
	// channel is off on this machine.
	ErrNoSource = errors.New("no source checkout is configured on this machine")
	// ErrSourceNotReady means the checkout cannot be built from as it stands —
	// dirty, on another branch, or already in step.
	ErrSourceNotReady = errors.New("the source checkout is not in a state to build from")
	// ErrNothingStaged is a restart-only apply with nothing waiting at the
	// install path.
	ErrNothingStaged = errors.New("the installed binary is already the one running")
)

// buildIdleTimeout bounds how long a finished build waits for the machine to go
// idle before giving up. It mirrors the arm deadline's intent — an upgrade must
// not sit indefinitely holding a built binary — but is far shorter, because the
// operator clicked minutes ago and is watching.
const buildIdleTimeout = 30 * time.Minute

// buildIdlePoll is how often the post-build wait re-asks the turn registry.
const buildIdlePoll = 5 * time.Second

// SetSource attaches the checkout this applier may build from. Nil leaves the
// source channel off, which is the state on every machine that has not
// configured one.
func (a *Applier) SetSource(src *SourceChecker) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.source = src
}

func (a *Applier) sourceChecker() *SourceChecker {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.source
}

// PreflightSource proves a source build could run here and resolves what it
// would produce. Every refusal is a sentence a row can render.
func (a *Applier) PreflightSource() (*plan, error) {
	src := a.sourceChecker()
	if src == nil {
		return nil, ErrNoSource
	}
	st := src.Status()
	if st.CheckError != "" {
		return nil, fmt.Errorf("%w: %s", ErrSourceNotReady, st.CheckError)
	}
	if st.Dirty {
		return nil, fmt.Errorf("%w: the checkout has uncommitted changes", ErrSourceNotReady)
	}
	if st.CheckedOut != "" && st.CheckedOut != st.Branch {
		return nil, fmt.Errorf("%w: the checkout is on %s, not %s", ErrSourceNotReady, st.CheckedOut, st.Branch)
	}
	if !st.Behind {
		return nil, fmt.Errorf("%w: it is already in step with the running build", ErrSourceNotReady)
	}
	if err := checkBuildTools(nil); err != nil {
		return nil, err
	}
	binary, err := a.installTarget()
	if err != nil {
		return nil, err
	}
	return &plan{
		kind:      KindSource,
		target:    st.Head,
		binary:    binary,
		sourceDir: src.Dir(),
	}, nil
}

// PreflightRestart proves there is a binary at the install path that the
// running process is not. It installs nothing, so it needs no toolchain and no
// checkout — only somewhere to restart into.
func (a *Applier) PreflightRestart() (*plan, error) {
	src := a.sourceChecker()
	if src == nil {
		return nil, ErrNoSource
	}
	st := src.Status()
	if !st.Staged {
		return nil, ErrNothingStaged
	}
	binary, err := a.installTarget()
	if err != nil {
		return nil, err
	}
	return &plan{
		kind:   KindRestart,
		target: st.InstalledVersion,
		binary: binary,
	}, nil
}

// runSource compiles the checkout, waits for the machine to be idle, and then
// takes the shared tail.
func (a *Applier) runSource(ctx context.Context, p *plan) {
	tail := newLogTail(buildLogLines)

	// Throttle the log reports for the same reason the release path throttles
	// bytes: the tail exists so a stalled build is visible, not so every line
	// npm prints gets its own websocket frame.
	var lastEmit time.Time
	a.setPhase(func(pr *Progress) { pr.Phase = PhaseBuilding })
	err := runBuild(ctx, p.sourceDir, tail, func() {
		if time.Since(lastEmit) < 250*time.Millisecond {
			return
		}
		lastEmit = time.Now()
		a.setPhase(func(pr *Progress) {
			pr.Phase = PhaseBuilding
			pr.Log = tail.snapshot()
		})
	})
	// Land the tail on its final state either way: the throttle can otherwise
	// leave the last reported lines short of the ones that explain a failure.
	a.setPhase(func(pr *Progress) { pr.Log = tail.snapshot() })
	if err != nil {
		a.fail(err)
		return
	}

	built, err := builtBinary(p.sourceDir)
	if err != nil {
		a.fail(err)
		return
	}

	// A build takes minutes, so the gate's answer when this started is stale.
	// A turn that opened while the compiler ran is a real turn, and restarting
	// into it would end work that began AFTER the operator agreed to the cost.
	if err := a.waitForIdle(ctx, p); err != nil {
		a.fail(err)
		return
	}

	a.finish(p, built)
}

// waitForIdle holds a finished build until the machine goes idle. It is not the
// drain gate — that decides whether to start — but the same rule applied at the
// other end of a long operation.
//
// `force` skips it: an operator who already accepted the cost of ending turns
// should not be asked again by a different code path.
func (a *Applier) waitForIdle(ctx context.Context, p *plan) error {
	if p.force {
		return nil
	}
	if len(a.Busy()) == 0 {
		return nil
	}

	slog.Info("update: build finished while the machine was busy, holding for idle", "target", p.target)
	a.setPhase(func(pr *Progress) {
		pr.Phase = PhaseWaitingIdle
		pr.Cancellable = true
	})

	deadline := time.NewTimer(buildIdleTimeout)
	defer deadline.Stop()
	t := time.NewTicker(buildIdlePoll)
	defer t.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return fmt.Errorf("the build succeeded but the machine never went idle within %s, so nothing was installed — try again when the turns have finished", buildIdleTimeout)
		case <-t.C:
			if len(a.Busy()) == 0 {
				return nil
			}
		}
	}
}
