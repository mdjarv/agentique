package brain

import (
	"context"
	"errors"
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
	// archiveAfter is the hard minimum disuse age before the churn archives a faded fact
	// (M5). 0 disables archiving (the pass behaves as before — no fade-out, no archive),
	// preserving today's behaviour until an operator opts in. archiveFloor is the
	// effective-confidence line (0 → memory.DefaultArchiveConfidenceFloor).
	archiveAfter time.Duration
	archiveFloor float64
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

func NewAutomation(svc *Service, runner msggen.Runner, bus eventbus.Broadcaster, interval time.Duration, model claudecli.Model, archiveAfter time.Duration, archiveFloor float64) *Automation {
	return &Automation{svc: svc, runner: runner, bus: bus, interval: interval, model: model, archiveAfter: archiveAfter, archiveFloor: archiveFloor, initialDelay: defaultInitialDelay, done: make(chan struct{})}
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
	//
	// A pass skipped because the vector backend was detached is owed, not forgotten: pending is
	// then the channel the next attach closes, and that attach runs it instead of the interval.
	var pending <-chan struct{}
	initial := time.NewTimer(a.initialDelay)
	defer initial.Stop()
	select {
	case <-a.done:
		return
	case <-initial.C:
		pending = a.runOnce(context.Background())
	}

	ticker := time.NewTicker(a.interval)
	defer ticker.Stop()
	for {
		select {
		case <-a.done:
			return
		case <-ticker.C:
			pending = a.runOnce(context.Background())
		case <-pending:
			slog.Info("brain: scheduled consolidation: vector backend attached; running the skipped pass")
			pending = a.runOnce(context.Background())
		}
	}
}

// runOnce consolidates every scope and refreshes the cross-scope areas. It returns nil after a
// pass that ran, and a channel the next attach closes after one it skipped: with a vector
// backend configured, a pass never runs while that backend is detached, because it would
// persist links, communities and areas computed without embeddings (ErrSemanticUnavailable).
// The same holds mid-pass — a scope refused by a backend lost underneath the pass ends the
// pass there, with no lexical areas refresh after it.
func (a *Automation) runOnce(ctx context.Context) <-chan struct{} {
	var ex memory.Extractor
	if a.model != "" && a.runner != nil {
		ex = NewClaudeExtractor(a.runner, a.model)
	}
	return a.runPass(ctx, ex)
}

// runPass is runOnce with the extractor chosen.
func (a *Automation) runPass(ctx context.Context, ex memory.Extractor) <-chan struct{} {
	// Taken before the check, so an attach landing between the two still wakes the loop.
	attached := a.svc.nextAttach()
	if a.svc.SemanticConfigured() && !a.svc.SemanticEnabled() {
		slog.Warn("brain: scheduled consolidation skipped: the vector backend is unreachable; it runs when the backend attaches")
		return attached
	}
	scopes, err := a.svc.ListScopes(ctx)
	if err != nil {
		slog.Warn("brain: scheduled consolidation: list scopes failed", "error", err)
		return nil
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
			return nil
		default:
		}
		// Archive-transition policy (M5): archiveAfter<=0 leaves the policy inert (no fade,
		// no archive — today's behaviour) until an operator sets archive-after. The pre-churn
		// snapshot above (M1) is the restore point for the archive writes.
		decay := memory.DecayPolicy{MaxAge: a.archiveAfter, ArchiveFloor: a.archiveFloor}
		rep, err := a.svc.Consolidate(ctx, scope, ex, decay, false, ConsolidateOpts{})
		if errors.Is(err, ErrSemanticUnavailable) {
			slog.Warn("brain: scheduled consolidation stopped: the vector backend was lost mid-pass; the rest runs when it attaches", "scope", scope)
			return attached
		}
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
	n, err := a.svc.AssignAreas(ctx, AreasOpts{})
	switch {
	case errors.Is(err, ErrSemanticUnavailable):
		slog.Warn("brain: scheduled consolidation: areas refresh deferred until the vector backend attaches")
		return attached
	case err != nil:
		slog.Warn("brain: scheduled consolidation: assign areas failed", "error", err)
	case n > 0:
		slog.Info("brain: scheduled consolidation: refreshed cross-scope areas", "changed", n)
		if a.bus != nil {
			a.bus.Broadcast(EventBrainUpdated, map[string]string{})
		}
	}
	return nil
}
