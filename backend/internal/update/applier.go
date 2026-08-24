package update

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// ProgressTopic is the WS global-topic key upgrade progress is published on.
const ProgressTopic = "update.progress"

var (
	// ErrNotSupported means this platform is published but not verified for
	// in-app apply, or has no asset at all.
	ErrNotSupported = errors.New("in-app upgrade is not enabled on this platform")
	// ErrNoRelease means no successful version check has happened yet.
	ErrNoRelease = errors.New("no release information yet — check for updates first")
	// ErrAlreadyRunning guards against a second apply.
	ErrAlreadyRunning = errors.New("an upgrade is already in progress")
	// ErrNotRunning is a cancel with nothing to cancel.
	ErrNotRunning = errors.New("no upgrade in progress")
	// ErrTooLate is a cancel that arrived past the point of no return.
	ErrTooLate = errors.New("too late to cancel — the new binary is being installed")
	// ErrBusy is the V3 refusal: a turn is running, and a restart is not a
	// pause. V4 replaces this with the drain gate.
	ErrBusy = errors.New("a turn is running on this machine")
	// ErrStale means the caller asked to install a version that is no longer
	// the published one — the client's picture is older than the server's.
	ErrStale = errors.New("that version is no longer the latest release")
)

// Deps are the pieces the applier needs but must not own: they are injected so
// the whole flow can be exercised against a throwaway install dir with no
// service manager in sight.
type Deps struct {
	// BinaryPath returns the file to replace — the running executable with
	// symlinks resolved.
	BinaryPath func() (string, error)
	// Restart hands over to the service manager. Called last; the process
	// making the call is the one being replaced.
	Restart func() error
	// ServiceInstalled reports whether there is a service to restart. Without
	// one, nothing would bring the new binary up.
	ServiceInstalled func() bool
	// BusyTurns returns the sessions running a turn right now.
	BusyTurns func() []string
	// Publish emits a WS global-topic event. Optional.
	Publish func(string, any)
	// MachineID stamps progress events so a client fanning in from several
	// machines knows which one is talking.
	MachineID string
	// Client downloads assets. Defaults to a 10-minute-timeout client — a
	// 33 MB binary over a bad link is slow, not broken.
	Client *http.Client
	// ArmDeadline bounds how long an armed upgrade waits for idle before it
	// gives up and says so. 0 takes DefaultArmDeadline.
	ArmDeadline time.Duration
}

// Applier performs the upgrade: download, verify, replace, restart. One at a
// time, cancellable up to the point where something is actually installed.
type Applier struct {
	checker *Checker
	deps    Deps

	mu       sync.Mutex
	progress *Progress
	cancel   context.CancelFunc
	// cancelRequested is read under mu by commit, so a cancel and the point of
	// no return contend on one lock: whichever takes it first decides, and a
	// cancel can never be accepted and then silently ignored.
	cancelRequested bool
	// arm is the drain gate's one-shot (gate.go). In-memory only, deliberately:
	// a restart for any other reason must forget it.
	arm *armState

	// The gate's backstop ticker, kept off the main mutex so a fire attempt
	// can stop it without deadlocking on itself.
	watchMu   sync.Mutex
	watchStop chan struct{}
	watchWG   sync.WaitGroup
}

func NewApplier(checker *Checker, deps Deps) *Applier {
	if deps.Client == nil {
		deps.Client = &http.Client{Timeout: 10 * time.Minute}
	}
	return &Applier{checker: checker, deps: deps}
}

// Progress returns the live upgrade state, or nil when none is running. Held
// as state so a reload mid-upgrade — or a second client — still sees it.
func (a *Applier) Progress() *Progress {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.progress == nil {
		return nil
	}
	p := *a.progress
	return &p
}

// plan is everything resolved before anything is touched.
type plan struct {
	target      string
	assetName   string
	assetURL    string
	checksumURL string
	binary      string
	size        int64
}

