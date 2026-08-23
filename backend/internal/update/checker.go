package update

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"runtime"
	"sync"
	"time"
)

// Status is what one server says about its own version. Every field is about
// THIS machine: only it knows its platform, its install method and whether it
// is busy (decision U1). Clients do no version arithmetic beyond comparing
// strings they were handed.
type Status struct {
	// Current is main.version, exactly as stamped by the build.
	Current string `json:"current"`
	// Latest is the newest published tag; "" until a check has succeeded.
	Latest string `json:"latest"`
	// Behind is true only for a release build with a strictly newer tag
	// published. A dev build is never behind.
	Behind bool `json:"behind"`
	// Channel is "release" or "dev".
	Channel string `json:"channel"`
	// Asset is the release asset for this platform, "" when we publish none.
	Asset string `json:"asset"`
	// Supported reports that in-app apply is enabled here: a verified platform
	// with its asset actually published. False means "manual upgrade".
	Supported bool `json:"supported"`
	// Platform is "GOOS/GOARCH", so a row can explain itself.
	Platform string `json:"platform"`
	// CheckedAt is when the last check completed (RFC3339 UTC), "" if none has.
	// It is stamped on success AND on failure, so the UI can age the answer.
	CheckedAt string `json:"checkedAt"`
	// CheckError is the last check's failure, "" when it succeeded. The cached
	// answer stands either way — a version check never blocks the UI.
	CheckError string `json:"checkError,omitempty"`
	// ReleaseURL links the release page. Notes are auto-generated from commits
	// and are often noise, so the link is the primary affordance.
	ReleaseURL string `json:"releaseUrl,omitempty"`
	// Notes are the release body, truncated.
	Notes string `json:"notes,omitempty"`
}

// notesLimit bounds the release body we put on the wire. Auto-generated notes
// grow with the commit count and nobody reads 40 kB of them in a dialog.
const notesLimit = 4000

// Options configures a Checker. Zero values take the documented defaults.
type Options struct {
	// Version is main.version, the build being checked. Required.
	Version string
	// APIURL overrides the GitHub releases endpoint ([update] api-url /
	// AGENTIQUE_UPDATE_API_URL) — a fork's repo, or a stub in a test.
	APIURL string
	// Interval is the background poll period (default 1h).
	Interval time.Duration
	// MinRefreshInterval coalesces on-demand refreshes (default 30s), so a
	// dialog opened five times in a row costs one request.
	MinRefreshInterval time.Duration
	// Client is the HTTP client (default: 15s timeout).
	Client *http.Client
	// GOOS/GOARCH override the platform, for tests.
	GOOS, GOARCH string
}

func (o Options) withDefaults() Options {
	if o.APIURL == "" {
		o.APIURL = DefaultAPIURL
	}
	if o.Interval <= 0 {
		o.Interval = time.Hour
	}
	if o.MinRefreshInterval <= 0 {
		o.MinRefreshInterval = 30 * time.Second
	}
	if o.Client == nil {
		o.Client = &http.Client{Timeout: 15 * time.Second}
	}
	if o.GOOS == "" {
		o.GOOS = runtime.GOOS
	}
	if o.GOARCH == "" {
		o.GOARCH = runtime.GOARCH
	}
	return o
}

// Checker holds the last answer GitHub gave and refreshes it on a slow beat.
// Status never performs IO: a check that fails keeps the previous answer and
// its age, so the UI degrades to "as of an hour ago" rather than to nothing.
type Checker struct {
	opts Options

	// checkMu serializes network checks so a burst of refreshes is one request.
	checkMu sync.Mutex

	mu        sync.RWMutex
	rel       *Release
	etag      string
	checkedAt time.Time
	lastErr   error

	done     chan struct{}
	stopOnce sync.Once
	wg       sync.WaitGroup
}

// NewChecker builds a Checker. It performs no IO — the poll loop starts from
// serve.go's production block, never from a constructor a test might call.
func NewChecker(opts Options) *Checker {
	return &Checker{opts: opts.withDefaults(), done: make(chan struct{})}
}

