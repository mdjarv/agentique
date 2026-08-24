package update

import (
	"context"
	"log/slog"
	"sort"
	"sync"
	"time"

	"github.com/allbin/agentkit/runtime"
)

// CLIStatus is one provider CLI's account of itself on this machine: the binary
// agentique would spawn for the next session, how it got there, and what would
// update it.
//
// Every field comes from the provider's own library, through the connector that
// spawns sessions with it. agentique never resolves a binary and never runs a
// CLI to fill this in — see the ownership rule in docs/upgrades.md (C1, C13).
//
// Nothing here is a verdict. There is deliberately no "behind" and no published
// version: the pinned stack cannot compute one yet, and a field that says
// `behind: false` because nobody looked is worse than no field (C15).
//
// When a verdict does arrive it must be THREE-valued, not a bool. A published
// version is only comparable when the channel consulted is the one this install
// actually tracks and both versions parse; otherwise there is no verdict, which
// is not the same as being up to date. claudecli-go reports that as an explicit
// `Comparable` flag after finding its own API could manufacture ten patch
// versions of "behind" on an install sitting exactly on its channel's head.
// Whatever lands here must preserve the distinction rather than flatten it.
type CLIStatus struct {
	// Tool is the provider this describes ("claude", "codex").
	Tool string `json:"tool"`
	// Installed is the version the CLI reports for itself, "" when the probe
	// could not read one. A binary that will not answer is still worth showing.
	Installed string `json:"installed"`
	// Path is the binary as found; RealPath is it with symlinks resolved. Both
	// are shown because the interesting cases live in the difference — a
	// version-manager shim, or a native install's versioned target.
	Path     string `json:"path"`
	RealPath string `json:"realPath,omitempty"`
	// Method is how it was installed, as a label to display. Never branch on
	// it: the provider libraries define these values differently on purpose
	// (C14). SelfManaged and UpdateCmd are the fields that carry decisions.
	Method string `json:"method"`
	// Source names the evidence behind Method, so a row can say how sure it is.
	Source string `json:"source,omitempty"`
	// SelfManaged is the library's verdict that this install is one the tool
	// updates itself. False means show the command, do not offer to run it —
	// including where the tool's own updater would nominally work but would
	// delegate to a package manager whose environment this server does not
	// control.
	SelfManaged bool `json:"selfManaged"`
	// UpdateCmd is the command to show, "" when none is known to be correct.
	// Empty means "update manually"; it never means "use npm".
	UpdateCmd string `json:"updateCmd,omitempty"`
	// VersionManager is set even for an npm-global install hosted by one: such
	// an install only updates for the node version currently active, which is
	// worth telling the user.
	VersionManager string `json:"versionManager,omitempty"`
	// PackageManager names whatever owns the install — an OS package manager,
	// or which of npm/pnpm/bun owns a global tree, since their commands differ.
	PackageManager string `json:"packageManager,omitempty"`
	// Warnings are conditions worth surfacing that are not failures: a second
	// copy on PATH, or the CLI's own record disagreeing with what was detected.
	// Both mean a version number may have stopped describing the binary that
	// runs. Prose, meant to be shown as-is.
	Warnings []string `json:"warnings,omitempty"`
}

// defaultCLIProbeTimeout bounds one provider's detection. Detection is offline
// — a resolve, some small file reads, and one `--version` spawn — so this is a
// guard against a wedged binary, not a budget for normal work.
const defaultCLIProbeTimeout = 10 * time.Second

// CLIProbe holds what each provider connector last said about its own CLI, and
// refreshes it on a slow beat.
//
// It exists because the connector is the only thing that knows which binary it
// will spawn: it owns the client options, so a binary-path override lives
// there. A PATH lookup here would agree today by coincidence and drift apart
// silently the moment one exists.
//
// Status never performs IO, exactly like Checker: a probe that fails leaves the
// previous answer standing rather than blanking a row that was correct a minute
// ago.
type CLIProbe struct {
	inspectors map[string]runtime.InstallInspectable
	interval   time.Duration
	timeout    time.Duration

	mu        sync.RWMutex
	cached    []CLIStatus
	checkedAt time.Time

	done     chan struct{}
	stopOnce sync.Once
	wg       sync.WaitGroup
}

// NewCLIProbe builds a probe over the connectors that can answer. Connectors
// that do not implement runtime.InstallInspectable are simply absent — not
// implementing it is not an error. Performs no IO; the poll loop starts from
// serve.go's production block, never from a constructor a test might call.
func NewCLIProbe(inspectors map[string]runtime.InstallInspectable, interval time.Duration) *CLIProbe {
	if interval <= 0 {
		interval = time.Hour
	}
	return &CLIProbe{
		inspectors: inspectors,
		interval:   interval,
		timeout:    defaultCLIProbeTimeout,
		done:       make(chan struct{}),
	}
}

// Start probes once and then re-probes on interval until Stop.
func (p *CLIProbe) Start(ctx context.Context) {
	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		p.Refresh(ctx)
		t := time.NewTicker(p.interval)
		defer t.Stop()
		for {
			select {
			case <-p.done:
				return
			case <-ctx.Done():
				return
			case <-t.C:
				p.Refresh(ctx)
			}
		}
	}()
}

// Stop halts the poll loop and waits for an in-flight probe to park.
func (p *CLIProbe) Stop() {
	p.stopOnce.Do(func() { close(p.done) })
	p.wg.Wait()
}

// Status returns the cached answer. Never blocks on a probe.
func (p *CLIProbe) Status() []CLIStatus {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.cached
}

// Refresh asks every connector and replaces the cache. Each provider gets its
// own deadline so one wedged binary cannot stall the others, and a provider
// that errors is left out entirely rather than rendered as a broken row: a
// machine without codex installed is a normal state, not a problem to solve.
func (p *CLIProbe) Refresh(ctx context.Context) []CLIStatus {
	out := make([]CLIStatus, 0, len(p.inspectors))
	for _, provider := range sortedProviders(p.inspectors) {
		st, ok := p.probeOne(ctx, provider, p.inspectors[provider])
		if !ok {
			continue
		}
		out = append(out, st)
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	p.cached = out
	p.checkedAt = time.Now().UTC()
	return out
}

func (p *CLIProbe) probeOne(ctx context.Context, provider string, in runtime.InstallInspectable) (CLIStatus, bool) {
	probeCtx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()

	info, err := in.InstallInfo(probeCtx)
	if err != nil {
		// Not installed and could-not-detect are indistinguishable here: the
		// neutral contract has no sentinel for "no CLI on PATH", and matching a
		// provider library's own error would mean importing it, which the
		// ownership rule forbids. Both collapse to "no row", which reads
		// correctly for the common case (a machine that simply lacks that CLI).
		slog.Debug("update: cli detection unavailable", "provider", provider, "error", err)
		return CLIStatus{}, false
	}
	if info == nil {
		return CLIStatus{}, false
	}

	tool := info.Tool
	if tool == "" {
		tool = provider
	}
	return CLIStatus{
		Tool:           tool,
		Installed:      info.Version,
		Path:           info.Path,
		RealPath:       info.RealPath,
		Method:         info.Method,
		Source:         info.Source,
		SelfManaged:    info.SelfManaged,
		UpdateCmd:      info.UpdateCmd,
		VersionManager: info.VersionManager,
		PackageManager: info.PackageManager,
		Warnings:       info.Warnings,
	}, true
}

// sortedProviders keeps row order stable across refreshes. Map iteration order
// would reshuffle the dialog's rows on every poll for no reason.
func sortedProviders(m map[string]runtime.InstallInspectable) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
