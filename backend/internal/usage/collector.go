package usage

import (
	"context"
	"log/slog"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/allbin/agentkit/runtime"
)

// The collector: one probe shared by every client.
//
// Polling server-side rather than per-browser means the reuse window is real —
// five tabs cost one request, and a closed browser does not stop the numbers
// being current. It follows the precedent the update checker set: constructed
// here, started from serve.go's production block, never from a constructor a
// test might call.

// DefaultInterval is the background probe period.
const DefaultInterval = 5 * time.Minute

// reuseWindow absorbs incidental reads. A user flicking the popover open and
// shut must not become one API request per flick. An explicit refresh bypasses
// it entirely — the interval exists to absorb accidents, not to overrule
// somebody who pressed the button.
const reuseWindow = 15 * time.Second

// Options configures a Collector.
type Options struct {
	Claude ClaudeOptions
	// Accounts are the provider connectors that can answer for their own
	// subscription, keyed by provider id. agentkit's AccountInspectable hangs
	// off CLIConnector, so a provider whose connector implements it needs no
	// collector of its own — which is why this is a map rather than a field per
	// vendor.
	Accounts map[string]runtime.AccountInspectable
	// Disk supplies the storage gauge. Nil omits it.
	Disk func() (DiskReading, bool)
	// Today reports what this server itself spent today, per provider.
	// Nil omits those figures.
	Today func(ctx context.Context) (map[string]Today, error)
	// Interval is the background probe period; 0 takes DefaultInterval.
	Interval time.Duration
	// Now is injected in tests.
	Now func() time.Time
}

// Today is one provider's spend through this server, since local midnight.
type Today struct {
	Tokens  int64
	Prompts int
}

// DiskReading is the storage gauge's input. UsedPercent comes from the storage
// package rather than being recomputed here, so the footer and the Storage page
// cannot disagree — that one is df-style (used / (used + available)), which
// excludes root-reserved blocks.
type DiskReading struct {
	UsedPercent float64
	FreeBytes   int64
}

func (o Options) withDefaults() Options {
	if o.Interval <= 0 {
		o.Interval = DefaultInterval
	}
	if o.Now == nil {
		o.Now = func() time.Time { return time.Now().UTC() }
	}
	return o
}

// Collector holds the last good answer and refreshes it on a slow beat.
type Collector struct {
	opts Options

	// probeMu serializes probes so a burst of refreshes is one request.
	probeMu sync.Mutex

	mu        sync.RWMutex
	agents    map[string]Agent
	fetchedAt time.Time

	done     chan struct{}
	stopOnce sync.Once
	wg       sync.WaitGroup
}

// New builds a Collector. It performs no IO.
func New(opts Options) *Collector {
	return &Collector{
		opts:   opts.withDefaults(),
		agents: make(map[string]Agent),
		done:   make(chan struct{}),
	}
}

// Start probes once and then polls until Stop.
func (c *Collector) Start(ctx context.Context) {
	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		c.probe(ctx)
		t := time.NewTicker(c.opts.Interval)
		defer t.Stop()
		for {
			select {
			case <-c.done:
				return
			case <-ctx.Done():
				return
			case <-t.C:
				c.probe(ctx)
			}
		}
	}()
}

// Stop halts the poll loop and waits for an in-flight probe to park.
func (c *Collector) Stop() {
	c.stopOnce.Do(func() { close(c.done) })
	c.wg.Wait()
}

// Document returns the current answer from cache, never touching the network.
func (c *Collector) Document(ctx context.Context) Document {
	c.mu.RLock()
	agents := make([]Agent, 0, len(c.agents)+1)
	for _, a := range c.agents {
		agents = append(agents, a)
	}
	fetchedAt := c.fetchedAt
	c.mu.RUnlock()

	agents = c.expireStale(agents)
	if gauge, ok := c.diskAgent(); ok {
		agents = append(agents, gauge)
	}
	sort.Slice(agents, func(i, j int) bool { return agents[i].ID < agents[j].ID })

	doc := Document{SchemaVersion: SchemaVersion, Agents: agents}
	if !fetchedAt.IsZero() {
		doc.FetchedAt = fetchedAt.Format(time.RFC3339)
	}
	return doc
}

// Refresh forces a probe unless one just ran, then returns the result.
func (c *Collector) Refresh(ctx context.Context, force bool) Document {
	c.mu.RLock()
	fresh := !c.fetchedAt.IsZero() && c.opts.Now().Sub(c.fetchedAt) < reuseWindow
	c.mu.RUnlock()
	if fresh && !force {
		return c.Document(ctx)
	}
	c.probe(ctx)
	return c.Document(ctx)
}

// expireStale drops readings whose window has rolled over, and marks the agent
// as no longer current when that empties it.
//
// This is the rule that keeps a cached 78% from misreporting an allowance that
// has since reset to zero. It is per-limit, because one window can roll over
// while another has days to run.
func (c *Collector) expireStale(agents []Agent) []Agent {
	now := c.opts.Now()
	out := make([]Agent, 0, len(agents))
	for _, a := range agents {
		kept := make([]Limit, 0, len(a.Limits))
		for _, l := range a.Limits {
			if l.Expired(now) {
				continue
			}
			kept = append(kept, l)
		}
		if len(kept) < len(a.Limits) && a.UsageStatusText == "" {
			a.UsageStatusText = "Some windows have reset — refreshing."
		}
		a.Limits = kept
		out = append(out, a)
	}
	return out
}