// Preflight resolves the upgrade and proves it could run: verified platform,
// published asset, a writable install dir, and a service to restart. Never
// offer a button that cannot work — so this runs before the row offers one.
func (a *Applier) Preflight() (*plan, error) {
	goos, goarch := a.checker.Platform()
	asset := AssetName(goos, goarch)
	if asset == "" || !Verified(goos, goarch) {
		return nil, fmt.Errorf("%w (%s/%s)", ErrNotSupported, goos, goarch)
	}
	rel, ok := a.checker.LatestRelease()
	if !ok {
		return nil, ErrNoRelease
	}
	bin := rel.Find(asset)
	sums := rel.Find(ChecksumsAsset)
	if bin == nil {
		return nil, fmt.Errorf("release %s publishes no %s", rel.TagName, asset)
	}
	if sums == nil {
		// Checksum before replace, always — a release without one cannot be
		// installed by us at all.
		return nil, fmt.Errorf("release %s publishes no %s", rel.TagName, ChecksumsAsset)
	}

	// The asset and checksum URLs come out of the release document, so they are
	// only as trustworthy as the transport that carried them. Require HTTPS for
	// both: a release naming http:// URLs would let one network position swap
	// the binary AND the digest it is checked against, consistently.
	if err := requireHTTPS(bin.URL); err != nil {
		return nil, fmt.Errorf("release asset %s: %w", asset, err)
	}
	if err := requireHTTPS(sums.URL); err != nil {
		return nil, fmt.Errorf("release %s: %w", ChecksumsAsset, err)
	}

	binary, err := a.deps.BinaryPath()
	if err != nil {
		return nil, fmt.Errorf("resolve install path: %w", err)
	}
	if err := writableDir(filepath.Dir(binary)); err != nil {
		return nil, fmt.Errorf("install directory %s is not writable: %w", filepath.Dir(binary), err)
	}
	if a.deps.ServiceInstalled != nil && !a.deps.ServiceInstalled() {
		return nil, errors.New("no agentique service is installed, so nothing would restart the new binary")
	}

	return &plan{
		target:      rel.TagName,
		assetName:   asset,
		assetURL:    bin.URL,
		checksumURL: sums.URL,
		binary:      binary,
		size:        bin.Size,
	}, nil
}

// ErrInsecureAsset rejects a release asset that is not served over TLS.
var ErrInsecureAsset = errors.New("release asset must be served over https")

// requireHTTPS mirrors the rule the machine catalog already applies to remote
// origins (internal/machine/url.go): TLS on the network, plain HTTP only over
// loopback, where there is no network position to attack from.
func requireHTTPS(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return fmt.Errorf("%w: unparseable url", ErrInsecureAsset)
	}
	if strings.EqualFold(u.Scheme, "https") {
		return nil
	}
	if strings.EqualFold(u.Scheme, "http") && isLoopbackHost(u.Hostname()) {
		return nil
	}
	return fmt.Errorf("%w: got %q", ErrInsecureAsset, u.Scheme)
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// Busy reports the sessions with a turn in flight. A restart is not a pause:
// the new process reaps orphaned CLI process groups on startup, so restarting
// mid-turn ends that turn.
func (a *Applier) Busy() []string {
	if a.deps.BusyTurns == nil {
		return nil
	}
	return a.deps.BusyTurns()
}

// Start begins an upgrade to `expect` and returns as soon as it is under way —
// the caller replies 202 and the narration continues over the WS topic.
//
// `expect` guards against a client acting on a staler picture than ours;
// empty means "whatever is latest". `force` overrides the busy refusal, and
// costs the running turns.
func (a *Applier) Start(expect string, force bool) error {
	p, err := a.Preflight()
	if err != nil {
		return err
	}
	if expect != "" && expect != p.target {
		return fmt.Errorf("%w: asked for %s, latest is %s", ErrStale, expect, p.target)
	}
	if busy := a.Busy(); len(busy) > 0 && !force {
		return fmt.Errorf("%w (%d)", ErrBusy, len(busy))
	}

	a.mu.Lock()
	if a.progress != nil && !a.progress.Phase.Terminal() {
		a.mu.Unlock()
		return ErrAlreadyRunning
	}
	ctx, cancel := context.WithCancel(context.Background())
	a.cancel = cancel
	a.cancelRequested = false
	now := nowStamp()
	a.progress = &Progress{
		MachineID:   a.deps.MachineID,
		Phase:       PhaseQueued,
		Target:      p.target,
		From:        a.checker.Version(),
		Total:       p.size,
		Cancellable: true,
		StartedAt:   now,
		UpdatedAt:   now,
	}
	started := *a.progress
	a.mu.Unlock()

	a.emit(started)
	go a.run(ctx, p)
	return nil
}

// Cancel stops an upgrade that has not yet installed anything.
func (a *Applier) Cancel() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.progress == nil || a.progress.Phase.Terminal() {
		return ErrNotRunning
	}
	if !a.progress.Phase.Cancellable() {
		return ErrTooLate
	}
	a.cancelRequested = true
	if a.cancel != nil {
		a.cancel()
	}
	return nil
}

