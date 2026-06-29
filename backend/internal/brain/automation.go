package brain

import (
	"context"
	"log/slog"
	"time"

	"github.com/allbin/agentkit/eventbus"
	claudecli "github.com/allbin/claudecli-go"

	"github.com/mdjarv/agentique/backend/internal/memory"
	"github.com/mdjarv/agentique/backend/internal/msggen"
)

// Automation runs scheduled consolidation: on each tick it consolidates every
// scope (merge duplicates, abstract repeated specifics, decay if configured).
// Opt-in — the server starts it only when an interval is set. With a model it runs
// the LLM reorganization; without, it's deterministic dedup only. Auto-apply is
// safe by construction: the consolidation guards refuse >half-deletions and never
// touch pinned/locked/human facts. A changed scope broadcasts EventBrainUpdated.
// (Conceptually this is the "sleep" of sleep-based memory consolidation — the brain
// consolidating itself while idle — but the feature is named "scheduled consolidation".)
type Automation struct {
	svc      *Service
	runner   msggen.Runner
	bus      eventbus.Broadcaster
	interval time.Duration
	model    claudecli.Model // "" => deterministic dedup/decay only
	// initialDelay is how long after Start the first pass runs. It is short relative to
	// interval so a frequently-restarted server still gets a near-boot refresh instead of
	// waiting (and resetting) a full interval each restart. Tests set it tiny.
	initialDelay time.Duration
	done         chan struct{}
}

// defaultInitialDelay lets the server finish coming up before the first consolidation
// pass, while still running it well within a single interval so restarts can't defer it
// indefinitely.
const defaultInitialDelay = 30 * time.Second

func NewAutomation(svc *Service, runner msggen.Runner, bus eventbus.Broadcaster, interval time.Duration, model claudecli.Model) *Automation {
	return &Automation{svc: svc, runner: runner, bus: bus, interval: interval, model: model, initialDelay: defaultInitialDelay, done: make(chan struct{})}
}

// Start launches the loop when an interval is configured; otherwise it is a no-op.
func (a *Automation) Start() {
	if a == nil || a.interval <= 0 {
		return
	}
	slog.Info("brain: scheduled consolidation enabled", "interval", a.interval, "model", string(a.model))
	go a.loop()
}

func (a *Automation) Stop() {
	if a == nil {
		return
	}
	select {
	case <-a.done:
	default:
		close(a.done)
	}
}

func (a *Automation) loop() {
	// Run once shortly after start, then on the interval. A bare NewTicker would defer the
	// first pass by a full interval and reset that clock on every process start — on a
	// frequently-restarted server the semantic refresh could be postponed forever. The
	// initial timer fixes both: the first pass lands near boot regardless of restarts.
	initial := time.NewTimer(a.initialDelay)
	defer initial.Stop()
	select {
	case <-a.done:
		return
	case <-initial.C:
		a.runOnce(context.Background())
	}

	ticker := time.NewTicker(a.interval)
	defer ticker.Stop()
	for {
		select {
		case <-a.done:
			return
		case <-ticker.C:
			a.runOnce(context.Background())
		}
	}
}

func (a *Automation) runOnce(ctx context.Context) {
	scopes, err := a.svc.ListScopes(ctx)
	if err != nil {
		slog.Warn("brain: scheduled consolidation: list scopes failed", "error", err)
		return
	}
	var ex memory.Extractor
	if a.model != "" && a.runner != nil {
		ex = NewClaudeExtractor(a.runner, a.model)
	}
	// Reversibility: take a pre-churn snapshot of the whole brain before mutating any
	// scope, so a consolidation pass is recoverable (brain restore <id>). Failure is
	// WARN-logged and non-fatal — a missing snapshot must not block the churn (the
	// archive-not-delete model keeps the pass itself reversible regardless).
	if len(scopes) > 0 {
		if info, err := a.svc.Snapshot(); err != nil {
			slog.Warn("brain: scheduled consolidation: snapshot failed", "error", err)
		} else {
			slog.Info("brain: pre-churn snapshot", "id", info.ID, "files", info.Files)
		}
	}
	for _, scope := range scopes {
		select {
		case <-a.done:
			return
		default:
		}
		rep, err := a.svc.Consolidate(ctx, scope, ex, memory.DecayPolicy{}, false, ConsolidateOpts{})
		if err != nil {
			slog.Warn("brain: scheduled consolidation: consolidate failed", "scope", scope, "error", err)
			continue
		}
		changed := len(rep.Promoted) + len(rep.Rewritten) + len(rep.Abstracted) + len(rep.Deleted) + len(rep.Decayed)
		if changed == 0 {
			continue
		}
		slog.Info("brain: scheduled consolidation: consolidated scope", "scope", scope,
			"rewritten", len(rep.Rewritten), "abstracted", len(rep.Abstracted),
			"deleted", len(rep.Deleted), "decayed", len(rep.Decayed))
		if a.bus != nil {
			a.bus.Broadcast(EventBrainUpdated, map[string]string{})
		}
	}
	// After every scope is consolidated, recompute cross-scope topic areas once (B).
	if n, err := a.svc.AssignAreas(ctx); err != nil {
		slog.Warn("brain: scheduled consolidation: assign areas failed", "error", err)
	} else if n > 0 {
		slog.Info("brain: scheduled consolidation: refreshed cross-scope areas", "changed", n)
		if a.bus != nil {
			a.bus.Broadcast(EventBrainUpdated, map[string]string{})
		}
	}
}