// probe runs every collector and folds the results into the cache.
//
// Collectors run IN PARALLEL, each bounded on its own: one dials a subprocess
// and one makes an HTTPS request, and a vendor that hangs must not hold up the
// others or take them down. A collector that panics is contained and dropped
// rather than killing the poll loop for the life of the process.
//
// A collector that fails does not blank its agent: the record it returns
// carries the failure as a state, and a record with no limits never overwrites
// one that has them. That is what keeps the last good numbers on screen.
func (c *Collector) probe(ctx context.Context) {
	c.probeMu.Lock()
	defer c.probeMu.Unlock()

	previous := c.snapshot()

	// The id rides the result rather than being read off the agent: a
	// collector that says "nothing at all" returns a ZERO agent, and deleting
	// by its empty id would silently leave the stale record in place.
	type result struct {
		id    string
		agent Agent
		ok    bool
	}
	results := make(chan result, len(c.opts.Accounts)+1)
	var wg sync.WaitGroup

	run := func(id string, fn func() (Agent, bool)) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() {
				// A collector is third-party code at the end of a subprocess or
				// a network. It may not take the others with it. A panic leaves
				// that agent untouched rather than deleting it, because a crash
				// is not evidence about the account.
				if r := recover(); r != nil {
					slog.Error("usage: collector panicked", "agent", id, "panic", r)
				}
			}()
			agent, ok := fn()
			results <- result{id, agent, ok}
		}()
	}

	run("claude", func() (Agent, bool) { return collectClaude(ctx, c.opts.Claude), true })
	for id, src := range c.opts.Accounts {
		prev := previous[id]
		run(id, func() (Agent, bool) {
			return collectConnector(ctx, id, connectorName(id), src, prev, c.opts.Now())
		})
	}

	wg.Wait()
	close(results)

	today := c.today(ctx)

	c.mu.Lock()
	defer c.mu.Unlock()
	c.fetchedAt = c.opts.Now()

	for r := range results {
		if !r.ok {
			// "Say nothing at all" — and forget anything we were saying.
			delete(c.agents, r.id)
			continue
		}
		fresh := r.agent
		if prev := previous[fresh.ID]; prev != nil && !fresh.Ready && len(fresh.Limits) == 0 && len(prev.Limits) > 0 {
			// The Claude collector reports failure as a bare record; give it the
			// same non-destructive treatment collectConnector applies itself.
			kept := *prev
			kept.Ready = false
			kept.UsageStatusText = fresh.UsageStatusText
			kept.AuthHelpText = fresh.AuthHelpText
			if fresh.TierLabel != "" {
				kept.TierLabel = fresh.TierLabel
			}
			fresh = kept
		}
		if t, ok := today[fresh.ID]; ok {
			fresh.TodayTokens = t.Tokens
			fresh.TodayPrompts = t.Prompts
		}
		c.agents[fresh.ID] = fresh
	}
}

// snapshot copies what we currently know, so collectors can consult it without
// holding the lock across a subprocess dial.
func (c *Collector) snapshot() map[string]*Agent {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make(map[string]*Agent, len(c.agents))
	for id, a := range c.agents {
		copied := a
		out[id] = &copied
	}
	return out
}

// connectorName is the display name for a provider id. An id we do not
// recognise still renders — under its own id, which is better than "Unknown".
func connectorName(id string) string {
	switch id {
	case "codex":
		return "Codex"
	case "claude":
		return "Claude Code"
	default:
		return id
	}
}

func (c *Collector) today(ctx context.Context) map[string]Today {
	if c.opts.Today == nil {
		return nil
	}
	t, err := c.opts.Today(ctx)
	if err != nil {
		slog.Debug("usage: could not read today's spend", "error", err)
		return nil
	}
	return t
}

// diskAgent renders the disk as a GAUGE, not an allowance.
//
// A gauge is a level that is simply where it is. It carries no reset, never
// escalates to a warning colour, and may not take a headline — a small machine
// sitting at 88% all year is its normal state, not news. The one thing it does
// carry is the absolute figure, because "9.2 GB free" says more than 88% does.
func (c *Collector) diskAgent() (Agent, bool) {
	if c.opts.Disk == nil {
		return Agent{}, false
	}
	d, ok := c.opts.Disk()
	if !ok {
		return Agent{}, false
	}
	return Agent{
		ID:    "storage",
		Name:  "Disk",
		Kind:  KindGauge,
		Ready: true,
		Limits: []Limit{{
			Label:   "Data directory",
			Percent: d.UsedPercent / 100,
			Detail:  formatBytes(d.FreeBytes) + " free",
		}},
	}, true
}

// formatBytes renders a size the way the panel reads it: one decimal below ten,
// none above — "9.2 GB", "63 GB".
func formatBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return strconv.FormatInt(n, 10) + " B"
	}
	div, exp := int64(unit), 0
	for v := n / unit; v >= unit && exp < 4; v /= unit {
		div *= unit
		exp++
	}
	value := float64(n) / float64(div)
	suffix := [...]string{"KB", "MB", "GB", "TB", "PB"}[exp]
	if value < 10 {
		return strconv.FormatFloat(value, 'f', 1, 64) + " " + suffix
	}
	return strconv.FormatInt(int64(value+0.5), 10) + " " + suffix
}