// Start runs an immediate check and then polls on Interval until Stop.
func (c *Checker) Start(ctx context.Context) {
	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		c.check(ctx)
		t := time.NewTicker(c.opts.Interval)
		defer t.Stop()
		for {
			select {
			case <-c.done:
				return
			case <-ctx.Done():
				return
			case <-t.C:
				c.check(ctx)
			}
		}
	}()
}

// Stop halts the poll loop and waits for an in-flight check to park.
func (c *Checker) Stop() {
	c.stopOnce.Do(func() { close(c.done) })
	c.wg.Wait()
}

// Status returns the current answer from cache. Never blocks on the network.
func (c *Checker) Status() Status {
	c.mu.RLock()
	rel, checkedAt, lastErr := c.rel, c.checkedAt, c.lastErr
	c.mu.RUnlock()
	return c.statusFrom(rel, checkedAt, lastErr)
}

// Refresh forces a check unless one just ran, then returns the result. Used by
// the explicit "check now" affordance; the hourly loop calls check directly.
func (c *Checker) Refresh(ctx context.Context) Status {
	c.mu.RLock()
	age := time.Since(c.checkedAt)
	fresh := !c.checkedAt.IsZero() && age < c.opts.MinRefreshInterval
	c.mu.RUnlock()
	if fresh {
		return c.Status()
	}
	c.check(ctx)
	return c.Status()
}

// LatestRelease returns the cached release, if a check has ever succeeded.
// The apply path (V3) resolves its download and checksum URLs from it.
func (c *Checker) LatestRelease() (*Release, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.rel, c.rel != nil
}

// Version is the build this checker speaks for.
func (c *Checker) Version() string { return c.opts.Version }

// Platform is the checker's GOOS/GOARCH pair.
func (c *Checker) Platform() (string, string) { return c.opts.GOOS, c.opts.GOARCH }

// check performs one network check and folds the result into the cache. A
// failure records the error and the time but keeps the previous release.
func (c *Checker) check(ctx context.Context) {
	c.checkMu.Lock()
	defer c.checkMu.Unlock()

	c.mu.RLock()
	etag := c.etag
	c.mu.RUnlock()

	rel, newETag, err := fetchRelease(ctx, c.opts.Client, c.opts.APIURL, etag)
	now := time.Now().UTC()

	c.mu.Lock()
	defer c.mu.Unlock()
	c.checkedAt = now
	switch {
	case errors.Is(err, errNotModified):
		// The cached release is still the newest one — the whole point of the
		// ETag. Not an error, and not a change.
		c.lastErr = nil
	case err != nil:
		c.lastErr = err
		slog.Debug("update check failed", "error", err, "url", c.opts.APIURL)
	default:
		c.rel = rel
		c.etag = newETag
		c.lastErr = nil
	}
}

func (c *Checker) statusFrom(rel *Release, checkedAt time.Time, lastErr error) Status {
	asset := AssetName(c.opts.GOOS, c.opts.GOARCH)
	st := Status{
		Current:  c.opts.Version,
		Channel:  Channel(c.opts.Version),
		Asset:    asset,
		Platform: c.opts.GOOS + "/" + c.opts.GOARCH,
	}
	if !checkedAt.IsZero() {
		st.CheckedAt = checkedAt.Format(time.RFC3339)
	}
	if lastErr != nil {
		st.CheckError = lastErr.Error()
	}
	if rel == nil {
		return st
	}

	st.Latest = rel.TagName
	st.ReleaseURL = rel.HTMLURL
	st.Notes = truncate(rel.Body, notesLimit)
	// A dev build is never behind, however old its describe base looks.
	st.Behind = st.Channel == ChannelRelease && CompareVersions(st.Current, st.Latest) < 0
	// Never offer a button that cannot work: apply needs a verified platform
	// AND the asset actually present on the release.
	st.Supported = asset != "" && Verified(c.opts.GOOS, c.opts.GOARCH) && rel.Find(asset) != nil
	return st
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
