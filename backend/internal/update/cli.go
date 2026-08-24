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
	// AutoUpdate is the CLI's own auto-update state, absent when it does not
	// report one. It is what makes "updates itself" honest: a self-managed
	// install with auto-updates switched off does NOT update itself, and saying
	// it does would be the most reassuring possible way to be wrong.
	AutoUpdate *CLIAutoUpdate `json:"autoUpdate,omitempty"`
	// LastRan is the version this CLI reported when a session on this machine
	// actually started, "" until one has. Every other field here is inspection;
	// this is the only observation, which makes it the one check on detection —
	// if it disagrees with Installed, detection is describing a binary the
	// product does not run. Empty after a restart, which is honest: nothing has
	// been observed yet.
	LastRan string `json:"lastRan,omitempty"`
}

// CLIAutoUpdate is what a CLI says about keeping itself current. Reported, not
// interpreted: the row says what the tool believes, and lets the user decide
// whether that matches what they wanted.
type CLIAutoUpdate struct {
	// Enabled is the tool's own answer. False with a DisabledBy of
	// "package-manager" is a normal, correct state — a brew or npm install is
	// not supposed to self-update behind its package manager.
	Enabled bool `json:"enabled"`
	// DisabledBy names what turned it off ("config", "env", "package-manager",
	// "unknown"), "" when it is on.
	DisabledBy string `json:"disabledBy,omitempty"`
	// Channel is the release stream it follows ("latest", "stable"). It matters
	// because channels disagree: comparing an install against the wrong one
	// manufactures a "behind" that is not true.
	Channel string `json:"channel,omitempty"`
	// LastOutcome, LastTo and LastAt describe the most recent attempt the tool
	// made on its own, "" when it has never recorded one. This is the tool's
	// account of its own background work, NOT a record that an update happened:
	// an update driven through the library leaves it untouched.
	LastOutcome string `json:"lastOutcome,omitempty"`
	LastTo      string `json:"lastTo,omitempty"`
	LastAt      string `json:"lastAt,omitempty"`
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
	// lastRan is what each provider's CLI reported when a session actually
	// started, keyed by provider. Kept beside the cache rather than inside it
	// because the two are refreshed by completely different events: detection
	// on a timer, this on a session starting.
	lastRan map[string]string

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
		lastRan:    map[string]string{},
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

// RecordRan notes the version a provider CLI reported when a session started.
// Wired to the session manager, so it fires on every session init — including a
// resume that lands on a CLI which updated underneath us.
func (p *CLIProbe) RecordRan(provider, version string) {
	if provider == "" || version == "" {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.lastRan[provider] = version
}

// Status returns the cached answer, with each row's observed version folded in.
// Never blocks on a probe.
//
// The fold happens on read rather than at refresh time because the two facts
// arrive independently: a session can start between two hourly probes, and that
// observation should not have to wait an hour to be visible.
func (p *CLIProbe) Status() []CLIStatus {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if len(p.lastRan) == 0 {
		return p.cached
	}
	out := make([]CLIStatus, len(p.cached))
	copy(out, p.cached)
	for i := range out {
		out[i].LastRan = p.lastRan[out[i].Tool]
	}
	return out
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
		AutoUpdate:     autoUpdate(info.AutoUpdate),
	}, true
}

// autoUpdate maps the provider's auto-update report onto the wire shape. A
// provider that reports nothing yields nil rather than a zero-valued struct:
// "Enabled: false" and "did not say" are different claims, and only one of them
// is safe to show a user.
func autoUpdate(in *runtime.AutoUpdate) *CLIAutoUpdate {
	if in == nil {
		return nil
	}
	out := &CLIAutoUpdate{
		Enabled:    in.Enabled,
		DisabledBy: in.DisabledBy,
		Channel:    in.Channel,
	}
	if in.LastAttempt != nil {
		out.LastOutcome = in.LastAttempt.Outcome
		out.LastTo = in.LastAttempt.To
		if !in.LastAttempt.Time.IsZero() {
			out.LastAt = in.LastAttempt.Time.UTC().Format(time.RFC3339)
		}
	}
	return out
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
