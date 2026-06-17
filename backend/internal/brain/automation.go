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

// Automation runs the periodic "sleep" pass: on each tick it consolidates every
// scope (merge duplicates, abstract repeated specifics, decay if configured).
// Opt-in — the server starts it only when an interval is set. With a model it runs
// the LLM reorganization; without, it's deterministic dedup only. Auto-apply is
// safe by construction: the consolidation guards refuse >half-deletions and never
// touch pinned/locked/human facts. A changed scope broadcasts EventBrainUpdated.
type Automation struct {
	svc      *Service
	runner   msggen.Runner
	bus      eventbus.Broadcaster
	interval time.Duration
	model    claudecli.Model // "" => deterministic dedup/decay only
	done     chan struct{}
}

func NewAutomation(svc *Service, runner msggen.Runner, bus eventbus.Broadcaster, interval time.Duration, model claudecli.Model) *Automation {
	return &Automation{svc: svc, runner: runner, bus: bus, interval: interval, model: model, done: make(chan struct{})}
}

// Start launches the loop when an interval is configured; otherwise it is a no-op.
func (a *Automation) Start() {
	if a == nil || a.interval <= 0 {
		return
	}
	slog.Info("brain: sleep scheduler enabled", "interval", a.interval, "model", string(a.model))
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
		slog.Warn("brain: sleep pass: list scopes failed", "error", err)
		return
	}
	var ex memory.Extractor
	if a.model != "" && a.runner != nil {
		ex = NewClaudeExtractor(a.runner, a.model)
	}
	for _, scope := range scopes {
		select {
		case <-a.done:
			return
		default:
		}
		rep, err := a.svc.Consolidate(ctx, scope, ex, memory.DecayPolicy{}, false)
		if err != nil {
			slog.Warn("brain: sleep pass: consolidate failed", "scope", scope, "error", err)
			continue
		}
		changed := len(rep.Promoted) + len(rep.Rewritten) + len(rep.Abstracted) + len(rep.Deleted) + len(rep.Decayed)
		if changed == 0 {
			continue
		}
		slog.Info("brain: sleep pass consolidated scope", "scope", scope,
			"rewritten", len(rep.Rewritten), "abstracted", len(rep.Abstracted),
			"deleted", len(rep.Deleted), "decayed", len(rep.Decayed))
		if a.bus != nil {
			a.bus.Broadcast(EventBrainUpdated, map[string]string{})
		}
	}
}