// commit takes the point of no return under the same lock Cancel uses. Returns
// false when a cancel got there first — the caller then aborts and deletes the
// temp file. Without this, a cancel accepted a microsecond before `replacing`
// would be reported as successful and then ignored.
func (a *Applier) commit() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.progress == nil || a.cancelRequested {
		return false
	}
	a.progress.Phase = PhaseReplacing
	a.progress.Cancellable = false
	a.progress.UpdatedAt = nowStamp()
	snapshot := *a.progress
	// Emitted while holding the lock on purpose: the phase that closes the
	// cancel window must be published before anything else can change it.
	a.emit(snapshot)
	return true
}

// run is the whole upgrade, off the request goroutine.
func (a *Applier) run(ctx context.Context, p *plan) {
	dir := filepath.Dir(p.binary)

	a.setPhase(func(pr *Progress) { pr.Phase = PhaseDownloading })
	checksums, err := fetchChecksums(ctx, a.deps.Client, p.checksumURL)
	if err != nil {
		a.fail(err)
		return
	}

	// Throttle the byte reports: the counter exists so a stalled download is
	// visible, not so every 128 kB gets its own websocket frame.
	var lastEmit time.Time
	tmp, digest, err := downloadTo(ctx, a.deps.Client, p.assetURL, dir, func(done, total int64) {
		if time.Since(lastEmit) < 250*time.Millisecond {
			return
		}
		lastEmit = time.Now()
		a.setPhase(func(pr *Progress) {
			pr.Phase = PhaseDownloading
			pr.Downloaded = done
			if total > 0 {
				pr.Total = total
			}
		})
	})
	if err != nil {
		a.fail(err)
		return
	}

	// Land the counter on the real total before moving on — the throttle can
	// otherwise leave the last reported figure a fraction of the file, so a
	// download that finished reads as one that stalled.
	a.setPhase(func(pr *Progress) {
		pr.Phase = PhaseDownloading
		if pr.Total > 0 {
			pr.Downloaded = pr.Total
		}
	})
	a.setPhase(func(pr *Progress) { pr.Phase = PhaseVerifying })
	if err := verifyDigest(checksums, p.assetName, digest); err != nil {
		a.fail(errors.Join(err, os.Remove(tmp)))
		return
	}

	// The point of no return, taken under the cancel lock.
	if !a.commit() {
		a.fail(errors.Join(context.Canceled, os.Remove(tmp)))
		return
	}
	if err := installOver(tmp, p.binary); err != nil {
		a.fail(err)
		return
	}
	slog.Info("update: installed", "target", p.target, "binary", p.binary, "previous", p.binary+PrevSuffix)

	// Nobody is left to report after this: the process making the call is the
	// process being replaced. The client treats the drop as expected and
	// confirms by re-reading the version.
	a.setPhase(func(pr *Progress) { pr.Phase = PhaseRestarting; pr.Cancellable = false })
	if a.deps.Restart == nil {
		return
	}
	if err := a.deps.Restart(); err != nil {
		// The binary IS installed — this is not a failed upgrade, it is an
		// upgrade waiting for a restart. Say exactly that.
		slog.Error("update: installed but restart failed", "error", err)
		a.setPhase(func(pr *Progress) {
			pr.Phase = PhaseFailed
			pr.Error = fmt.Sprintf("installed %s, but the restart failed: %v — restart agentique to finish", p.target, err)
		})
	}
}

// fail records a terminal outcome, distinguishing a cancel from a real error.
func (a *Applier) fail(err error) {
	a.mu.Lock()
	cancelled := a.cancelRequested || errors.Is(err, context.Canceled)
	a.mu.Unlock()
	a.setPhase(func(pr *Progress) {
		if cancelled {
			pr.Phase = PhaseCancelled
			pr.Error = ""
			return
		}
		pr.Phase = PhaseFailed
		pr.Error = err.Error()
	})
	if cancelled {
		slog.Info("update: cancelled")
		return
	}
	slog.Error("update: failed", "error", err)
}

// setPhase mutates the live progress and publishes it — state and event
// together, always in that order, so a client that polls right after an event
// never sees the older answer.
func (a *Applier) setPhase(mutate func(*Progress)) {
	a.mu.Lock()
	if a.progress == nil {
		a.mu.Unlock()
		return
	}
	mutate(a.progress)
	a.progress.Cancellable = a.progress.Cancellable && a.progress.Phase.Cancellable()
	a.progress.UpdatedAt = nowStamp()
	snapshot := *a.progress
	a.mu.Unlock()
	a.emit(snapshot)
}

func (a *Applier) emit(p Progress) {
	if a.deps.Publish != nil {
		a.deps.Publish(ProgressTopic, p)
	}
}
